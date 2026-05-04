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

Use strong values for `PCP_ADMIN_PASSWORD` and `PCP_SECRET_KEY` outside
disposable local development.

`PCP_AUTO_MIGRATE=false` keeps server startup from changing table structure.
Run the versioned SQL migrations explicitly with:

```bash
./proxy-control-plane db migrate
```

GORM AutoMigrate is still available for active development:

```bash
./proxy-control-plane db automigrate
```
