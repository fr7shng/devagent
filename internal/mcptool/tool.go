package mcptool

import (
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/ng/devagent/internal/model"
	"github.com/ng/devagent/internal/protocol"
)

func CompileTool(deviceID string, cap model.Capability, maintenance bool) mcp.Tool {
	name := fmt.Sprintf("%s.%s", deviceID, cap.Name)
	intentID := cap.IntentID
	if intentID == 0 {
		intentID = protocol.IntentID(deviceID + "." + cap.Name)
	}
	_ = intentID
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
			for _, v := range prop.Enum {
				intOpts = append(intOpts, mcp.Enum(fmt.Sprintf("%d", v)))
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
