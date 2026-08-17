package protocol

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"fmt"

	"github.com/fxamacker/cbor/v2"
)

const (
	DCPVersion = 1
	DCPCall    = 0x01
	DCPReply   = 0x02
	DCPEvent   = 0x03
	DCPError   = 0x04
	DCPDryRun  = 0x81

	DCPReplyOK          = "ok"
	DCPReplyDenied      = "denied"
	DCPReplyRange       = "range"
	DCPReplyBusy        = "busy"
	DCPReplyUnknown     = "unknown_intent"
	DCPReplyCapRequired = "capability_required"

	DCPHeaderSize = 6
	DCPHMACSize   = 16
	DCPMaxPayload = 64
)

type DCPFrame struct {
	Ver      byte
	Kind     byte
	Seq      byte
	IntentID uint16
	Payload  map[string]any
	HMAC     []byte
}

func IntentID(name string) uint16 {
	hash := sha256.Sum256([]byte(name))
	return binary.BigEndian.Uint16(hash[:2])
}

func EncodeDCPCall(seq byte, intentID uint16, params map[string]any) ([]byte, error) {
	payload, err := cbor.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("cbor marshal: %w", err)
	}
	if len(payload) > DCPMaxPayload {
		return nil, fmt.Errorf("payload too large: %d > %d", len(payload), DCPMaxPayload)
	}

	frame := make([]byte, DCPHeaderSize+len(payload))
	frame[0] = DCPVersion
	frame[1] = DCPCall
	frame[2] = seq
	binary.BigEndian.PutUint16(frame[3:5], intentID)
	frame[5] = byte(len(payload))
	copy(frame[DCPHeaderSize:], payload)

	return frame, nil
}

func EncodeDCPDryRun(seq byte, intentID uint16, params map[string]any) ([]byte, error) {
	data, err := EncodeDCPCall(seq, intentID, params)
	if err != nil {
		return nil, err
	}
	data[1] = DCPDryRun
	return data, nil
}

func EncodeDCPReply(seq byte, intentID uint16, status string, result map[string]any) ([]byte, error) {
	payload, err := cbor.Marshal(map[string]any{
		"status": status,
		"result": result,
	})
	if err != nil {
		return nil, fmt.Errorf("cbor marshal reply: %w", err)
	}

	frame := make([]byte, DCPHeaderSize+len(payload))
	frame[0] = DCPVersion
	frame[1] = DCPReply
	frame[2] = seq
	binary.BigEndian.PutUint16(frame[3:5], intentID)
	frame[5] = byte(len(payload))
	copy(frame[DCPHeaderSize:], payload)

	return frame, nil
}

func DecodeDCPFrame(data []byte) (*DCPFrame, error) {
	if len(data) < DCPHeaderSize {
		return nil, fmt.Errorf("frame too short: %d < %d", len(data), DCPHeaderSize)
	}
	ver := data[0]
	if ver != DCPVersion {
		return nil, fmt.Errorf("unsupported DCP version: %d", ver)
	}

	kind := data[1]
	seq := data[2]
	intentID := binary.BigEndian.Uint16(data[3:5])
	payloadLen := int(data[5])

	if len(data) < DCPHeaderSize+payloadLen {
		return nil, fmt.Errorf("payload truncated: need %d, have %d", payloadLen, len(data)-DCPHeaderSize)
	}

	var params map[string]any
	if payloadLen > 0 {
		if err := cbor.Unmarshal(data[DCPHeaderSize:DCPHeaderSize+payloadLen], &params); err != nil {
			return nil, fmt.Errorf("cbor unmarshal: %w", err)
		}
	}

	var hmac []byte
	if len(data) > DCPHeaderSize+payloadLen {
		hmac = data[DCPHeaderSize+payloadLen:]
	}

	return &DCPFrame{
		Ver:      ver,
		Kind:     kind,
		Seq:      seq,
		IntentID: intentID,
		Payload:  params,
		HMAC:     hmac,
	}, nil
}

func VerifyDCPHMAC(frameData []byte, secret []byte) bool {
	if len(frameData) < DCPHeaderSize {
		return false
	}
	payloadLen := int(frameData[5])
	frameEnd := DCPHeaderSize + payloadLen
	if len(frameData) <= frameEnd {
		return false
	}
	receivedHMAC := frameData[frameEnd:]
	mac := hmac.New(sha256.New, secret)
	mac.Write(frameData[:frameEnd])
	expectedHMAC := mac.Sum(nil)[:DCPHMACSize]
	return hmac.Equal(receivedHMAC, expectedHMAC)
}

func AppendDCPHMAC(frame []byte, secret []byte) []byte {
	mac := hmac.New(sha256.New, secret)
	mac.Write(frame)
	hmacBytes := mac.Sum(nil)[:DCPHMACSize]
	return append(frame, hmacBytes...)
}
