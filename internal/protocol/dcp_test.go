package protocol

import (
	"testing"
)

func TestIntentID(t *testing.T) {
	id1 := IntentID("set_brightness")
	id2 := IntentID("set_brightness")
	id3 := IntentID("read_temp")
	if id1 != id2 {
		t.Error("same name should produce same intent_id")
	}
	if id1 == id3 {
		t.Error("different names should produce different intent_id")
	}
}

func TestEncodeDCPCall(t *testing.T) {
	params := map[string]any{"level": float64(50)}
	frame, err := EncodeDCPCall(1, IntentID("set_brightness"), params)
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}
	if frame[0] != DCPVersion {
		t.Errorf("expected version %d, got %d", DCPVersion, frame[0])
	}
	if frame[1] != DCPCall {
		t.Errorf("expected kind DCPCall, got 0x%02X", frame[1])
	}
	if frame[2] != 1 {
		t.Errorf("expected seq 1, got %d", frame[2])
	}
}

func TestDecodeDCPFrame(t *testing.T) {
	params := map[string]any{"level": float64(50)}
	intentID := IntentID("set_brightness")
	encoded, err := EncodeDCPCall(1, intentID, params)
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}

	decoded, err := DecodeDCPFrame(encoded)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if decoded.Kind != DCPCall {
		t.Errorf("expected kind DCPCall, got 0x%02X", decoded.Kind)
	}
	if decoded.IntentID != intentID {
		t.Errorf("expected intent_id %d, got %d", intentID, decoded.IntentID)
	}
	if decoded.Payload["level"] != float64(50) {
		t.Errorf("expected level=50, got %v", decoded.Payload["level"])
	}
}

func TestEncodeDCPDryRun(t *testing.T) {
	params := map[string]any{"level": float64(50)}
	frame, err := EncodeDCPDryRun(1, IntentID("set_brightness"), params)
	if err != nil {
		t.Fatalf("encode dry-run failed: %v", err)
	}
	if frame[1] != DCPDryRun {
		t.Errorf("expected kind DCPDryRun (0x81), got 0x%02X", frame[1])
	}
}

func TestEncodeDCPReply(t *testing.T) {
	result := map[string]any{"actual_level": float64(50)}
	frame, err := EncodeDCPReply(1, IntentID("set_brightness"), DCPReplyOK, result)
	if err != nil {
		t.Fatalf("encode reply failed: %v", err)
	}
	if frame[1] != DCPReply {
		t.Errorf("expected kind DCPReply, got 0x%02X", frame[1])
	}
}
