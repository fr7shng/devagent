package protocol

import (
	"testing"
)

func TestEncodeRequest(t *testing.T) {
	req := &URPCRequest{
		Seq:     1,
		Cmd:     0xA1,
		Payload: []byte{0x01, 0x01},
	}
	frame, err := EncodeRequest(req)
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}
	if frame[0] != 0xAA {
		t.Errorf("expected header 0xAA, got 0x%02X", frame[0])
	}
	if frame[1] != 1 {
		t.Errorf("expected seq 1, got %d", frame[1])
	}
	if frame[2] != 0xA1 {
		t.Errorf("expected cmd 0xA1, got 0x%02X", frame[2])
	}
}

func TestDecodeAck(t *testing.T) {
	ack := &URPCAck{Seq: 1, Status: 0x00, QueueDepth: 2}
	frame := ack.Encode()

	decoded, err := DecodeAck(frame)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if decoded.Seq != 1 {
		t.Errorf("expected seq 1, got %d", decoded.Seq)
	}
	if decoded.Status != StatusOK {
		t.Errorf("expected status OK, got %d", decoded.Status)
	}
	if decoded.QueueDepth != 2 {
		t.Errorf("expected queue_depth 2, got %d", decoded.QueueDepth)
	}
}

func TestCRC8(t *testing.T) {
	data := []byte{0x01, 0xA1, 0x02, 0x01, 0x01}
	crc := crc8(data)
	if crc == 0 {
		t.Error("CRC should not be zero for non-trivial data")
	}
}

func TestEncodeRequestPayloadTooLarge(t *testing.T) {
	req := &URPCRequest{Seq: 1, Cmd: 0xA1, Payload: make([]byte, MaxPayloadSize+1)}
	_, err := EncodeRequest(req)
	if err == nil {
		t.Error("expected error for oversized payload")
	}
}

func TestDecodeAckTooShort(t *testing.T) {
	_, err := DecodeAck([]byte{HeaderAck, 0x01})
	if err == nil {
		t.Error("expected error for too-short frame")
	}
}

func TestDecodeAckBadHeader(t *testing.T) {
	_, err := DecodeAck([]byte{HeaderRequest, 0x01, 0x00, 0x00, 0x00})
	if err == nil {
		t.Error("expected error for invalid ack header")
	}
}

func TestDecodeAckCRCError(t *testing.T) {
	frame := (&URPCAck{Seq: 1, Status: StatusOK, QueueDepth: 0}).Encode()
	frame[len(frame)-1] ^= 0xFF
	_, err := DecodeAck(frame)
	if err == nil {
		t.Error("expected CRC mismatch error")
	}
}
