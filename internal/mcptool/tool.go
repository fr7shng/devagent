package mcptool

import (
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/ng/devagent/internal/model"
)

func CompileTool(deviceID string, cap model.Capability, maintenance bool) mcp.Tool {
	name := fmt.Sprintf("%s.%s", deviceID, cap.Name)
	desc := cap.Description

	if maintenance {
		desc = "[维护中] " + desc
	}

	var unitParts []string
	for propName, prop := range cap.InputSchema.Properties {
		if prop.Unit != "" {
			unitParts = append(unitParts, fmt.Sprintf("%s(%s)", propName, prop.Unit))
		}
	}
	if len(unitParts) > 0 {
		desc = desc + " " + strings.Join(unitParts, ", ")
	}

	opts := []mcp.ToolOption{mcp.WithDescription(desc)}

	for propName, prop := range cap.InputSchema.Properties {
		switch prop.Type {
		case "integer":
			intOpts := []mcp.PropertyOption{}
			if len(prop.Enum) > 0 {
				intOpts = append(intOpts, enumOption(prop.Enum))
			}
			if prop.Min != nil {
				intOpts = append(intOpts, mcp.Min(int(*prop.Min)))
			}
			if prop.Max != nil {
				intOpts = append(intOpts, mcp.Max(int(*prop.Max)))
			}
			if model.IsRequired(propName, cap.InputSchema.Required) {
				intOpts = append(intOpts, mcp.Required())
			}
			opts = append(opts, mcp.WithInteger(propName, intOpts...))
		case "string":
			strOpts := []mcp.PropertyOption{}
			if model.IsRequired(propName, cap.InputSchema.Required) {
				strOpts = append(strOpts, mcp.Required())
			}
			opts = append(opts, mcp.WithString(propName, strOpts...))
		case "boolean":
			boolOpts := []mcp.PropertyOption{}
			if model.IsRequired(propName, cap.InputSchema.Required) {
				boolOpts = append(boolOpts, mcp.Required())
			}
			opts = append(opts, mcp.WithBoolean(propName, boolOpts...))
		case "number":
			numOpts := []mcp.PropertyOption{}
			if len(prop.Enum) > 0 {
				numOpts = append(numOpts, enumOption(prop.Enum))
			}
			if prop.Min != nil {
				numOpts = append(numOpts, mcp.Min(*prop.Min))
			}
			if prop.Max != nil {
				numOpts = append(numOpts, mcp.Max(*prop.Max))
			}
			if model.IsRequired(propName, cap.InputSchema.Required) {
				numOpts = append(numOpts, mcp.Required())
			}
			opts = append(opts, mcp.WithNumber(propName, numOpts...))
		}
	}

	return mcp.NewTool(name, opts...)
}

// enumOption 生成把属性 enum 以正确 JSON 类型写入 schema 的选项。
// mcp.Enum 只接受 string 且每次调用都会覆盖 schema["enum"]（多次调用只会保留最后一个值），
// 因此这里把整数/浮点枚举直接写成数值数组，生成合法的 JSON Schema。
func enumOption(enums []int) mcp.PropertyOption {
	return func(schema map[string]any) {
		vals := make([]any, 0, len(enums))
		for _, v := range enums {
			vals = append(vals, float64(v))
		}
		schema["enum"] = vals
	}
}
