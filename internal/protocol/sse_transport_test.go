package protocol

import (
	"encoding/json"
	"testing"
)

func TestSSEMessage_Marshal(t *testing.T) {
	msg := SSEMessage{
		Type:      "register",
		DeviceID:  "shelf_01",
		Timestamp: 1718600000,
	}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	var decoded SSEMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if decoded.Type != "register" {
		t.Errorf("expected type 'register', got '%s'", decoded.Type)
	}
	if decoded.DeviceID != "shelf_01" {
		t.Errorf("expected device_id 'shelf_01', got '%s'", decoded.DeviceID)
	}
}

func TestSSEMessage_Invoke(t *testing.T) {
	msg := SSEMessage{
		Type:       "invoke",
		RequestID:  "req_001",
		DeviceID:   "shelf_01",
		Capability: "set_relay",
		Params:     map[string]any{"pin": 1, "state": true},
		JobID:      "job_123",
		Timestamp:  1718600030,
	}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal invoke failed: %v", err)
	}
	var decoded SSEMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal invoke failed: %v", err)
	}
	if decoded.DeviceID != "shelf_01" {
		t.Errorf("expected device_id 'shelf_01', got '%s'", decoded.DeviceID)
	}
}

func TestSSEMessage_InvokeResponse(t *testing.T) {
	msg := SSEMessage{
		Type:       "invoke_response",
		RequestID:  "req_001",
		DeviceID:   "shelf_01",
		Capability: "set_relay",
		Status:     "ok",
		Result:     map[string]any{"queue_depth": 0},
		Timestamp:  1718600060,
	}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal response failed: %v", err)
	}
	var decoded SSEMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal response failed: %v", err)
	}
	if decoded.Type != "invoke_response" {
		t.Errorf("expected type 'invoke_response', got '%s'", decoded.Type)
	}
	if decoded.Status != "ok" {
		t.Errorf("expected status 'ok', got '%s'", decoded.Status)
	}
}
