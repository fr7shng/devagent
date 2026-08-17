package sidecar

import (
	"testing"
)

func TestLoadDeviceConfig(t *testing.T) {
	cfg, err := LoadDeviceConfig("../../configs/example_device.yaml")
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}
	if cfg.Device.ID != "shelf_01" {
		t.Errorf("expected device id 'shelf_01', got '%s'", cfg.Device.ID)
	}
	if len(cfg.Capabilities) < 1 {
		t.Error("expected at least 1 capability")
	}
}
