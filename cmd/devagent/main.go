package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/ng/devagent/internal/config"
	"github.com/ng/devagent/internal/daemon"
	"github.com/ng/devagent/internal/model"
	"github.com/ng/devagent/internal/sidecar"
	"github.com/ng/devagent/internal/version"
)

var pidFile string

func main() {
	if runCLI(os.Args[1:]) {
		return
	}

	mode := flag.String("mode", "sidecar", "运行模式: sidecar | daemon")
	port := flag.Int("port", 8080, "daemon 模式监听端口")
	config_ := flag.String("config", "", "设备物模型 YAML 配置路径")
	globalConfig := flag.String("global-config", "configs/devagent.yaml", "全局配置 YAML 路径")
	gatewayID := flag.String("gateway-id", "gw_1", "网关ID (daemon模式)")
	logLevel := flag.String("log-level", "", "日志级别: debug | info | warn | error (覆盖配置文件)")
	flag.StringVar(&pidFile, "pid-file", "", "PID 文件路径 (daemon 模式)")
	flag.Parse()

	gcfg, err := config.Load(*globalConfig)
	if err != nil {
		slog.Error("加载全局配置失败", "error", err)
		os.Exit(1)
	}

	effectiveLogLevel := gcfg.LogLevel
	if *logLevel != "" {
		effectiveLogLevel = *logLevel
	}
	initLogger(effectiveLogLevel)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	switch *mode {
	case "sidecar":
		rt := model.NewRouteTable()
		dedup := sidecar.NewDedupWindowWithTTL(gcfg.Sidecar.DedupTTL)
		dedup.StartCleanup(ctx)
		progress := sidecar.NewProgressTracker()
		progress.StartCleanup(ctx)
		router := sidecar.NewRouter(rt, gcfg.Token.Secret, gcfg.Token.DefaultTTL, &gcfg.Sidecar)
		srv := sidecar.NewSidecarServer(rt, dedup, progress, router, gcfg.Token.Secret)

		router.OnDiscover(func(cfg *model.DeviceConfig) {
			srv.RegisterDeviceTools(cfg)
		})
		router.OnRemove(func(deviceID string) {
			srv.UnregisterDeviceTools(deviceID)
		})

		for _, gw := range gcfg.Sidecar.StaticGateways {
			if gw.ID == "" || gw.URL == "" {
				slog.Warn("静态网关配置缺少 id 或 url", "gateway", gw)
				continue
			}
			router.AddStaticGateway(gw.ID, gw.URL)
			slog.Info("注册静态网关", "gateway_id", gw.ID, "url", gw.URL)
		}

		go router.Discover(ctx)

		slog.Info("devagent sidecar 启动中",
			"protocol", "stdio",
			"mcp_server", "devagent-sidecar",
			"version", version.Version,
			"mdns_service", "_devagent._tcp",
			"tools", "list_devices|diagnose_connectivity|get_device_schema|get_job_status",
			"claude_config", `{"mcpServers":{"devagent":{"command":"devagent","args":["-mode","sidecar"]}}}`,
		)
		if err := srv.ServeStdio(); err != nil {
			slog.Error("sidecar 错误", "error", err)
			os.Exit(1)
		}
	case "daemon":
		if pidFile != "" {
			if err := acquirePIDFile(pidFile); err != nil {
				slog.Error("PID 文件锁定失败，可能已有实例运行", "error", err)
				os.Exit(1)
			}
			defer os.Remove(pidFile)
		}
		ds := daemon.NewDaemonServer(*gatewayID, *port, *config_, gcfg)
		slog.Info("devagent daemon 启动中",
			"protocol", "SSE",
			"mcp_server", "devagent-daemon",
			"version", version.Version,
			"port", *port,
			"config", *config_,
			"gateway_id", *gatewayID,
			"sse_endpoint", fmt.Sprintf("http://localhost:%d/sse", *port),
		)
		if err := ds.Start(ctx); err != nil {
			slog.Error("daemon 错误", "error", err)
			os.Exit(1)
		}
	default:
		slog.Error("未知模式", "mode", *mode)
		os.Exit(1)
	}
}

func initLogger(level string) {
	var l slog.Level
	switch level {
	case "debug":
		l = slog.LevelDebug
	case "warn":
		l = slog.LevelWarn
	case "error":
		l = slog.LevelError
	default:
		l = slog.LevelInfo
	}
	handler := slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: l})
	slog.SetDefault(slog.New(handler))
}

func acquirePIDFile(path string) error {
	data, err := os.ReadFile(path)
	if err == nil && len(data) > 0 {
		existingPID, parseErr := strconv.Atoi(strings.TrimSpace(string(data)))
		if parseErr == nil {
			if proc, err := os.FindProcess(existingPID); err == nil {
				if proc.Signal(syscall.Signal(0)) == nil {
					return fmt.Errorf("process %d already running", existingPID)
				}
			}
		}
		os.Remove(path)
	}
	return os.WriteFile(path, []byte(fmt.Sprintf("%d", os.Getpid())), 0644)
}
