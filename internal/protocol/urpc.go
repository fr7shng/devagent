package protocol

import (
	"fmt"
)

const (
	HeaderRequest = 0xAA
	HeaderAck     = 0xBB

	StatusOK        = 0x00
	StatusBusy      = 0x01
	StatusInvalidCmd = 0x02
	StatusError     = 0xFF

	MaxPayloadSize = 64
)

type URPCRequest struct {
	Seq     byte
	Cmd     byte
	Payload []byte
}

type URPCAck struct {
	Seq        byte
	Status     byte
	QueueDepth byte
}

func crc8(data []byte) byte {
	var crc byte = 0x00
	for _, b := range data {
		crc ^= b
		for i := 0; i < 8; i++ {
			if crc&0x80 != 0 {
				crc = (crc << 1) ^ 0x07
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}

func EncodeRequest(req *URPCRequest) ([]byte, error) {
	if len(req.Payload) > MaxPayloadSize {
		return nil, fmt.Errorf("payload too large: %d > %d", len(req.Payload), MaxPayloadSize)
	}
	frame := []byte{
		HeaderRequest,
		req.Seq,
		req.Cmd,
		byte(len(req.Payload)),
	}
	frame = append(frame, req.Payload...)
	frame = append(frame, crc8(frame[1:]))
	return frame, nil
}

func (ack *URPCAck) Encode() []byte {
	data := []byte{HeaderAck, ack.Seq, ack.Status, ack.QueueDepth}
	data = append(data, crc8(data[1:]))
	return data
}

func DecodeAck(frame []byte) (*URPCAck, error) {
	if len(frame) < 5 {
		return nil, fmt.Errorf("ack frame too short: %d", len(frame))
	}
	if frame[0] != HeaderAck {
		return nil, fmt.Errorf("invalid ack header: 0x%02X", frame[0])
	}
	expectedCRC := crc8(frame[1 : len(frame)-1])
	if frame[len(frame)-1] != expectedCRC {
		return nil, fmt.Errorf("CRC mismatch: expected 0x%02X, got 0x%02X", expectedCRC, frame[len(frame)-1])
	}
	return &URPCAck{
		Seq:        frame[1],
		Status:     frame[2],
		QueueDepth: frame[3],
	}, nil
}

func StatusString(status byte) string {
	switch status {
	case StatusOK:
		return "OK"
	case StatusBusy:
		return "BUSY"
	case StatusInvalidCmd:
		return "INVALID_CMD"
	case StatusError:
		return "ERROR"
	default:
		return fmt.Sprintf("UNKNOWN(0x%02X)", status)
	}
}
