package model

import (
	"testing"
	"time"
)

func TestRouteTable_RegisterAndLookup(t *testing.T) {
	rt := NewRouteTable()
	rt.Register(&GatewayMeta{ID: "gw_1", URL: "http://gw1:8080", Devices: []string{"shelf_01", "shelf_02"}})

	url, ok := rt.Lookup("shelf_01")
	if !ok || url != "http://gw1:8080" {
		t.Errorf("expected lookup shelf_01 → gw1 url, got %q ok=%v", url, ok)
	}
	if _, ok := rt.Lookup("ghost_01"); ok {
		t.Error("unknown device should not resolve")
	}
}

func TestRouteTable_AddDevice(t *testing.T) {
	rt := NewRouteTable()
	rt.Register(&GatewayMeta{ID: "gw_1", URL: "http://gw1:8080"})
	rt.AddDevice("gw_1", "shelf_01")

	url, ok := rt.Lookup("shelf_01")
	if !ok || url != "http://gw1:8080" {
		t.Errorf("expected lookup after AddDevice, got %q ok=%v", url, ok)
	}
}

func TestRouteTable_AddDeviceWithoutGateway(t *testing.T) {
	rt := NewRouteTable()
	rt.AddDevice("gw_1", "shelf_01")

	_, ok := rt.Lookup("shelf_01")
	if !ok {
		t.Error("AddDevice should create the gateway implicitly")
	}
}

func TestRouteTable_Unregister(t *testing.T) {
	rt := NewRouteTable()
	rt.Register(&GatewayMeta{ID: "gw_1", URL: "http://gw1:8080", Devices: []string{"shelf_01", "shelf_02"}})

	removed := rt.Unregister("gw_1")
	if len(removed) != 2 {
		t.Fatalf("expected 2 devices removed, got %v", removed)
	}
	if _, ok := rt.Lookup("shelf_01"); ok {
		t.Error("device should be gone after gateway unregister")
	}
	all := rt.AllDevices()
	if len(all) != 0 {
		t.Errorf("expected no devices after unregister, got %d", len(all))
	}
}

func TestRouteTable_RegisterMergeDevices(t *testing.T) {
	rt := NewRouteTable()
	rt.Register(&GatewayMeta{ID: "gw_1", URL: "http://gw1:8080", Devices: []string{"shelf_01"}})
	rt.Register(&GatewayMeta{ID: "gw_1", URL: "http://gw1:8080", Devices: []string{"shelf_02"}})

	all := rt.AllDevices()
	if len(all) != 2 {
		t.Errorf("expected merged 2 devices, got %d", len(all))
	}
}

func TestRouteTable_AllDevicesStatus(t *testing.T) {
	rt := NewRouteTable()
	rt.Register(&GatewayMeta{ID: "gw_1", URL: "http://gw1:8080", Devices: []string{"shelf_01"}})
	rt.SetGatewayStatus("gw_1", GatewayMaintenance)

	all := rt.AllDevices()
	if len(all) != 1 || all[0]["status"] != GatewayMaintenance {
		t.Errorf("expected maintenance status, got %+v", all)
	}
}

func TestRouteTable_RegisterDoesNotRefreshHeartbeat(t *testing.T) {
	rt := NewRouteTable()
	rt.Register(&GatewayMeta{ID: "gw_1", URL: "http://gw1:8080", Devices: []string{"shelf_01"}})

	rt.mu.Lock()
	rt.gateways["gw_1"].LastHeartbeat = time.Now().Add(-2 * time.Minute).Unix()
	rt.mu.Unlock()

	// 重复发现不应刷新心跳
	rt.Register(&GatewayMeta{ID: "gw_1", URL: "http://gw1:8080"})

	rt.mu.RLock()
	last := rt.gateways["gw_1"].LastHeartbeat
	rt.mu.RUnlock()
	if time.Since(time.Unix(last, 0)) < time.Minute {
		t.Error("re-register should not refresh an existing gateway heartbeat")
	}
}

func TestRouteTable_StaleGateways(t *testing.T) {
	rt := NewRouteTable()
	rt.Register(&GatewayMeta{ID: "gw_alive", URL: "http://a:1"})
	rt.Register(&GatewayMeta{ID: "gw_stale", URL: "http://b:1"})

	gw := rt.gateways["gw_stale"]
	gw.LastHeartbeat = time.Now().Add(-45 * time.Second).Unix()

	maintenance, offline := rt.StaleGateways(30*time.Second, 60*time.Second)
	if len(maintenance) != 1 || maintenance[0] != "gw_stale" {
		t.Errorf("expected gw_stale in maintenance, got %v", maintenance)
	}
	if len(offline) != 0 {
		t.Errorf("expected no offline, got %v", offline)
	}
}

func TestRouteTable_DevicesByGateway(t *testing.T) {
	rt := NewRouteTable()
	rt.Register(&GatewayMeta{ID: "gw_1", URL: "http://gw1:8080", Devices: []string{"shelf_01", "shelf_02"}})

	devs := rt.DevicesByGateway("gw_1")
	if len(devs) != 2 {
		t.Fatalf("expected 2 devices, got %v", devs)
	}
	if len(rt.DevicesByGateway("ghost")) != 0 {
		t.Error("unknown gateway should return no devices")
	}
}

func TestRouteTable_Gateways(t *testing.T) {
	rt := NewRouteTable()
	rt.Register(&GatewayMeta{ID: "gw_1", URL: "http://gw1:8080", Devices: []string{"shelf_01"}})

	gws := rt.Gateways()
	if len(gws) != 1 || gws[0].ID != "gw_1" {
		t.Errorf("expected 1 gateway gw_1, got %+v", gws)
	}
}

func TestDeviceConfig_Sanitized(t *testing.T) {
	cfg := DeviceConfig{
		Device: Device{ID: "shelf_01", Name: "货架", Type: "mcu_proxy"},
		Capabilities: []Capability{
			{Name: "set_relay", Implementation: Implementation{HMACSecret: "s3cret"}},
		},
	}
	san := cfg.Sanitized()
	if san.Capabilities[0].Implementation.HMACSecret != "" {
		t.Error("Sanitized should strip HMAC secret")
	}
	// 原始配置不被污染。
	if cfg.Capabilities[0].Implementation.HMACSecret != "s3cret" {
		t.Error("original config must not be mutated")
	}
}
