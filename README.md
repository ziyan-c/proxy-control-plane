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
- Generates VLESS subscription output in base64 or raw URI format
- Provides admin/customer login with access-token and refresh-token sessions
- Records admin audit logs
- Collects Xray user traffic through StatsService for future quota and plan enforcement
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

After importing the static clients, clear those static users from the node
`config.json` and restart/reload the Xray container. Runtime sync then adds the
managed `pcp-*` users from PostgreSQL. From that point on, user add/remove and
disable behavior is controlled by the control plane instead of node-local JSON.

Existing public subscription files can also be imported. The file remains on
Caddy for temporary compatibility. The control plane treats the imported users
as normal proxy accounts under the legacy customer and serves new managed
subscriptions through regular `/sub/{token}` URLs:

```bash
./proxy-control-plane import subscription-file \
  --file .local/imports/legacy-public.txt
```

The importer accepts raw VLESS URI lists or base64-encoded subscription files.
It creates or reuses the same legacy customer, imports missing VLESS accounts,
tries to bind each link to an existing `proxy_nodes` row by public host, port,
transport, security, path, and Reality fields, and creates one subscription
token if the customer does not already have any. The existing Caddy public file
is not modified or deleted. Re-running the import is safe; existing tokens are
kept and cannot be reprinted because only token hashes are stored.

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

Traffic collection uses the same Xray gRPC endpoint, but calls `StatsService`
instead of `HandlerService`. Xray must have `stats: {}` and policy
`statsUserUplink/statsUserDownlink` enabled on level `0`; the Ansible Xray
roles render those settings when `proxy_control_plane_runtime_api_enabled` is
true. When `PCP_TRAFFIC_SYNC_ENABLED=true`, the server queries managed-user
counters such as
`user>>>pcp-<proxy_account_id>@proxy-control-plane>>>traffic>>>uplink`, resets
them, and writes the returned deltas to `traffic_usage`. This project does not
target VMess; managed runtime users and generated subscriptions are VLESS.

PostgreSQL keeps exact byte counters, while API JSON responses also include
decimal GB helper fields: `upload_gb`, `download_gb`, and `total_gb`.

Domain access analytics are separate from Xray `StatsService`. StatsService only
provides counters, not visited hostnames. The control plane has
`domain_access_logs` plus admin ingestion and summary endpoints so a node-side
Xray access-log shipper can submit sanitized domain-only events later. Do not
send full URLs, paths, query strings, or request bodies.

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
- Xray gRPC API: runtime user reconciliation through `HandlerService` and
  traffic aggregation through `StatsService`

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
internal/security/        Auth tokens, subscription tokens, password checks, hashes
internal/store/           GORM PostgreSQL access and migrations
internal/subscription/    VLESS subscription generation
internal/trafficsync/     Xray StatsService traffic collection
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
PCP_LISTEN_ADDR=0.0.0.0:9710
PCP_DATABASE_URL=postgres://user:password@host:5432/proxy_control?sslmode=require
PCP_ADMIN_EMAIL=admin@proxy.example
PCP_ADMIN_PASSWORD=change-this-to-a-long-admin-password
PCP_ADMIN_SESSION_EPOCH=
PCP_SECRET_KEY=change-this-with-openssl-rand-base64-32-before-serving
PCP_DATABASE_ENCRYPTION_KEY=ZXhhbXBsZS1kYXRhYmFzZS1lbmNyeXB0aW9uLWtleSE=
PCP_ACCESS_TOKEN_EXPIRE_MINUTES=30
PCP_REFRESH_TOKEN_EXPIRE_HOURS=360
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
PCP_MAINTENANCE_DOMAIN_ACCESS_RETENTION=30d
PCP_MAINTENANCE_AUTH_REFRESH_RETENTION=30d
PCP_MAINTENANCE_AUDIT_RETENTION=90d
```

For a remote PostgreSQL server, use `sslmode=require` when the server supports
SSL. Use `sslmode=disable` only for trusted private networks or local testing.
Keep `PCP_AUTO_MIGRATE=false` for normal use so the server does not change table
structure during startup. Use `./proxy-control-plane db migrate` for schema
changes. `PCP_AUTO_MIGRATE=true` is only a development shortcut when you
intentionally want the server to run GORM `AutoMigrate` before serving.
The server refuses to start with the example admin email, placeholder admin
password, placeholder secret key, a password shorter than 12 characters, a
secret key shorter than 32 characters, or a missing/invalid
`PCP_DATABASE_ENCRYPTION_KEY`.

Admin and customer logins return a short-lived access token plus a long-lived
refresh token. The access token TTL is controlled by
`PCP_ACCESS_TOKEN_EXPIRE_MINUTES`; the default access-token TTL is 30 minutes.
The refresh token TTL is controlled by
`PCP_REFRESH_TOKEN_EXPIRE_HOURS`. The default refresh window is 15 days.
Refresh tokens are random 32-byte values, and
only their SHA-256 hashes are stored in PostgreSQL. Every refresh rotates the
refresh token and revokes the previous one.
Admin refresh tokens are also bound to an internal session version derived from
the admin email, admin password, `PCP_SECRET_KEY`, and optional
`PCP_ADMIN_SESSION_EPOCH`. Changing any of those values invalidates existing
admin refresh tokens; changing only `PCP_ADMIN_SESSION_EPOCH` is a simple way to
force all admin sessions to log in again.
Customer refresh tokens are bound to the customer id, email, password hash, and
hidden `session_epoch`. Changing a customer's email, password, status, expiry,
or PATCHing `{"reset_sessions":true}` invalidates that customer's existing
refresh tokens by bumping `session_epoch`; database revoke markers are kept for
observability and cleanup.

`PCP_DATABASE_ENCRYPTION_KEY` is required in server mode. It must be a
base64-encoded 32-byte key, for example from `openssl rand -base64 32`. New
subscription tokens are still verified by `token_hash`, but the plain token is
also stored as an AES-256-GCM encrypted database column so a future customer app
can show the saved subscription URL after login. Existing hash-only tokens
continue to work, but their plaintext cannot be recovered unless they are
rotated or recreated. The example value above is deliberately fake; replace it
in `.local/app.env` before any real deployment.

Runtime sync is disabled by default. Enable `PCP_RUNTIME_SYNC_ENABLED=true`
after Ansible has registered Xray nodes with runtime API fields. The sync loop
uses `GetInboundUsers`, computes a managed-user hash, skips unchanged nodes, and
diffs only changed nodes. Static users that already live in Xray config are not
removed by this project. Do not store real customer contact emails in Xray's
runtime `email` field; use PostgreSQL for customer identity and let the control
plane generate the Xray runtime key.

Traffic sync is also disabled by default. Enable `PCP_TRAFFIC_SYNC_ENABLED=true`
after the Xray containers have been redeployed with `StatsService`, `stats: {}`
and user stats policy enabled. The default interval is 10 minutes and the API
timeout is 30 seconds. The collector uses Xray's reset mode, so each row written
to `traffic_usage` is the traffic delta since the previous successful stats
query.

Maintenance cleanup is disabled by default. Enable
`PCP_MAINTENANCE_CLEANUP_ENABLED=true` if you want the API process itself to
periodically aggregate old traffic detail and delete old rows. The default
interval is 24 hours. The retention defaults are:

- `traffic_usage`: 7 days
- `traffic_usage_daily`: 30 days
- `domain_access_logs`: 30 days
- `auth_refresh_tokens`: 30 days after expiry or revocation
- `audit_logs`: 90 days

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
not try to read `.local/` from inside the image. Keep every `PCP_*` runtime
setting in the env file; Compose only describes the container and port binding.
For Docker, set `PCP_LISTEN_ADDR=0.0.0.0:9710` in that env file, then restrict
host exposure through the Compose `ports` binding. Compose uses
`restart: unless-stopped`, so Docker restarts the API container after machine
reboot or unexpected container exit unless you explicitly stop it.

## GitHub Container Registry Release

GitHub Actions publishes container images to GitHub Container Registry only when
a version tag is pushed. It uses the built-in `GITHUB_TOKEN`, so no Docker Hub
secret is required.

Then create and push a semver tag:

```bash
git tag v0.1.1
git push origin v0.1.1
```

The release workflow builds and pushes:

- `ghcr.io/ziyan-c/proxy-control-plane:0.1.1`
- `ghcr.io/ziyan-c/proxy-control-plane:0.1`
- `ghcr.io/ziyan-c/proxy-control-plane:0`
- `ghcr.io/ziyan-c/proxy-control-plane:latest` for non-prerelease tags

## CLI Commands

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

Useful flags:

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
./proxy-control-plane maintenance cleanup --traffic-retention=7d --traffic-daily-retention=30d --domain-access-retention=30d --audit-retention=90d
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

## Maintenance

Traffic details are intentionally append-only in `traffic_usage` so the project
can keep precise history while data is fresh. Long-term history is stored in
`traffic_usage_daily`, one row per account, node, and day. Cleanup is based on
age retention only:

- `traffic_usage`: 7 days
- `traffic_usage_daily`: 30 days
- `domain_access_logs`: 30 days
- `auth_refresh_tokens`: 30 days after expiry or revocation
- `audit_logs`: 90 days

Traffic total APIs sum both retained sources: fresh `traffic_usage` detail rows
and older `traffic_usage_daily` rows that have already been aggregated by
maintenance cleanup.

Run cleanup in dry-run mode first:

```bash
./proxy-control-plane maintenance cleanup \
  --audit-retention=90d \
  --traffic-retention=7d \
  --traffic-daily-retention=30d \
  --domain-access-retention=30d \
  --dry-run
```

Run the write mode after checking the counts. Without `--dry-run`, cleanup
writes to PostgreSQL:

```bash
./proxy-control-plane maintenance cleanup \
  --audit-retention=90d \
  --traffic-retention=7d \
  --traffic-daily-retention=30d \
  --domain-access-retention=30d
```

For normal deployment, you can let the API process run the same cleanup
periodically by setting `PCP_MAINTENANCE_CLEANUP_ENABLED=true`. Keep the manual
command for dry-runs, one-off cleanup, and checking a retention change before
you enable it.

The write path runs in one database transaction. It first aggregates old
`traffic_usage` rows older than the detail retention into `traffic_usage_daily`,
then deletes those detail rows. It also deletes `traffic_usage_daily`,
`domain_access_logs`, `auth_refresh_tokens`, and `audit_logs` after their
configured retention windows.
PostgreSQL may keep physical files allocated after deletes; the space is still
reusable by future writes.

## Main API

Health:

- `GET /health`

Auth:

- `POST /auth/refresh`
- `POST /auth/logout`

Admin:

- `POST /admin/login`

Customer login:

- `POST /customer/login`
- `GET /customer/me`
- `GET /customer/subscription-tokens`
- `GET /customer/traffic-usage/total`

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
- `GET /admin/traffic-usage/total`
- `GET /admin/domain-access-logs`
- `POST /admin/domain-access-logs`
- `GET /admin/domain-access-summary`

Traffic total endpoints accept optional `proxy_account_id`, `since`, and
`until` query parameters. The admin endpoint also accepts `customer_id`.
`since` and `until` are calendar-day boundaries in `YYYY-MM-DD` form; `since`
is inclusive at 00:00 UTC and `until` includes that whole day.

Client subscription:

- `GET /sub/{token}` or `GET /sub/{token}?fmt=base64` returns base64 VLESS subscription content
- `GET /sub/{token}?fmt=raw` returns raw VLESS URI text

`POST /admin/login` and `POST /customer/login` return:

```json
{
  "access_token": "...",
  "refresh_token": "...",
  "token_type": "bearer",
  "expires_in": 1800,
  "refresh_token_expires_at": "2026-06-08T12:00:00Z",
  "principal": {
    "type": "admin",
    "email": "admin@proxy.example"
  }
}
```

`POST /auth/refresh` accepts `{"refresh_token":"..."}` and returns a new access
token plus a new refresh token. `POST /auth/logout` accepts the same body and
revokes that refresh token.

Admin endpoints require an access token with the `admin` role. Customer
endpoints require an access token with the `customer` role:

```text
Authorization: Bearer <access_token>
```

Access-token JWT payloads are intentionally minimal. Admin tokens use
`sub=configured-admin`; customer tokens use `sub=<customers.id>`. Customer and
admin emails are returned in the login response `principal` object for display,
but they are not embedded in the JWT.

## Database Tables

- `customers`
- `proxy_nodes`
- `proxy_accounts`
- `proxy_account_nodes`
- `subscription_tokens`
- `auth_refresh_tokens`
- `traffic_usage`
- `traffic_usage_daily`
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
7. Optionally import an existing public subscription file with
   `./proxy-control-plane import subscription-file`; this creates a normal
   subscription token for the legacy customer if needed.
8. For an already-imported legacy customer without a token, run
   `./proxy-control-plane subscription token ensure --customer-email ... --output-file .local/generated/...`.
9. Create customers, nodes, proxy accounts, account-node bindings, and
   subscription tokens.
10. Give clients subscription URLs like `http://host:9710/sub/{token}`.
11. Enable `PCP_MAINTENANCE_CLEANUP_ENABLED=true` or periodically run
   `./proxy-control-plane maintenance cleanup` to aggregate old traffic detail
   and prune old audit rows.

## Verification

```bash
go test ./...
go vet ./...
docker compose -f docker-compose.yml config --quiet
```

## License

Apache License 2.0. See [LICENSE](LICENSE).
