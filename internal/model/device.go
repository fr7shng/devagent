package model

import (
	"sync"
	"time"
)

type Device struct {
	ID           string       `json:"device_id" yaml:"id"`
	Name         string       `json:"device_name" yaml:"name"`
	Type         string       `json:"device_type" yaml:"type"`
	Capabilities []Capability `json:"capabilities" yaml:"capabilities"`
}

type GatewayMeta struct {
	ID            string   `json:"gateway_id"`
	URL           string   `json:"gateway_url"`
	Devices       []string `json:"devices"`
	LastHeartbeat int64    `json:"timestamp"`
	Status        string   `json:"status"`
}

const (
	GatewayOnline     = "online"
	GatewayMaintenance = "maintenance"
	GatewayOffline    = "offline"
)

type RouteTable struct {
	devices map[string]string
	gwMeta  map[string]*GatewayMeta
	mu      sync.RWMutex
	onStatusChange func(gwID string, oldStatus, newStatus string)
}

func NewRouteTable() *RouteTable {
	return &RouteTable{
		devices: make(map[string]string),
		gwMeta:  make(map[string]*GatewayMeta),
	}
}

func (rt *RouteTable) OnStatusChange(fn func(gwID string, oldStatus, newStatus string)) {
	rt.onStatusChange = fn
}

func (rt *RouteTable) Register(gw *GatewayMeta) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	gw.Status = GatewayOnline
	rt.gwMeta[gw.ID] = gw
	for _, d := range gw.Devices {
		rt.devices[d] = gw.URL
	}
}

func (rt *RouteTable) Unregister(gwID string) []string {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	gw, ok := rt.gwMeta[gwID]
	if !ok {
		return nil
	}
	var removed []string
	for _, d := range gw.Devices {
		delete(rt.devices, d)
		removed = append(removed, d)
	}
	delete(rt.gwMeta, gwID)
	return removed
}

func (rt *RouteTable) Lookup(deviceID string) (string, bool) {
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	url, ok := rt.devices[deviceID]
	return url, ok
}

func (rt *RouteTable) AllDevices() []map[string]any {
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	var result []map[string]any
	for _, gw := range rt.gwMeta {
		for _, d := range gw.Devices {
			result = append(result, map[string]any{
				"device_id":   d,
				"gateway_id":  gw.ID,
				"gateway_url": gw.URL,
				"status":      gw.Status,
			})
		}
	}
	return result
}

func (rt *RouteTable) UpdateHeartbeat(gwID string) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if gw, ok := rt.gwMeta[gwID]; ok {
		old := gw.Status
		gw.LastHeartbeat = time.Now().Unix()
		gw.Status = GatewayOnline
		if old != GatewayOnline && rt.onStatusChange != nil {
			rt.onStatusChange(gwID, old, GatewayOnline)
		}
	}
}

func (rt *RouteTable) SetGatewayStatus(gwID, newStatus string) bool {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	gw, ok := rt.gwMeta[gwID]
	if !ok {
		return false
	}
	old := gw.Status
	if old == newStatus {
		return true
	}
	gw.Status = newStatus
	if rt.onStatusChange != nil {
		rt.onStatusChange(gwID, old, newStatus)
	}
	return true
}

func (rt *RouteTable) StaleGateways(maintenanceTimeout, offlineTimeout time.Duration) (maintenance []string, offline []string) {
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	now := time.Now().Unix()
	for id, gw := range rt.gwMeta {
		elapsed := now - gw.LastHeartbeat
		if elapsed > int64(offlineTimeout.Seconds()) {
			offline = append(offline, id)
		} else if elapsed > int64(maintenanceTimeout.Seconds()) {
			maintenance = append(maintenance, id)
		}
	}
	return
}
