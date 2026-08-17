package sidecar

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ng/devagent/internal/model"
	"github.com/ng/devagent/internal/protocol"
)

func TestForwardInvoke(t *testing.T) {
	var gotMsg protocol.SSEMessage
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/invoke" {
			t.Errorf("expected /invoke, got %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotMsg); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(&protocol.SSEMessage{
			Type: "invoke_result", Status: "ok", DeviceID: gotMsg.DeviceID, Capability: gotMsg.Capability,
		})
	}))
	defer mock.Close()

	rt := model.NewRouteTable()
	rt.Register(&model.GatewayMeta{ID: "shelf_01", URL: mock.URL, Devices: []string{"shelf_01"}})
	router := NewRouter(rt, "", 5*time.Minute, nil)

	result, err := router.ForwardInvoke(context.Background(), "shelf_01", "set_relay", map[string]any{"pin": 1, "state": true}, "req_1", "job_1")
	if err != nil {
		t.Fatalf("ForwardInvoke failed: %v", err)
	}
	if result.Status != "ok" {
		t.Errorf("expected status ok, got %s", result.Status)
	}
	if gotMsg.DeviceID != "shelf_01" || gotMsg.Capability != "set_relay" || gotMsg.RequestID != "req_1" || gotMsg.JobID != "job_1" {
		t.Errorf("unexpected invoke message: %+v", gotMsg)
	}
}

func TestForwardInvokeRouteNotFound(t *testing.T) {
	rt := model.NewRouteTable()
	router := NewRouter(rt, "", 5*time.Minute, nil)

	_, err := router.ForwardInvoke(context.Background(), "ghost_01", "set_relay", nil, "req_1", "job_1")
	if err == nil {
		t.Fatal("expected route not found error")
	}
}
