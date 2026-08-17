package daemon

import (
	"testing"
	"time"

	"github.com/ng/devagent/internal/model"
)

func TestDeviceRegistry_Register(t *testing.T) {
	reg := NewDeviceRegistry()
	cfg := model.DeviceConfig{
		Device:       model.Device{ID: "shelf_01", Name: "货架01", Type: "mcu_proxy"},
		Capabilities: []model.Capability{
			{Name: "set_relay", Description: "控制继电器"},
		},
	}
	reg.Register(cfg)
	devices := reg.ListDevices()
	if len(devices) != 1 {
		t.Fatalf("expected 1 device, got %d", len(devices))
	}
	if devices[0].Config.Device.ID != "shelf_01" {
		t.Errorf("expected device_id 'shelf_01', got '%s'", devices[0].Config.Device.ID)
	}
}

func TestDeviceRegistry_Heartbeat(t *testing.T) {
	reg := NewDeviceRegistry()
	cfg := model.DeviceConfig{
		Device: model.Device{ID: "shelf_01", Name: "货架01", Type: "mcu_proxy"},
	}
	reg.Register(cfg)
	reg.Heartbeat("shelf_01")
	d, ok := reg.GetDevice("shelf_01")
	if !ok {
		t.Fatal("expected device to exist")
	}
	if time.Since(d.LastSeen) > time.Second {
		t.Error("heartbeat should update LastSeen to recent time")
	}
}

func TestDeviceRegistry_RemoveStale(t *testing.T) {
	reg := NewDeviceRegistry()
	cfg := model.DeviceConfig{
		Device: model.Device{ID: "shelf_01", Name: "货架01", Type: "mcu_proxy"},
	}
	reg.Register(cfg)
	d, _ := reg.GetDevice("shelf_01")
	d.LastSeen = time.Now().Add(-60 * time.Second)
	removed := reg.RemoveStale(30 * time.Second)
	if len(removed) != 0 {
		t.Errorf("static device should not be removed by heartbeat, got %v", removed)
	}
	_, ok := reg.GetDevice("shelf_01")
	if !ok {
		t.Error("static device should remain registered")
	}
}

func TestDeviceRegistry_RemoveStaleDynamic(t *testing.T) {
	reg := NewDeviceRegistry()
	reg.mu.Lock()
	reg.devices["dyn_01"] = &RegisteredDevice{
		Config:   model.DeviceConfig{Device: model.Device{ID: "dyn_01"}},
		LastSeen: time.Now().Add(-60 * time.Second),
	}
	reg.mu.Unlock()

	removed := reg.RemoveStale(30 * time.Second)
	if len(removed) != 1 || removed[0] != "dyn_01" {
		t.Errorf("expected dyn_01 removed, got %v", removed)
	}
}
