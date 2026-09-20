# 接入 new-api

[new-api](https://github.com/QuantumNous/new-api) 内置了易支付客户端，本网关与它的协议实现（`Calcium-Ion/go-epay`）做过兼容性测试（见 `internal/server/newapi_compat_test.go`）。

## 步骤

**1. 在网关后台准备好渠道与商户**

- 「支付渠道」中至少启用一个渠道，记下每个渠道的**支付方式标识**（如 `alipay`、`wxpay`）。
- 「商户」中新建一个商户，弹窗里会给出支付地址、商户 ID、商户密钥。

**2. 在 new-api 填写支付设置**

「系统设置 → 支付设置」：

| new-api 设置项 | 填写内容 |
| --- | --- |
| 支付地址 | 网关地址，如 `https://pay.example.com`。**不要带 `/submit.php`**，new-api 会自动拼接 |
| 易支付商户 ID | 后台商户页面的商户 ID（如 `1001`） |
| 易支付商户密钥 | 后台商户页面的密钥 |
| 回调地址 | **new-api 自己的地址**（如 `https://api.example.com`），new-api 用它拼出 `notify_url` 传给网关 |

**3. 配置充值方式**

充值方式里每一项的 `type` 必须与网关渠道的「支付方式标识」完全一致：

```json
[
  {"name": "支付宝", "color": "rgba(var(--semi-blue-5), 1)", "type": "alipay"},
  {"name": "微信",   "color": "rgba(var(--semi-green-5), 1)", "type": "wxpay"},
  {"name": "PayPal", "color": "rgba(var(--semi-indigo-5), 1)", "type": "paypal"},
  {"name": "Stripe", "color": "rgba(var(--semi-violet-5), 1)", "type": "stripe"}
]
```

**4. 验证**

建议先在网关后台新建一个「模拟支付」渠道（标识 `mock`），在 new-api 充值方式里临时加一项 `{"name": "测试", "type": "mock"}`，走一次完整充值：

1. new-api 中点充值 → 跳转到网关收银台；
2. 点「模拟支付成功」；
3. 页面跳回 new-api，额度到账；
4. 网关后台「订单」中该订单为「已支付 / 已通知」。

验证通过后删除模拟渠道和对应的充值方式。

## 工作流程

```
用户在 new-api 点充值
  └─ new-api 用商户密钥签名，表单 POST 到 {支付地址}/submit.php
       └─ 网关验签、创建订单，调用上游渠道下单
            ├─ 跳转类渠道（支付宝网页 / PayPal / Stripe）→ 直接跳到上游收银台
            └─ 扫码类渠道（微信 Native / 当面付）→ 网关收银台展示二维码
用户付款
  └─ 上游回调网关 /notify/{type}（买家跳回时网关也会主动查一次）
       └─ 核对金额后入账，GET 请求 new-api 的 notify_url，new-api 返回 success 即完成
            └─ 用户被带着签名参数跳回 new-api 的 return_url
```

## 注意事项

- **金额与币种**：new-api 按其「充值价格」以人民币计算金额；PayPal / Stripe 渠道会按你设定的汇率换算成外币扣款，收银台会显示实付外币金额。
- **通知重试**：new-api 短暂宕机不会丢单，网关会自动重试 12 次（约 25 小时）。恢复后仍未到账的，可在网关后台订单详情点「补发通知」。
- **订单号**：new-api 每次充值都会生成新的商户订单号，重复提交同一个订单号时网关会复用原订单（金额或支付方式不一致则拒绝）。
- **多套 new-api**：为每套 new-api 各建一个商户，互不影响，订单列表里可按商户筛选。

## 对接其他易支付客户端

本网关实现的是通用易支付协议（`submit.php` / `mapi.php` / `api.php`），理论上兼容其他按该协议开发的商户系统，接口细节见 [接口文档](api.md)。
