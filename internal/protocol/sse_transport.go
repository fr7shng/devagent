package protocol

import "encoding/json"

type SSEMessage struct {
	Type       string      `json:"type"`
	GatewayID  string      `json:"gateway_id,omitempty"`
	GatewayURL string      `json:"gateway_url,omitempty"`
	Devices    []SSEDevice `json:"devices,omitempty"`
	DeviceID   string      `json:"device_id,omitempty"`
	DeviceName string      `json:"device_name,omitempty"`
	DeviceType string      `json:"device_type,omitempty"`
	Capability string      `json:"capability,omitempty"`
	RequestID  string      `json:"request_id,omitempty"`
	JobID      string      `json:"job_id,omitempty"`
	Params     any         `json:"params,omitempty"`
	Status     string      `json:"status,omitempty"`
	Progress   int         `json:"progress,omitempty"`
	Message    string      `json:"message,omitempty"`
	Data       any         `json:"data,omitempty"`
	Result     any         `json:"result,omitempty"`
	Code       string      `json:"code,omitempty"`
	RetryAfter int         `json:"retry_after_ms,omitempty"`
	Added      []SSEDevice `json:"added,omitempty"`
	Removed    []string    `json:"removed,omitempty"`
	Timestamp  int64       `json:"timestamp"`
}

type SSEDevice struct {
	DeviceID     string   `json:"device_id"`
	DeviceName   string   `json:"device_name,omitempty"`
	DeviceType   string   `json:"device_type,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`
}

func ParseSSEMessage(data []byte) (*SSEMessage, error) {
	var msg SSEMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, err
	}
	return &msg, nil
}

func (msg *SSEMessage) Marshal() ([]byte, error) {
	return json.Marshal(msg)
}
