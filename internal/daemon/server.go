package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/hashicorp/mdns"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/ng/devagent/internal/config"
	"github.com/ng/devagent/internal/mcptool"
	"github.com/ng/devagent/internal/model"
	"github.com/ng/devagent/internal/protocol"
	"gopkg.in/yaml.v3"
)

type DaemonServer struct {
	mcpServer      *server.MCPServer
	registry       *DeviceRegistry
	gatewayID      string
	port           int
	configPath     string
	hal            HAL
	nativeHandlers map[string]NativeHandler
	httpServer     *http.Server
	mdnsServer     *mdns.Server
	cfg            *config.GlobalConfig
	seqCounter     atomic.Uint32
	tokenManager   *TokenManager
	rateLimiter    *RateLimiter
}

func NewDaemonServer(gatewayID string, port int, configPath string, cfg *config.GlobalConfig) *DaemonServer {
	if cfg == nil {
		cfg = config.DefaultGlobalConfig()
	}
	ds := &DaemonServer{
		gatewayID:      gatewayID,
		port:           port,
		configPath:     configPath,
		registry:       NewDeviceRegistry(),
		nativeHandlers: NewNativeHandlerRegistry(),
		cfg:            cfg,
		rateLimiter:    NewRateLimiter(30, time.Second),
	}
	if cfg.Token.Secret != "" {
		ds.tokenManager = NewTokenManager(cfg.Token.Secret)
	}

	mcpSrv := server.NewMCPServer(
		"devagent-daemon",
		"0.4.0",
		server.WithToolCapabilities(true),
	)

	ds.mcpServer = mcpSrv
	return ds
}

func (ds *DaemonServer) MCPServer() *server.MCPServer {
	return ds.mcpServer
}

func (ds *DaemonServer) Start(ctx context.Context) error {
	if ds.configPath != "" {
		if err := ds.loadConfig(ds.configPath); err != nil {
			return fmt.Errorf("load config: %w", err)
		}
	}

	if ds.cfg.Daemon.StatePath != "" {
		ds.registry.SetStatePath(ds.cfg.Daemon.StatePath)
		if err := ds.registry.Restore(); err != nil {
			slog.Warn("状态恢复失败，从配置启动", "error", err)
		} else {
			slog.Info("状态恢复成功", "path", ds.cfg.Daemon.StatePath)
		}
	}

	sseServer := server.NewSSEServer(ds.mcpServer,
		server.WithBaseURL(fmt.Sprintf("http://localhost:%d", ds.port)),
	)

	go ds.startHeartbeatMonitor(ds.cfg.Daemon.HeartbeatInterval, ds.cfg.Daemon.HeartbeatTimeout)
	go ds.startSerialReconnect(ctx)
	go ds.startStateSnapshot(ctx)

	mux := http.NewServeMux()
	mux.HandleFunc("/invoke", ds.handleHTTPInvoke)
	mux.HandleFunc("/devices", ds.handleHTTPDevices)
	mux.HandleFunc("/healthz", ds.handleHealthz)
	mux.HandleFunc("/readyz", ds.handleReadyz)
	mux.Handle("/", sseServer)

	ds.httpServer = &http.Server{
		Addr:    fmt.Sprintf(":%d", ds.port),
		Handler: mux,
	}

	if err := ds.startMDNS(); err != nil {
		slog.Warn("mDNS 广播启动失败", "error", err)
	}

	go func() {
		<-ctx.Done()
		slog.Info("daemon 收到关机信号，开始优雅关闭")
		if ds.hal != nil {
			ds.hal.Close()
		}
		if ds.mdnsServer != nil {
			ds.mdnsServer.Shutdown()
		}
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		ds.httpServer.Shutdown(shutdownCtx)
	}()

	slog.Info("daemon 启动", "port", ds.port, "tls", ds.cfg.TLS.CertPath != "")
	if ds.cfg.TLS.CertPath != "" && ds.cfg.TLS.KeyPath != "" {
		if err := ds.httpServer.ListenAndServeTLS(ds.cfg.TLS.CertPath, ds.cfg.TLS.KeyPath); err != nil && err != http.ErrServerClosed {
			return err
		}
	} else {
		if err := ds.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			return err
		}
	}
	return nil
}

func (ds *DaemonServer) loadConfig(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var cfg model.DeviceConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return err
	}
	ds.registry.Register(cfg)
	ds.registerDeviceTools(cfg)

	if cfg.Device.Type == "mcu_proxy" {
		if len(cfg.Capabilities) > 0 {
			proto := cfg.Capabilities[0].Implementation.Protocol
			if proto == "DCP" || proto == "dcp" {
				ds.hal = NewDCPBridge()
			} else {
				ds.hal = NewSerialBridge()
			}
		} else {
			ds.hal = NewSerialBridge()
		}
		if ds.hal != nil {
			channel := cfg.Capabilities[0].Implementation.Channel
			baudrate := cfg.Capabilities[0].Implementation.Baudrate
			if baudrate == 0 {
				baudrate = 115200
			}
			if err := ds.hal.Open(channel, baudrate); err != nil {
				slog.Error("HAL 串口打开失败，将后台重试", "channel", channel, "baudrate", baudrate, "error", err)
			} else {
				slog.Info("HAL 串口已打开", "channel", channel, "baudrate", baudrate)
			}
		}
	}

	return nil
}

func (ds *DaemonServer) registerDeviceTools(cfg model.DeviceConfig) {
	for _, cap := range cfg.Capabilities {
		tool := mcptool.CompileTool(cfg.Device.ID, cap, false)
		ds.mcpServer.AddTool(tool, ds.makeInvokeHandler(cfg.Device.ID, cap))
		slog.Info("注册设备工具", "tool", tool.Name)
	}
}

func (ds *DaemonServer) RegisterDeviceTools(cfg model.DeviceConfig) {
	ds.registerDeviceTools(cfg)
}

type invokeResult struct {
	Status   string
	Protocol string
	Result   any
	IntentID uint16
}

func (ds *DaemonServer) invokeCore(ctx context.Context, deviceID string, cap model.Capability, params map[string]any) (*invokeResult, error) {
	impl := cap.Implementation
	slog.Info("invoke 调用", "device_id", deviceID, "capability", cap.Name, "native", impl.Native)

	if impl.Native != "" {
		result, err := ExecuteNative(ds.nativeHandlers, impl.Native, ctx, deviceID, cap, params)
		if err != nil {
			return nil, err
		}
		return &invokeResult{Status: result.Status, Result: result}, nil
	}

	if ds.hal == nil || !ds.hal.IsConnected() {
		return &invokeResult{Status: "hal_not_available"}, nil
	}

	if ds.hal.Transport() == TransportDCP {
		intentID := cap.IntentID
		if intentID == 0 {
			intentID = protocol.IntentID(deviceID + "." + cap.Name)
		}
		reply, err := ds.hal.SendDCP(ctx, 1, intentID, params)
		if err != nil {
			return nil, fmt.Errorf("DCP send failed: %w", err)
		}
		if reply.Kind == protocol.DCPError {
			errMsg := "unknown dcp error"
			if reply.Payload != nil {
				if msg, ok := reply.Payload["error"].(string); ok {
					errMsg = msg
				}
			}
			return &invokeResult{Status: "dcp_error", Protocol: "dcp", Result: map[string]any{"error": errMsg, "intent_id": intentID}, IntentID: intentID}, nil
		}
		status := "ok"
		if reply.Payload != nil {
			if s, ok := reply.Payload["status"].(string); ok {
				status = s
			}
		}
		return &invokeResult{Status: status, Protocol: "dcp", Result: reply.Payload, IntentID: intentID}, nil
	}

	cmdEntry, ok := impl.CmdMap[cap.Name]
	if !ok {
		return &invokeResult{Status: "cmd_map_not_found"}, nil
	}

	payload := formatURPCPayload(cmdEntry.Fmt, params)
	urpcReq := &protocol.URPCRequest{
		Seq:     byte(ds.seqCounter.Add(1) % 256),
		Cmd:     byte(cmdEntry.Cmd),
		Payload: payload,
	}

	ack, err := ds.hal.SendURPC(ctx, urpcReq)
	if err != nil {
		return nil, fmt.Errorf("uRPC send failed: %w", err)
	}
	return &invokeResult{
		Status:   protocol.StatusString(ack.Status),
		Protocol: "urpc",
		Result:   map[string]any{"queue_depth": ack.QueueDepth},
	}, nil
}

func (ds *DaemonServer) makeInvokeHandler(deviceID string, cap model.Capability) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		params := req.GetArguments()
		if params == nil {
			params = make(map[string]any)
		}
		result, err := ds.invokeCore(ctx, deviceID, cap, params)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if result.Status == "hal_not_available" {
			return mcp.NewToolResultError("HAL not available"), nil
		}
		if result.Status == "cmd_map_not_found" {
			return mcp.NewToolResultError(fmt.Sprintf("cmd_map entry not found for %s", cap.Name)), nil
		}
		resultJSON, _ := mcp.NewToolResultJSON(result)
		return resultJSON, nil
	}
}

func formatURPCPayload(fmtStr string, params map[string]any) []byte {
	if fmtStr == "" {
		return []byte{}
	}
	result := fmtStr
	for k, v := range params {
		result = replacePlaceholder(result, k, v)
	}
	return []byte(result)
}

func replacePlaceholder(s, key string, val any) string {
	placeholder := "{" + key + "}"
	var replacement string
	switch v := val.(type) {
	case float64:
		if v == float64(int(v)) {
			replacement = fmt.Sprintf("%d", int(v))
		} else {
			replacement = fmt.Sprintf("%g", v)
		}
	case int:
		replacement = fmt.Sprintf("%d", v)
	case bool:
		if v {
			replacement = "1"
		} else {
			replacement = "0"
		}
	case string:
		replacement = v
	default:
		replacement = fmt.Sprintf("%v", v)
	}
	return strings.ReplaceAll(s, placeholder, replacement)
}

func (ds *DaemonServer) startMDNS() error {
	service, err := mdns.NewMDNSService(ds.gatewayID, "_devagent._tcp", "", "", ds.port, nil, []string{"gateway=" + ds.gatewayID})
	if err != nil {
		return fmt.Errorf("create mDNS service: %w", err)
	}

	server, err := mdns.NewServer(&mdns.Config{Zone: service, Iface: nil})
	if err != nil {
		return fmt.Errorf("start mDNS server: %w", err)
	}

	ds.mdnsServer = server
	slog.Info("mDNS 广播已启动", "gateway_id", ds.gatewayID, "port", ds.port)
	return nil
}

func (ds *DaemonServer) startStateSnapshot(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			ds.registry.Snapshot()
			return
		case <-ticker.C:
			if err := ds.registry.Snapshot(); err != nil {
				slog.Error("状态快照失败", "error", err)
			}
		}
	}
}

func (ds *DaemonServer) startSerialReconnect(ctx context.Context) {
	if ds.hal == nil {
		return
	}
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !ds.hal.IsConnected() {
				slog.Warn("串口断开，尝试重连")
				if err := ds.hal.Reconnect(); err != nil {
					slog.Error("串口重连失败", "error", err)
				}
			}
		}
	}
}

func (ds *DaemonServer) startHeartbeatMonitor(interval, timeout time.Duration) {
	ticker := time.NewTicker(interval)
	for range ticker.C {
		removed := ds.registry.RemoveStale(timeout)
		for _, id := range removed {
			slog.Info("设备心跳超时，已移除", "device_id", id)
		}
	}
}

func (ds *DaemonServer) Registry() *DeviceRegistry {
	return ds.registry
}

func (ds *DaemonServer) HandleHTTPInvoke() http.HandlerFunc {
	return ds.handleHTTPInvoke
}

func (ds *DaemonServer) HandleHTTPDevices() http.HandlerFunc {
	return ds.handleHTTPDevices
}

func (ds *DaemonServer) handleHTTPInvoke(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	clientKey := r.RemoteAddr
	if !ds.rateLimiter.Allow(clientKey) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(&protocol.SSEMessage{
			Type:      "invoke_result",
			Status:    "rate_limited",
			Message:   "too many requests",
			Timestamp: time.Now().Unix(),
		})
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read body failed", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var msg protocol.SSEMessage
	if err := json.Unmarshal(body, &msg); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	if ds.tokenManager != nil {
		tokenHeader := r.Header.Get("Authorization")
		if strings.HasPrefix(tokenHeader, "Bearer ") {
			tokenHeader = strings.TrimPrefix(tokenHeader, "Bearer ")
		}
		if tokenHeader == "" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(&protocol.SSEMessage{
				Type:      "invoke_result",
				Status:    "unauthorized",
				Message:   "missing token",
				Timestamp: time.Now().Unix(),
			})
			return
		}
		claims, err := ds.tokenManager.Verify(tokenHeader)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(&protocol.SSEMessage{
				Type:      "invoke_result",
				Status:    "unauthorized",
				Message:   err.Error(),
				Timestamp: time.Now().Unix(),
			})
			return
		}
		if !ds.tokenManager.CheckCap(claims, msg.DeviceID+"."+msg.Capability) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(&protocol.SSEMessage{
				Type:      "invoke_result",
				Status:    "forbidden",
				Message:   fmt.Sprintf("capability %s.%s not allowed", msg.DeviceID, msg.Capability),
				Timestamp: time.Now().Unix(),
			})
			return
		}
	}

	dev, ok := ds.registry.GetDevice(msg.DeviceID)
	if !ok {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(&protocol.SSEMessage{
			Type:      "invoke_result",
			RequestID: msg.RequestID,
			DeviceID:  msg.DeviceID,
			Status:    "device_not_found",
			Timestamp: time.Now().Unix(),
		})
		return
	}

	var targetCap *model.Capability
	for i := range dev.Config.Capabilities {
		if dev.Config.Capabilities[i].Name == msg.Capability {
			targetCap = &dev.Config.Capabilities[i]
			break
		}
	}
	if targetCap == nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(&protocol.SSEMessage{
			Type:       "invoke_result",
			RequestID:  msg.RequestID,
			DeviceID:   msg.DeviceID,
			Capability: msg.Capability,
			Status:     "capability_not_found",
			Timestamp:  time.Now().Unix(),
		})
		return
	}

	params, _ := msg.Params.(map[string]any)
	if params == nil {
		params = make(map[string]any)
	}

	result, err := ds.invokeCore(r.Context(), msg.DeviceID, *targetCap, params)

	w.Header().Set("Content-Type", "application/json")
	respMsg := &protocol.SSEMessage{
		Type:       "invoke_result",
		RequestID:  msg.RequestID,
		DeviceID:   msg.DeviceID,
		Capability: msg.Capability,
		Timestamp:  time.Now().Unix(),
	}

	if err != nil {
		respMsg.Status = "error"
		respMsg.Message = err.Error()
	} else {
		respMsg.Status = result.Status
		respMsg.Result = result.Result
	}
	json.NewEncoder(w).Encode(respMsg)
}

func (ds *DaemonServer) handleHTTPDevices(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	devices := ds.registry.ListDevices()
	cfgs := make([]model.DeviceConfig, 0, len(devices))
	for _, d := range devices {
		cfgs = append(cfgs, d.Config)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(cfgs)
}

func (ds *DaemonServer) handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
}

func (ds *DaemonServer) handleReadyz(w http.ResponseWriter, r *http.Request) {
	serialConnected := ds.hal != nil && ds.hal.IsConnected()
	deviceCount := len(ds.registry.ListDevices())
	ready := deviceCount > 0 && (ds.hal == nil || serialConnected)

	w.Header().Set("Content-Type", "application/json")
	if ready {
		json.NewEncoder(w).Encode(map[string]any{
			"status":           "ok",
			"serial_connected": serialConnected,
			"device_count":     deviceCount,
		})
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]any{
			"status":           "not_ready",
			"serial_connected": serialConnected,
			"device_count":     deviceCount,
		})
	}
}
