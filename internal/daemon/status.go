package daemon

// busyQueueDepthThreshold 设备队列深度达到该值时返回 DEVICE_BUSY 背压信号。
// 与固件 cmd_queue 容量（8）对齐。
const busyQueueDepthThreshold = 8

const (
	StatusRateLimited        = "rate_limited"
	StatusUnauthorized       = "unauthorized"
	StatusForbidden          = "forbidden"
	StatusDeviceNotFound     = "device_not_found"
	StatusCapabilityNotFound = "capability_not_found"
	StatusHalNotAvailable    = "hal_not_available"
	StatusCmdMapNotFound     = "cmd_map_not_found"
	StatusDCPError           = "dcp_error"
	StatusDeviceBusy         = "device_busy"
	StatusTimeout            = "timeout"
)
