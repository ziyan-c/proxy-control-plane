# proxy-control-plane

[简体中文](README.zh-CN.md)

`proxy-control-plane` is a Go-based business control plane for a proxy service.
It is not a proxy server itself. It provides backend APIs for managing customers,
proxy nodes, proxy accounts, subscription tokens, subscription output, and
business data.

The main design goal is to keep the business control plane separate from real
proxy node provisioning. This project owns dynamic business state, while VPS
provisioning, Xray, sing-box, certificates, firewalls, and other infrastructure
concerns are handled by separate operations tooling.

## What This Project Does

This project is responsible for:

- Managing customer records: email, display name, status, and expiration time
- Managing proxy nodes: name, hostname, public host, region, port, transport,
  and security parameters
- Managing proxy accounts: VLESS UUID, account label, enabled state, expiration
  time, and traffic limit
- Managing account-to-node bindings
- Creating and rotating subscription tokens while storing only token hashes in
  the database
- Creating the target PostgreSQL database automatically when configured and
  permitted by the database role
- Generating VLESS subscription output from subscription tokens
- Supporting both `raw` and base64-encoded `v2ray` subscription formats
- Recording admin audit logs
- Providing a traffic reporting endpoint and traffic usage table for future
  statistics, limits, and plan enforcement
- Running GORM `AutoMigrate` automatically on startup

## What This Project Does Not Do

This project does not:

- Buy or create VPS instances
- Install Xray, sing-box, Nginx, Caddy, or other server software
- Configure host firewalls, security groups, or port forwarding
- Issue or renew TLS certificates
- Deploy WireGuard
- Push generated configuration directly to real proxy nodes
- Collect traffic directly from real proxy processes

Those responsibilities belong to infrastructure or node operations tooling, such
as `ansible-infra`, a node agent, CI/CD, Ansible, Terraform, or another
deployment system.

## What Starts After Deployment

`docker-compose.yml` starts two local services:

- `postgres`: PostgreSQL 17, used for customer, node, account, subscription,
  traffic, and audit data
- `api`: the Go backend API service, used for admin APIs and subscription APIs

`docker-compose.remote-db.yml` starts only `api` and connects it to a remote
PostgreSQL database through `.local/api.remote.env`.

Docker resources are explicitly prefixed to avoid name collisions:

- Project name: `proxy-control-plane`
- API container: `proxy-control-plane_api`
- PostgreSQL container: `proxy-control-plane_postgres`
- PostgreSQL volume: `proxy-control-plane_postgres-data`
- Network: `proxy-control-plane_network`

Default ports:

- PostgreSQL: `127.0.0.1:5432`
- API: `127.0.0.1:9710`

Important: Docker deployment starts only the database and/or the control-plane
API. It does not deploy any real proxy node software.

## Tech Stack

- Language: Go
- CLI: Cobra
- HTTP: Gin
- Database: PostgreSQL
- ORM: GORM
- Database driver: `gorm.io/driver/postgres`
- Migrations: GORM `AutoMigrate`
- Tests: `go test`
- Deployment: Docker / Docker Compose

## Project Structure

```text
cmd/proxy-control-plane/              Thin CLI entrypoint
internal/cli/            Cobra CLI commands and local configuration bootstrap
internal/config/         Environment-based configuration
internal/domain/         Core business models
internal/httpapi/        Gin HTTP API, authentication, middleware, and responses
internal/security/       Admin tokens, subscription tokens, passwords, and hashes
internal/store/          GORM-based PostgreSQL access and migrations
internal/subscription/   VLESS subscription generation
.local.example/          Tracked example configuration templates
.local/                  Ignored private local configuration
```

## Database

Main tables:

- `customers`: customers
- `proxy_nodes`: proxy nodes
- `proxy_accounts`: proxy accounts
- `proxy_account_nodes`: many-to-many account/node bindings
- `subscription_tokens`: subscription token records; stores token hashes, not
  plaintext tokens
- `traffic_usage`: traffic usage records
- `audit_logs`: admin audit logs

## Main API

Health check:

- `GET /health`

Admin login:

- `POST /admin/login`

Customer management:

- `GET /admin/customers`
- `POST /admin/customers`
- `GET /admin/customers/{id}`
- `PATCH /admin/customers/{id}`
- `DELETE /admin/customers/{id}`

Node management:

- `GET /admin/nodes`
- `POST /admin/nodes`
- `GET /admin/nodes/{id}`
- `PATCH /admin/nodes/{id}`
- `DELETE /admin/nodes/{id}`

Proxy account management:

- `GET /admin/proxy-accounts`
- `POST /admin/proxy-accounts`
- `GET /admin/proxy-accounts/{id}`
- `PATCH /admin/proxy-accounts/{id}`
- `DELETE /admin/proxy-accounts/{id}`

Subscription token management:

- `GET /admin/subscription-tokens`
- `POST /admin/subscription-tokens`
- `GET /admin/subscription-tokens/{id}`
- `PATCH /admin/subscription-tokens/{id}`
- `POST /admin/subscription-tokens/{id}/rotate`

Traffic records:

- `POST /admin/traffic-usage`

Client subscription:

- `GET /sub/{token}`: returns base64-encoded VLESS subscription content
- `GET /sub/{token}?fmt=raw`: returns raw VLESS URI text

All admin endpoints except `/health`, `/admin/login`, and `/sub/{token}` require:

```text
Authorization: Bearer <access_token>
```

## Basic Flow

1. Admin logs in through `/admin/login` and receives a Bearer token.
2. Admin creates a customer.
3. Admin creates one or more proxy nodes.
4. Admin creates a proxy account for the customer and binds accessible nodes.
5. Admin creates a subscription token for the customer.
6. A client fetches subscription content from `/sub/{token}`.

## Local Development

Build the CLI:

```bash
go build -o proxy-control-plane ./cmd/proxy-control-plane
```

Prepare configuration:

```bash
./proxy-control-plane config init
```

This copies missing private config files from `.local.example/` into `.local/`:

```text
.local/api.local.env
.local/api.docker.env
.local/api.remote.env
.local/cli.env
.local/postgres.env
```

The whole `.local/` directory is ignored by Git and Docker. Edit these private
files locally before running the service. `config init` is mostly a bootstrap
helper; the other CLI commands call it automatically before they run.

Choose the default database mode in `.local/cli.env`:

- `DB=local`: use the local Compose PostgreSQL service
- `DB=remote`: use the remote PostgreSQL URL in `.local/api.remote.env`

You can still override the local default for one command, for example
`./proxy-control-plane docker up --db=remote`.

Start only the local PostgreSQL service for host-based development:

```bash
docker compose up -d postgres
```

Run migrations against the configured database:

```bash
./proxy-control-plane db migrate
```

Start the API directly on the host:

```bash
./proxy-control-plane server serve
```

Health check:

```bash
curl http://127.0.0.1:9710/health
```

Run tests:

```bash
go test ./...
```

## Docker

The preferred Docker entrypoint is a single command:

```bash
./proxy-control-plane docker up
```

The database mode comes from `.local/cli.env`. Command-line values still work
as temporary overrides:

```bash
./proxy-control-plane docker up --db=local
./proxy-control-plane docker up --db=remote
```

For `DB=local`, Compose starts both `postgres` and `api`. For `DB=remote`,
Compose starts only `api` and reads the remote database URL from
`.local/api.remote.env`.

The API runs at:

```text
http://127.0.0.1:9710
```

## CLI Options

Common examples:

```bash
./proxy-control-plane server serve --db=remote --listen=:9710
./proxy-control-plane db migrate --db=remote
./proxy-control-plane docker up --db=remote --detach
./proxy-control-plane server serve --env-file=.local/api.remote.env
```

The selected `--db` profile decides which local config file is loaded:

- `--db=local`: `.local/api.local.env` for host commands; Docker uses
  `.local/api.docker.env`
- `--db=remote`: `.local/api.remote.env`

Direct flags such as `--listen`, `--database-url`, `--auto-create-database`,
and `--auto-migrate` override values loaded from `.local/*.env`.
Use `--example-dir` if you keep templates somewhere other than
`.local.example/`.

## Configuration

Tracked examples live under `.local.example/`. Real private local configuration
lives under `.local/`.

Tracked templates:

- `.local.example/api.local.env`: host-local API config for `server serve --db=local` and
  `db migrate --db=local`
- `.local.example/api.docker.env`: API container config for Docker Compose with
  `--db=local`
- `.local.example/api.remote.env`: API config for host or Docker runs with
  `--db=remote`
- `.local.example/cli.env`: local CLI defaults, including the default `DB`
  profile
- `.local.example/postgres.env`: PostgreSQL container config for Docker Compose

Ignored real config directory:

- `.local/`

The API uses environment variables with the `PCP_` prefix:

- `PCP_APP_NAME`: application name
- `PCP_ENVIRONMENT`: runtime environment
- `PCP_LISTEN_ADDR`: API listen address, defaults to `:9710`
- `PCP_DATABASE_URL`: PostgreSQL connection URL, including the `sslmode`
  choice for remote databases
- `PCP_ADMIN_EMAIL`: admin email
- `PCP_ADMIN_PASSWORD`: admin password; plaintext env bootstrap is supported
  for the MVP stage
- `PCP_SECRET_KEY`: access token signing key
- `PCP_ACCESS_TOKEN_EXPIRE_MINUTES`: admin access token lifetime
- `PCP_AUTO_CREATE_DATABASE`: whether to create the target PostgreSQL database
  automatically before connecting to it
- `PCP_AUTO_MIGRATE`: whether to run GORM table migrations automatically when
  the API starts

Automatic database creation connects to the maintenance database named
`postgres`, checks whether the database from `PCP_DATABASE_URL` exists, and runs
`CREATE DATABASE` when it is missing. The configured PostgreSQL user must have
permission to create databases.

GORM `AutoMigrate` is the table migration step. It creates or updates tables,
columns, indexes, and constraints based on the Go model definitions. It does
not create the PostgreSQL database itself; that is handled separately by
`PCP_AUTO_CREATE_DATABASE`.

PostgreSQL uses:

- `POSTGRES_USER`: database user
- `POSTGRES_PASSWORD`: database password
- `POSTGRES_DB`: database name

## Subscription Parameters

The node model already includes common VLESS parameters:

- `transport`
- `security`
- `sni`
- `fingerprint`
- `alpn`
- `path`
- `host_header`
- `reality_public_key`
- `reality_short_id`

Subscription generation converts these fields into VLESS URI query parameters.

## Current Status

The project has been rebuilt from a Python/FastAPI MVP into a Go service with a
more complete control-plane foundation:

- Go API service
- PostgreSQL data model and GORM migrations
- Admin login and Bearer token authentication
- Basic CRUD for customers, nodes, proxy accounts, and subscription tokens
- Subscription token rotation
- VLESS subscription generation
- Traffic recording endpoint
- Audit logs
- Docker and CI

Not implemented yet:

- Real node configuration delivery
- Node agent
- Automatic Xray/sing-box installation or updates
- Billing and plans
- Automatic disabling when traffic limits are exceeded
- Web admin UI

These can be added later as separate modules so the control plane and
infrastructure deployment logic stay cleanly separated.
