package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type TLSConfig struct {
	CertPath string `json:"cert_path" yaml:"cert_path"`
	KeyPath  string `json:"key_path" yaml:"key_path"`
}

type GlobalConfig struct {
	Sidecar  SidecarConfig  `json:"sidecar" yaml:"sidecar"`
	Daemon   DaemonConfig   `json:"daemon" yaml:"daemon"`
	LogLevel string        `json:"log_level" yaml:"log_level"`
	Token    TokenConfig    `json:"token" yaml:"token"`
	TLS      TLSConfig      `json:"tls" yaml:"tls"`
}

type SidecarConfig struct {
	MDNSInterval        time.Duration  `json:"mdns_interval" yaml:"mdns_interval"`
	DedupTTL            time.Duration  `json:"dedup_ttl" yaml:"dedup_ttl"`
	HealthCheckInterval time.Duration  `json:"health_check_interval" yaml:"health_check_interval"`
	MaintenanceTimeout  time.Duration  `json:"maintenance_timeout" yaml:"maintenance_timeout"`
	HeartbeatTimeout    time.Duration  `json:"heartbeat_timeout" yaml:"heartbeat_timeout"`
	StaticGateways      []StaticGateway `json:"static_gateways,omitempty" yaml:"static_gateways,omitempty"`
}

type StaticGateway struct {
	ID  string `json:"id" yaml:"id"`
	URL string `json:"url" yaml:"url"`
}

type DaemonConfig struct {
	HeartbeatInterval time.Duration `json:"heartbeat_interval" yaml:"heartbeat_interval"`
	HeartbeatTimeout  time.Duration `json:"heartbeat_timeout" yaml:"heartbeat_timeout"`
	StatePath         string        `json:"state_path" yaml:"state_path"`
}

type TokenConfig struct {
	Secret     string        `json:"secret" yaml:"secret"`
	DefaultTTL time.Duration `json:"default_ttl" yaml:"default_ttl"`
}

func DefaultGlobalConfig() *GlobalConfig {
	return &GlobalConfig{
		Sidecar: SidecarConfig{
			MDNSInterval:        10 * time.Second,
			DedupTTL:            3 * time.Second,
			HealthCheckInterval: 30 * time.Second,
			MaintenanceTimeout:  60 * time.Second,
			HeartbeatTimeout:    90 * time.Second,
		},
		Daemon: DaemonConfig{
			HeartbeatInterval: 30 * time.Second,
			HeartbeatTimeout:  60 * time.Second,
		},
		LogLevel: "info",
		Token: TokenConfig{
			Secret:     "",
			DefaultTTL: time.Hour,
		},
	}
}

func Load(path string) (*GlobalConfig, error) {
	cfg := DefaultGlobalConfig()
	if path == "" {
		return cfg, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	applyEnvOverrides(cfg)
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func applyEnvOverrides(cfg *GlobalConfig) {
	if v := os.Getenv("DEVAGENT_TOKEN_SECRET"); v != "" {
		cfg.Token.Secret = v
	}
	if v := os.Getenv("DEVAGENT_LOG_LEVEL"); v != "" {
		cfg.LogLevel = v
	}
	if v := os.Getenv("DEVAGENT_TLS_CERT"); v != "" {
		cfg.TLS.CertPath = v
	}
	if v := os.Getenv("DEVAGENT_TLS_KEY"); v != "" {
		cfg.TLS.KeyPath = v
	}
	if v := os.Getenv("DEVAGENT_STATE_PATH"); v != "" {
		cfg.Daemon.StatePath = v
	}
}

func (c *GlobalConfig) Validate() error {
	if c.Token.Secret != "" && len(c.Token.Secret) < 16 {
		return fmt.Errorf("token.secret too short (min 16 chars)")
	}
	if c.Sidecar.DedupTTL <= 0 {
		return fmt.Errorf("sidecar.dedup_ttl must be positive")
	}
	if c.Daemon.HeartbeatTimeout <= c.Daemon.HeartbeatInterval {
		return fmt.Errorf("daemon.heartbeat_timeout must be greater than heartbeat_interval")
	}
	switch c.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("invalid log_level: %q", c.LogLevel)
	}
	return nil
}
