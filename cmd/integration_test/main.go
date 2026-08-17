package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"time"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/ng/devagent/internal/config"
	"github.com/ng/devagent/internal/daemon"
	"github.com/ng/devagent/internal/mcptool"
	"github.com/ng/devagent/internal/model"
	"github.com/ng/devagent/internal/protocol"
	"github.com/ng/devagent/internal/sidecar"
)

func main() {
	fmt.Println("=== devagent 集成测试 ===")
	fmt.Println()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	rt := model.NewRouteTable()
	dedup := sidecar.NewDedupWindow()
	progress := sidecar.NewProgressTracker()
	router := sidecar.NewRouter(rt, "", 5*time.Minute, nil)
	srv := sidecar.NewSidecarServer(rt, dedup, progress, router, "")

	mcpSrv := srv.MCPServer()

	c, err := client.NewInProcessClient(mcpSrv)
	if err != nil {
		fmt.Printf("[FAIL] 创建客户端失败: %v\n", err)
		os.Exit(1)
	}

	initResult, err := c.Initialize(ctx, mcp.InitializeRequest{
		Params: mcp.InitializeParams{
			ProtocolVersion: "2024-11-05",
			ClientInfo: mcp.Implementation{
				Name:    "integration-test",
				Version: "1.0",
			},
		},
	})
	if err != nil {
		fmt.Printf("[FAIL] Initialize 失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("[PASS] Initialize: server=%s v%s\n", initResult.ServerInfo.Name, initResult.ServerInfo.Version)

	toolsResult, err := c.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		fmt.Printf("[FAIL] ListTools 失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("[PASS] ListTools: %d 个工具已注册\n", len(toolsResult.Tools))
	for _, tool := range toolsResult.Tools {
		fmt.Printf("       - %s: %s\n", tool.Name, tool.Description)
	}

	callResult, err := c.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "__system__.list_devices",
		},
	})
	if err != nil {
		fmt.Printf("[FAIL] list_devices 调用失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("[PASS] list_devices: %v\n", callResult.Content[0].(mcp.TextContent).Text)

	rt.Register(&model.GatewayMeta{
		ID:      "gw_1",
		URL:     "http://192.168.1.50:8080",
		Devices: []string{"shelf_01", "shelf_02"},
	})

	callResult2, err := c.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "__system__.list_devices",
		},
	})
	if err != nil {
		fmt.Printf("[FAIL] list_devices (注册后) 调用失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("[PASS] list_devices (注册后): %v\n", callResult2.Content[0].(mcp.TextContent).Text)

	diagResult, err := c.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      "__system__.diagnose_connectivity",
			Arguments: map[string]any{"device_id": "shelf_01"},
		},
	})
	if err != nil {
		fmt.Printf("[FAIL] diagnose 调用失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("[PASS] diagnose (shelf_01): %v\n", diagResult.Content[0].(mcp.TextContent).Text)

	diagResult2, err := c.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      "__system__.diagnose_connectivity",
			Arguments: map[string]any{"device_id": "unknown_dev"},
		},
	})
	if err != nil {
		fmt.Printf("[FAIL] diagnose (unknown) 调用失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("[PASS] diagnose (unknown_dev): %v\n", diagResult2.Content[0].(mcp.TextContent).Text)

	jobResult, err := c.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      "__system__.get_job_status",
			Arguments: map[string]any{"job_id": "nonexistent"},
		},
	})
	if err != nil {
		fmt.Printf("[FAIL] get_job_status 调用失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("[PASS] get_job_status (nonexistent): %v\n", jobResult.Content[0].(mcp.TextContent).Text)

	progress.Register("job_test_1", "req_001", "shelf_01")
	progress.UpdateProgress("job_test_1", 50, "running")
	jobResult2, err := c.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      "__system__.get_job_status",
			Arguments: map[string]any{"job_id": "job_test_1"},
		},
	})
	if err != nil {
		fmt.Printf("[FAIL] get_job_status (已注册) 调用失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("[PASS] get_job_status (job_test_1): %v\n", jobResult2.Content[0].(mcp.TextContent).Text)

	cfg, err := sidecar.LoadDeviceConfig("configs/example_device.yaml")
	if err != nil {
		fmt.Printf("[WARN] 加载YAML配置失败: %v\n", err)
	} else {
		fmt.Printf("[PASS] YAML配置加载: device=%s, capabilities=%d\n", cfg.Device.ID, len(cfg.Capabilities))
		tool := mcptool.CompileTool(cfg.Device.ID, cfg.Capabilities[0], false)
		fmt.Printf("[PASS] Tool编译: name=%s, description=%s\n", tool.Name, tool.Description)
	}

	fmt.Println()
	fmt.Println("=== Daemon 集成测试 ===")

	daemonSrv := daemon.NewDaemonServer("gw_test", 0, "", nil)
	daemonCfg := model.DeviceConfig{
		Device: model.Device{ID: "shelf_01", Name: "货架控制器", Type: "mcu_proxy"},
		Capabilities: []model.Capability{
			{
				Name:        "set_relay",
				Description: "控制继电器",
				InputSchema: model.InputSchema{
					Type: "object",
					Properties: map[string]model.PropertySchema{
						"pin":   {Type: "integer", Enum: []int{1, 2, 3}},
						"state": {Type: "boolean"},
					},
					Required: []string{"pin", "state"},
				},
				Implementation: model.Implementation{
					Proxy: "uart", Protocol: "uRPC",
					CmdMap: map[string]model.CmdMap{"set_relay": {Cmd: 161, Fmt: "{pin} {state}"}},
				},
			},
		},
	}
	daemonSrv.Registry().Register(daemonCfg)
	daemonSrv.RegisterDeviceTools(daemonCfg)
	mcpDaemon := daemonSrv.MCPServer()

	dc, err := client.NewInProcessClient(mcpDaemon)
	if err != nil {
		fmt.Printf("[FAIL] 创建 Daemon 客户端失败: %v\n", err)
		os.Exit(1)
	}

	daemonInit, err := dc.Initialize(ctx, mcp.InitializeRequest{
		Params: mcp.InitializeParams{
			ProtocolVersion: "2024-11-05",
			ClientInfo: mcp.Implementation{
				Name:    "daemon-test",
				Version: "1.0",
			},
		},
	})
	if err != nil {
		fmt.Printf("[FAIL] Daemon Initialize 失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("[PASS] Daemon Initialize: server=%s v%s\n", daemonInit.ServerInfo.Name, daemonInit.ServerInfo.Version)

	daemonTools, err := dc.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		fmt.Printf("[FAIL] Daemon ListTools 失败: %v\n", err)
		os.Exit(1)
	}
	if len(daemonTools.Tools) != 1 || daemonTools.Tools[0].Name != "shelf_01.set_relay" {
		fmt.Printf("[FAIL] Daemon 工具注册: 期望 shelf_01.set_relay, 实际 %d 个工具\n", len(daemonTools.Tools))
		os.Exit(1)
	}
	fmt.Printf("[PASS] Daemon 工具注册: %d 个工具\n", len(daemonTools.Tools))
	for _, tool := range daemonTools.Tools {
		fmt.Printf("       - %s: %s\n", tool.Name, tool.Description)
	}

	fmt.Println()
	fmt.Println("=== DCP 协议测试 ===")

	intentID := protocol.IntentID("shelf_01.set_relay")
	fmt.Printf("[PASS] IntentID(shelf_01.set_relay) = %d\n", intentID)

	params := map[string]any{"pin": float64(1), "state": true}
	callFrame, err := protocol.EncodeDCPCall(1, intentID, params)
	if err != nil {
		fmt.Printf("[FAIL] DCP encode call: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("[PASS] DCP encode call: %d bytes, kind=0x%02X\n", len(callFrame), callFrame[1])

	dryRunFrame, err := protocol.EncodeDCPDryRun(1, intentID, params)
	if err != nil {
		fmt.Printf("[FAIL] DCP encode dry-run: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("[PASS] DCP encode dry-run: kind=0x%02X\n", dryRunFrame[1])

	decoded, err := protocol.DecodeDCPFrame(callFrame)
	if err != nil {
		fmt.Printf("[FAIL] DCP decode: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("[PASS] DCP decode: intent_id=%d, level=%v\n", decoded.IntentID, decoded.Payload["pin"])

	replyFrame, err := protocol.EncodeDCPReply(1, intentID, protocol.DCPReplyOK, map[string]any{"queue_depth": 0})
	if err != nil {
		fmt.Printf("[FAIL] DCP encode reply: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("[PASS] DCP encode reply: %d bytes, kind=0x%02X\n", len(replyFrame), replyFrame[1])

	fmt.Println()
	fmt.Println("=== HMAC Token 测试 ===")

	tm := sidecar.NewTokenManager("test-integration-secret")
	token, err := tm.Mint([]string{"shelf_01.set_relay", "shelf_01.read_temp"}, time.Hour)
	if err != nil {
		fmt.Printf("[FAIL] Token mint: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("[PASS] Token minted: %s...%s\n", token[:20], token[len(token)-8:])

	claims, err := tm.Verify(token)
	if err != nil {
		fmt.Printf("[FAIL] Token verify: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("[PASS] Token verified: caps=%v\n", claims.Caps)

	if !tm.CheckCap(claims, "shelf_01.set_relay") {
		fmt.Println("[FAIL] CheckCap denied shelf_01.set_relay")
		os.Exit(1)
	}
	fmt.Println("[PASS] CheckCap allowed shelf_01.set_relay")

	if tm.CheckCap(claims, "motor.start") {
		fmt.Println("[FAIL] CheckCap allowed motor.start (should deny)")
		os.Exit(1)
	}
	fmt.Println("[PASS] CheckCap denied motor.start")

	fmt.Println()
	fmt.Println("=== Native Shell Handler 测试 ===")

	shellHandler := &daemon.ShellHandler{}
	nativeResult, err := shellHandler.Execute(ctx, "test_pc", model.Capability{
		Name: "shell_exec",
		Implementation: model.Implementation{
			Native:    "shell",
			TimeoutMs: 5000,
		},
	}, map[string]any{"command": "echo hello_devagent"})
	if err != nil {
		fmt.Printf("[FAIL] ShellHandler execute: %v\n", err)
		os.Exit(1)
	}
	if nativeResult.Status != "ok" {
		fmt.Printf("[FAIL] ShellHandler status: expected ok, got %s\n", nativeResult.Status)
		os.Exit(1)
	}
	fmt.Printf("[PASS] ShellHandler: status=%s, stdout=%q\n", nativeResult.Status, nativeResult.Stdout)

	errResult, _ := shellHandler.Execute(ctx, "test_pc", model.Capability{
		Name: "shell_exec",
		Implementation: model.Implementation{
			Native:    "shell",
			TimeoutMs: 5000,
		},
	}, map[string]any{"command": "exit 1"})
	if errResult.Status != "error" {
		fmt.Printf("[FAIL] ShellHandler error status: expected error, got %s\n", errResult.Status)
		os.Exit(1)
	}
	fmt.Printf("[PASS] ShellHandler error: status=%s, exit_code=%d\n", errResult.Status, errResult.ExitCode)

	missingResult, _ := shellHandler.Execute(ctx, "test_pc", model.Capability{
		Name: "shell_exec",
		Implementation: model.Implementation{
			Native:    "shell",
			TimeoutMs: 5000,
		},
	}, map[string]any{})
	if missingResult.Status != "error" {
		fmt.Printf("[FAIL] ShellHandler missing param: expected error, got %s\n", missingResult.Status)
		os.Exit(1)
	}
	fmt.Printf("[PASS] ShellHandler missing param: status=%s\n", missingResult.Status)

	fmt.Println()
	fmt.Println("=== /invoke + /devices HTTP 路由测试 ===")

	daemonSrv2 := daemon.NewDaemonServer("gw_http_test", 0, "", nil)
	pcCfg := model.DeviceConfig{
		Device: model.Device{
			ID:   "my_pc",
			Name: "我的电脑",
			Type: "direct",
		},
		Capabilities: []model.Capability{
			{
				Name:        "shell_exec",
				Description: "执行shell命令",
				InputSchema: model.InputSchema{
					Type: "object",
					Properties: map[string]model.PropertySchema{
						"command": {Type: "string"},
					},
					Required: []string{"command"},
				},
				Implementation: model.Implementation{
					Native:    "shell",
					TimeoutMs: 5000,
				},
			},
		},
	}
	daemonSrv2.Registry().Register(pcCfg)

	mux := http.NewServeMux()
	mux.HandleFunc("/invoke", daemonSrv2.HandleHTTPInvoke())
	mux.HandleFunc("/devices", daemonSrv2.HandleHTTPDevices())
	testServer := httptest.NewServer(mux)
	defer testServer.Close()

	devicesResp, err := http.Get(testServer.URL + "/devices")
	if err != nil {
		fmt.Printf("[FAIL] GET /devices: %v\n", err)
		os.Exit(1)
	}
	var deviceCfgs []model.DeviceConfig
	json.NewDecoder(devicesResp.Body).Decode(&deviceCfgs)
	devicesResp.Body.Close()
	if len(deviceCfgs) != 1 || deviceCfgs[0].Device.ID != "my_pc" {
		fmt.Printf("[FAIL] GET /devices: expected my_pc, got %v\n", deviceCfgs)
		os.Exit(1)
	}
	fmt.Printf("[PASS] GET /devices: %d devices, first=%s\n", len(deviceCfgs), deviceCfgs[0].Device.ID)

	invokeBody := &protocol.SSEMessage{
		Type:       "invoke",
		RequestID:  "req_test_1",
		DeviceID:   "my_pc",
		Capability: "shell_exec",
		Params:     map[string]any{"command": "echo invoke_test"},
	}
	bodyBytes, _ := json.Marshal(invokeBody)
	invokeResp, err := http.Post(testServer.URL+"/invoke", "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		fmt.Printf("[FAIL] POST /invoke: %v\n", err)
		os.Exit(1)
	}
	var invokeResult protocol.SSEMessage
	json.NewDecoder(invokeResp.Body).Decode(&invokeResult)
	invokeResp.Body.Close()
	if invokeResult.Status != "ok" {
		fmt.Printf("[FAIL] POST /invoke: expected ok, got %s\n", invokeResult.Status)
		os.Exit(1)
	}
	fmt.Printf("[PASS] POST /invoke: status=%s, type=%s\n", invokeResult.Status, invokeResult.Type)

	notFoundBody := &protocol.SSEMessage{
		Type:       "invoke",
		RequestID:  "req_test_2",
		DeviceID:   "unknown_device",
		Capability: "shell_exec",
		Params:     map[string]any{"command": "echo test"},
	}
	nfBytes, _ := json.Marshal(notFoundBody)
	nfResp, err := http.Post(testServer.URL+"/invoke", "application/json", bytes.NewReader(nfBytes))
	if err != nil {
		fmt.Printf("[FAIL] POST /invoke (unknown device): %v\n", err)
		os.Exit(1)
	}
	var nfResult protocol.SSEMessage
	json.NewDecoder(nfResp.Body).Decode(&nfResult)
	nfResp.Body.Close()
	if nfResult.Status != "device_not_found" {
		fmt.Printf("[FAIL] POST /invoke (unknown device): expected device_not_found, got %s\n", nfResult.Status)
		os.Exit(1)
	}
	fmt.Printf("[PASS] POST /invoke (unknown device): status=%s\n", nfResult.Status)

	fmt.Println()
	fmt.Println("=== Token 认证测试 ===")

	daemonSrv3 := daemon.NewDaemonServer("gw_auth_test", 0, "", &config.GlobalConfig{
		Token: config.TokenConfig{Secret: "auth-test-secret", DefaultTTL: time.Hour},
	})
	daemonSrv3.Registry().Register(pcCfg)

	authMux := http.NewServeMux()
	authMux.HandleFunc("/invoke", daemonSrv3.HandleHTTPInvoke())
	authMux.HandleFunc("/devices", daemonSrv3.HandleHTTPDevices())
	authServer := httptest.NewServer(authMux)
	defer authServer.Close()

	noAuthResp, err := http.Post(authServer.URL+"/invoke", "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		fmt.Printf("[FAIL] POST /invoke (no auth): %v\n", err)
		os.Exit(1)
	}
	var noAuthResult protocol.SSEMessage
	json.NewDecoder(noAuthResp.Body).Decode(&noAuthResult)
	noAuthResp.Body.Close()
	if noAuthResult.Status != "unauthorized" {
		fmt.Printf("[FAIL] POST /invoke (no auth): expected unauthorized, got %s\n", noAuthResult.Status)
		os.Exit(1)
	}
	fmt.Printf("[PASS] POST /invoke (no auth): status=%s\n", noAuthResult.Status)

	daemonTM := daemon.NewTokenManager("auth-test-secret")
	authToken, _ := daemonTM.Mint([]string{"my_pc.shell_exec"}, time.Hour)
	authReq, _ := http.NewRequest("POST", authServer.URL+"/invoke", bytes.NewReader(bodyBytes))
	authReq.Header.Set("Authorization", "Bearer "+authToken)
	authReq.Header.Set("Content-Type", "application/json")
	authResp, err := http.DefaultClient.Do(authReq)
	if err != nil {
		fmt.Printf("[FAIL] POST /invoke (with auth): %v\n", err)
		os.Exit(1)
	}
	var authResult protocol.SSEMessage
	json.NewDecoder(authResp.Body).Decode(&authResult)
	authResp.Body.Close()
	if authResult.Status != "ok" {
		fmt.Printf("[FAIL] POST /invoke (with auth): expected ok, got %s\n", authResult.Status)
		os.Exit(1)
	}
	fmt.Printf("[PASS] POST /invoke (with auth): status=%s\n", authResult.Status)

	fmt.Println()
	fmt.Println("=== Sidecar 自动注册设备工具测试 ===")

	rt2 := model.NewRouteTable()
	dedup2 := sidecar.NewDedupWindow()
	progress2 := sidecar.NewProgressTracker()
	router2 := sidecar.NewRouter(rt2, "", 5*time.Minute, nil)
	srv2 := sidecar.NewSidecarServer(rt2, dedup2, progress2, router2, "")

	registered := make(chan string, 1)
	router2.OnDiscover(func(cfg *model.DeviceConfig) {
		srv2.RegisterDeviceTools(cfg)
		registered <- cfg.Device.ID
	})

	devicesMux := http.NewServeMux()
	devicesMux.HandleFunc("/devices", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]model.DeviceConfig{
			{
				Device: model.Device{ID: "auto_pc", Name: "自动注册电脑", Type: "direct"},
				Capabilities: []model.Capability{
					{
						Name:        "shell_exec",
						Description: "执行shell命令",
						InputSchema: model.InputSchema{
							Type:       "object",
							Properties: map[string]model.PropertySchema{"command": {Type: "string"}},
							Required:   []string{"command"},
						},
						Implementation: model.Implementation{Native: "shell", TimeoutMs: 5000},
					},
				},
			},
		})
	})
	mockGW := httptest.NewServer(devicesMux)
	defer mockGW.Close()

	router2.FetchAndRegisterDevices("gw_auto", mockGW.URL)

	select {
	case devID := <-registered:
		fmt.Printf("[PASS] 自动注册设备: device_id=%s\n", devID)
	case <-time.After(3 * time.Second):
		fmt.Println("[FAIL] 自动注册设备超时")
		os.Exit(1)
	}

	srv2Tools, _ := client.NewInProcessClient(srv2.MCPServer())
	srv2Tools.Initialize(ctx, mcp.InitializeRequest{
		Params: mcp.InitializeParams{
			ProtocolVersion: "2024-11-05",
			ClientInfo:      mcp.Implementation{Name: "auto-reg-test", Version: "1.0"},
		},
	})
	tools2, err := srv2Tools.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		fmt.Printf("[FAIL] 自动注册后 ListTools: %v\n", err)
		os.Exit(1)
	}
	found := false
	for _, t := range tools2.Tools {
		if t.Name == "auto_pc.shell_exec" {
			found = true
			break
		}
	}
	if !found {
		fmt.Printf("[FAIL] 自动注册后未找到 auto_pc.shell_exec 工具\n")
		os.Exit(1)
	}
	fmt.Printf("[PASS] 自动注册后工具已可用: auto_pc.shell_exec\n")

	fmt.Println()
	fmt.Println("=== 全局配置加载测试 ===")

	gcfg, err := config.Load("")
	if err != nil {
		fmt.Printf("[FAIL] config.Load empty: %v\n", err)
		os.Exit(1)
	}
	if gcfg.Sidecar.DedupTTL != 3*time.Second {
		fmt.Printf("[FAIL] default dedup_ttl: %v\n", gcfg.Sidecar.DedupTTL)
		os.Exit(1)
	}
	fmt.Printf("[PASS] 默认配置: dedup_ttl=%v, heartbeat_interval=%v\n", gcfg.Sidecar.DedupTTL, gcfg.Daemon.HeartbeatInterval)

	gcfg2, err := config.Load("configs/devagent.yaml")
	if err != nil {
		fmt.Printf("[FAIL] config.Load devagent.yaml: %v\n", err)
		os.Exit(1)
	}
	if gcfg2.Sidecar.DedupTTL != 3*time.Second {
		fmt.Printf("[FAIL] file dedup_ttl: %v\n", gcfg2.Sidecar.DedupTTL)
		os.Exit(1)
	}
	fmt.Printf("[PASS] 文件配置: dedup_ttl=%v, daemon.heartbeat_interval=%v\n", gcfg2.Sidecar.DedupTTL, gcfg2.Daemon.HeartbeatInterval)

	fmt.Println()
	fmt.Println("=== 全部测试通过 ===")
}
