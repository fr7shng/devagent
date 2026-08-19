package protocol

import "encoding/json"

// SSEMessage 是 Sidecar 与 Daemon 之间的统一 JSON 信封（type 区分语义），
// 当前用于 HTTP invoke 请求/响应、限流/鉴权错误；命名保留 "SSE" 以便将来
// 复用同一结构承载 SSE 推送（register/heartbeat/progress/result/error）。
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
