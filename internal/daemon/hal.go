package daemon

import (
	"context"
	"sync"

	"github.com/ng/devagent/internal/protocol"
)

type TransportType string

const (
	TransportURP = TransportType("urpc")
	TransportDCP = TransportType("dcp")
)

type HAL interface {
	Open(channel string, baudrate int) error
	SendURPC(ctx context.Context, req *protocol.URPCRequest) (*protocol.URPCAck, error)
	SendDCP(ctx context.Context, seq byte, intentID uint16, params map[string]any) (*protocol.DCPFrame, error)
	Close() error
	Transport() TransportType
	IsConnected() bool
	Reconnect() error
}

type MockHAL struct {
	mu       sync.Mutex
	OpenFn   func(channel string, baudrate int) error
	SendURFn func(ctx context.Context, req *protocol.URPCRequest) (*protocol.URPCAck, error)
	SendDFn  func(ctx context.Context, seq byte, intentID uint16, params map[string]any) (*protocol.DCPFrame, error)
	CloseFn  func() error
	TType    TransportType
}

func (m *MockHAL) Open(channel string, baudrate int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.OpenFn != nil {
		return m.OpenFn(channel, baudrate)
	}
	return nil
}

func (m *MockHAL) SendURPC(ctx context.Context, req *protocol.URPCRequest) (*protocol.URPCAck, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.SendURFn != nil {
		return m.SendURFn(ctx, req)
	}
	return &protocol.URPCAck{Seq: req.Seq, Status: protocol.StatusOK, QueueDepth: 0}, nil
}

func (m *MockHAL) SendDCP(ctx context.Context, seq byte, intentID uint16, params map[string]any) (*protocol.DCPFrame, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.SendDFn != nil {
		return m.SendDFn(ctx, seq, intentID, params)
	}
	return &protocol.DCPFrame{Ver: protocol.DCPVersion, Kind: protocol.DCPReply, Seq: seq, IntentID: intentID}, nil
}

func (m *MockHAL) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.CloseFn != nil {
		return m.CloseFn()
	}
	return nil
}

func (m *MockHAL) Transport() TransportType {
	if m.TType != "" {
		return m.TType
	}
	return TransportURP
}

func (m *MockHAL) IsConnected() bool {
	return true
}

func (m *MockHAL) Reconnect() error {
	return nil
}
