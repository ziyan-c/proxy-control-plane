# proxy-control-plane

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
- 根据订阅 token 输出 VLESS 订阅内容
- 支持 `raw` 和 base64 编码的 `v2ray` 订阅格式
- 记录管理操作审计日志
- 提供流量上报入口和流量记录表，为后续统计、限流、套餐能力打基础
- 启动时自动执行数据库迁移

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

当前 `docker-compose.yml` 会启动两个服务：

- `postgres`：PostgreSQL 17 数据库，保存客户、节点、账号、订阅、流量和审计数据
- `api`：Go 后端 API 服务，提供管理接口和订阅接口

默认端口：

- PostgreSQL：`127.0.0.1:5432`
- API：`127.0.0.1:8000`

注意：Docker 部署只会启动数据库和控制面 API，不会部署任何真实代理节点软件。

## 技术栈

- 语言：Go
- HTTP：标准库 `net/http`
- 数据库：PostgreSQL
- 数据库驱动：`github.com/jackc/pgx/v5`
- 迁移：项目内置 Go 迁移逻辑
- 测试：`go test`
- 部署：Docker / Docker Compose

## 目录结构

```text
cmd/server/              服务入口，支持 migrate 和 serve 命令
internal/config/         环境变量配置
internal/domain/         核心业务模型
internal/httpapi/        HTTP API、鉴权、中间件和响应处理
internal/security/       管理员 token、订阅 token、密码校验和哈希
internal/store/          PostgreSQL 访问和数据库迁移
internal/subscription/   VLESS 订阅生成逻辑
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
- `schema_migrations`：数据库迁移记录

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

准备配置：

```bash
cp .env.example .env
```

启动数据库：

```bash
docker compose up -d postgres
```

执行迁移：

```bash
make migrate
```

启动 API：

```bash
make run
```

健康检查：

```bash
curl http://127.0.0.1:8000/health
```

运行测试：

```bash
make test
```

## Docker 启动

```bash
docker compose up --build
```

API 服务运行在：

```text
http://127.0.0.1:8000
```

## 常用配置项

本项目使用 `PCP_` 前缀的环境变量：

- `PCP_APP_NAME`：应用名称
- `PCP_ENVIRONMENT`：运行环境
- `PCP_LISTEN_ADDR`：API 监听地址，默认 `:8000`
- `PCP_DATABASE_URL`：PostgreSQL 连接地址
- `PCP_ADMIN_EMAIL`：管理员邮箱
- `PCP_ADMIN_PASSWORD`：管理员密码，MVP 阶段可以使用环境变量明文引导
- `PCP_SECRET_KEY`：访问 token 签名密钥
- `PCP_ACCESS_TOKEN_EXPIRE_MINUTES`：管理员访问 token 有效期
- `PCP_AUTO_MIGRATE`：启动 API 时是否自动执行迁移

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
- PostgreSQL 数据模型和内置迁移
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
