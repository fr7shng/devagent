package sidecar

import (
	"testing"
	"time"

	"github.com/ng/devagent/internal/model"
)

func TestNewSidecarServer(t *testing.T) {
	rt := model.NewRouteTable()
	srv := NewSidecarServer(rt, NewDedupWindow(), NewProgressTracker(), NewRouter(rt, "", 5*time.Minute, nil), "")
	if srv == nil {
		t.Fatal("expected non-nil server")
	}
}

func TestSystemListDevices(t *testing.T) {
	rt := model.NewRouteTable()
	rt.Register(&model.GatewayMeta{
		ID:      "gw_1",
		URL:     "http://192.168.1.50:8080",
		Devices: []string{"shelf_01"},
	})
	srv := NewSidecarServer(rt, NewDedupWindow(), NewProgressTracker(), NewRouter(rt, "", 5*time.Minute, nil), "")
	devices := srv.ListDevices()
	if len(devices) != 1 {
		t.Fatalf("expected 1 device, got %d", len(devices))
	}
	if devices[0]["device_id"] != "shelf_01" {
		t.Errorf("expected device_id 'shelf_01', got %v", devices[0]["device_id"])
	}
}
