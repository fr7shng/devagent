package daemon

import (
	"context"
	"strings"
	"testing"

	"github.com/ng/devagent/internal/model"
)

func TestShellHandlerCmdMapParams(t *testing.T) {
	sh := &ShellHandler{}
	cap := model.Capability{
		Name: "set_relay",
		Implementation: model.Implementation{
			Native:          "shell",
			AllowedCommands: []string{"echo"},
			CmdMap:          map[string]model.CmdMap{"set_relay": {Cmd: 0, Fmt: "echo pin={pin} state={state}"}},
			TimeoutMs:       5000,
		},
	}

	result, err := sh.Execute(context.Background(), "mock_gw", cap, map[string]any{"pin": 1, "state": true})
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if result.Status != "ok" {
		t.Errorf("expected ok, got %s (stderr=%s)", result.Status, result.Stderr)
	}
	if !strings.Contains(result.Stdout, "pin=1 state=1") {
		t.Errorf("expected 'pin=1 state=1' in stdout, got %q", result.Stdout)
	}
}

func TestShellHandlerCmdMapDeniedByWhitelist(t *testing.T) {
	sh := &ShellHandler{}
	cap := model.Capability{
		Name: "set_relay",
		Implementation: model.Implementation{
			Native:          "shell",
			AllowedCommands: []string{"mock_relay.sh"},
			CmdMap:          map[string]model.CmdMap{"set_relay": {Cmd: 0, Fmt: "echo {pin}"}},
			TimeoutMs:       5000,
		},
	}

	result, err := sh.Execute(context.Background(), "mock_gw", cap, map[string]any{"pin": 1})
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if result.Status != "denied" {
		t.Errorf("expected denied (echo not whitelisted), got %s", result.Status)
	}
}
