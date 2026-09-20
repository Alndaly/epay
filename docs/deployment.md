# 部署指南

网关是单个 Go 二进制（管理后台前端已嵌入其中），数据保存在一个 SQLite 文件里，没有其他外部依赖。

## 前置条件

- 一个**公网可访问的 HTTPS 域名**。支付宝、微信、PayPal、Stripe 都需要回调网关，内网地址收不到回调。
- 服务器时间准确（微信支付、Stripe 的回调验签会校验时间戳，偏差超过 5 分钟会被拒绝）。

## 全新服务器从零部署（推荐流程）

以一台干净的 Ubuntu / Debian 云服务器为例，从零到可收款大约 10 分钟。下文假设域名为 `pay.example.com`。

### 1. 域名解析

在域名服务商处添加一条 A 记录，把 `pay.example.com` 指向服务器公网 IP。等解析生效（`ping pay.example.com` 能看到你的 IP）再继续，否则后面申请证书会失败。

### 2. 放开端口

安全组 / 防火墙只需要放开 **80** 与 **443**（80 用于证书校验和 HTTP 跳转）。网关本身的 8080 不需要对外开放。

```bash
# 若服务器启用了 ufw
sudo ufw allow 80,443/tcp
sudo ufw enable
```

### 3. 安装 Docker

```bash
curl -fsSL https://get.docker.com | sudo sh
sudo systemctl enable --now docker
```

### 4. 拉取代码并配置

```bash
sudo mkdir -p /opt && cd /opt
sudo git clone git@github.com:Alndaly/epay.git epay   # 无 SSH 密钥可改用 https 地址
cd epay

cp config.example.yaml config.yaml
cp Caddyfile.example Caddyfile
```

编辑两个文件，各改一行：

```bash
vi config.yaml    # server.base_url 改成 https://pay.example.com
vi Caddyfile      # 第一行的 pay.example.com 改成你的域名
```

生成管理员密码：

```bash
echo "ADMIN_PASSWORD=$(openssl rand -hex 8)" > .env
cat .env          # 记下这个密码
chmod 600 .env
```

### 5. 启动（自带自动 HTTPS）

```bash
sudo docker compose -f docker-compose.yml -f docker-compose.https.yml up -d
```

这会启动两个容器：`epay`（网关）与 `epay-caddy`（反向代理，自动申请并续期 Let's Encrypt 证书）。
加上 `docker-compose.https.yml` 后网关不再直接暴露 8080，只能通过 Caddy 访问。

查看状态与日志：

```bash
sudo docker compose -f docker-compose.yml -f docker-compose.https.yml ps      # epay 应为 healthy
sudo docker compose -f docker-compose.yml -f docker-compose.https.yml logs -f
```

> 首次启动 Caddy 申请证书需要几十秒。若一直失败，检查域名解析是否生效、80 端口是否放开。

### 6. 验证

```bash
curl https://pay.example.com/healthz     # 期望输出 ok
```

浏览器打开 `https://pay.example.com/admin/`，用 `admin` + `.env` 里的密码登录。

### 7. 配置支付渠道与商户

1. 「支付渠道 → 新建渠道」，按[渠道申请与配置](channels.md)填写参数。PayPal / Stripe 需要把页面上给出的回调地址填到其后台。
2. 「商户 → 新建商户」，把支付地址、商户 ID、商户密钥填进 new-api，详见[接入 new-api](new-api.md)。
3. 建议先用「模拟支付」渠道跑通一笔，确认 new-api 能正常到账后删除该渠道。

### 8. 设置自动备份

数据库里有订单和全部渠道密钥，务必备份：

```bash
sudo apt install -y sqlite3
sudo mkdir -p /opt/epay-backup

# 每天凌晨 3 点备份，保留 30 天。注意用追加的方式写 crontab，避免覆盖已有任务
( sudo crontab -l 2>/dev/null; \
  echo '0 3 * * * cd /opt/epay && /usr/bin/sqlite3 data/epay.db ".backup /opt/epay-backup/epay-$(date +\%F).db" && find /opt/epay-backup -name "epay-*.db" -mtime +30 -delete' \
) | sudo crontab -
```

> WAL 模式下必须用 `.backup`，直接 `cp` 可能拿到不一致的数据库。cron 里的 `%` 需要写成 `\%`。

### 9. 日常维护

```bash
cd /opt/epay
sudo git pull
sudo docker compose -f docker-compose.yml -f docker-compose.https.yml up -d --build   # 升级
```

为了少敲参数，可以在 shell 里加个别名：

```bash
echo "alias epaydc='docker compose -f /opt/epay/docker-compose.yml -f /opt/epay/docker-compose.https.yml'" >> ~/.bashrc
```

之后就能用 `epaydc ps`、`epaydc logs -f`、`epaydc restart`。

## 方式二：已有反向代理时的 Docker Compose

已经有 Nginx / Caddy 等反向代理时，用基础配置即可，网关监听 8080 交给你的代理转发（见下文[反向代理与 HTTPS](#反向代理与-https)）。

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

## 方式三：直接编译运行

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

两者选其一即可，它们不能同时占用 80 / 443 端口：

- **服务器上还没有反向代理** → 用上面「全新服务器从零部署」的 Caddy 方案，证书自动申请与续期，配置最少。
- **服务器上已经有 Nginx** → 继续用 Nginx，网关用基础的 `docker compose up -d` 启动并监听 8080，不必再引入 Caddy。

Caddy（自动申请证书，最省事）：

```caddyfile
pay.example.com {
    reverse_proxy 127.0.0.1:8080
}
```

Nginx：仓库里提供了现成的站点配置模板 [nginx-site.example.conf](../nginx-site.example.conf)，
已包含 HTTP 跳转 HTTPS、转发买家真实 IP 所需的请求头、管理后台前端的 gzip 压缩，
以及可选的后台 IP 白名单。

```bash
sudo cp nginx-site.example.conf /etc/nginx/sites-available/epay.conf
sudo ln -s /etc/nginx/sites-available/epay.conf /etc/nginx/sites-enabled/
sudo sed -i 's/pay.example.com/你的域名/g' /etc/nginx/sites-available/epay.conf

# 申请证书（会自动改写配置中的证书路径并配置自动续期）
sudo apt install -y certbot python3-certbot-nginx
sudo certbot --nginx -d 你的域名

sudo nginx -t && sudo systemctl reload nginx
```

> 不要对 `/notify/` 路径做 IP 白名单，否则收不到支付宝 / 微信 / PayPal / Stripe 的回调。
> 需要限制来源时只限制 `/admin/`，模板里给了注释好的写法。

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
