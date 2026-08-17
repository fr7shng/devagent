package daemon

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/ng/devagent/internal/model"
)

var shellDangerousChars = []string{";", "|", "&", "$(", "`", ">", "<", "&&", "||"}

type NativeResult struct {
	Status   string `json:"status"`
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exit_code"`
}

type NativeHandler interface {
	Execute(ctx context.Context, deviceID string, cap model.Capability, params map[string]any) (*NativeResult, error)
}

type ShellHandler struct{}

func (sh *ShellHandler) Execute(ctx context.Context, deviceID string, cap model.Capability, params map[string]any) (*NativeResult, error) {
	cmdStr, ok := params["command"].(string)
	if !ok {
		return &NativeResult{
			Status:   "error",
			Stderr:   "missing or invalid 'command' parameter",
			ExitCode: -1,
		}, nil
	}

	if len(cap.Implementation.AllowedCommands) > 0 {
		baseCmd := extractBaseCommand(cmdStr)
		found := false
		for _, allowed := range cap.Implementation.AllowedCommands {
			if baseCmd == allowed {
				found = true
				break
			}
		}
		if !found {
			return &NativeResult{
				Status:   "denied",
				Stderr:   fmt.Sprintf("command '%s' not in allowed list", baseCmd),
				ExitCode: -1,
			}, nil
		}
	}

	if err := validateCommandSafety(cmdStr); err != nil {
		return &NativeResult{
			Status:   "denied",
			Stderr:   err.Error(),
			ExitCode: -1,
		}, nil
	}

	timeout := 30 * time.Second
	if cap.Implementation.TimeoutMs > 0 {
		timeout = time.Duration(cap.Implementation.TimeoutMs) * time.Millisecond
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd", "/C", cmdStr)
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", cmdStr)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	exitErr := cmd.Run()

	result := &NativeResult{
		Stdout: stdout.String(),
		Stderr: stderr.String(),
	}

	if exitErr != nil {
		result.Status = "error"
		if exitErr, ok := exitErr.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			result.ExitCode = -1
		}
		if result.Stderr == "" {
			result.Stderr = exitErr.Error()
		}
	} else {
		result.Status = "ok"
		result.ExitCode = 0
	}

	return result, nil
}

func extractBaseCommand(cmdStr string) string {
	cmdStr = strings.TrimSpace(cmdStr)
	if idx := strings.IndexAny(cmdStr, " \t"); idx > 0 {
		return cmdStr[:idx]
	}
	return cmdStr
}

func validateCommandSafety(cmdStr string) error {
	for _, dangerous := range shellDangerousChars {
		if strings.Contains(cmdStr, dangerous) {
			return fmt.Errorf("command contains dangerous character sequence: %q", dangerous)
		}
	}
	return nil
}

func NewNativeHandlerRegistry() map[string]NativeHandler {
	return map[string]NativeHandler{
		"shell": &ShellHandler{},
	}
}

func ExecuteNative(handlers map[string]NativeHandler, nativeType string, ctx context.Context, deviceID string, cap model.Capability, params map[string]any) (*NativeResult, error) {
	handler, ok := handlers[nativeType]
	if !ok {
		return nil, fmt.Errorf("native handler not found: %s", nativeType)
	}
	return handler.Execute(ctx, deviceID, cap, params)
}
