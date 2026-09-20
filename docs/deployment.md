# 部署指南

网关是单个 Go 二进制（管理后台前端已嵌入其中），数据保存在一个 SQLite 文件里，没有其他外部依赖。

## 前置条件

- 一个**公网可访问的 HTTPS 域名**。支付宝、微信、PayPal、Stripe 都需要回调网关，内网地址收不到回调。
- 服务器时间准确（微信支付、Stripe 的回调验签会校验时间戳，偏差超过 5 分钟会被拒绝）。

## 方式一：Docker Compose（推荐）

```bash
git clone git@github.com:Alndaly/epay.git
cd epay

cp config.example.yaml config.yaml
# 修改 config.yaml：server.base_url 改成你的域名，其余保持默认即可

# 管理员密码写在 .env 中，配置文件通过 ${ADMIN_PASSWORD} 引用
echo "ADMIN_PASSWORD=$(openssl rand -hex 8)" > .env
cat .env      # 记下这个密码，用于登录管理后台

docker compose up -d
docker compose logs -f
```

启动后访问 `https://你的域名/admin/`，用 `admin` + `.env` 中的密码登录。

`docker-compose.yml` 默认挂载了三个路径：

| 宿主机路径 | 容器内 | 用途 |
| --- | --- | --- |
| `./config.yaml` | `/app/config.yaml` | 配置文件（只读） |
| `./data` | `/app/data` | SQLite 数据库，**需要备份** |
| `./certs` | `/app/certs` | 微信支付等证书文件（只读，可选） |

镜像也可以自行构建并指定版本号：

```bash
docker build -t epay-gateway:v0.1.0 --build-arg VERSION=v0.1.0 .
```

## 方式二：直接编译运行

需要 Go 1.25+ 与 pnpm（用于构建管理后台前端）：

```bash
make build            # 构建前端 + 编译，产物 bin/epay
cp config.example.yaml config.yaml
./bin/epay -config config.yaml
```

只改后端、不需要管理后台时，可以跳过前端构建直接 `go build ./cmd/epay`；此时访问 `/admin/` 会提示前端未构建，支付功能不受影响。

配合 systemd 常驻：

```ini
# /etc/systemd/system/epay.service
[Unit]
Description=epay gateway
After=network-online.target

[Service]
WorkingDirectory=/opt/epay
ExecStart=/opt/epay/bin/epay -config /opt/epay/config.yaml
EnvironmentFile=/opt/epay/.env
Restart=always
RestartSec=3
User=epay

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl enable --now epay
```

## 反向代理与 HTTPS

网关自身只监听 HTTP，HTTPS 交给前面的反向代理。启用代理后请在 `config.yaml` 中设置 `server.trust_proxy: true`，否则拿到的买家 IP 会是代理的 IP。

Caddy（自动申请证书，最省事）：

```caddyfile
pay.example.com {
    reverse_proxy 127.0.0.1:8080
}
```

Nginx：

```nginx
server {
    listen 443 ssl http2;
    server_name pay.example.com;

    ssl_certificate     /etc/letsencrypt/live/pay.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/pay.example.com/privkey.pem;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host              $host;
        proxy_set_header X-Real-IP         $remote_addr;
        proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

> 只信任你自己的代理：`trust_proxy` 为 true 时网关会直接采用 `X-Forwarded-For`，若网关端口同时暴露在公网，买家 IP 可被伪造。

## 配置文件说明

完整示例见 [config.example.yaml](../config.example.yaml)。配置文件只保存基础设施相关配置，**商户与支付渠道在管理后台维护**。

| 配置项 | 说明 |
| --- | --- |
| `server.listen` | 监听地址，默认 `:8080` |
| `server.base_url` | 网关公网地址，必填。上游回调与买家跳转地址都基于它生成 |
| `server.trust_proxy` | 是否信任反向代理传来的客户端 IP |
| `database.path` | SQLite 文件路径 |
| `order.expire` | 订单有效期，默认 `30m` |
| `order.notify_timeout` | 单次商户通知的超时时间，默认 `10s` |
| `log.level` / `log.format` | 日志级别与格式（`text` / `json`） |
| `admin.username` / `admin.password` | 管理后台账号；密码为空则不启用后台 |

任意配置值都支持 `${ENV_NAME}` 形式引用环境变量，密钥建议放在 `.env` 或容器 Secret 中，不要直接写进配置文件。

`merchants` / `channels` 两段是可选的迁移入口：**仅当数据库中还没有任何商户与渠道时（首次启动）导入一次**，之后以数据库为准，修改配置文件不再生效。

## 升级

```bash
git pull
docker compose up -d --build     # Docker
# 或：make build && sudo systemctl restart epay
```

数据库表结构使用 `CREATE TABLE IF NOT EXISTS` 自动创建，升级不需要手动迁移。建议升级前先备份。

## 备份与恢复

需要备份的只有 SQLite 数据库（订单、商户、渠道配置都在里面）和 `config.yaml`。

```bash
# 备份（WAL 模式下请用 sqlite3 .backup，不要直接 cp）
sqlite3 data/epay.db ".backup '/backup/epay-$(date +%F).db'"

# 恢复：停止服务后放回原处即可
```

> 数据库中保存着渠道密钥与商户密钥，备份文件请与密钥同等级别保管。

## 健康检查与日志

- `GET /healthz` 返回 `ok`，可用于负载均衡或容器健康检查。
- 日志输出到标准输出，包含下单、支付成功、通知成败与渠道错误；排查问题时可把 `log.level` 调成 `debug`。

```bash
docker compose logs -f --tail=100
```

## 安全建议

- `admin.password` 使用随机长密码；修改密码后所有已登录会话立即失效。
- 生产环境不要启用「模拟支付」渠道，它允许任何人把订单标记为已支付（启动时会打印警告）。
- 管理后台可以在反向代理层再加一层 IP 白名单：
  ```nginx
  location /admin/ {
      allow 1.2.3.4;      # 你的办公网 IP
      deny all;
      proxy_pass http://127.0.0.1:8080;
  }
  ```
- `api.php` 使用明文商户密钥鉴权，只应由商户服务端调用，不要放到浏览器里。
