package sidecar

import (
	"fmt"
	"os"

	"github.com/ng/devagent/internal/model"
	"gopkg.in/yaml.v3"
)

func LoadDeviceConfig(path string) (*model.DeviceConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var cfg model.DeviceConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return &cfg, nil
}
