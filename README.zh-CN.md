# proxy-control-plane

[English](README.md)

`proxy-control-plane` 是一个用 Go 实现的代理业务控制面。它不是代理服务器
本身，而是负责提供后端 API 和业务数据模型，用来管理客户、代理节点、代理账号、
订阅 token、订阅输出、流量记录和管理审计日志。

这个项目的核心设计是分层：本项目只管理动态业务状态，真实节点部署放到别的
基础设施工具里做。VPS 创建、Xray 或 sing-box 安装、TLS 证书、防火墙规则、
节点配置下发，都不应该塞进这个控制面里。

## 项目负责什么

- 管理客户、代理节点、代理账号、账号与节点绑定关系
- 创建和轮换订阅 token，数据库只保存 token 哈希值
- 输出 VLESS 订阅，支持 base64 `v2ray` 格式和原始 URI 格式
- 提供管理员登录和 Bearer token 保护的管理 API
- 记录管理操作审计日志
- 接收流量使用上报，为后续套餐、配额、限流打基础
- 通过 GORM 连接 PostgreSQL
- 在数据库账号权限允许时自动创建目标 PostgreSQL database
- 执行版本化 SQL migration，也可以在本地开发时保留 GORM `AutoMigrate`
- 提供 Cobra CLI，用来初始化配置、执行数据库迁移、启动 API、启动 Docker Compose

## 项目不负责什么

这个仓库不会部署真实代理节点。它不负责：

- 购买或创建 VPS
- 安装 Xray、sing-box、Nginx、Caddy 等服务器软件
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

Docker 不会启动或配置 Xray、sing-box、证书、防火墙、真实代理节点软件。

## 技术栈

- Go：部署简单，可以编译成单个二进制，启动快，适合长期运行的 API 服务
- Cobra：适合做分组清晰的命令行工具，帮助信息和参数体验更好
- Gin：轻量、成熟，适合这个 API 层
- PostgreSQL：可靠的关系型数据库，适合客户、账号、节点、订阅、流量、审计数据
- GORM：用模型驱动 CRUD，并保留开发阶段的 `AutoMigrate`
- SQL migration：用版本化 SQL 管理更可控的生产 schema 变更
- Docker Compose：用来稳定启动 API 容器

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
PCP_LISTEN_ADDR=:9710
PCP_DATABASE_URL=postgres://user:password@host:5432/proxy_control?sslmode=require
PCP_ADMIN_EMAIL=admin@proxy.example
PCP_ADMIN_PASSWORD=change-this-to-a-long-admin-password
PCP_SECRET_KEY=change-this-with-openssl-rand-base64-32-before-serving
PCP_AUTO_CREATE_DATABASE=true
PCP_AUTO_MIGRATE=false
```

远程 PostgreSQL 如果支持 SSL，建议用 `sslmode=require`。只有在可信内网或本机
测试时才用 `sslmode=disable`。
正常使用建议保持 `PCP_AUTO_MIGRATE=false`，这样服务启动时不会自动改表结构。
数据库结构变化统一用 `./proxy-control-plane db migrate`。只有你明确想在开发时
让服务启动前自动跑 GORM `AutoMigrate`，才临时改成 `true`。
服务会拒绝使用示例管理员邮箱、占位管理员密码、占位 secret key、少于 12 个字符
的管理员密码，或少于 32 个字符的 secret key 启动。

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

## CLI 命令

```bash
./proxy-control-plane config init
./proxy-control-plane db migrate
./proxy-control-plane db automigrate
./proxy-control-plane server serve
./proxy-control-plane docker up
```

常用参数：

```bash
./proxy-control-plane server serve --listen=:9710
./proxy-control-plane server serve --env-file=.local/app.env
./proxy-control-plane server serve --database-url='postgres://user:password@host:5432/proxy_control?sslmode=require'
./proxy-control-plane server serve --auto-create-database=true --auto-migrate=true
./proxy-control-plane db migrate --database-url='postgres://user:password@host:5432/proxy_control?sslmode=require'
./proxy-control-plane db migrate --migrations-dir=migrations
./proxy-control-plane db automigrate
./proxy-control-plane docker up --detach
```

只有想换配置目录时，才需要用 `--config-dir` 和 `--example-dir`：

```bash
./proxy-control-plane --config-dir=.local --example-dir=.local.example config init
```

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

- `GET /sub/{token}` 返回 base64 VLESS 订阅内容
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
- `audit_logs`
- `schema_migrations`：记录已经执行过的 SQL migration

## 基本操作流程

1. 配置 `.local/app.env`。
2. 执行 `./proxy-control-plane db migrate`。
3. 用 `./proxy-control-plane server serve` 或 `./proxy-control-plane docker up` 启动 API。
4. 通过 `POST /admin/login` 登录。
5. 创建客户、节点、代理账号、账号节点绑定、订阅 token。
6. 给客户端使用类似 `http://host:9710/sub/{token}` 的订阅地址。

## 验证

```bash
go test ./...
go vet ./...
docker compose -f docker-compose.yml config --quiet
```
