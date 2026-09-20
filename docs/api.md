# 接口文档（易支付协议）

网关对商户暴露的接口与「彩虹易支付」保持一致，已有的易支付客户端可以直接对接。

| 接口 | 方法 | 用途 |
| --- | --- | --- |
| `/submit.php` | GET / POST | 页面跳转下单（买家浏览器直接提交） |
| `/mapi.php` | POST | API 下单，返回支付链接或二维码内容 |
| `/api.php` | GET / POST | 查询商户、查询订单、退款 |
| `/pay/{trade_no}` | GET | 网关收银台（扫码类渠道展示二维码） |
| `/healthz` | GET | 健康检查 |

## 签名算法

1. 去掉 `sign`、`sign_type` 以及值为空的参数；
2. 按参数名 ASCII 升序，拼成 `a=1&b=2`（值不做 URL 编码）；
3. 末尾直接拼接商户密钥，取 MD5 的 32 位小写十六进制。

```php
// PHP
function epay_sign(array $params, string $key): string {
    ksort($params);
    $parts = [];
    foreach ($params as $k => $v) {
        if ($v === '' || $k === 'sign' || $k === 'sign_type') continue;
        $parts[] = "$k=$v";
    }
    return md5(implode('&', $parts) . $key);
}
```

```python
# Python
import hashlib
def epay_sign(params: dict, key: str) -> str:
    raw = "&".join(f"{k}={params[k]}" for k in sorted(params)
                   if params[k] != "" and k not in ("sign", "sign_type"))
    return hashlib.md5((raw + key).encode()).hexdigest()
```

## 页面跳转下单 `GET|POST /submit.php`

| 参数 | 必填 | 说明 |
| --- | --- | --- |
| `pid` | 是 | 商户 ID |
| `type` | 是 | 支付方式标识，对应后台渠道配置 |
| `out_trade_no` | 是 | 商户订单号，同一商户内唯一，≤64 字符 |
| `notify_url` | 是 | 异步通知地址（http/https） |
| `return_url` | 否 | 支付完成后跳回的地址 |
| `name` | 是 | 商品名称 |
| `money` | 是 | 金额（元，最多两位小数，须大于 0） |
| `param` | 否 | 自定义参数，通知与跳转时原样回传 |
| `device` | 否 | `pc` / `mobile` / `wechat` / `alipay`，不传时按浏览器 UA 判断 |
| `sign` / `sign_type` | 是 | 签名 / 固定 `MD5` |

响应：302 跳转。跳转类渠道直接跳到上游收银台，扫码类渠道跳到网关收银台 `/pay/{trade_no}`。参数有误时返回 HTML 错误页。

## API 下单 `POST /mapi.php`

参数同上，另可传 `clientip`（买家 IP，服务端调用时建议带上）。

```json
{
  "code": 1,
  "msg": "success",
  "trade_no": "20260920011743740085",
  "payurl": "https://openapi.alipay.com/gateway.do?...",
  "qrcode": "weixin://wxpay/bizpayurl?pr=..."
}
```

- 跳转类渠道返回 `payurl`（上游收银台地址）。
- 扫码类渠道返回 `qrcode`（二维码内容，自行渲染）与 `payurl`（网关收银台，可直接跳转）。
- 失败时 `code` 为 `-1`，`msg` 为原因。

## 查询与退款 `GET|POST /api.php`

以 `pid` + `key`（**明文商户密钥**）鉴权，只应由商户服务端调用。

**查询商户** `act=query`

```json
{"code": 1, "pid": "1001", "name": "new-api", "active": 1}
```

**查询订单** `act=order`，传 `trade_no`（平台订单号）或 `out_trade_no`（商户订单号）：

```json
{
  "code": 1, "msg": "查询订单成功",
  "trade_no": "20260920011743740085", "out_trade_no": "USR1NO0000",
  "api_trade_no": "2024092022001", "type": "alipay", "pid": "1001",
  "addtime": "2026-09-20 01:46:19", "endtime": "2026-09-20 01:46:25",
  "name": "额度充值", "money": "72.50", "refundmoney": "0.00",
  "status": 1, "param": "uid=42", "buyer": "a***@b.com"
}
```

`status`：`1` 已支付，`0` 未支付。查询时网关会（带节流地）向上游确认一次真实状态。

**退款** `act=refund`，传 `trade_no` 或 `out_trade_no`，以及 `money`（退款金额，元）：

```json
{"code": 1, "msg": "退款成功"}
```

支持多次部分退款，累计不得超过订单金额。失败返回 `code: -1` 与原因。

## 异步通知（网关 → 商户）

订单支付成功后，网关以 **GET** 请求商户的 `notify_url`：

```
GET {notify_url}?pid=1001&trade_no=20260920011743740085&out_trade_no=USR1NO0000
   &type=alipay&name=额度充值&money=72.50&trade_status=TRADE_SUCCESS
   &param=uid%3D42&sign=xxx&sign_type=MD5
```

商户必须：

1. 用自己的密钥验签；
2. 确认 `trade_status` 为 `TRADE_SUCCESS`；
3. 按 `out_trade_no` 做幂等处理（同一订单可能收到多次通知）；
4. 返回纯文本 `success`（不区分大小写）。

返回其他内容视为失败，网关按 15s → 15s → 30s → 3m → 10m → 20m → 30m → 1h → 2h → 6h → 15h 重试，共 12 次、跨度约 25 小时。全部失败后可在管理后台手动补发。

## 同步跳转（买家浏览器）

买家付款后会被带着与异步通知相同的签名参数跳回 `return_url`。同步跳转**只能用于展示**，不能作为发货依据——买家可能直接关闭页面。请以异步通知或主动查单为准。

## 错误码

| 场景 | 返回 |
| --- | --- |
| 商户不存在 / 已停用 | `商户不存在或已停用` |
| 签名不正确 | `签名校验失败` |
| 支付方式未配置或已停用 | `不支持的支付方式：xxx` |
| 商户订单号重复且金额或方式不一致 | `商户订单号已存在且金额或支付方式不一致` |
| 订单已支付后再次下单 | `该订单已支付` |
| 上游下单失败（密钥错误、网络异常等） | `支付渠道下单失败，请稍后重试`，详细原因见网关日志 |
