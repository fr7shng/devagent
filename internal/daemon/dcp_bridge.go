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

type DCPBridge struct {
	port       goserial.Port
	channel    string
	baudrate   int
	hmacSecret []byte
	mu         sync.Mutex
	connected  bool
}

func NewDCPBridge() *DCPBridge {
	return &DCPBridge{}
}

func (db *DCPBridge) SetHMACSecret(secret []byte) {
	db.hmacSecret = secret
}

func (db *DCPBridge) Open(channel string, baudrate int) error {
	mode := &goserial.Mode{
		BaudRate: baudrate,
	}
	p, err := goserial.Open(channel, mode)
	if err != nil {
		return fmt.Errorf("open serial %s: %w", channel, err)
	}
	if err := p.SetReadTimeout(500 * time.Millisecond); err != nil {
		return fmt.Errorf("set read timeout: %w", err)
	}
	db.port = p
	db.channel = channel
	db.baudrate = baudrate
	db.connected = true
	return nil
}

const maxDCPRetries = 3

func (db *DCPBridge) SendDCP(ctx context.Context, seq byte, intentID uint16, params map[string]any) (*protocol.DCPFrame, error) {
	db.mu.Lock()
	defer db.mu.Unlock()

	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
	}

	for attempt := 0; attempt < maxDCPRetries; attempt++ {
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

		if len(reply.Frame.HMAC) > 0 && db.hmacSecret != nil {
			if !protocol.VerifyDCPHMAC(reply.RawFrame, db.hmacSecret) {
				slog.Warn("DCP reply HMAC 校验失败", "intent_id", intentID)
			}
		}

		return reply.Frame, nil
	}

	return nil, fmt.Errorf("DCP failed after %d retries", maxDCPRetries)
}

type dcpReadResult struct {
	Frame     *protocol.DCPFrame
	RawFrame  []byte
}

func (db *DCPBridge) readDCPReply() (*dcpReadResult, error) {
	headerBuf := make([]byte, protocol.DCPHeaderSize)
	hn, err := db.port.Read(headerBuf)
	if err != nil {
		return nil, fmt.Errorf("read DCP header: %w", err)
	}
	if hn < protocol.DCPHeaderSize {
		return nil, fmt.Errorf("DCP header incomplete: %d < %d", hn, protocol.DCPHeaderSize)
	}

	payloadLen := int(headerBuf[5])
	totalLen := protocol.DCPHeaderSize + payloadLen

	payloadBuf := make([]byte, payloadLen+protocol.DCPHMACSize)
	pn, err := db.port.Read(payloadBuf)
	if err != nil {
		return nil, fmt.Errorf("read DCP payload: %w", err)
	}

	rawFrame := make([]byte, 0, totalLen+protocol.DCPHMACSize)
	rawFrame = append(rawFrame, headerBuf[:hn]...)
	rawFrame = append(rawFrame, payloadBuf[:pn]...)

	reply, err := protocol.DecodeDCPFrame(rawFrame)
	if err != nil {
		return nil, fmt.Errorf("decode DCP reply: %w", err)
	}

	return &dcpReadResult{Frame: reply, RawFrame: rawFrame}, nil
}

func (db *DCPBridge) Close() error {
	db.connected = false
	if db.port != nil {
		return db.port.Close()
	}
	return nil
}

func (db *DCPBridge) SendURPC(ctx context.Context, req *protocol.URPCRequest) (*protocol.URPCAck, error) {
	return nil, fmt.Errorf("DCPBridge does not support uRPC transport")
}

func (db *DCPBridge) Transport() TransportType {
	return TransportDCP
}

func (db *DCPBridge) IsConnected() bool {
	return db.connected
}

func (db *DCPBridge) Reconnect() error {
	db.mu.Lock()
	defer db.mu.Unlock()
	if db.port != nil {
		db.port.Close()
	}
	mode := &goserial.Mode{BaudRate: db.baudrate}
	p, err := goserial.Open(db.channel, mode)
	if err != nil {
		db.connected = false
		return fmt.Errorf("reconnect serial %s: %w", db.channel, err)
	}
	if err := p.SetReadTimeout(500 * time.Millisecond); err != nil {
		db.connected = false
		return fmt.Errorf("set read timeout on reconnect: %w", err)
	}
	db.port = p
	db.connected = true
	slog.Info("DCP 串口重连成功", "channel", db.channel, "baudrate", db.baudrate)
	return nil
}
