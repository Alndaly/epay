// Package all 通过匿名导入注册全部内置支付驱动。
// 新增渠道时：实现 provider.Provider 并在 init() 中 Register，然后在此处加一行导入。
package all

import (
	_ "epay/internal/provider/alipay"
	_ "epay/internal/provider/mock"
	_ "epay/internal/provider/paypal"
	_ "epay/internal/provider/stripe"
	_ "epay/internal/provider/wechat"
)
