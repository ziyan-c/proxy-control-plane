# Local Configuration Example

This directory contains the tracked example configuration for local development
and deployment. Real private configuration belongs in `.local/`, which is
ignored by Git and Docker.

## Files

- `app.env`: API runtime configuration. This is the only local config file the
  CLI and Docker Compose read by default.

## First Setup

From the repository root:

```bash
./proxy-control-plane config init
```

That command copies `.local.example/app.env` into `.local/app.env` when the
private file is missing.

Then edit:

```text
.local/app.env
```

Use strong values for `PCP_ADMIN_EMAIL`, `PCP_ADMIN_PASSWORD`,
`PCP_SECRET_KEY`, and `PCP_DATABASE_ENCRYPTION_KEY` before serving. The server
refuses to start with the example admin email, placeholder password,
placeholder secret key, passwords shorter than 12 characters, secret keys
shorter than 32 characters, or a missing/invalid database encryption key.

Set `PCP_DATABASE_ENCRYPTION_KEY` to a base64-encoded 32-byte key so new
sensitive database columns, such as stored subscription tokens, are encrypted
at rest while still being recoverable by the application:

```bash
openssl rand -base64 32
```

The key in `.local.example/app.env` is deliberately fake and only exists so the
example file is syntactically complete. Replace it in `.local/app.env` before
serving real traffic.

`PCP_AUTO_MIGRATE=false` keeps server startup from changing table structure.
Run the versioned SQL migrations explicitly with:

```bash
./proxy-control-plane db migrate
```

GORM AutoMigrate is still available for active development:

```bash
./proxy-control-plane db automigrate
```

`PCP_RUNTIME_SYNC_ENABLED=false` keeps Xray runtime reconciliation disabled by
default. Turn it on only after Ansible has registered nodes with
`runtime_api_enabled=true`, `runtime_api_host`, `runtime_api_port`, and
`runtime_inbound_tag`.

`PCP_MAINTENANCE_CLEANUP_ENABLED=false` keeps database cleanup disabled by
default. Turn it on when you want the API process to periodically aggregate old
`traffic_usage` rows into `traffic_usage_daily` and prune old traffic/audit
data. The default interval is 24 hours.
