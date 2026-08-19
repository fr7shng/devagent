package daemon

import (
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	goserial "go.bug.st/serial"
)

// ErrDeviceTimeout 表示设备在重试次数耗尽后仍无 ACK/回复（spec 的 TIMEOUT 语义）。
var ErrDeviceTimeout = errors.New("device timeout")

const serialReadTimeout = 500 * time.Millisecond

// serialPort 封装 go.bug.st/serial 通用的打开/关闭/重连/连接状态逻辑，
// 供 SerialBridge 与 DCPBridge 共用，消除重复的串口脚手架。
type serialPort struct {
	port      goserial.Port
	channel   string
	baudrate  int
	mu        sync.Mutex
	connected bool
}

func (sp *serialPort) Open(channel string, baudrate int) error {
	mode := &goserial.Mode{BaudRate: baudrate}
	p, err := goserial.Open(channel, mode)
	if err != nil {
		return fmt.Errorf("open serial %s: %w", channel, err)
	}
	if err := p.SetReadTimeout(serialReadTimeout); err != nil {
		return fmt.Errorf("set read timeout: %w", err)
	}
	sp.port = p
	sp.channel = channel
	sp.baudrate = baudrate
	sp.connected = true
	return nil
}

func (sp *serialPort) Close() error {
	sp.mu.Lock()
	defer sp.mu.Unlock()
	sp.connected = false
	if sp.port != nil {
		return sp.port.Close()
	}
	return nil
}

func (sp *serialPort) IsConnected() bool {
	return sp.connected
}

func (sp *serialPort) Reconnect() error {
	sp.mu.Lock()
	defer sp.mu.Unlock()
	if sp.port != nil {
		sp.port.Close()
	}
	if sp.channel == "" {
		sp.connected = false
		return fmt.Errorf("reconnect serial: no channel configured")
	}
	mode := &goserial.Mode{BaudRate: sp.baudrate}
	p, err := goserial.Open(sp.channel, mode)
	if err != nil {
		sp.connected = false
		return fmt.Errorf("reconnect serial %s: %w", sp.channel, err)
	}
	if err := p.SetReadTimeout(serialReadTimeout); err != nil {
		sp.connected = false
		return fmt.Errorf("set read timeout on reconnect: %w", err)
	}
	sp.port = p
	sp.connected = true
	slog.Info("串口重连成功", "channel", sp.channel, "baudrate", sp.baudrate)
	return nil
}
