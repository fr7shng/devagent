package model

import "time"

type Job struct {
	ID        string    `json:"job_id"`
	RequestID string    `json:"request_id"`
	DeviceID  string    `json:"device_id"`
	Status    string    `json:"status"`
	Progress  int       `json:"progress"`
	Result    any       `json:"data,omitempty"`
	Error     string    `json:"error,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}
