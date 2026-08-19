package sidecar

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/ng/devagent/internal/mcptool"
	"github.com/ng/devagent/internal/model"
	"github.com/ng/devagent/internal/version"
)

type RouteTable = model.RouteTable
type GatewayMeta = model.GatewayMeta

type SidecarServer struct {
	mcpServer   *server.MCPServer
	routeTable  *RouteTable
	dedup       *DedupWindow
	progress    *ProgressTracker
	router      *Router
	configs     map[string]*model.DeviceConfig
	configsMu   sync.RWMutex
	tokenSecret string
}

func NewSidecarServer(rt *RouteTable, dedup *DedupWindow, progress *ProgressTracker, router *Router, tokenSecret string) *SidecarServer {
	s := &SidecarServer{
		routeTable:  rt,
		dedup:       dedup,
		progress:    progress,
		router:      router,
		configs:     make(map[string]*model.DeviceConfig),
		tokenSecret: tokenSecret,
	}

	mcpSrv := server.NewMCPServer(
		"devagent-sidecar",
		version.Version,
		server.WithToolCapabilities(true),
	)
	mcpSrv.AddTool(mcp.NewTool("__system__.list_devices",
		mcp.WithDescription("列出所有已注册设备及状态"),
	), s.handleListDevices)

	mcpSrv.AddTool(mcp.NewTool("__system__.diagnose_connectivity",
		mcp.WithDescription("检查到指定设备的全链路状态"),
		mcp.WithString("device_id", mcp.Required()),
	), s.handleDiagnose)

	mcpSrv.AddTool(mcp.NewTool("__system__.get_device_schema",
		mcp.WithDescription("获取指定设备的完整能力描述"),
		mcp.WithString("device_id", mcp.Required()),
	), s.handleGetSchema)

	mcpSrv.AddTool(mcp.NewTool("__system__.get_job_status",
		mcp.WithDescription("查询异步任务状态"),
		mcp.WithString("job_id", mcp.Required()),
	), s.handleGetJobStatus)

	s.mcpServer = mcpSrv
	return s
}

func (s *SidecarServer) MCPServer() *server.MCPServer {
	return s.mcpServer
}

func (s *SidecarServer) ListDevices() []map[string]any {
	return s.routeTable.AllDevices()
}

func (s *SidecarServer) ServeStdio() error {
	return server.ServeStdio(s.mcpServer)
}

func (s *SidecarServer) RegisterDeviceTools(cfg *model.DeviceConfig) {
	s.configsMu.Lock()
	defer s.configsMu.Unlock()
	s.configs[cfg.Device.ID] = cfg
	gwStatus := s.getGatewayStatus(cfg.Device.ID)
	maintenance := gwStatus == model.GatewayMaintenance
	for _, cap := range cfg.Capabilities {
		tool := mcptool.CompileTool(cfg.Device.ID, cap, maintenance)
		s.mcpServer.AddTool(tool, s.makeDeviceInvokeHandler(cfg.Device.ID, cap))
	}
}

func (s *SidecarServer) getGatewayStatus(deviceID string) string {
	all := s.routeTable.AllDevices()
	for _, d := range all {
		if d["device_id"] == deviceID {
			if status, ok := d["status"].(string); ok {
				return status
			}
		}
	}
	return model.GatewayOnline
}

func (s *SidecarServer) UnregisterDeviceTools(deviceID string) {
	s.configsMu.Lock()
	defer s.configsMu.Unlock()
	cfg, ok := s.configs[deviceID]
	if !ok {
		return
	}
	for _, cap := range cfg.Capabilities {
		toolName := fmt.Sprintf("%s.%s", deviceID, cap.Name)
		s.mcpServer.DeleteTools(toolName)
	}
	delete(s.configs, deviceID)
}

// RefreshDeviceTools 按当前路由状态重新编译设备的工具（网关进入/退出维护时调用），
// 使 [维护中] 描述与 ONLINE 恢复即时生效。mcp-go AddTool 同名覆盖，可安全重复注册。
func (s *SidecarServer) RefreshDeviceTools(deviceID string) {
	s.configsMu.RLock()
	cfg, ok := s.configs[deviceID]
	s.configsMu.RUnlock()
	if !ok {
		return
	}
	s.RegisterDeviceTools(cfg)
}

func (s *SidecarServer) makeDeviceInvokeHandler(deviceID string, cap model.Capability) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		gwStatus := s.getGatewayStatus(deviceID)
		if gwStatus == model.GatewayMaintenance {
			return mcp.NewToolResultError(fmt.Sprintf("device %s is under maintenance", deviceID)), nil
		}

		key := s.dedup.MakeKey(deviceID, cap.Name, req.Params.Arguments)
		if !s.dedup.Allow(key) {
			return mcp.NewToolResultError("duplicate request within dedup window"), nil
		}

		jobID := fmt.Sprintf("job_%d", time.Now().UnixNano())
		requestID := fmt.Sprintf("req_%d", time.Now().UnixNano())
		s.progress.Register(jobID, requestID, deviceID)

		go func() {
			result, err := s.router.ForwardInvoke(context.Background(), deviceID, cap.Name, req.Params.Arguments, requestID, jobID)
			if err != nil {
				s.progress.Fail(jobID, err.Error())
				return
			}
			s.progress.Complete(jobID, result)
		}()

		resultJSON, _ := mcp.NewToolResultJSON(map[string]any{
			"job_id":  jobID,
			"status":  "pending",
			"message": "invoke dispatched, use __system__.get_job_status to check progress",
		})
		return resultJSON, nil
	}
}

func (s *SidecarServer) handleListDevices(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	devices := s.routeTable.AllDevices()
	result, _ := mcp.NewToolResultJSON(devices)
	return result, nil
}

func (s *SidecarServer) handleDiagnose(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	deviceID, err := req.RequireString("device_id")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	gwURL, ok := s.routeTable.Lookup(deviceID)
	if !ok {
		result, _ := mcp.NewToolResultJSON(map[string]any{
			"device_id":          deviceID,
			"sidecar_to_gateway": "route_not_found",
			"gateway_to_device":  "unknown",
		})
		return result, nil
	}

	var gwStatus string
	var heartbeatAge int64 = -1
	for _, d := range s.routeTable.AllDevices() {
		if d["device_id"] == deviceID {
			if st, ok2 := d["status"].(string); ok2 {
				gwStatus = st
			}
			if hb, ok2 := d["last_heartbeat"].(int64); ok2 && hb > 0 {
				heartbeatAge = time.Now().Unix() - hb
			}
			break
		}
	}

	// 探测网关侧到物理设备的链路：/readyz 报告串口连接与设备数。
	gwToDevice := "unknown"
	if s.router != nil && s.router.httpClient != nil {
		req0, err := http.NewRequestWithContext(ctx, http.MethodGet, gwURL+"/readyz", nil)
		if err == nil {
			s.router.setAuthHeader(req0)
			resp, err := s.router.httpClient.Do(req0)
			if err != nil {
				gwToDevice = "unreachable"
			} else {
				defer resp.Body.Close()
				var ready struct {
					Status          string `json:"status"`
					SerialConnected bool   `json:"serial_connected"`
					DeviceCount     int    `json:"device_count"`
				}
				if err := json.NewDecoder(resp.Body).Decode(&ready); err == nil {
					if ready.SerialConnected && ready.DeviceCount > 0 {
						gwToDevice = "ok"
					} else {
						gwToDevice = "not_ready"
					}
				}
			}
		}
	}

	result, _ := mcp.NewToolResultJSON(map[string]any{
		"device_id":          deviceID,
		"gateway_url":        gwURL,
		"gateway_status":     gwStatus,
		"sidecar_to_gateway": "ok",
		"gateway_to_device":  gwToDevice,
		"heartbeat_age_sec":  heartbeatAge,
	})
	return result, nil
}
func (s *SidecarServer) handleGetSchema(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	deviceID, err := req.RequireString("device_id")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	s.configsMu.RLock()
	cfg, ok := s.configs[deviceID]
	s.configsMu.RUnlock()
	if !ok {
		return mcp.NewToolResultError(fmt.Sprintf("device %s schema not available", deviceID)), nil
	}
	// 暴露给 AI 的 schema 剥离 HMAC 密钥等敏感字段。
	result, _ := mcp.NewToolResultJSON(cfg.Sanitized())
	return result, nil
}

func (s *SidecarServer) handleGetJobStatus(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	jobID, err := req.RequireString("job_id")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	job, ok := s.progress.GetJob(jobID)
	if !ok {
		return mcp.NewToolResultError(fmt.Sprintf("job %s not found", jobID)), nil
	}
	result, _ := mcp.NewToolResultJSON(job)
	return result, nil
}
