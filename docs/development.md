# 开发指南

## 环境要求

- Go 1.25+
- Node 22+ 与 pnpm（仅在需要构建管理后台前端时）

## 常用命令

```bash
make run    # 启动网关，读取 config.yaml
make dev    # 另开终端启动前端开发服务器（http://localhost:5173/admin/），接口代理到 127.0.0.1:8080
make build  # 构建前端 + 编译，产物 bin/epay
make test   # Go 测试 + 前端类型检查
make fmt    # gofmt + prettier
make docker # 构建 Docker 镜像
```

前端开发时，把 `config.yaml` 的 `server.listen` 设为 `:8080`（与 Vite 代理目标一致），先 `make run` 再 `make dev`。

## 分层结构

```
cmd/epay/                 程序入口：加载配置、组装依赖、优雅退出
internal/
  config/                 YAML 配置（支持 ${ENV} 引用环境变量）
  epay/                   易支付签名算法（纯函数，无依赖）
  money/                  以「分」为单位的金额类型
  model/                  领域模型：订单、商户、渠道配置
  store/                  存储接口；sqlite/ 为默认实现
  gateway/                核心业务：下单、确认支付、查单、退款、商户通知、配置热更新
  provider/               上游渠道抽象与驱动注册表
    alipay/ wechat/ paypal/ stripe/ mock/
    keyutil/              RSA 密钥解析与 SHA256withRSA
  server/                 HTTP 层：易支付接口、收银台页面
  admin/                  管理后台接口：登录会话、渠道 / 商户 / 订单管理
  httputil/               HTTP 小工具
web/                      管理后台前端（Vite + React + shadcn/ui）
```

依赖方向是单向的：`server` / `admin` → `gateway` → `provider` / `store`。`gateway` 只依赖接口，不认识具体渠道与数据库实现。

## 关键设计

**金额**：全程用 `money.Cents`（int64，单位分）计算，只在协议边界与字符串互转，杜绝浮点误差。外币渠道额外记录 `PayCurrency` / `PayAmount`，回调时逐一核对。

**幂等**：`MarkPaid` 等状态变更都是带条件的 UPDATE（CAS），重复回调只有第一次会真正改变状态并触发通知。

**通知队列**：就是订单表本身（`notify_status` + `next_notify_at`），进程重启不会丢失待发通知；支付成功后主动唤醒投递循环，不必等轮询。

**配置热更新**：`gateway` 持有一个原子替换的配置快照（写时复制）。后台保存配置后调用 `Reload`，正在处理的请求继续用旧快照，不加锁也不会读到半更新状态。未变更的渠道会复用原实例，保留其内部状态（如 PayPal 的 token 缓存）。

**双重确认**：上游异步回调、买家跳回时的主动查单、收银台轮询时的节流查单，三条路径任一先到都能正确入账。

## 新增一个支付渠道

1. 新建 `internal/provider/<name>/<name>.go`，实现 `provider.Provider`：

   ```go
   type Provider interface {
       Pay(ctx, *PayRequest) (*PayResult, error)          // 上游下单
       ParseNotify(ctx, *http.Request) (*Payment, error)  // 验签解析回调
       AckNotify(w http.ResponseWriter, err error)        // 按渠道要求应答
       Query(ctx, *model.Order) (*Payment, error)         // 主动查询
   }
   ```

   支持退款则再实现 `provider.Refunder`。

2. 在 `init()` 中注册驱动，并声明配置字段——管理后台据此自动生成表单：

   ```go
   func init() {
       provider.Register(provider.Driver{
           Name:        "mydriver",
           Title:       "某某支付",
           Description: "一句话说明",
           DefaultType: "mypay",
           Webhook:     "需要在其后台配置回调地址时写说明，否则留空",
           Fields: []provider.Field{
               {Key: "app_id", Label: "AppID", Type: provider.FieldText, Required: true},
               {Key: "private_key", Label: "私钥", Type: provider.FieldTextarea, Required: true, Secret: true},
           },
           New: func(opts provider.Options) (provider.Provider, error) {
               var cfg Config           // 字段用 yaml 标签，与 Field.Key 对应
               if err := opts.Decode(&cfg); err != nil {
                   return nil, err
               }
               return New(cfg)
           },
       })
   }
   ```

   `Secret: true` 的字段在接口中会脱敏为 `******`，提交时保持原值即沿用旧值。

3. 在 `internal/provider/all/all.go` 中加一行匿名导入。

4. 完成。后台「新建渠道」里就能选到它，前端无需任何改动。

构造函数（`New`）应当校验全部必填配置并解析密钥——后台保存配置前会调用它，配置有问题会直接在表单里报错。

## 更换存储

实现 `store.Store`（订单 + 管理查询 + 配置三组接口）即可，例如 MySQL / PostgreSQL 版本。注意保持这些语义：

- `Create` 在 `(pid, out_trade_no)` 冲突时返回 `store.ErrDuplicate`；
- `MarkPaid` 必须是条件更新，返回是否由本次调用完成状态变更；
- `ReserveRefund` 必须原子地校验「已退 + 本次 ≤ 订单金额」。

## 测试

```bash
go test ./...
```

覆盖的内容：

| 测试 | 说明 |
| --- | --- |
| `internal/epay` | 签名算法与篡改检测 |
| `internal/money` | 金额解析、格式化、汇率换算 |
| `internal/store/sqlite` | 订单生命周期、重复下单、重复回调、超额退款 |
| `internal/provider/alipay` | 请求签名、回调验签与篡改拒绝 |
| `internal/provider/wechat` | 回调验签、AES-GCM 解密、篡改拒绝 |
| `internal/provider/stripe` | Webhook 签名校验与过期拒绝 |
| `internal/server` | 端到端：下单 → 收银台 → 回调 → 通知商户 → 跳转 → 查单 → 退款 |
| `internal/server`（兼容性） | 用 new-api 依赖的 `go-epay` 客户端验证协议兼容 |
| `internal/admin` | 登录、限流、CSRF、渠道密钥脱敏、配置热更新 |

> 若本机 Go 缺少 `vet` 工具，用 `go test -vet=off ./...`。

## 前端约定

- 业务页面只组合 shadcn/ui 组件（Card、Table、Field、Empty…），**不手写样式 class**；需要响应式时优先用组件自带能力（如 `Field orientation="responsive"`）。
- 页面骨架沿用官方区块 dashboard-01 与 login-03，这些文件保留区块原有的 class。
- `src/components/ui/` 由 shadcn CLI 生成，不要手改；需要新组件执行 `pnpm dlx shadcn@latest add <组件名>`。
- 服务端状态统一用 TanStack Query（`src/hooks/queries.ts`），接口调用集中在 `src/lib/api.ts`，类型定义在 `src/lib/types.ts` 与后端 DTO 对应。
- 路由级懒加载，新增页面时在 `src/router.tsx` 里照现有写法加一项。
