package mcptool

import (
	"strings"
	"testing"

	"github.com/ng/devagent/internal/model"
)

func TestCompileTool(t *testing.T) {
	cap := model.Capability{
		Name:        "set_relay",
		Description: "控制继电器通断",
		InputSchema: model.InputSchema{
			Type: "object",
			Properties: map[string]model.PropertySchema{
				"pin":   {Type: "integer", Enum: []int{1, 2, 3}},
				"state": {Type: "boolean"},
			},
			Required: []string{"pin", "state"},
		},
	}

	tool := CompileTool("shelf_01", cap, false)

	if tool.Name != "shelf_01.set_relay" {
		t.Errorf("expected tool name 'shelf_01.set_relay', got '%s'", tool.Name)
	}
	if tool.Description != "控制继电器通断" {
		t.Errorf("expected description match, got '%s'", tool.Description)
	}
}

func TestCompileDirectTool(t *testing.T) {
	cap := model.Capability{
		Name:        "shell_exec",
		Description: "执行shell命令",
		InputSchema: model.InputSchema{
			Type: "object",
			Properties: map[string]model.PropertySchema{
				"command": {Type: "string"},
			},
			Required: []string{"command"},
		},
	}

	tool := CompileTool("my_pc", cap, false)

	if tool.Name != "my_pc.shell_exec" {
		t.Errorf("expected tool name 'my_pc.shell_exec', got '%s'", tool.Name)
	}
}

func TestCompileToolWithUnit(t *testing.T) {
	cap := model.Capability{
		Name:        "set_temperature",
		Description: "设置温度",
		InputSchema: model.InputSchema{
			Type: "object",
			Properties: map[string]model.PropertySchema{
				"value": {Type: "number", Unit: "celsius", Min: float64Ptr(16), Max: float64Ptr(30)},
			},
			Required: []string{"value"},
		},
	}

	tool := CompileTool("thermo_01", cap, false)

	if !strings.Contains(tool.Description, "value(celsius)") {
		t.Errorf("expected description to contain unit info, got '%s'", tool.Description)
	}
}

func TestCompileToolMaintenance(t *testing.T) {
	cap := model.Capability{
		Name:        "set_relay",
		Description: "控制继电器",
		InputSchema: model.InputSchema{Type: "object"},
	}

	tool := CompileTool("shelf_01", cap, true)
	if !strings.Contains(tool.Description, "[维护中]") {
		t.Errorf("expected maintenance prefix, got '%s'", tool.Description)
	}

	tool2 := CompileTool("shelf_01", cap, false)
	if strings.Contains(tool2.Description, "[维护中]") {
		t.Errorf("expected no maintenance prefix, got '%s'", tool2.Description)
	}
}

// integer 属性的 enum 应还原为数值，而非 mcp.Enum 产出的字符串。
func TestCompileToolIntegerEnumAsNumbers(t *testing.T) {
	cap := model.Capability{
		Name:        "set_relay",
		Description: "控制继电器",
		InputSchema: model.InputSchema{
			Type: "object",
			Properties: map[string]model.PropertySchema{
				"pin": {Type: "integer", Enum: []int{1, 2, 3}},
			},
			Required: []string{"pin"},
		},
	}
	tool := CompileTool("shelf_01", cap, false)
	raw, ok := tool.InputSchema.Properties["pin"].(map[string]any)
	if !ok {
		t.Fatalf("expected property pin schema map, got %T", tool.InputSchema.Properties["pin"])
	}
	enum, ok := raw["enum"].([]any)
	if !ok {
		t.Fatalf("expected numeric enum, got %T (%v)", raw["enum"], raw["enum"])
	}
	if len(enum) != 3 {
		t.Errorf("expected 3 enum values, got %v", enum)
	}
	if v, ok := enum[0].(float64); !ok || v != 1 {
		t.Errorf("expected enum[0] == 1, got %v (%T)", enum[0], enum[0])
	}
}

func float64Ptr(v float64) *float64 {
	return &v
}
