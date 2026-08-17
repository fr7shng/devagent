package daemon

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTempDevice(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "device.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write temp device: %v", err)
	}
	return path
}

func TestLoadConfigDCPSetHMACSecret(t *testing.T) {
	path := writeTempDevice(t, `
device:
  id: "shelf_01"
  name: "货架"
  type: "mcu_proxy"
capabilities:
  - name: set_relay
    description: "control relay"
    implementation:
      proxy: "uart"
      channel: "/dev/ttyUSB0"
      baudrate: 115200
      protocol: "DCP"
      hmac_secret: "device-hmac-secret"
      cmd_map: {}
`)
	ds := NewDaemonServer("gw_test", 0, "", nil)
	if err := ds.loadConfig(path); err != nil {
		t.Fatalf("loadConfig failed: %v", err)
	}
	dcp, ok := ds.hal.(*DCPBridge)
	if !ok {
		t.Fatalf("expected *DCPBridge, got %T", ds.hal)
	}
	if len(dcp.hmacSecret) == 0 {
		t.Error("expected DCP HMAC secret to be set")
	}
}

func TestLoadConfigDCPNoHMACSecret(t *testing.T) {
	path := writeTempDevice(t, `
device:
  id: "shelf_01"
  name: "货架"
  type: "mcu_proxy"
capabilities:
  - name: set_relay
    description: "control relay"
    implementation:
      proxy: "uart"
      channel: "/dev/ttyUSB0"
      baudrate: 115200
      protocol: "DCP"
      cmd_map: {}
`)
	ds := NewDaemonServer("gw_test", 0, "", nil)
	if err := ds.loadConfig(path); err != nil {
		t.Fatalf("loadConfig failed: %v", err)
	}
	dcp, ok := ds.hal.(*DCPBridge)
	if !ok {
		t.Fatalf("expected *DCPBridge, got %T", ds.hal)
	}
	if len(dcp.hmacSecret) != 0 {
		t.Error("expected no DCP HMAC secret when hmac_secret unset")
	}
}
