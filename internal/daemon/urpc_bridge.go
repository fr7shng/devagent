package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/ng/devagent/internal/protocol"
	goserial "go.bug.st/serial"
)

type SerialBridge struct {
	port       goserial.Port
	channel    string
	baudrate   int
	mu         sync.Mutex
	connected  bool
}

func NewSerialBridge() *SerialBridge {
	return &SerialBridge{}
}

func (sb *SerialBridge) Open(channel string, baudrate int) error {
	mode := &goserial.Mode{
		BaudRate: baudrate,
	}
	p, err := goserial.Open(channel, mode)
	if err != nil {
		return fmt.Errorf("open serial %s: %w", channel, err)
	}
	sb.port = p
	sb.channel = channel
	sb.baudrate = baudrate
	sb.connected = true
	return nil
}

const maxURPCRetries = 3

func (sb *SerialBridge) SendURPC(ctx context.Context, req *protocol.URPCRequest) (*protocol.URPCAck, error) {
	sb.mu.Lock()
	defer sb.mu.Unlock()

	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
	}

	frame, err := protocol.EncodeRequest(req)
	if err != nil {
		return nil, fmt.Errorf("encode request: %w", err)
	}

	for attempt := 0; attempt < maxURPCRetries; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("context cancelled: %w", err)
		}

		if _, err := sb.port.Write(frame); err != nil {
			return nil, fmt.Errorf("write serial: %w", err)
		}

		ack, err := sb.readURPCAck()
		if err != nil {
			slog.Warn("uRPC ACK 读取失败，重试", "attempt", attempt+1, "error", err)
			continue
		}
		return ack, nil
	}

	return nil, fmt.Errorf("uRPC failed after %d retries", maxURPCRetries)
}

func (sb *SerialBridge) readURPCAck() (*protocol.URPCAck, error) {
	var buf []byte
	remaining := 5
	for remaining > 0 {
		chunk := make([]byte, remaining)
		n, err := sb.port.Read(chunk)
		if err != nil {
			return nil, fmt.Errorf("read serial: %w", err)
		}
		buf = append(buf, chunk[:n]...)
		remaining -= n
	}

	headerIdx := -1
	for i, b := range buf {
		if b == protocol.HeaderAck {
			headerIdx = i
			break
		}
	}
	if headerIdx < 0 {
		return nil, fmt.Errorf("uRPC ACK header not found")
	}
	if headerIdx > 0 {
		buf = buf[headerIdx:]
	}

	if len(buf) < 5 {
		chunk := make([]byte, 5-len(buf))
		n, err := sb.port.Read(chunk)
		if err != nil {
			return nil, fmt.Errorf("read serial remainder: %w", err)
		}
		buf = append(buf, chunk[:n]...)
	}

	ack, err := protocol.DecodeAck(buf[:5])
	if err != nil {
		return nil, fmt.Errorf("decode ack: %w", err)
	}
	return ack, nil
}

func (sb *SerialBridge) Close() error {
	sb.connected = false
	if sb.port != nil {
		return sb.port.Close()
	}
	return nil
}

func (sb *SerialBridge) SendDCP(ctx context.Context, seq byte, intentID uint16, params map[string]any) (*protocol.DCPFrame, error) {
	return nil, fmt.Errorf("SerialBridge does not support DCP transport")
}

func (sb *SerialBridge) Transport() TransportType {
	return TransportURP
}

func (sb *SerialBridge) IsConnected() bool {
	return sb.connected
}

func (sb *SerialBridge) Reconnect() error {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	if sb.port != nil {
		sb.port.Close()
	}
	mode := &goserial.Mode{BaudRate: sb.baudrate}
	p, err := goserial.Open(sb.channel, mode)
	if err != nil {
		sb.connected = false
		return fmt.Errorf("reconnect serial %s: %w", sb.channel, err)
	}
	sb.port = p
	sb.connected = true
	slog.Info("串口重连成功", "channel", sb.channel, "baudrate", sb.baudrate)
	return nil
}
