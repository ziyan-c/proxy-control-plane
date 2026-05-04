# proxy-control-plane

[简体中文](README.zh-CN.md)

`proxy-control-plane` is a Go business control plane for a proxy service. It is
not a proxy server. It provides the backend API and data model for customers,
proxy nodes, proxy accounts, subscription tokens, subscription output, traffic
records, and admin audit logs.

The main design choice is separation: this project manages dynamic business
state, while real node provisioning is handled elsewhere. VPS creation, Xray or
sing-box installation, TLS certificates, firewall rules, and node-side config
delivery should be owned by separate infrastructure tooling.

## What It Does

- Manages customers, proxy nodes, proxy accounts, and account-to-node bindings
- Creates and rotates subscription tokens while storing only token hashes
- Generates VLESS subscription output in base64 `v2ray` format or raw URI format
- Provides admin login and Bearer-token protected management APIs
- Records admin audit logs
- Accepts traffic usage reports for future quota and plan enforcement
- Connects to PostgreSQL through GORM
- Can create the target PostgreSQL database when the configured role is allowed
- Applies versioned SQL migrations and can optionally run GORM `AutoMigrate`
  during local development
- Provides a Cobra CLI for config bootstrap, database migration, API serving,
  and Docker Compose startup

## What It Does Not Do

This repository does not deploy real proxy nodes. It does not:

- Buy or create VPS instances
- Install Xray, sing-box, Nginx, Caddy, or other server software
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

Docker does not start or configure Xray, sing-box, certificates, firewalls, or
real proxy-node software.

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
PCP_ADMIN_EMAIL=admin@example.com
PCP_ADMIN_PASSWORD=change-this
PCP_SECRET_KEY=change-this-with-a-long-random-secret
PCP_AUTO_CREATE_DATABASE=true
PCP_AUTO_MIGRATE=false
```

For a remote PostgreSQL server, use `sslmode=require` when the server supports
SSL. Use `sslmode=disable` only for trusted private networks or local testing.
Keep `PCP_AUTO_MIGRATE=false` for normal use so the server does not change table
structure during startup. Use `./proxy-control-plane db migrate` for schema
changes. `PCP_AUTO_MIGRATE=true` is only a development shortcut when you
intentionally want the server to run GORM `AutoMigrate` before serving.

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
./proxy-control-plane server serve
./proxy-control-plane docker up
```

Useful flags:

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
5. Create customers, nodes, proxy accounts, account-node bindings, and
   subscription tokens.
6. Give clients subscription URLs like `http://host:9710/sub/{token}`.

## Verification

```bash
go test ./...
go vet ./...
docker compose -f docker-compose.yml config --quiet
```
