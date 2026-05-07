# proxy-control-plane

[English](README.md)

`proxy-control-plane` 是一个用 Go 实现的代理业务控制面。它不是代理服务器
本身，而是负责提供后端 API 和业务数据模型，用来管理客户、代理节点、代理账号、
订阅 token、订阅输出、流量记录和管理审计日志。

这个项目的核心设计是分层：本项目只管理动态业务状态，真实节点部署放到别的
基础设施工具里做。VPS 创建、Xray、V2Ray 或 sing-box 安装、TLS 证书、防火墙规则、
节点配置下发，都不应该塞进这个控制面里。

## 项目负责什么

- 管理客户、代理节点、代理账号、账号与节点绑定关系
- 创建和轮换订阅 token，数据库只保存 token 哈希值
- 输出 VLESS 订阅，支持 base64 和原始 URI 格式
- 提供管理员登录和 Bearer token 保护的管理 API
- 记录管理操作审计日志
- 通过 Xray StatsService 汇总用户流量，为后续套餐、配额、限流打基础
- 可选地通过 Xray gRPC API 同步本项目托管的 Xray runtime 用户
- 通过 GORM 连接 PostgreSQL
- 在数据库账号权限允许时自动创建目标 PostgreSQL database
- 执行版本化 SQL migration，也可以在本地开发时保留 GORM `AutoMigrate`
- 提供 Cobra CLI，用来初始化配置、执行数据库迁移、启动 API、启动 Docker Compose

## 项目不负责什么

这个仓库不会部署真实代理节点。它不负责：

- 购买或创建 VPS
- 安装 Xray、V2Ray、sing-box、Nginx、Caddy 等服务器软件
- 配置系统防火墙、云安全组或端口转发
- 申请或续签 TLS 证书
- 部署 WireGuard
- 把节点配置下发到真实服务器
- 直接从真实代理进程采集流量

这些事情更适合放到节点 agent、Ansible、Terraform、CI/CD 或另一个基础设施项目里。

## 部署后会启动什么

`docker-compose.yml` 现在只启动一个服务：

- `api`：Go 控制面 API

PostgreSQL 被设计成外部依赖，不再混在这个 Compose 里。它可以是远程专用
PostgreSQL、云数据库，也可以是你自己另外启动的本机 PostgreSQL。API 会从
`.local/app.env` 读取数据库连接地址。

如果 `PCP_AUTO_CREATE_DATABASE=true`，服务会先尝试连接目标 database。目标
database 不存在时，它会连接同一台 PostgreSQL 的 `postgres` 维护库，然后执行
`CREATE DATABASE <target>`。这要求数据库账号有足够权限。schema 管理路径是显式
执行 `./proxy-control-plane db migrate`，它会按顺序执行 `migrations/` 里的 SQL
文件，并把已执行版本记录到 `schema_migrations`。GORM `AutoMigrate` 仍然保留为
开发辅助命令：`./proxy-control-plane db automigrate`。

Docker 名字都加了项目前缀，避免和其他项目冲突：

- Compose 项目名：`proxy-control-plane`
- API 镜像：`proxy-control-plane_api:local`
- API 容器：`proxy-control-plane_api`
- 网络：`proxy-control-plane_network`

默认 API 端口：

- `127.0.0.1:9710`

Docker 不会启动或配置 Xray、V2Ray、sing-box、证书、防火墙、真实代理节点软件。

## Xray 节点

这个控制面现在会和旁边 `ansible-infra` 项目的两类节点分组对齐：

- `runtime=xray`：Xray VLESS Reality 节点，通常是公网 `443/tcp`
- `runtime=xray`：Xray VLESS WebSocket 节点，跑在 Caddy 后面，通常是公网
  `443/tcp`，WebSocket 路径是 `/v2ray`
- `runtime=custom`：不直接对应这两个 Ansible role 的自定义节点

现在支持的 runtime 值就是 `xray` 和 `custom`。旧的 `runtime=v2ray` 记录会迁移成
`xray`，因为 Caddy 后面的 WebSocket 后端现在也已经换成 Xray。

这里的节点记录保存的是客户端订阅要用的公网参数，不是服务端内部私密参数。比如
Xray under Caddy 在容器里监听的是内部 `10010`，但客户端经过 Caddy 访问时，订阅里通常应该是
`security=tls`、`transport=ws`、`path=/v2ray` 和公网域名。Xray Reality 节点要填
`reality_public_key`，这里要放 Reality 公钥，不要把 Ansible 里的服务端私钥写进
控制面数据库。

Xray under Caddy 节点示例：

```json
{
  "name": "xray-under-caddy-la-1",
  "runtime": "xray",
  "hostname": "gfw-la-us.example.com",
  "public_host": "gfw-la-us.example.com",
  "port": 443,
  "transport": "ws",
  "security": "tls",
  "path": "/v2ray",
  "host_header": "gfw-la-us.example.com"
}
```

Xray Reality 节点示例：

```json
{
  "name": "xray-fr-1",
  "runtime": "xray",
  "hostname": "node.example.com",
  "public_host": "node.example.com",
  "port": 443,
  "transport": "tcp",
  "security": "reality",
  "sni": "www.example.com",
  "fingerprint": "chrome",
  "reality_public_key": "<xray-reality-public-key>",
  "reality_short_id": "<short-id>"
}
```

Ansible 部署完节点后，应该调用控制面 API 注册节点，而不是直接写 PostgreSQL。
这个同步接口是非破坏式的：请求里的节点会按 `name` 创建或更新，请求里没出现的
节点不会被自动删除或禁用。

```bash
curl -X POST http://127.0.0.1:9710/admin/nodes/sync \
  -H "Authorization: Bearer <access_token>" \
  -H "Content-Type: application/json" \
  -d '{
    "nodes": [
      {
        "name": "xray-fr-1",
        "runtime": "xray",
        "hostname": "node.example.com",
        "public_host": "node.example.com",
        "port": 443,
        "transport": "tcp",
        "security": "reality",
        "sni": "www.example.com",
        "fingerprint": "chrome",
        "reality_public_key": "<xray-reality-public-key>",
        "reality_short_id": "<short-id>"
      }
    ]
  }'
```

如果 `enabled` 没传，已有节点会保持原来的启用状态，新节点默认启用。

在 runtime sync 接管之前，可以先把现有 Xray `config.json` 里的静态 clients 导入
PostgreSQL。节点必须已经在 PostgreSQL 里存在，通常先通过 Ansible node sync 注册：

```bash
mkdir -p .local/imports
scp root@node.example.com:/opt/xray-under-caddy/config.json .local/imports/xray-under-caddy-la-1.json

./proxy-control-plane import xray-config \
  --node xray-under-caddy-la-1 \
  --file .local/imports/xray-under-caddy-la-1.json
```

这个导入命令可以重复执行。它会创建或复用
`legacy-public@proxy-control-plane.local` 这个 legacy customer，为 config 里的静态
VLESS client 创建缺失的 proxy account，并绑定到指定节点。它不会把 Xray 的
`email` 字段当客户邮箱用。如果同一个 UUID 在不同 config 里出现了不同 `flow`，命令
会失败，因为当前数据模型的 flow 是存在 proxy account 上的。

导入静态 client 以后，把节点 `config.json` 里的静态 users 清空并重启/重载 Xray
容器。runtime sync 会再从 PostgreSQL 添加 `pcp-*` 托管用户。之后新增、删除、禁用
用户都以 control plane 和 PostgreSQL 为准，不再依赖节点本地 JSON。

现有 Caddy public 订阅文件也可以导入。文件本身先保留在 Caddy 上作为临时兼容，
control plane 则把导入出来的用户当作 legacy customer 下的普通 proxy account
管理，并通过普通 `/sub/{token}` 生成新的托管订阅：

```bash
./proxy-control-plane import subscription-file \
  --file .local/imports/legacy-public.txt
```

导入器支持原始 VLESS URI 列表，也支持 base64 编码的订阅文件。它会创建或复用同一个
legacy customer，导入缺失的 VLESS account，并按 public host、端口、transport、
security、path、Reality 参数匹配已有 `proxy_nodes` 记录来建立节点绑定。如果这个
customer 还没有订阅 token，它会自动创建一个普通订阅 token。已有 Caddy public 文件
不会被修改或删除。这个命令可以重复执行；已有 token 会保留，因为数据库只保存 token
hash，旧 token 明文不能重新打印。

Ansible 开启 runtime API 后，节点同步也可以带上：

```json
{
  "runtime_api_enabled": true,
  "runtime_api_host": "10.66.0.1",
  "runtime_api_port": 10085,
  "runtime_inbound_tag": "proxy-control-plane-vless-in"
}
```

这些字段记录控制面应该如何访问 Xray gRPC API。设置
`PCP_RUNTIME_SYNC_ENABLED=true` 后，服务会周期性读取 Xray 当前 runtime 用户，
和 PostgreSQL 里这个节点应该拥有的用户列表算 hash 比较。hash 一样就只更新同步
时间，hash 不一样才做 `AddUser`/`RemoveUser` diff；常规流程不会定期 full
reconcile。同步器只管理本项目生成 email 的 runtime 用户，比如
`pcp-<proxy_account_id>@proxy-control-plane`；原本写在 Xray config 里的静态用户、
或者你手工加的旧用户，不会被这个项目删除。这里的 Xray `email` 是
`AddUser`/`RemoveUser` 使用的 runtime 身份键，不是客户联系邮箱；客户邮箱仍然保存在
PostgreSQL 的 `customers` 表里。

流量汇总也使用同一个 Xray gRPC endpoint，但调用的是 `StatsService`，不是
`HandlerService`。Xray 必须启用 `stats: {}`，并在 policy level `0` 上打开
`statsUserUplink/statsUserDownlink`；Ansible 的 Xray roles 在
`proxy_control_plane_runtime_api_enabled=true` 时会渲染这些配置。设置
`PCP_TRAFFIC_SYNC_ENABLED=true` 后，服务会查询本项目托管用户的计数器，比如
`user>>>pcp-<proxy_account_id>@proxy-control-plane>>>traffic>>>uplink`，读取后重置，
再把这段时间的增量写入 `traffic_usage`。这个项目后续不以 VMess 为目标；托管运行时用户
和生成订阅都按 VLESS 走。

## 技术栈

- Go：部署简单，可以编译成单个二进制，启动快，适合长期运行的 API 服务
- Cobra：适合做分组清晰的命令行工具，帮助信息和参数体验更好
- Gin：轻量、成熟，适合这个 API 层
- PostgreSQL：可靠的关系型数据库，适合客户、账号、节点、订阅、流量、审计数据
- GORM：用模型驱动 CRUD，并保留开发阶段的 `AutoMigrate`
- SQL migration：用版本化 SQL 管理更可控的生产 schema 变更
- Docker Compose：用来稳定启动 API 容器
- Xray gRPC API：`HandlerService` 做运行时用户同步，`StatsService` 做流量汇总

这个技术栈的取向是运维简单：项目既可以作为一个 Go 二进制直接跑，也可以作为
一个 Docker 容器跑；PostgreSQL 是明确的外部依赖。

## 目录结构

```text
cmd/proxy-control-plane/  很薄的 CLI 入口
internal/cli/             Cobra 命令和本地配置初始化
internal/config/          基于环境变量的配置加载
internal/domain/          核心业务模型
internal/httpapi/         Gin API、鉴权中间件、处理函数和响应
internal/security/        管理员 token、订阅 token、密码校验、哈希
internal/store/           GORM PostgreSQL 访问和迁移
internal/subscription/    VLESS 订阅生成逻辑
internal/trafficsync/     Xray StatsService 流量采集
migrations/               版本化 SQL 数据库迁移
.local.example/           进入仓库的示例配置
.local/                   不进入仓库的私密配置
```

## 配置

现在配置模型故意收敛成一个文件：

```text
.local.example/app.env  示例配置，进入 Git
.local/app.env          真实私密配置，被 Git 和 Docker 忽略
```

创建私密配置：

```bash
./proxy-control-plane config init
```

然后编辑 `.local/app.env`。

关键配置项：

```env
PCP_LISTEN_ADDR=0.0.0.0:9710
PCP_DATABASE_URL=postgres://user:password@host:5432/proxy_control?sslmode=require
PCP_ADMIN_EMAIL=admin@proxy.example
PCP_ADMIN_PASSWORD=change-this-to-a-long-admin-password
PCP_SECRET_KEY=change-this-with-openssl-rand-base64-32-before-serving
PCP_DATABASE_ENCRYPTION_KEY=base64-encoded-32-byte-key
PCP_AUTO_CREATE_DATABASE=true
PCP_AUTO_MIGRATE=false
PCP_RUNTIME_SYNC_ENABLED=false
PCP_RUNTIME_SYNC_INTERVAL=5m
PCP_RUNTIME_SYNC_TIMEOUT=30s
PCP_RUNTIME_SYNC_CONCURRENCY=3
PCP_TRAFFIC_SYNC_ENABLED=false
PCP_TRAFFIC_SYNC_INTERVAL=10m
PCP_TRAFFIC_SYNC_TIMEOUT=30s
PCP_TRAFFIC_SYNC_CONCURRENCY=3
PCP_MAINTENANCE_CLEANUP_ENABLED=false
PCP_MAINTENANCE_CLEANUP_INTERVAL=24h
PCP_MAINTENANCE_TRAFFIC_RETENTION=7d
PCP_MAINTENANCE_TRAFFIC_DAILY_RETENTION=30d
PCP_MAINTENANCE_AUDIT_RETENTION=90d
```

远程 PostgreSQL 如果支持 SSL，建议用 `sslmode=require`。只有在可信内网或本机
测试时才用 `sslmode=disable`。
正常使用建议保持 `PCP_AUTO_MIGRATE=false`，这样服务启动时不会自动改表结构。
数据库结构变化统一用 `./proxy-control-plane db migrate`。只有你明确想在开发时
让服务启动前自动跑 GORM `AutoMigrate`，才临时改成 `true`。
服务会拒绝使用示例管理员邮箱、占位管理员密码、占位 secret key、少于 12 个字符
的管理员密码，或少于 32 个字符的 secret key 启动。

`PCP_DATABASE_ENCRYPTION_KEY` 是可选但推荐配置的数据库敏感列加密密钥。它必须是
base64 编码的 32 字节 key，可以用 `openssl rand -base64 32` 生成。配置以后，新建
的 subscription token 仍然用 `token_hash` 验证，同时会把 token 明文用 AES-256-GCM
加密后存入数据库字段，方便未来客户 App 登录后重新展示订阅链接。旧的 hash-only token
仍然能用，但除非 rotate 或重建，否则无法恢复明文。

runtime sync 默认关闭。等 Ansible 已经把 Xray 节点的
`runtime_api_enabled`、`runtime_api_host`、`runtime_api_port` 和
`runtime_inbound_tag` 注册进控制面后，再设置 `PCP_RUNTIME_SYNC_ENABLED=true`。
同步逻辑会用 `GetInboundUsers` 读取节点用户，hash 一致就跳过，只有不一致才 diff。
Xray config 里的静态用户不会被控制面清理。不要把真实客户联系邮箱写进 Xray runtime
`email` 字段；客户身份放 PostgreSQL，Xray runtime key 由控制面生成。

traffic sync 默认也关闭。等 Xray 容器已经重新部署，并且启用了 `StatsService`、
`stats: {}` 和 user stats policy 后，再设置 `PCP_TRAFFIC_SYNC_ENABLED=true`。
默认 10 分钟采集一次，单次 API 超时 30 秒。采集器使用 Xray 的 reset 模式，所以每次
写进 `traffic_usage` 的都是上一次成功读取之后产生的流量增量。

maintenance cleanup 默认也关闭。如果希望 API 服务进程自己定期聚合旧流量明细并删除
旧行，可以设置 `PCP_MAINTENANCE_CLEANUP_ENABLED=true`。默认 24 小时跑一次，清理
策略默认是：

- `traffic_usage`：7 天
- `traffic_usage_daily`：30 天
- `audit_logs`：90 天

运行时优先级是：

1. 命令行直接参数，比如 `--database-url`、`--listen`
2. `.local/app.env` 里的配置
3. 代码里的开发默认值

## 本地开发

构建 CLI：

```bash
go build -o proxy-control-plane ./cmd/proxy-control-plane
```

初始化并编辑配置：

```bash
./proxy-control-plane config init
$EDITOR .local/app.env
```

执行版本化 SQL 迁移：

```bash
./proxy-control-plane db migrate
```

开发模型时，也可以直接执行 GORM AutoMigrate：

```bash
./proxy-control-plane db automigrate
```

在本机启动 API：

```bash
./proxy-control-plane server serve
```

健康检查：

```bash
curl http://127.0.0.1:9710/health
```

## Docker 启动

启动 API 容器：

```bash
./proxy-control-plane docker up
```

常用参数：

```bash
./proxy-control-plane docker up --detach
./proxy-control-plane docker up --build=false
```

Docker 命令默认读取 `.local/app.env`，并通过 `PCP_APP_ENV_FILE` 传给 Compose。
容器内部实际执行：

```bash
/app/proxy-control-plane server serve --no-local-config
```

也就是说，Compose 负责注入环境变量，容器不会再去镜像内部读取 `.local/`。
所有 `PCP_*` 运行配置都放在 env file 里；Compose 只描述容器和端口绑定。
Docker 场景下 env file 里的 `PCP_LISTEN_ADDR` 应该写成 `0.0.0.0:9710`，
再通过 Compose 的 `ports` 绑定限制宿主机暴露范围。Compose 使用
`restart: unless-stopped`，所以机器重启或容器异常退出后，Docker 会自动拉起
API 容器，除非你明确 stop 它。

## GitHub Container Registry 发布

GitHub Actions 只会在推送版本 tag 时把 container image 发布到 GitHub Container
Registry。它使用内置的 `GITHUB_TOKEN`，不需要配置 Docker Hub secret。

然后创建并推送 semver tag：

```bash
git tag v0.1.1
git push origin v0.1.1
```

发布 workflow 会构建并推送：

- `ghcr.io/ziyan-c/proxy-control-plane:0.1.1`
- `ghcr.io/ziyan-c/proxy-control-plane:0.1`
- `ghcr.io/ziyan-c/proxy-control-plane:0`
- 非 prerelease 版本会额外推 `ghcr.io/ziyan-c/proxy-control-plane:latest`

## CLI 命令

```bash
./proxy-control-plane config init
./proxy-control-plane db migrate
./proxy-control-plane db automigrate
./proxy-control-plane import xray-config --node xray-under-caddy-la-1 --file .local/imports/xray-under-caddy-la-1.json
./proxy-control-plane import subscription-file --file .local/imports/legacy-public.txt
./proxy-control-plane subscription token ensure --customer-email legacy-public@proxy-control-plane.local --name legacy-public --output-file .local/generated/legacy-public-subscription.txt
./proxy-control-plane subscription token encrypt --token-file .local/generated/legacy-public-subscription.txt
./proxy-control-plane maintenance cleanup
./proxy-control-plane server serve
./proxy-control-plane docker up
```

常用参数：

```bash
./proxy-control-plane server serve --listen=127.0.0.1:9710
./proxy-control-plane server serve --env-file=.local/app.env
./proxy-control-plane server serve --database-url='postgres://user:password@host:5432/proxy_control?sslmode=require'
./proxy-control-plane server serve --auto-create-database=true --auto-migrate=true
./proxy-control-plane server serve --runtime-sync=true --runtime-sync-interval=5m
./proxy-control-plane server serve --traffic-sync=true --traffic-sync-interval=10m
./proxy-control-plane server serve --maintenance-cleanup=true --maintenance-cleanup-interval=24h
./proxy-control-plane import xray-config --node xray-under-caddy-la-1 --file .local/imports/xray-under-caddy-la-1.json
./proxy-control-plane import xray-config --node xray-fr-1 --file .local/imports/xray-fr-1.json --dry-run
./proxy-control-plane import subscription-file --file .local/imports/legacy-public.txt --dry-run
./proxy-control-plane import subscription-file --file .local/imports/legacy-public.txt --create-subscription-token=false
./proxy-control-plane subscription token ensure --customer-email legacy-public@proxy-control-plane.local --name legacy-public --output-file .local/generated/legacy-public-subscription.txt
./proxy-control-plane subscription token encrypt --token-file .local/generated/legacy-public-subscription.txt
./proxy-control-plane maintenance cleanup --dry-run
./proxy-control-plane maintenance cleanup
./proxy-control-plane maintenance cleanup --traffic-retention=7d --traffic-daily-retention=30d --audit-retention=90d
./proxy-control-plane db migrate --database-url='postgres://user:password@host:5432/proxy_control?sslmode=require'
./proxy-control-plane db migrate --migrations-dir=migrations
./proxy-control-plane db automigrate
./proxy-control-plane docker up --detach
```

只有想换配置目录时，才需要用 `--config-dir` 和 `--example-dir`：

```bash
./proxy-control-plane --config-dir=.local --example-dir=.local.example config init
```

## 维护清理

`traffic_usage` 是明细表，设计上会持续追加记录，这样最近的数据可以精确追踪。
长期历史放在 `traffic_usage_daily`，每个账号、节点、日期一行。cleanup 只按保留天数
清理：

- `traffic_usage`：7 天
- `traffic_usage_daily`：30 天
- `audit_logs`：90 天

先用 dry-run 看会影响多少数据：

```bash
./proxy-control-plane maintenance cleanup \
  --audit-retention=90d \
  --traffic-retention=7d \
  --traffic-daily-retention=30d \
  --dry-run
```

确认以后再真正写库。不带 `--dry-run` 就会写 PostgreSQL：

```bash
./proxy-control-plane maintenance cleanup \
  --audit-retention=90d \
  --traffic-retention=7d \
  --traffic-daily-retention=30d
```

正常部署时，也可以设置 `PCP_MAINTENANCE_CLEANUP_ENABLED=true`，让 API 服务进程
自己周期性执行同一套 cleanup。手动命令保留给 dry-run、临时清理，以及改 retention
参数前先确认影响范围。

写库时会在一个数据库事务里完成：先把超过明细保留期的 `traffic_usage` 聚合进
`traffic_usage_daily`，再删除这些明细。然后按日汇总保留期清理
`traffic_usage_daily`，按审计保留期清理 `audit_logs`。PostgreSQL 删除后物理文件
不一定立刻变小，但释放出的空间会被后续写入复用。

## 主要接口

健康检查：

- `GET /health`

管理员：

- `POST /admin/login`

客户：

- `GET /admin/customers`
- `POST /admin/customers`
- `GET /admin/customers/{id}`
- `PATCH /admin/customers/{id}`
- `DELETE /admin/customers/{id}`

代理节点：

- `GET /admin/nodes`
- `POST /admin/nodes`
- `POST /admin/nodes/sync`
- `GET /admin/nodes/{id}`
- `PATCH /admin/nodes/{id}`
- `DELETE /admin/nodes/{id}`

代理账号：

- `GET /admin/proxy-accounts`
- `POST /admin/proxy-accounts`
- `GET /admin/proxy-accounts/{id}`
- `PATCH /admin/proxy-accounts/{id}`
- `DELETE /admin/proxy-accounts/{id}`

订阅 token：

- `GET /admin/subscription-tokens`
- `POST /admin/subscription-tokens`
- `GET /admin/subscription-tokens/{id}`
- `PATCH /admin/subscription-tokens/{id}`
- `POST /admin/subscription-tokens/{id}/rotate`

流量：

- `POST /admin/traffic-usage`

客户端订阅：

- `GET /sub/{token}` 或 `GET /sub/{token}?fmt=base64` 返回 base64 VLESS 订阅内容
- `GET /sub/{token}?fmt=raw` 返回原始 VLESS URI 文本

除 `/health`、`/admin/login` 和 `/sub/{token}` 外，管理接口都需要：

```text
Authorization: Bearer <access_token>
```

## 数据库表

- `customers`
- `proxy_nodes`
- `proxy_accounts`
- `proxy_account_nodes`
- `subscription_tokens`
- `traffic_usage`
- `traffic_usage_daily`
- `audit_logs`
- `schema_migrations`：记录已经执行过的 SQL migration

## 基本操作流程

1. 配置 `.local/app.env`。
2. 执行 `./proxy-control-plane db migrate`。
3. 用 `./proxy-control-plane server serve` 或 `./proxy-control-plane docker up` 启动 API。
4. 通过 `POST /admin/login` 登录。
5. 通过 Ansible node sync 注册节点，或通过 API 创建节点。
6. 如需接管老 config，执行 `./proxy-control-plane import xray-config` 导入静态用户。
7. 如需接管老 public 订阅文件，执行 `./proxy-control-plane import subscription-file`；
   它会在需要时给 legacy customer 创建普通订阅 token。
8. 如果 legacy customer 已经导入过但还没有 token，执行
   `./proxy-control-plane subscription token ensure --customer-email ... --output-file .local/generated/...`。
9. 创建客户、节点、代理账号、账号节点绑定、订阅 token。
10. 给客户端使用类似 `http://host:9710/sub/{token}` 的订阅地址。
11. 设置 `PCP_MAINTENANCE_CLEANUP_ENABLED=true`，或定期执行
    `./proxy-control-plane maintenance cleanup`，聚合旧流量明细并清理旧审计日志。

## 验证

```bash
go test ./...
go vet ./...
docker compose -f docker-compose.yml config --quiet
```
