package daemon

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/ng/devagent/internal/protocol"
)

const maxDCPRetries = 3

// DCPBridge 通过串口承载 DCP 协议，负责帧收发、HMAC 签名/验签与超时重试。
type DCPBridge struct {
	serialPort
	hmacSecret []byte
	retry      int
}

func NewDCPBridge() *DCPBridge {
	return &DCPBridge{retry: maxDCPRetries}
}

// SetRetry 覆盖默认重试次数（<=0 时回落到默认值）。
func (db *DCPBridge) SetRetry(n int) {
	if n <= 0 {
		n = maxDCPRetries
	}
	db.retry = n
}

func (db *DCPBridge) SetHMACSecret(secret []byte) {
	db.hmacSecret = secret
}

func (db *DCPBridge) SendDCP(ctx context.Context, seq byte, intentID uint16, params map[string]any) (*protocol.DCPFrame, error) {
	db.mu.Lock()
	defer db.mu.Unlock()

	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
	}

	for attempt := 0; attempt < db.retry; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("context cancelled: %w", err)
		}

		frame, err := protocol.EncodeDCPCall(seq, intentID, params)
		if err != nil {
			return nil, fmt.Errorf("encode DCP call: %w", err)
		}

		if db.hmacSecret != nil {
			frame = protocol.AppendDCPHMAC(frame, db.hmacSecret)
		}

		if _, err := db.port.Write(frame); err != nil {
			return nil, fmt.Errorf("write serial: %w", err)
		}

		reply, err := db.readDCPReply()
		if err != nil {
			slog.Warn("DCP reply 读取失败，重试", "attempt", attempt+1, "error", err)
			continue
		}

		// 已启用 HMAC 时，强制要求回复携带合法 HMAC，否则不信任该帧（重试）。
		if db.hmacSecret != nil {
			if len(reply.Frame.HMAC) == 0 || !protocol.VerifyDCPHMAC(reply.RawFrame, db.hmacSecret) {
				slog.Warn("DCP reply HMAC 校验失败，重试", "intent_id", intentID, "attempt", attempt+1)
				continue
			}
		}

		return reply.Frame, nil
	}

	return nil, fmt.Errorf("%w: DCP failed after %d retries", ErrDeviceTimeout, db.retry)
}

type dcpReadResult struct {
	Frame    *protocol.DCPFrame
	RawFrame []byte
}

// readDCPReply 读取一帧 DCP 回复。仅当启用 HMAC 时才读取并带上 HMAC 尾，
// 避免无 HMAC 的设备回复导致读入下一帧字节造成流错位。
func (db *DCPBridge) readDCPReply() (*dcpReadResult, error) {
	headerBuf := make([]byte, protocol.DCPHeaderSize)
	if _, err := io.ReadFull(db.port, headerBuf); err != nil {
		return nil, fmt.Errorf("read DCP header: %w", err)
	}

	payloadLen := int(headerBuf[5])
	readLen := payloadLen
	if db.hmacSecret != nil {
		readLen += protocol.DCPHMACSize
	}

	payloadBuf := make([]byte, readLen)
	if _, err := io.ReadFull(db.port, payloadBuf); err != nil {
		return nil, fmt.Errorf("read DCP payload: %w", err)
	}

	rawFrame := append(headerBuf, payloadBuf...)

	reply, err := protocol.DecodeDCPFrame(rawFrame)
	if err != nil {
		return nil, fmt.Errorf("decode DCP reply: %w", err)
	}

	return &dcpReadResult{Frame: reply, RawFrame: rawFrame}, nil
}

func (db *DCPBridge) SendURPC(ctx context.Context, req *protocol.URPCRequest) (*protocol.URPCAck, error) {
	return nil, fmt.Errorf("DCPBridge does not support uRPC transport")
}

func (db *DCPBridge) Transport() TransportType {
	return TransportDCP
}
