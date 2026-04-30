# proxy-control-plane

Proxy Control Plane is the business control service for proxy users, nodes,
accounts, and subscriptions. Infrastructure remains in `ansible-infra`; this
service owns dynamic proxy business state.

## Scope

This project manages:

- customers
- proxy nodes
- proxy accounts
- account-to-node access
- subscription tokens
- VLESS subscription output
- traffic and audit tables for later collection

It does not install servers, configure firewalls, request certificates, or manage
WireGuard. Those remain infrastructure concerns.

## Local Development

```bash
python -m venv .venv
. .venv/bin/activate
make install
cp .env.example .env
docker compose up -d postgres
make migrate
make run
```

Health check:

```bash
curl http://127.0.0.1:8000/health
```

Admin login uses `PCP_ADMIN_EMAIL` and `PCP_ADMIN_PASSWORD` from the environment.

## Docker

```bash
docker compose up --build
```

The API runs on `http://127.0.0.1:8000`.

## First MVP Flow

1. Create a customer.
2. Create one or more proxy nodes.
3. Create a proxy account for the customer and bind nodes.
4. Create a subscription token.
5. Fetch `/sub/{token}` from a client.

