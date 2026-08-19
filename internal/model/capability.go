package model

type Capability struct {
	Name           string         `json:"name" yaml:"name"`
	Description    string         `json:"description" yaml:"description"`
	IntentID       uint16         `json:"intent_id,omitempty" yaml:"intent_id,omitempty"`
	InputSchema    InputSchema    `json:"inputSchema" yaml:"inputSchema"`
	Implementation Implementation `json:"implementation" yaml:"implementation"`
}

type InputSchema struct {
	Type       string                    `json:"type" yaml:"type"`
	Properties map[string]PropertySchema `json:"properties" yaml:"properties"`
	Required   []string                  `json:"required" yaml:"required"`
}

type PropertySchema struct {
	Type string   `json:"type" yaml:"type"`
	Enum []int    `json:"enum,omitempty" yaml:"enum,omitempty"`
	Unit string   `json:"unit,omitempty" yaml:"unit,omitempty"`
	Min  *float64 `json:"min,omitempty" yaml:"min,omitempty"`
	Max  *float64 `json:"max,omitempty" yaml:"max,omitempty"`
}

type Implementation struct {
	Native          string            `json:"native,omitempty" yaml:"native,omitempty"`
	Proxy           string            `json:"proxy,omitempty" yaml:"proxy,omitempty"`
	Channel         string            `json:"channel,omitempty" yaml:"channel,omitempty"`
	Baudrate        int               `json:"baudrate,omitempty" yaml:"baudrate,omitempty"`
	Protocol        string            `json:"protocol,omitempty" yaml:"protocol,omitempty"`
	HMACSecret      string            `json:"hmac_secret,omitempty" yaml:"hmac_secret,omitempty"`
	CmdMap          map[string]CmdMap `json:"cmd_map,omitempty" yaml:"cmd_map,omitempty"`
	TimeoutMs       int               `json:"timeout_ms,omitempty" yaml:"timeout_ms,omitempty"`
	Retry           int               `json:"retry,omitempty" yaml:"retry,omitempty"`
	AllowedCommands []string          `json:"allowed_commands,omitempty" yaml:"allowed_commands,omitempty"`
}

type CmdMap struct {
	Cmd int    `json:"cmd" yaml:"cmd"`
	Fmt string `json:"fmt" yaml:"fmt"`
}

func IsRequired(name string, required []string) bool {
	for _, r := range required {
		if r == name {
			return true
		}
	}
	return false
}

type DeviceConfig struct {
	Device       Device       `json:"device" yaml:"device"`
	Capabilities []Capability `json:"capabilities" yaml:"capabilities"`
}

// Sanitized 返回去除敏感字段（HMAC 密钥）的配置副本，用于对外暴露（/devices、get_device_schema）。
// 调用方不应把原始配置中的密钥泄漏给未授权的 AI 或局域网对端。
func (c DeviceConfig) Sanitized() DeviceConfig {
	out := c
	out.Device = c.Device
	out.Device.Capabilities = make([]Capability, len(c.Device.Capabilities))
	copy(out.Device.Capabilities, c.Device.Capabilities)
	out.Capabilities = make([]Capability, len(c.Capabilities))
	copy(out.Capabilities, c.Capabilities)
	for i := range out.Capabilities {
		out.Capabilities[i].Implementation.HMACSecret = ""
	}
	for i := range out.Device.Capabilities {
		out.Device.Capabilities[i].Implementation.HMACSecret = ""
	}
	return out
}
