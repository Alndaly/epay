# epay-gateway：统一支付网关

一个**兼容易支付（epay）协议**的轻量支付网关，使用 Go 编写，单文件部署、无需 CGO。
商户（如 [new-api](https://github.com/QuantumNous/new-api)）按易支付协议接入，网关再对接真实的支付渠道：

| 易支付 type（可自定义） | 驱动 | 支付产品 |
| --- | --- | --- |
| `alipay` | `alipay` | 电脑网站支付 / 手机网站支付 / 当面付扫码（RSA2 公钥模式） |
| `wxpay` | `wechat` | 微信支付 APIv3：Native 扫码 / H5（微信支付公钥验签） |
| `paypal` | `paypal` | PayPal Orders v2（自动汇率换算、Webhook 兜底） |
| `stripe` | `stripe` | Stripe Checkout（银行卡、Apple Pay、Google Pay 等） |
| `mock` | `mock` | 模拟支付，仅用于联调测试 |

## 特性

- **协议兼容**：`submit.php` / `mapi.php` / `api.php`（查单、退款），MD5 签名规则与彩虹易支付一致；用 new-api 实际使用的 `go-epay` 客户端做了兼容性测试。
- **可靠通知**：通知队列持久化在数据库中，失败按 15s → 15s → 30s → 3m → … → 15h 重试共 12 次，进程重启不丢失。
- **双重确认**：上游异步通知 + 买家跳回 / 收银台轮询时主动查单，任一路径先到都能正确入账；重复回调严格幂等。
- **资金安全**：金额全程以"分"为整数计算；入账前核对上游实付金额与币种；退款先原子占用额度再请求上游，防止超额退款。
- **易扩展**：新增渠道只需实现一个接口并注册，无需改动已有代码。

## 架构

```
            易支付协议                                上游渠道协议
 商户 ───────────────────▶ ┌──────────────────────┐ ───────────────▶ 支付宝 / 微信 / PayPal / Stripe
(new-api) ◀── 异步通知 ─── │  server   HTTP 协议适配 │ ◀── 异步通知 ────
                           ├──────────────────────┤
                           │  gateway  核心业务    │  下单·确认支付·通知重试·查单·退款
                           ├───────────┬──────────┤
                           │  provider │  store   │  渠道抽象 / 订单存储（均为接口）
                           └───────────┴──────────┘
```

```
cmd/epay/                 程序入口：加载配置、组装依赖、优雅退出
internal/
  config/                 YAML 配置（支持 ${ENV} 引用环境变量）
  epay/                   易支付签名算法（纯函数）
  money/                  以"分"为单位的金额类型
  model/                  订单领域模型
  store/                  存储接口；sqlite/ 为默认实现
  gateway/                核心业务：Service（下单/确认/查单/退款）+ Notifier（商户通知）
  provider/               渠道接口与注册表
    alipay/ wechat/ paypal/ stripe/ mock/
    keyutil/              RSA 密钥解析与 SHA256withRSA
  server/                 HTTP 路由、易支付接口、收银台页面
```

**一笔订单的生命周期**

1. 商户提交 `submit.php` → 网关验签、落库，调用渠道 `Pay` 下单；
2. 跳转类渠道（支付宝网页、PayPal、Stripe）直接 302 到上游；扫码类（微信 Native、当面付）进入网关收银台 `/pay/{trade_no}` 展示二维码；
3. 买家付款后，上游回调 `/notify/{type}`（或买家跳回 `/return/{trade_no}` 时网关主动查单）→ 核对金额 → 订单置为已支付；
4. Notifier 以 GET 请求商户 `notify_url`（带签名的标准易支付参数），商户返回 `success` 即完成；
5. 买家被带着同样的签名参数跳回商户 `return_url`。

## 快速开始

```bash
cp config.example.yaml config.yaml   # 按注释修改
go run ./cmd/epay -config config.yaml
```

Docker：

```bash
cp config.example.yaml config.yaml
echo "EPAY_MERCHANT_KEY=$(openssl rand -hex 16)" > .env
docker compose up -d
```

> 想先跑通流程？在 `config.yaml` 中把 `mock` 渠道设为 `enabled: true`，在收银台点「模拟支付成功」即可走完整个回调与通知流程。

网关必须通过公网 HTTPS 地址（`server.base_url`）访问，上游渠道才能回调。建议置于 Nginx / Caddy 之后并开启 `trust_proxy`。

## 接入 new-api

在 new-api「系统设置 → 支付设置」中填写：

| new-api 设置项 | 填写 |
| --- | --- |
| 支付地址 | `server.base_url`，如 `https://pay.example.com`（不要带 `/submit.php`） |
| 易支付商户 ID | `merchants[].pid` |
| 易支付商户密钥 | `merchants[].key` |
| 回调地址 | new-api 自身的地址（new-api 会据此生成 `notify_url`） |
| 充值方式 | 每项的 `type` 与网关 `channels[].type` 一致，例如： |

```json
[
  {"name": "支付宝", "color": "rgba(var(--semi-blue-5), 1)", "type": "alipay"},
  {"name": "微信",   "color": "rgba(var(--semi-green-5), 1)", "type": "wxpay"},
  {"name": "PayPal", "color": "rgba(var(--semi-indigo-5), 1)", "type": "paypal"},
  {"name": "Stripe", "color": "rgba(var(--semi-violet-5), 1)", "type": "stripe"}
]
```

> new-api 按其"充值价格"以人民币计算 `money`；PayPal / Stripe 渠道会按 `exchange_rate` 换算为外币扣款，收银台会显示实付外币金额。

## 渠道配置要点

完整示例见 [config.example.yaml](config.example.yaml)。所有密钥字段既可以直接填内容，也可以填文件路径。

- **支付宝**：开放平台应用须使用「公钥」加签模式（暂不支持公钥证书模式）。`mode` 可选 `auto` / `page` / `wap` / `qrcode`，个人开发者通常只能开通当面付，请使用 `qrcode`。
- **微信支付**：使用 APIv3 +「微信支付公钥」验签（新商户默认）。`mode: native` 为扫码（所有商户可用）；`h5` 需在商户平台单独开通。
- **PayPal**：需在应用下创建 Webhook（`{base_url}/notify/paypal`，订阅 *Checkout order approved* 与 *Payment capture completed*）并填入 `webhook_id`。
- **Stripe**：需创建 Webhook（`{base_url}/notify/stripe`，订阅 `checkout.session.completed` 与 `checkout.session.async_payment_succeeded`）并填入 `webhook_secret`。
- 外币渠道暂不支持 JPY、KRW 等无小数位币种。

## 接口说明

所有参数均以 MD5 签名：去掉 `sign`、`sign_type` 与空值，按参数名 ASCII 升序拼成 `a=1&b=2`，末尾直接拼接商户密钥后取 32 位小写 MD5。

### 页面跳转下单 `GET|POST /submit.php`

| 参数 | 必填 | 说明 |
| --- | --- | --- |
| pid | 是 | 商户 ID |
| type | 是 | 支付方式，对应 `channels[].type` |
| out_trade_no | 是 | 商户订单号（同一商户唯一，≤64 字符） |
| notify_url | 是 | 异步通知地址 |
| return_url | 否 | 支付完成后跳转地址 |
| name | 是 | 商品名称 |
| money | 是 | 金额（元，最多两位小数） |
| param | 否 | 自定义参数，原样返回 |
| device | 否 | `pc` / `mobile` / `wechat` / `alipay`，不传时按浏览器 UA 判断 |
| sign / sign_type | 是 | 签名 / `MD5` |

### API 下单 `POST /mapi.php`

参数同上，可额外传 `clientip`（买家 IP）。返回：

```json
{"code": 1, "msg": "success", "trade_no": "20260920011743740085",
 "payurl": "https://...", "qrcode": "weixin://wxpay/bizpayurl?pr=..."}
```

跳转类渠道返回 `payurl`；扫码类渠道同时返回 `qrcode`（二维码内容）与 `payurl`（网关收银台）。失败时 `code` 为 `-1`，`msg` 为原因。

### 查询与退款 `GET|POST /api.php`

以 `pid` + `key`（明文商户密钥）鉴权，**只应由商户服务端调用**。

- `act=query`：商户信息
- `act=order&trade_no=` 或 `&out_trade_no=`：查询订单，返回 `status`（1 已支付 / 0 未支付）、`money`、`api_trade_no`、`addtime`、`endtime` 等
- `act=refund&trade_no=&money=`：退款，支持多次部分退款

### 异步通知（网关 → 商户）

支付成功后以 **GET** 请求 `notify_url`，参数：`pid`、`trade_no`、`out_trade_no`、`type`、`name`、`money`、`trade_status=TRADE_SUCCESS`、`param`、`sign`、`sign_type`。
商户验签并处理后需返回纯文本 `success`，否则按重试计划继续通知。商户应根据 `out_trade_no` 做幂等处理。

## 扩展新渠道

1. 在 `internal/provider/<name>/` 中实现 `provider.Provider`（`Pay` / `ParseNotify` / `AckNotify` / `Query`），支持退款则再实现 `provider.Refunder`；
2. 在 `init()` 中 `provider.Register("<name>", factory)`，factory 通过 `opts.Decode(&cfg)` 读取自己的配置；
3. 在 `internal/provider/all/all.go` 中加一行匿名导入；
4. 在配置文件 `channels` 中使用 `driver: <name>`。

## 开发

```bash
go test ./...        # 单元测试 + 端到端测试 + new-api 兼容性测试
make build           # 输出 bin/epay
```

## 注意事项

- `mock` 渠道任何人都能触发支付成功，**生产环境务必关闭**（启动时会打印警告）。
- SQLite 以单连接 + WAL 模式运行，足以支撑中小规模业务；如需多实例部署，可按 `store.OrderStore` 接口实现 MySQL / PostgreSQL 存储。
- 退款接口对上游请求失败时会回滚本地额度，但若上游实际已退款而响应丢失，需要到渠道后台核对。
