# Local Configuration Examples

This directory contains tracked example configuration files for local
development and deployment.

Real private configuration belongs in `.local/`. The `.local/` directory is
ignored by Git and Docker so secrets stay on your machine.

## Files

- `api.local.env`: example for running the API directly on the host with
  `proxy-control-plane server serve --db=local` or
  `proxy-control-plane db migrate --db=local`
- `api.docker.env`: example for the API container when Docker Compose starts a
  local PostgreSQL container
- `api.remote.env`: example for host or Docker runs that connect the API to a
  remote PostgreSQL database
- `cli.env`: example CLI defaults, such as `DB=remote`
- `postgres.env`: example PostgreSQL container settings for Docker Compose

## First Setup

From the repository root:

```bash
./proxy-control-plane config init
```

That command copies missing files from `.local.example/` into `.local/`.

Then edit the generated private files:

```text
.local/cli.env
.local/api.local.env
.local/api.docker.env
.local/api.remote.env
.local/postgres.env
```

Set `DB=local` or `DB=remote` in `.local/cli.env` to choose the default database
profile for `server serve`, `db migrate`, and `docker up`. A command like
`proxy-control-plane docker up --db=local` can still override that default for
one run.

Use strong values for `PCP_ADMIN_PASSWORD`, `PCP_SECRET_KEY`, and
`POSTGRES_PASSWORD` outside disposable local development.
