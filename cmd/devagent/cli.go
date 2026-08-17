package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ng/devagent/internal/protocol"
	"github.com/ng/devagent/internal/sidecar"
)

const globalConfigTemplate = `# devagent 全局配置
sidecar:
  mdns_interval: 10s
  dedup_ttl: 3s
  health_check_interval: 30s
  maintenance_timeout: 60s
  heartbeat_timeout: 90s
  # 静态网关（可选）：sidecar 与 daemon 同机运行、或 mDNS 不可用时直接指定
  # static_gateways:
  #   - { id: "gw_mock", url: "http://localhost:8082" }

daemon:
  heartbeat_interval: 30s
  heartbeat_timeout: 60s
  state_path: ""

log_level: info

token:
  secret: ""          # 留空则不校验 token；生产环境建议 >= 16 字符
  default_ttl: 3600s
`

const deviceConfigTemplate = `# 示例设备物模型（无硬件，可直接运行）
# 启动：devagent -mode daemon -port 8082 -config device.yaml -gateway-id gw_mock
device:
  id: "mock_gw"
  name: "模拟设备（无需硬件）"
  type: "direct"

capabilities:
  - name: set_relay
    description: "控制继电器通断（模拟，无真实硬件）"
    inputSchema:
      type: object
      properties:
        pin:   { type: integer, enum: [1, 2, 3], unit: "gpio_pin" }
        state: { type: boolean, unit: "on_off" }
      required: [pin, state]
    implementation:
      native: "shell"
      allowed_commands: ["echo"]
      cmd_map:
        set_relay: { cmd: 0, fmt: "echo mock relay pin={pin} state={state}" }
      timeout_ms: 5000
`

func runCLI(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "init":
		dir := "."
		if len(args) > 1 {
			dir = args[1]
		}
		if err := cmdInit(dir); err != nil {
			fmt.Fprintln(os.Stderr, "init 失败:", err)
			os.Exit(1)
		}
		return true
	case "validate":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "用法: devagent validate <device.yaml>")
			os.Exit(1)
		}
		if err := cmdValidate(args[1]); err != nil {
			fmt.Fprintln(os.Stderr, "校验失败:", err)
			os.Exit(1)
		}
		return true
	case "schema":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "用法: devagent schema <device.yaml>")
			os.Exit(1)
		}
		if err := cmdSchema(args[1]); err != nil {
			fmt.Fprintln(os.Stderr, "schema 失败:", err)
			os.Exit(1)
		}
		return true
	}
	return false
}

func cmdInit(dir string) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("创建目录: %w", err)
	}
	files := map[string]string{
		"devagent.yaml": globalConfigTemplate,
		"device.yaml":   deviceConfigTemplate,
	}
	for name, content := range files {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); err == nil {
			fmt.Printf("跳过 %s（已存在）\n", path)
			continue
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			return fmt.Errorf("写入 %s: %w", path, err)
		}
		fmt.Printf("生成 %s\n", path)
	}
	fmt.Println("完成。下一步：devagent -mode daemon -port 8082 -config device.yaml -gateway-id gw_mock")
	return nil
}

func cmdValidate(path string) error {
	cfg, err := sidecar.LoadDeviceConfig(path)
	if err != nil {
		return err
	}
	if cfg.Device.ID == "" {
		return fmt.Errorf("device.id 不能为空")
	}
	if len(cfg.Capabilities) == 0 {
		return fmt.Errorf("capabilities 至少需要 1 个")
	}
	for _, cap := range cfg.Capabilities {
		if cap.Name == "" {
			return fmt.Errorf("存在 name 为空的能力")
		}
		impl := cap.Implementation
		if impl.Native == "" && impl.Proxy == "" {
			return fmt.Errorf("能力 %s 缺少实现（native 或 proxy）", cap.Name)
		}
	}
	fmt.Printf("OK: device=%s, capabilities=%d\n", cfg.Device.ID, len(cfg.Capabilities))
	return nil
}

func cmdSchema(path string) error {
	cfg, err := sidecar.LoadDeviceConfig(path)
	if err != nil {
		return err
	}
	fmt.Printf("设备: %s (%s)\n", cfg.Device.ID, cfg.Device.Name)
	for _, cap := range cfg.Capabilities {
		intent := cap.IntentID
		if intent == 0 {
			intent = protocol.IntentID(cfg.Device.ID + "." + cap.Name)
		}
		fmt.Printf("  - %s.%s\n", cfg.Device.ID, cap.Name)
		fmt.Printf("      描述: %s\n", cap.Description)
		fmt.Printf("      intent_id: %d\n", intent)
		if impl := cap.Implementation; impl.Native != "" {
			fmt.Printf("      实现: native=%s\n", impl.Native)
		} else if impl.Proxy != "" {
			fmt.Printf("      实现: proxy=%s protocol=%s channel=%s\n", impl.Proxy, impl.Protocol, impl.Channel)
		}
		props := cap.InputSchema.Properties
		if len(props) > 0 {
			fmt.Printf("      参数:\n")
			for name, p := range props {
				fmt.Printf("        %s (%s)\n", name, p.Type)
			}
		}
	}
	return nil
}
