package gateway

import (
	"strings"

	"epay/internal/provider"
)

// DetectDevice 根据易支付 device 参数与买家浏览器 User-Agent 判断终端类型。
// 显式传入的 device 参数优先；mapi.php 为服务端调用，此时 ua 应传空。
func DetectDevice(param, ua string) provider.Device {
	switch strings.ToLower(param) {
	case "pc":
		return provider.DevicePC
	case "wechat":
		return provider.DeviceWechat
	case "alipay":
		return provider.DeviceAlipay
	case "mobile", "qq", "jump":
		return provider.DeviceMobile
	}
	switch {
	case strings.Contains(ua, "MicroMessenger"):
		return provider.DeviceWechat
	case strings.Contains(ua, "AlipayClient"):
		return provider.DeviceAlipay
	case strings.Contains(ua, "Mobile") || strings.Contains(ua, "Android") || strings.Contains(ua, "iPhone"):
		return provider.DeviceMobile
	}
	return provider.DevicePC
}
