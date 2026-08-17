package protocol

import "encoding/json"

type SSEMessage struct {
	Type       string `json:"type"`
	DeviceID   string `json:"device_id,omitempty"`
	Capability string `json:"capability,omitempty"`
	RequestID  string `json:"request_id,omitempty"`
	JobID      string `json:"job_id,omitempty"`
	Params     any    `json:"params,omitempty"`
	Status     string `json:"status,omitempty"`
	Message    string `json:"message,omitempty"`
	Result     any    `json:"result,omitempty"`
	Timestamp  int64  `json:"timestamp"`
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
