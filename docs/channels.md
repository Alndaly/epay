# 渠道申请与配置

所有参数都在管理后台「支付渠道 → 新建渠道」中填写。密钥类字段既可以直接粘贴内容，也可以填服务器上的文件路径（例如 `/app/certs/apiclient_key.pem`）。

下文中的 `{base_url}` 指 `config.yaml` 里配置的网关公网地址，例如 `https://pay.example.com`。

## 支付宝

**需要**：[支付宝开放平台](https://open.alipay.com/)自研应用，并开通对应的支付产品。

1. 开放平台 → 你的应用 → **开发设置 → 接口加签方式**，选择**公钥模式**（本网关暂不支持「公钥证书」模式）。
2. 用官方「密钥工具」生成 RSA2 密钥对：**应用私钥**自己保存，**应用公钥**上传到开放平台。
3. 上传后页面会显示**支付宝公钥**，复制下来（注意：不是你刚上传的应用公钥）。

| 字段 | 从哪里取 |
| --- | --- |
| AppID | 应用详情页的 APPID |
| 应用私钥 | 密钥工具生成的私钥（`PKCS#8` 或 `PKCS#1`，PEM 或裸 Base64 均可） |
| 支付宝公钥 | 开放平台「接口加签方式」中展示的支付宝公钥 |
| 支付产品 | 见下表 |
| 沙箱环境 | 使用开放平台沙箱时打开 |

支付产品（`mode`）：

| 选项 | 对应产品 | 说明 |
| --- | --- | --- |
| 自动 | 电脑网站支付 + 手机网站支付 | PC 浏览器跳转收银台，手机浏览器跳转 App/H5。需要同时开通两个产品 |
| 电脑网站支付 | `alipay.trade.page.pay` | 企业资质 |
| 手机网站支付 | `alipay.trade.wap.pay` | 企业资质 |
| 当面付扫码 | `alipay.trade.precreate` | **个人开发者通常只能开通这个**，在网关收银台展示二维码 |

回调无需在支付宝后台配置，下单时会自动传给支付宝。

## 微信支付

**需要**：微信支付商户号（个人无法申请），以及一个与商户号绑定的 AppID（公众号 / 小程序 / 移动应用 / 服务号均可）。

1. [商户平台](https://pay.weixin.qq.com/) → **账户中心 → API 安全**：
   - 申请 **API 证书**，得到 `apiclient_key.pem` 与**证书序列号**；
   - 设置 **APIv3 密钥**（32 位，自己设定并保存）；
   - 开通 **微信支付公钥**，下载 `pub_key.pem` 并记下**公钥 ID**（形如 `PUB_KEY_ID_...`）。
2. 证书文件放到服务器上（Docker 部署可放进 `./certs`，容器内路径为 `/app/certs/...`）。

| 字段 | 从哪里取 |
| --- | --- |
| AppID | 与商户号绑定的应用 AppID |
| 商户号 | 商户平台的商户号（mchid） |
| APIv3 密钥 | 账户中心 → API 安全中设置的 32 位密钥 |
| 商户证书序列号 | 申请 API 证书后显示 |
| 商户 API 私钥 | `apiclient_key.pem` 的内容或路径 |
| 微信支付公钥 ID | `PUB_KEY_ID_...` |
| 微信支付公钥 | `pub_key.pem` 的内容或路径 |

支付产品（`mode`）：

| 选项 | 说明 |
| --- | --- |
| Native 扫码 | 默认，所有商户可用；在网关收银台展示二维码 |
| H5 跳转 | 手机浏览器直接唤起微信支付，**需在商户平台单独开通 H5 支付**，且要配置授权域名 |
| 自动 | 手机浏览器（非微信内）用 H5，其余用扫码 |

> 本网关使用「微信支付公钥」验签（2024 年后新商户的默认方式），不需要下载平台证书。回调地址由下单请求传递，无需在商户平台配置。

## PayPal

**需要**：[PayPal 开发者后台](https://developer.paypal.com/)的应用（Sandbox 或 Live）。

1. **Apps & Credentials** → 创建 App，得到 **Client ID** 与 **Secret**。
2. 在该 App 下 **Add Webhook**：
   - Webhook URL 填 `{base_url}/notify/paypal`（新建渠道页面底部会直接给出这个地址）；
   - 勾选事件：**Checkout order approved** 与 **Payment capture completed**；
   - 保存后记下 **Webhook ID**。

| 字段 | 说明 |
| --- | --- |
| Client ID / Client Secret | 应用凭据 |
| Webhook ID | 用于校验回调签名，必填 |
| 商家名称 | PayPal 支付页展示的名称 |
| 沙箱环境 | 使用 Sandbox 凭据时打开 |
| 扣款币种 | 如 `USD`，PayPal 不支持人民币结算 |
| 汇率 | 1 元人民币兑换多少扣款币种，例如 `0.14` |

订单金额（人民币）会按汇率换算成外币扣款，收银台会显示实付外币金额。汇率是你自己设定的固定值，需要定期检查。

## Stripe

**需要**：[Stripe Dashboard](https://dashboard.stripe.com/) 账号。

1. **Developers → API keys**：复制 **Secret key**（`sk_live_...` / `sk_test_...`）。
2. **Developers → Webhooks → Add endpoint**：
   - URL 填 `{base_url}/notify/stripe`；
   - 订阅事件 `checkout.session.completed` 与 `checkout.session.async_payment_succeeded`；
   - 创建后复制 **Signing secret**（`whsec_...`）。

| 字段 | 说明 |
| --- | --- |
| Secret Key | `sk_live_...` |
| Webhook 签名密钥 | `whsec_...`，用于校验回调 |
| 支付方式 | 留空则用 Stripe 后台的动态支付方式；也可指定如 `card, alipay, wechat_pay` |
| 扣款币种 / 汇率 | 同 PayPal |

## 模拟支付（仅测试）

无需任何参数。启用后，用该支付方式下单会在收银台显示「模拟支付成功」按钮，点击即可走完整的回调 → 入账 → 通知商户流程，用于联调 new-api。

> **任何人都能点这个按钮把订单标记为已支付，生产环境务必删除该渠道。** 网关启动时若检测到启用的模拟渠道会打印警告。

## 通用说明

- **外币币种限制**：暂不支持 JPY、KRW、TWD 等没有小数位的币种。
- **金额核对**：下单时记录了上游币种与金额，回调入账前会逐一核对，金额不符会拒绝入账并记录错误日志。
- **退款**：四个渠道都支持通过 `api.php?act=refund` 发起（支持多次部分退款），管理后台暂未提供退款入口，以避免误操作。
