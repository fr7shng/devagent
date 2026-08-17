package sidecar

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/hashicorp/mdns"
	"github.com/ng/devagent/internal/config"
	"github.com/ng/devagent/internal/model"
	"github.com/ng/devagent/internal/protocol"
)

type Router struct {
	routeTable   *model.RouteTable
	conns        map[string]*GWConn
	onDiscover   func(cfg *model.DeviceConfig)
	onRemove     func(deviceID string)
	httpClient   *http.Client
	tokenSecret  string
	tokenManager *TokenManager
	cfg          *config.SidecarConfig
}

type GWConn struct {
	GatewayID string
	URL       string
	LastSeen  time.Time
}

func NewRouter(rt *model.RouteTable, tokenSecret string, cfg *config.SidecarConfig) *Router {
	if cfg == nil {
		defaultCfg := config.DefaultGlobalConfig()
		cfg = &defaultCfg.Sidecar
	}
	var tm *TokenManager
	if tokenSecret != "" {
		tm = NewTokenManager(tokenSecret)
	}
	return &Router{
		routeTable:   rt,
		conns:        make(map[string]*GWConn),
		httpClient:   &http.Client{Timeout: 10 * time.Second},
		tokenSecret:  tokenSecret,
		tokenManager: tm,
		cfg:          cfg,
	}
}

func (r *Router) OnDiscover(fn func(cfg *model.DeviceConfig)) {
	r.onDiscover = fn
}

func (r *Router) OnRemove(fn func(deviceID string)) {
	r.onRemove = fn
}

func (r *Router) Discover(ctx context.Context) {
	entriesCh := make(chan *mdns.ServiceEntry, 10)
	go func() {
		for entry := range entriesCh {
			if !strings.HasSuffix(entry.Name, "._devagent._tcp.") && !strings.Contains(entry.Name, "._devagent._tcp.") {
				continue
			}
			if entry.AddrV4 == nil {
				continue
			}
			gwURL := fmt.Sprintf("http://%s:%d", entry.AddrV4.String(), entry.Port)
			parts := strings.SplitN(entry.Name, ".", 2)
			gwID := parts[0]
			slog.Info("发现网关", "gateway_id", gwID, "url", gwURL)
			r.routeTable.Register(&model.GatewayMeta{
				ID:            gwID,
				URL:           gwURL,
				LastHeartbeat: time.Now().Unix(),
				Status:        "online",
			})
			r.conns[gwID] = &GWConn{GatewayID: gwID, URL: gwURL, LastSeen: time.Now()}
			r.fetchAndRegisterDevices(gwURL)
		}
	}()

	go r.startGatewayHealthCheck(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		default:
			if err := mdns.Lookup("_devagent._tcp", entriesCh); err != nil {
				slog.Error("mDNS 查询失败", "error", err)
			}
			time.Sleep(r.cfg.MDNSInterval)
		}
	}
}

func (r *Router) startGatewayHealthCheck(ctx context.Context) {
	ticker := time.NewTicker(r.cfg.HealthCheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			maintenance, offline := r.routeTable.StaleGateways(r.cfg.MaintenanceTimeout, r.cfg.HeartbeatTimeout)
			for _, gwID := range maintenance {
				r.routeTable.SetGatewayStatus(gwID, model.GatewayMaintenance)
				slog.Info("网关进入维护状态", "gateway_id", gwID)
			}
			for _, gwID := range offline {
				slog.Info("网关心跳超时，反注册", "gateway_id", gwID)
				removed := r.routeTable.Unregister(gwID)
				for _, deviceID := range removed {
					if r.onRemove != nil {
						r.onRemove(deviceID)
					}
				}
				delete(r.conns, gwID)
			}
			for gwID, conn := range r.conns {
				resp, err := r.httpClient.Get(conn.URL + "/devices")
				if err != nil {
					slog.Warn("网关健康检查失败", "gateway_id", gwID, "error", err)
					continue
				}
				resp.Body.Close()
				r.routeTable.UpdateHeartbeat(gwID)
				conn.LastSeen = time.Now()
			}
		}
	}
}

func (r *Router) FetchAndRegisterDevices(gwURL string) {
	r.fetchAndRegisterDevices(gwURL)
}

func (r *Router) fetchAndRegisterDevices(gwURL string) {
	if r.onDiscover == nil {
		return
	}

	resp, err := r.httpClient.Get(gwURL + "/devices")
	if err != nil {
		slog.Error("获取设备列表失败", "gateway_url", gwURL, "error", err)
		return
	}
	defer resp.Body.Close()

	var cfgs []model.DeviceConfig
	if err := json.NewDecoder(resp.Body).Decode(&cfgs); err != nil {
		slog.Error("解析设备列表失败", "gateway_url", gwURL, "error", err)
		return
	}

	for i := range cfgs {
		slog.Info("注册设备工具", "device_id", cfgs[i].Device.ID, "gateway_url", gwURL)
		r.onDiscover(&cfgs[i])

		r.routeTable.Register(&model.GatewayMeta{
			ID:            cfgs[i].Device.ID,
			URL:           gwURL,
			Devices:       []string{cfgs[i].Device.ID},
			LastHeartbeat: time.Now().Unix(),
			Status:        "online",
		})
	}
}

func (r *Router) ForwardInvoke(ctx context.Context, deviceID, capability string, params any, requestID, jobID string) (*protocol.SSEMessage, error) {
	gwURL, ok := r.routeTable.Lookup(deviceID)
	if !ok {
		return nil, fmt.Errorf("ROUTE_NOT_FOUND: device %s not registered", deviceID)
	}

	invokeMsg := &protocol.SSEMessage{
		Type:       "invoke",
		RequestID:  requestID,
		DeviceID:   deviceID,
		Capability: capability,
		Params:     params,
		JobID:      jobID,
		Timestamp:  time.Now().Unix(),
	}

	body, err := json.Marshal(invokeMsg)
	if err != nil {
		return nil, fmt.Errorf("marshal invoke: %w", err)
	}

	endpoint := fmt.Sprintf("%s/invoke", gwURL)
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	if r.tokenManager != nil {
		token, err := r.tokenManager.Mint([]string{"*"}, 5*time.Minute)
		if err == nil {
			req.Header.Set("Authorization", "Bearer "+token)
		}
	}

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("invoke daemon: %w", err)
	}
	defer resp.Body.Close()

	var result protocol.SSEMessage
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return &result, nil
}
