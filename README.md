# proxy-control-plane

[简体中文](README.zh-CN.md)

`proxy-control-plane` is a Go business control plane for a proxy service. It is
not a proxy server. It provides the backend API and data model for customers,
proxy nodes, proxy accounts, subscription tokens, subscription output, traffic
records, and admin audit logs.

The main design choice is separation: this project manages dynamic business
state, while real node provisioning is handled elsewhere. VPS creation, Xray,
V2Ray, or sing-box installation, TLS certificates, firewall rules, and
node-side config delivery should be owned by separate infrastructure tooling.

## What It Does

- Manages customers, proxy nodes, proxy accounts, and account-to-node bindings
- Creates and rotates subscription tokens while storing only token hashes
- Generates VLESS subscription output in base64 `v2ray` format or raw URI format
- Provides admin login and Bearer-token protected management APIs
- Records admin audit logs
- Accepts traffic usage reports for future quota and plan enforcement
- Optionally reconciles managed Xray runtime users through the Xray gRPC API
- Connects to PostgreSQL through GORM
- Can create the target PostgreSQL database when the configured role is allowed
- Applies versioned SQL migrations and can optionally run GORM `AutoMigrate`
  during local development
- Provides a Cobra CLI for config bootstrap, database migration, API serving,
  and Docker Compose startup

## What It Does Not Do

This repository does not deploy real proxy nodes. It does not:

- Buy or create VPS instances
- Install Xray, V2Ray, sing-box, Nginx, Caddy, or other server software
- Configure system firewalls, cloud security groups, or port forwarding
- Issue or renew TLS certificates
- Deploy WireGuard
- Push generated node configuration to real servers
- Collect traffic directly from running proxy processes

Those pieces should be implemented by a node agent, Ansible, Terraform, CI/CD,
or another infrastructure project.

## Deployment Shape

`docker-compose.yml` starts one service:

- `api`: the Go control-plane API

PostgreSQL is intentionally external to this Compose stack. It can be a remote
dedicated PostgreSQL server, a managed database, or a local PostgreSQL instance
that you run separately. The API reads its database URL from `.local/app.env`.

If `PCP_AUTO_CREATE_DATABASE=true`, the service first tries to connect to the
configured target database. If that database does not exist, it connects to the
same PostgreSQL server's `postgres` maintenance database and runs
`CREATE DATABASE <target>`. This only works when the database role has enough
permission. The schema path is `./proxy-control-plane db migrate`, which applies
ordered SQL files from `migrations/` and records completed versions in
`schema_migrations`. GORM `AutoMigrate` remains available as an explicit
development helper through `./proxy-control-plane db automigrate`.

Docker names are prefixed to avoid collisions:

- Compose project: `proxy-control-plane`
- API image: `proxy-control-plane_api:local`
- API container: `proxy-control-plane_api`
- Network: `proxy-control-plane_network`

Default API port:

- `127.0.0.1:9710`

Docker does not start or configure Xray, sing-box, certificates,
firewalls, or real proxy-node software.

## Xray Nodes

This control plane now mirrors the two node groups used by the companion
`ansible-infra` project:

- `runtime=xray`: Xray VLESS Reality nodes, usually public `443/tcp`
- `runtime=xray`: Xray VLESS WebSocket nodes under Caddy, usually public
  `443/tcp` with WebSocket path `/v2ray`
- `runtime=custom`: a generic node when the runtime is managed outside those
  two Ansible roles

Supported runtime values are `xray` and `custom`. Legacy `runtime=v2ray`
records are migrated to `xray` because the WebSocket-under-Caddy backend is now
Xray too.

The node record stores client-facing subscription values, not the private
server-only values. For Xray under Caddy, that means the subscription usually
uses `security=tls`, `transport=ws`, `path=/v2ray`, and the public domain even
though the Xray container itself listens on internal port `10010`. For Xray
Reality, use the Reality public key in `reality_public_key`; do not put the
server private key from Ansible into this database.

Example Xray-under-Caddy node:

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

Example Xray Reality node:

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

Ansible should register deployed nodes through the control-plane API instead of
writing PostgreSQL directly. The sync endpoint is non-destructive: nodes in the
request are created or updated by `name`, while nodes omitted from the request
are left unchanged.

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

If `enabled` is omitted, existing nodes keep their current enabled state and new
nodes default to enabled.

Existing static clients can be imported from copied Xray `config.json` files
before runtime sync takes over. The node must already exist in PostgreSQL,
usually through Ansible node sync:

```bash
mkdir -p .local/imports
scp root@node.example.com:/opt/xray-under-caddy/config.json .local/imports/xray-under-caddy-la-1.json

./proxy-control-plane import xray-config \
  --node xray-under-caddy-la-1 \
  --file .local/imports/xray-under-caddy-la-1.json
```

The import is idempotent. It creates or reuses the legacy customer
`legacy-public@proxy-control-plane.local`, creates missing VLESS proxy accounts
for static clients, and binds them to the named node. It does not use Xray's
`email` field as a customer email. If the same UUID appears with different
`flow` values across configs, the command fails because the current data model
stores flow on the proxy account.

During migration, runtime sync treats a matching unmanaged static Xray user
with the same UUID and flow as already present, so it does not duplicate-add the
legacy account. To make a legacy user fully removable by PostgreSQL, remove that
static client from node config after confirming runtime sync has added the
managed user.

When Ansible enables the runtime API, node sync can also send:

```json
{
  "runtime_api_enabled": true,
  "runtime_api_host": "10.66.0.1",
  "runtime_api_port": 10085,
  "runtime_inbound_tag": "proxy-control-plane-vless-in"
}
```

These fields record how the control plane reaches the Xray gRPC API. When
`PCP_RUNTIME_SYNC_ENABLED=true`, the server periodically reads runtime users
from Xray, compares their hash with the target users in PostgreSQL, and only
calls `AddUser`/`RemoveUser` when the hashes differ. Routine sync is an inspect
and diff flow, not a periodic full reconcile. The syncer only manages runtime
users whose email is generated by this project, such as
`pcp-<proxy_account_id>@proxy-control-plane`; static config users or manually
added legacy users are left alone. In Xray this field is the runtime identity
used by `AddUser`/`RemoveUser`, not the customer contact email; customer email
stays in PostgreSQL on the `customers` table.

## Tech Stack

- Go: simple static deployment, fast startup, strong standard library, and a
  good fit for long-running API services
- Cobra: grouped CLI commands with clear flags and help output
- Gin: lightweight, mature HTTP routing and middleware for the API layer
- PostgreSQL: durable relational storage for customers, accounts, nodes,
  tokens, usage, and audit data
- GORM: model-driven CRUD and optional development `AutoMigrate`
- SQL migrations: versioned schema changes for production-style deployments
- Docker Compose: repeatable local or server startup for the API container

The current stack favors operational simplicity. It keeps the app deployable as
a single Go binary or as one Docker container, while PostgreSQL remains an
explicit dependency.

## Project Structure

```text
cmd/proxy-control-plane/  Thin CLI entrypoint
internal/cli/             Cobra commands and local config bootstrap
internal/config/          Environment-based configuration loading
internal/domain/          Core business models
internal/httpapi/         Gin API, auth middleware, handlers, and responses
internal/security/        Admin tokens, subscription tokens, password checks, hashes
internal/store/           GORM PostgreSQL access and migrations
internal/subscription/    VLESS subscription generation
migrations/               Versioned SQL schema migrations
.local.example/           Tracked example configuration
.local/                   Ignored private configuration
```

## Configuration

The config model is intentionally one file:

```text
.local.example/app.env  tracked example
.local/app.env          private real config, ignored by Git and Docker
```

Create the private config file:

```bash
./proxy-control-plane config init
```

Then edit `.local/app.env`.

Important variables:

```env
PCP_LISTEN_ADDR=:9710
PCP_DATABASE_URL=postgres://user:password@host:5432/proxy_control?sslmode=require
PCP_ADMIN_EMAIL=admin@proxy.example
PCP_ADMIN_PASSWORD=change-this-to-a-long-admin-password
PCP_SECRET_KEY=change-this-with-openssl-rand-base64-32-before-serving
PCP_AUTO_CREATE_DATABASE=true
PCP_AUTO_MIGRATE=false
PCP_RUNTIME_SYNC_ENABLED=false
PCP_RUNTIME_SYNC_INTERVAL=5m
PCP_RUNTIME_SYNC_TIMEOUT=30s
PCP_RUNTIME_SYNC_CONCURRENCY=3
```

For a remote PostgreSQL server, use `sslmode=require` when the server supports
SSL. Use `sslmode=disable` only for trusted private networks or local testing.
Keep `PCP_AUTO_MIGRATE=false` for normal use so the server does not change table
structure during startup. Use `./proxy-control-plane db migrate` for schema
changes. `PCP_AUTO_MIGRATE=true` is only a development shortcut when you
intentionally want the server to run GORM `AutoMigrate` before serving.
The server refuses to start with the example admin email, placeholder admin
password, placeholder secret key, a password shorter than 12 characters, or a
secret key shorter than 32 characters.

Runtime sync is disabled by default. Enable `PCP_RUNTIME_SYNC_ENABLED=true`
after Ansible has registered Xray nodes with runtime API fields. The sync loop
uses `GetInboundUsers`, computes a managed-user hash, skips unchanged nodes, and
diffs only changed nodes. Static users that already live in Xray config are not
removed by this project. Do not store real customer contact emails in Xray's
runtime `email` field; use PostgreSQL for customer identity and let the control
plane generate the Xray runtime key.

Runtime precedence is:

1. Direct CLI flags, such as `--database-url` or `--listen`
2. Values loaded from `.local/app.env`
3. Built-in development defaults

## Local Development

Build the CLI:

```bash
go build -o proxy-control-plane ./cmd/proxy-control-plane
```

Initialize and edit config:

```bash
./proxy-control-plane config init
$EDITOR .local/app.env
```

Run versioned SQL migrations:

```bash
./proxy-control-plane db migrate
```

During active model development, you can also run GORM AutoMigrate directly:

```bash
./proxy-control-plane db automigrate
```

Start the API on the host:

```bash
./proxy-control-plane server serve
```

Health check:

```bash
curl http://127.0.0.1:9710/health
```

## Docker

Start the API container:

```bash
./proxy-control-plane docker up
```

Common options:

```bash
./proxy-control-plane docker up --detach
./proxy-control-plane docker up --build=false
```

The Docker command reads `.local/app.env` by default and passes it to Compose as
`PCP_APP_ENV_FILE`. The container itself runs:

```bash
/app/proxy-control-plane server serve --no-local-config
```

That means Docker Compose injects environment variables, and the container does
not try to read `.local/` from inside the image.

## CLI Commands

```bash
./proxy-control-plane config init
./proxy-control-plane db migrate
./proxy-control-plane db automigrate
./proxy-control-plane import xray-config --node xray-under-caddy-la-1 --file .local/imports/xray-under-caddy-la-1.json
./proxy-control-plane server serve
./proxy-control-plane docker up
```

Useful flags:

```bash
./proxy-control-plane server serve --listen=:9710
./proxy-control-plane server serve --env-file=.local/app.env
./proxy-control-plane server serve --database-url='postgres://user:password@host:5432/proxy_control?sslmode=require'
./proxy-control-plane server serve --auto-create-database=true --auto-migrate=true
./proxy-control-plane server serve --runtime-sync=true --runtime-sync-interval=5m
./proxy-control-plane import xray-config --node xray-under-caddy-la-1 --file .local/imports/xray-under-caddy-la-1.json
./proxy-control-plane import xray-config --node xray-fr-1 --file .local/imports/xray-fr-1.json --dry-run
./proxy-control-plane db migrate --database-url='postgres://user:password@host:5432/proxy_control?sslmode=require'
./proxy-control-plane db migrate --migrations-dir=migrations
./proxy-control-plane db automigrate
./proxy-control-plane docker up --detach
```

Use `--config-dir` and `--example-dir` only when you want a non-default config
location:

```bash
./proxy-control-plane --config-dir=.local --example-dir=.local.example config init
```

## Main API

Health:

- `GET /health`

Admin:

- `POST /admin/login`

Customers:

- `GET /admin/customers`
- `POST /admin/customers`
- `GET /admin/customers/{id}`
- `PATCH /admin/customers/{id}`
- `DELETE /admin/customers/{id}`

Proxy nodes:

- `GET /admin/nodes`
- `POST /admin/nodes`
- `POST /admin/nodes/sync`
- `GET /admin/nodes/{id}`
- `PATCH /admin/nodes/{id}`
- `DELETE /admin/nodes/{id}`

Proxy accounts:

- `GET /admin/proxy-accounts`
- `POST /admin/proxy-accounts`
- `GET /admin/proxy-accounts/{id}`
- `PATCH /admin/proxy-accounts/{id}`
- `DELETE /admin/proxy-accounts/{id}`

Subscription tokens:

- `GET /admin/subscription-tokens`
- `POST /admin/subscription-tokens`
- `GET /admin/subscription-tokens/{id}`
- `PATCH /admin/subscription-tokens/{id}`
- `POST /admin/subscription-tokens/{id}/rotate`

Traffic:

- `POST /admin/traffic-usage`

Client subscription:

- `GET /sub/{token}` returns base64 VLESS subscription content
- `GET /sub/{token}?fmt=raw` returns raw VLESS URI text

All admin endpoints except `/health`, `/admin/login`, and `/sub/{token}` require:

```text
Authorization: Bearer <access_token>
```

## Database Tables

- `customers`
- `proxy_nodes`
- `proxy_accounts`
- `proxy_account_nodes`
- `subscription_tokens`
- `traffic_usage`
- `audit_logs`
- `schema_migrations`: tracks applied SQL migration files

## Basic Operating Flow

1. Configure `.local/app.env`.
2. Run `./proxy-control-plane db migrate`.
3. Start the API with `./proxy-control-plane server serve` or
   `./proxy-control-plane docker up`.
4. Log in through `POST /admin/login`.
5. Register nodes through Ansible node sync or create them through the API.
6. Optionally import existing static Xray clients with
   `./proxy-control-plane import xray-config`.
7. Create customers, nodes, proxy accounts, account-node bindings, and
   subscription tokens.
8. Give clients subscription URLs like `http://host:9710/sub/{token}`.

## Verification

```bash
go test ./...
go vet ./...
docker compose -f docker-compose.yml config --quiet
```
