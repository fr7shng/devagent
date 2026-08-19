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

// Router 以 RouteTable 为唯一事实来源（gateway_id → URL/状态/设备）。
// 之前还维护了一份并行的 conns 映射，既重复建模又与健康检查 goroutine 产生并发 map 读写竞争，已移除。
type Router struct {
	routeTable   *model.RouteTable
	onDiscover   func(cfg *model.DeviceConfig)
	onRemove     func(deviceID string)
	httpClient   *http.Client
	tokenSecret  string
	tokenTTL     time.Duration
	tokenManager *TokenManager
	cfg          *config.SidecarConfig
}

func NewRouter(rt *model.RouteTable, tokenSecret string, tokenTTL time.Duration, cfg *config.SidecarConfig) *Router {
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
		httpClient:   &http.Client{Timeout: 10 * time.Second},
		tokenSecret:  tokenSecret,
		tokenTTL:     tokenTTL,
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
			scheme := "http"
			for _, info := range entry.InfoFields {
				if info == "tls=1" {
					scheme = "https"
				}
			}
			gwURL := fmt.Sprintf("%s://%s:%d", scheme, entry.AddrV4.String(), entry.Port)
			parts := strings.SplitN(entry.Name, ".", 2)
			gwID := parts[0]
			slog.Info("发现网关", "gateway_id", gwID, "url", gwURL)
			r.routeTable.Register(&model.GatewayMeta{
				ID:            gwID,
				URL:           gwURL,
				LastHeartbeat: time.Now().Unix(),
				Status:        "online",
			})
			r.fetchAndRegisterDevices(gwID, gwURL)
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
			}
			for _, gw := range r.routeTable.Gateways() {
				resp, err := r.httpClient.Get(gw.URL + "/healthz")
				if err != nil {
					slog.Warn("网关健康检查失败", "gateway_id", gw.ID, "error", err)
					continue
				}
				resp.Body.Close()
				r.routeTable.UpdateHeartbeat(gw.ID)
			}
		}
	}
}

func (r *Router) FetchAndRegisterDevices(gwID, gwURL string) {
	r.fetchAndRegisterDevices(gwID, gwURL)
}

// AddStaticGateway 注册一个不依赖 mDNS 发现的网关（同机运行或 mDNS 不可用场景）。
// 与 mDNS 发现的网关一样纳入健康检查与心跳管理。
func (r *Router) AddStaticGateway(gwID, gwURL string) {
	r.routeTable.Register(&model.GatewayMeta{ID: gwID, URL: gwURL})
	r.fetchAndRegisterDevices(gwID, gwURL)
}

func (r *Router) fetchAndRegisterDevices(gwID, gwURL string) {
	if r.onDiscover == nil {
		return
	}

	req, err := http.NewRequest(http.MethodGet, gwURL+"/devices", nil)
	if err != nil {
		slog.Error("构造设备列表请求失败", "gateway_url", gwURL, "error", err)
		return
	}
	r.setAuthHeader(req)
	resp, err := r.httpClient.Do(req)
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
		r.routeTable.AddDevice(gwID, cfgs[i].Device.ID)
	}
}

// setAuthHeader 在请求上附加 Bearer token（若已配置鉴权）。
func (r *Router) setAuthHeader(req *http.Request) {
	if r.tokenManager == nil {
		return
	}
	ttl := r.tokenTTL
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	token, err := r.tokenManager.Mint([]string{"*"}, ttl)
	if err != nil {
		return
	}
	req.Header.Set("Authorization", "Bearer "+token)
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
	r.setAuthHeader(req)

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
