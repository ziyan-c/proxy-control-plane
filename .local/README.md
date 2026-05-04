# Local Configuration

This directory is the private local configuration boundary for this project.

Tracked files in this directory are templates and documentation only. Real
configuration files such as `*.env` are ignored by Git and should stay local to
your machine or deployment environment.

## Files

- `api.local.env.example`: template for running the API directly on the host
  with `make run` or `make migrate`
- `api.docker.env.example`: template for the API container in Docker Compose
- `postgres.env.example`: template for the PostgreSQL container in Docker Compose

## First Setup

From the repository root:

```bash
make init-local
```

Then edit the generated files:

```text
.local/api.local.env
.local/api.docker.env
.local/postgres.env
```

Use strong values for `PCP_ADMIN_PASSWORD`, `PCP_SECRET_KEY`, and
`POSTGRES_PASSWORD` outside disposable local development.
