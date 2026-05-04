# proxy-control-plane

[English](README.md)

`proxy-control-plane` 是一个用 Go 实现的代理业务控制面服务。它不是代理服务器
本身，而是管理客户、代理节点、代理账号、订阅 token、订阅输出和业务数据的
后端 API。

这个项目的核心目标是把“业务控制面”和“真实代理节点部署”分开：本项目保存和
管理动态业务状态，真实 VPS、Xray、sing-box、证书、防火墙等基础设施由其他
运维工具负责。

## 这个项目负责什么

本项目负责：

- 管理客户信息：邮箱、显示名称、状态、过期时间
- 管理代理节点信息：节点名、域名、公网地址、地区、端口、传输方式、安全参数
- 管理代理账号：VLESS UUID、账号标识、启用状态、过期时间、流量上限
- 管理账号与节点之间的绑定关系
- 生成和轮换订阅 token，数据库只保存 token 哈希值
- 在配置允许且数据库用户有权限时，自动创建目标 PostgreSQL database
- 根据订阅 token 输出 VLESS 订阅内容
- 支持 `raw` 和 base64 编码的 `v2ray` 订阅格式
- 记录管理操作审计日志
- 提供流量上报入口和流量记录表，为后续统计、限流、套餐能力打基础
- 启动时自动执行 GORM `AutoMigrate`

## 这个项目不负责什么

本项目不负责：

- 不购买或创建 VPS
- 不安装 Xray、sing-box、Nginx、Caddy 等服务器软件
- 不配置系统防火墙、安全组或端口转发
- 不申请或续签 TLS 证书
- 不部署 WireGuard
- 不把配置自动下发到真实代理节点
- 不直接采集真实代理进程的流量

这些属于基础设施或节点运维层面的工作，可以由 `ansible-infra`、节点 agent、
CI/CD、Ansible、Terraform 或其他运维系统负责。

## 部署后会启动什么

`docker-compose.yml` 会启动两个本地服务：

- `postgres`：PostgreSQL 17 数据库，保存客户、节点、账号、订阅、流量和审计数据
- `api`：Go 后端 API 服务，提供管理接口和订阅接口

`docker-compose.remote-db.yml` 只启动 `api`，并通过 `.local/api.remote.env`
连接远程 PostgreSQL。

Docker 资源会显式加项目前缀，避免和其他项目重名：

- 项目名：`proxy-control-plane`
- API 容器：`proxy-control-plane_api`
- PostgreSQL 容器：`proxy-control-plane_postgres`
- PostgreSQL 数据卷：`proxy-control-plane_postgres-data`
- 网络：`proxy-control-plane_network`

默认端口：

- PostgreSQL：`127.0.0.1:5432`
- API：`127.0.0.1:9710`

注意：Docker 部署只会启动数据库和/或控制面 API，不会部署任何真实代理节点软件。

## 技术栈

- 语言：Go
- CLI：Cobra
- HTTP：Gin
- 数据库：PostgreSQL
- ORM：GORM
- 数据库驱动：`gorm.io/driver/postgres`
- 迁移：GORM `AutoMigrate`
- 测试：`go test`
- 部署：Docker / Docker Compose

## 目录结构

```text
cmd/proxy-control-plane/              很薄的 CLI 入口
internal/cli/            Cobra CLI 命令和本地配置初始化
internal/config/         环境变量配置
internal/domain/         核心业务模型
internal/httpapi/        Gin HTTP API、鉴权、中间件和响应处理
internal/security/       管理员 token、订阅 token、密码校验和哈希
internal/store/          基于 GORM 的 PostgreSQL 访问和数据库迁移
internal/subscription/   VLESS 订阅生成逻辑
.local.example/          会进入仓库的示例配置模板
.local/                  不进入仓库的本机私密配置
```

## 数据库

当前主要数据表：

- `customers`：客户
- `proxy_nodes`：代理节点
- `proxy_accounts`：代理账号
- `proxy_account_nodes`：代理账号与代理节点的多对多绑定关系
- `subscription_tokens`：订阅 token 记录，保存 token 哈希，不保存明文 token
- `traffic_usage`：流量使用记录
- `audit_logs`：管理操作审计日志

## 主要接口

健康检查：

- `GET /health`

管理员登录：

- `POST /admin/login`

客户管理：

- `GET /admin/customers`
- `POST /admin/customers`
- `GET /admin/customers/{id}`
- `PATCH /admin/customers/{id}`
- `DELETE /admin/customers/{id}`

节点管理：

- `GET /admin/nodes`
- `POST /admin/nodes`
- `GET /admin/nodes/{id}`
- `PATCH /admin/nodes/{id}`
- `DELETE /admin/nodes/{id}`

代理账号管理：

- `GET /admin/proxy-accounts`
- `POST /admin/proxy-accounts`
- `GET /admin/proxy-accounts/{id}`
- `PATCH /admin/proxy-accounts/{id}`
- `DELETE /admin/proxy-accounts/{id}`

订阅 token 管理：

- `GET /admin/subscription-tokens`
- `POST /admin/subscription-tokens`
- `GET /admin/subscription-tokens/{id}`
- `PATCH /admin/subscription-tokens/{id}`
- `POST /admin/subscription-tokens/{id}/rotate`

流量记录：

- `POST /admin/traffic-usage`

客户端订阅：

- `GET /sub/{token}`：返回 base64 编码后的 VLESS 订阅内容
- `GET /sub/{token}?fmt=raw`：返回原始 VLESS 链接文本

除 `/health`、`/admin/login` 和 `/sub/{token}` 外，管理接口都需要
`Authorization: Bearer <access_token>`。

## 基本使用流程

1. 管理员通过 `/admin/login` 登录，拿到 Bearer token。
2. 创建客户。
3. 创建一个或多个代理节点。
4. 为客户创建代理账号，并绑定可访问的节点。
5. 为客户创建订阅 token。
6. 客户端访问 `/sub/{token}` 获取订阅内容。

## 本地开发

构建 CLI：

```bash
go build -o proxy-control-plane ./cmd/proxy-control-plane
```

准备配置：

```bash
./proxy-control-plane config init
```

该命令会把 `.local.example/` 里的模板复制到 `.local/`，只创建缺失的文件：

```text
.local/api.local.env
.local/api.docker.env
.local/api.remote.env
.local/cli.env
.local/postgres.env
```

整个 `.local/` 目录都会被 Git 和 Docker 忽略。运行前请按本机情况编辑这些私密
配置。`config init` 主要是初始化配置的辅助命令；其他 CLI 命令在运行前也会自动
调用它。

默认数据库模式写在 `.local/cli.env`：

- `DB=local`：使用本地 Compose PostgreSQL
- `DB=remote`：使用 `.local/api.remote.env` 里的远程 PostgreSQL

命令行仍然可以临时覆盖本机默认值，例如
`./proxy-control-plane docker up --db=remote`。

本机开发时，只启动本地数据库：

```bash
docker compose up -d postgres
```

对配置里的数据库执行迁移：

```bash
./proxy-control-plane db migrate
```

在本机直接启动 API：

```bash
./proxy-control-plane server serve
```

健康检查：

```bash
curl http://127.0.0.1:9710/health
```

运行测试：

```bash
go test ./...
```

## Docker 启动

推荐的 Docker 入口就是一个命令：

```bash
./proxy-control-plane docker up
```

数据库模式来自 `.local/cli.env`。命令行仍然可以临时覆盖：

```bash
./proxy-control-plane docker up --db=local
./proxy-control-plane docker up --db=remote
```

`DB=local` 会启动 `postgres` 和 `api`；`DB=remote` 只启动 `api`，远程数据库地址
从 `.local/api.remote.env` 读取。

API 服务运行在：

```text
http://127.0.0.1:9710
```

## CLI 参数

常用例子：

```bash
./proxy-control-plane server serve --db=remote --listen=:9710
./proxy-control-plane db migrate --db=remote
./proxy-control-plane docker up --db=remote --detach
./proxy-control-plane server serve --env-file=.local/api.remote.env
```

`--db` 选择要读取哪组本地配置：

- `--db=local`：本机命令读取 `.local/api.local.env`；Docker 读取
  `.local/api.docker.env`
- `--db=remote`：读取 `.local/api.remote.env`

`--listen`、`--database-url`、`--auto-create-database`、`--auto-migrate` 这类
直接参数会覆盖 `.local/*.env` 里的值。如果模板目录不在 `.local.example/`，
可以用 `--example-dir` 指定。

## 常用配置项

示例配置统一放在 `.local.example/`；真实私密配置统一放在 `.local/`。

会进入仓库的模板文件：

- `.local.example/api.local.env`：本机执行 `server serve --db=local` 和 `db migrate --db=local` 使用的 API 配置模板
- `.local.example/api.docker.env`：`docker up --db=local` 使用的 API 容器配置模板
- `.local.example/api.remote.env`：`--db=remote` 使用的远程数据库 API 配置模板
- `.local.example/cli.env`：本机 CLI 默认值配置模板，包括默认 `DB` 模式
- `.local.example/postgres.env`：Docker Compose 里的 PostgreSQL 容器配置模板

不会进入仓库的真实配置目录：

- `.local/`

API 使用 `PCP_` 前缀的环境变量：

- `PCP_APP_NAME`：应用名称
- `PCP_ENVIRONMENT`：运行环境
- `PCP_LISTEN_ADDR`：API 监听地址，默认 `:9710`
- `PCP_DATABASE_URL`：PostgreSQL 连接地址，远程数据库是否使用 SSL 也通过
  这里的 `sslmode` 控制
- `PCP_ADMIN_EMAIL`：管理员邮箱
- `PCP_ADMIN_PASSWORD`：管理员密码，MVP 阶段可以使用环境变量明文引导
- `PCP_SECRET_KEY`：访问 token 签名密钥
- `PCP_ACCESS_TOKEN_EXPIRE_MINUTES`：管理员访问 token 有效期
- `PCP_AUTO_CREATE_DATABASE`：连接前是否自动创建目标 PostgreSQL database
- `PCP_AUTO_MIGRATE`：启动 API 时是否自动执行 GORM 表结构迁移

自动创建数据库的逻辑会先连接名为 `postgres` 的维护数据库，检查
`PCP_DATABASE_URL` 里的目标 database 是否存在；如果不存在，就执行
`CREATE DATABASE`。配置里的 PostgreSQL 用户必须有创建数据库的权限。

GORM `AutoMigrate` 是表结构迁移步骤：它会根据 Go model 自动创建或更新表、
字段、索引和约束。它不负责创建 PostgreSQL database 本身；database 创建由
`PCP_AUTO_CREATE_DATABASE` 单独负责。

PostgreSQL 使用：

- `POSTGRES_USER`：数据库用户
- `POSTGRES_PASSWORD`：数据库密码
- `POSTGRES_DB`：数据库名

## 订阅参数能力

节点模型已经预留常见 VLESS 参数：

- `transport`
- `security`
- `sni`
- `fingerprint`
- `alpn`
- `path`
- `host_header`
- `reality_public_key`
- `reality_short_id`

订阅生成时会把这些字段转换成 VLESS URI 查询参数。

## 当前状态

当前版本已经从 Python/FastAPI MVP 重构为 Go 服务，并补齐了更完整的控制面骨架：

- Go API 服务
- PostgreSQL 数据模型和 GORM 迁移
- 管理员登录和 Bearer token 鉴权
- 客户、节点、代理账号、订阅 token 的基础 CRUD
- 订阅 token 轮换
- VLESS 订阅生成
- 流量记录入口
- 审计日志
- Docker 和 CI

还没有实现的内容：

- 真实节点配置下发
- 节点 agent
- 自动安装或更新 Xray/sing-box
- 套餐计费
- 流量超额自动禁用
- Web 管理后台页面

这些后续可以作为独立模块逐步接入，避免控制面和基础设施部署逻辑混在一起。
