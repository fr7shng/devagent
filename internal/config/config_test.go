package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeTempConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "devagent.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write temp config: %v", err)
	}
	return path
}

func TestDefaultGlobalConfig(t *testing.T) {
	cfg := DefaultGlobalConfig()
	if cfg.Sidecar.DedupTTL != 3*time.Second {
		t.Errorf("expected dedup_ttl 3s, got %v", cfg.Sidecar.DedupTTL)
	}
	if cfg.Daemon.HeartbeatInterval != 30*time.Second {
		t.Errorf("expected heartbeat_interval 30s, got %v", cfg.Daemon.HeartbeatInterval)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("expected log_level info, got %s", cfg.LogLevel)
	}
	if cfg.Token.DefaultTTL != time.Hour {
		t.Errorf("expected token default_ttl 1h, got %v", cfg.Token.DefaultTTL)
	}
}

func TestLoadEmptyPath(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load empty path failed: %v", err)
	}
	if cfg.Sidecar.DedupTTL != 3*time.Second {
		t.Errorf("expected defaults for empty path, got %v", cfg.Sidecar.DedupTTL)
	}
}

func TestLoadFromFile(t *testing.T) {
	path := writeTempConfig(t, `
log_level: debug
sidecar:
  dedup_ttl: 5s
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load file failed: %v", err)
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("expected log_level debug, got %s", cfg.LogLevel)
	}
	if cfg.Sidecar.DedupTTL != 5*time.Second {
		t.Errorf("expected dedup_ttl 5s, got %v", cfg.Sidecar.DedupTTL)
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "nope.yaml"))
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoadInvalidYAML(t *testing.T) {
	path := writeTempConfig(t, "not: [valid\n  yaml: {")
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected parse error")
	}
}

func TestValidateSecretTooShort(t *testing.T) {
	cfg := DefaultGlobalConfig()
	cfg.Token.Secret = "short"
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for secret shorter than 16 chars")
	}
}

func TestValidateSecretOK(t *testing.T) {
	cfg := DefaultGlobalConfig()
	cfg.Token.Secret = "0123456789abcdef"
	if err := cfg.Validate(); err != nil {
		t.Errorf("expected valid secret, got %v", err)
	}
}

func TestValidateDedupTTLZero(t *testing.T) {
	cfg := DefaultGlobalConfig()
	cfg.Sidecar.DedupTTL = 0
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for zero dedup_ttl")
	}
}

func TestValidateHeartbeatTimeout(t *testing.T) {
	cfg := DefaultGlobalConfig()
	cfg.Daemon.HeartbeatTimeout = cfg.Daemon.HeartbeatInterval
	if err := cfg.Validate(); err == nil {
		t.Error("expected error when timeout <= interval")
	}
}

func TestValidateLogLevel(t *testing.T) {
	cfg := DefaultGlobalConfig()
	cfg.LogLevel = "verbose"
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for invalid log_level")
	}
}

func TestEnvOverrides(t *testing.T) {
	path := writeTempConfig(t, `
token:
  secret: "0123456789abcdef"
log_level: info
`)
	t.Setenv("DEVAGENT_TOKEN_SECRET", "fedcba9876543210")
	t.Setenv("DEVAGENT_LOG_LEVEL", "warn")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.Token.Secret != "fedcba9876543210" {
		t.Errorf("expected env secret override, got %q", cfg.Token.Secret)
	}
	if cfg.LogLevel != "warn" {
		t.Errorf("expected env log_level warn, got %s", cfg.LogLevel)
	}
}

func TestEnvOverridesIgnoredWhenEmpty(t *testing.T) {
	path := writeTempConfig(t, `
token:
  secret: "0123456789abcdef"
`)
	t.Setenv("DEVAGENT_LOG_LEVEL", "")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("expected file default info when env empty, got %s", cfg.LogLevel)
	}
}
