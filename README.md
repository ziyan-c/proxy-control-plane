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

The current `docker-compose.yml` starts two services:

- `postgres`: PostgreSQL 17, used for customer, node, account, subscription,
  traffic, and audit data
- `api`: the Go backend API service, used for admin APIs and subscription APIs

Docker resources are explicitly prefixed to avoid name collisions:

- Project name: `proxy-control-plane`
- API container: `proxy-control-plane_api`
- PostgreSQL container: `proxy-control-plane_postgres`
- PostgreSQL volume: `proxy-control-plane_postgres-data`
- Network: `proxy-control-plane_network`

Default ports:

- PostgreSQL: `127.0.0.1:5432`
- API: `127.0.0.1:8000`

Important: Docker deployment starts only the database and the control-plane API.
It does not deploy any real proxy node software.

## Tech Stack

- Language: Go
- HTTP: Gin
- Database: PostgreSQL
- ORM: GORM
- Database driver: `gorm.io/driver/postgres`
- Migrations: GORM `AutoMigrate`
- Tests: `go test`
- Deployment: Docker / Docker Compose

## Project Structure

```text
cmd/server/              Service entrypoint, supports migrate and serve commands
internal/config/         Environment-based configuration
internal/domain/         Core business models
internal/httpapi/        Gin HTTP API, authentication, middleware, and responses
internal/security/       Admin tokens, subscription tokens, passwords, and hashes
internal/store/          GORM-based PostgreSQL access and migrations
internal/subscription/   VLESS subscription generation
.local/                  Local-only private configuration templates and files
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

Prepare configuration:

```bash
make init-local
```

This creates local private config files from templates:

```text
.local/api.local.env
.local/api.docker.env
.local/postgres.env
```

These `*.env` files are ignored by Git. Edit them locally before running the
service. The host commands use `.local/api.local.env`; Docker Compose uses
`.local/api.docker.env` and `.local/postgres.env`.

Start PostgreSQL:

```bash
docker compose up -d postgres
```

Run migrations:

```bash
make migrate
```

Start the API:

```bash
make run
```

Health check:

```bash
curl http://127.0.0.1:8000/health
```

Run tests:

```bash
make test
```

## Docker

```bash
docker compose up --build
```

Or let `make` initialize local config first:

```bash
make docker-up
```

The API runs at:

```text
http://127.0.0.1:8000
```

## Configuration

Private local configuration lives under `.local/`.

Tracked templates:

- `.local/api.local.env.example`: host-local API config for `make run` and
  `make migrate`
- `.local/api.docker.env.example`: API container config for Docker Compose
- `.local/postgres.env.example`: PostgreSQL container config for Docker Compose

Ignored real config files:

- `.local/api.local.env`
- `.local/api.docker.env`
- `.local/postgres.env`

The API uses environment variables with the `PCP_` prefix:

- `PCP_APP_NAME`: application name
- `PCP_ENVIRONMENT`: runtime environment
- `PCP_LISTEN_ADDR`: API listen address, defaults to `:8000`
- `PCP_DATABASE_URL`: PostgreSQL connection URL
- `PCP_ADMIN_EMAIL`: admin email
- `PCP_ADMIN_PASSWORD`: admin password; plaintext env bootstrap is supported
  for the MVP stage
- `PCP_SECRET_KEY`: access token signing key
- `PCP_ACCESS_TOKEN_EXPIRE_MINUTES`: admin access token lifetime
- `PCP_AUTO_MIGRATE`: whether to run migrations automatically when the API starts

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
