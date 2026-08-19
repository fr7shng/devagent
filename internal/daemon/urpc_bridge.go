package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/ng/devagent/internal/protocol"
)

const maxURPCRetries = 3

// SerialBridge 通过串口承载 uRPC 协议，负责帧收发与超时重试。
type SerialBridge struct {
	serialPort
	retry int
}

func NewSerialBridge() *SerialBridge {
	return &SerialBridge{retry: maxURPCRetries}
}

// SetRetry 覆盖默认重试次数（<=0 时回落到默认值）。
func (sb *SerialBridge) SetRetry(n int) {
	if n <= 0 {
		n = maxURPCRetries
	}
	sb.retry = n
}

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

	for attempt := 0; attempt < sb.retry; attempt++ {
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

	return nil, fmt.Errorf("%w: uRPC failed after %d retries", ErrDeviceTimeout, sb.retry)
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

func (sb *SerialBridge) SendDCP(ctx context.Context, seq byte, intentID uint16, params map[string]any) (*protocol.DCPFrame, error) {
	return nil, fmt.Errorf("SerialBridge does not support DCP transport")
}

func (sb *SerialBridge) Transport() TransportType {
	return TransportURP
}
