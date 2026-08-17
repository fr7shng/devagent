package daemon

import (
	"encoding/json"
	"os"
	"sync"
	"time"

	"github.com/ng/devagent/internal/model"
)

type RegisteredDevice struct {
	Config   model.DeviceConfig
	LastSeen time.Time
}

type DeviceRegistry struct {
	devices    map[string]*RegisteredDevice
	mu         sync.RWMutex
	statePath  string
}

func NewDeviceRegistry() *DeviceRegistry {
	return &DeviceRegistry{
		devices: make(map[string]*RegisteredDevice),
	}
}

func (reg *DeviceRegistry) SetStatePath(path string) {
	reg.statePath = path
}

func (reg *DeviceRegistry) Register(cfg model.DeviceConfig) {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	reg.devices[cfg.Device.ID] = &RegisteredDevice{
		Config:   cfg,
		LastSeen: time.Now(),
	}
}

func (reg *DeviceRegistry) GetDevice(id string) (*RegisteredDevice, bool) {
	reg.mu.RLock()
	defer reg.mu.RUnlock()
	d, ok := reg.devices[id]
	if !ok {
		return nil, false
	}
	return d, true
}

func (reg *DeviceRegistry) Heartbeat(id string) {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if d, ok := reg.devices[id]; ok {
		d.LastSeen = time.Now()
	}
}

func (reg *DeviceRegistry) ListDevices() []RegisteredDevice {
	reg.mu.RLock()
	defer reg.mu.RUnlock()
	var result []RegisteredDevice
	for _, d := range reg.devices {
		result = append(result, *d)
	}
	return result
}

func (reg *DeviceRegistry) RemoveStale(timeout time.Duration) []string {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	var removed []string
	now := time.Now()
	for id, d := range reg.devices {
		if now.Sub(d.LastSeen) > timeout {
			delete(reg.devices, id)
			removed = append(removed, id)
		}
	}
	return removed
}

func (reg *DeviceRegistry) Snapshot() error {
	if reg.statePath == "" {
		return nil
	}
	reg.mu.RLock()
	defer reg.mu.RUnlock()
	cfgs := make([]model.DeviceConfig, 0, len(reg.devices))
	for _, d := range reg.devices {
		cfgs = append(cfgs, d.Config)
	}
	data, err := json.Marshal(cfgs)
	if err != nil {
		return err
	}
	return os.WriteFile(reg.statePath, data, 0644)
}

func (reg *DeviceRegistry) Restore() error {
	if reg.statePath == "" {
		return nil
	}
	data, err := os.ReadFile(reg.statePath)
	if err != nil {
		return err
	}
	var cfgs []model.DeviceConfig
	if err := json.Unmarshal(data, &cfgs); err != nil {
		return err
	}
	reg.mu.Lock()
	defer reg.mu.Unlock()
	for _, cfg := range cfgs {
		reg.devices[cfg.Device.ID] = &RegisteredDevice{
			Config:   cfg,
			LastSeen: time.Now(),
		}
	}
	return nil
}
