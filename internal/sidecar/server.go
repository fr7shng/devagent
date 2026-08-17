package sidecar

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/ng/devagent/internal/mcptool"
	"github.com/ng/devagent/internal/model"
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
		"0.1.0",
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
	result, _ := mcp.NewToolResultJSON(map[string]any{
		"device_id":          deviceID,
		"gateway_url":        gwURL,
		"sidecar_to_gateway": "ok",
		"gateway_to_device":  "check_not_implemented",
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
	result, _ := mcp.NewToolResultJSON(cfg)
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
