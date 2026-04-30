from __future__ import annotations

import base64
from datetime import datetime
from urllib.parse import quote, urlencode

from proxy_control_plane.models.customer import Customer
from proxy_control_plane.models.proxy_account import ProxyAccount
from proxy_control_plane.models.proxy_node import ProxyNode


def _is_active(expires_at: datetime | None, now: datetime) -> bool:
    return expires_at is None or expires_at >= now


def _vless_uri(account: ProxyAccount, node: ProxyNode) -> str:
    host = node.public_host or node.hostname
    params = {
        "encryption": "none",
        "type": node.transport,
        "security": node.security,
    }
    if account.flow:
        params["flow"] = account.flow
    label = quote(f"{node.name}-{account.email_tag}")
    return f"vless://{account.uuid}@{host}:{node.port}?{urlencode(params)}#{label}"


def build_subscription(customer: Customer, fmt: str, now: datetime) -> str:
    if customer.status != "active" or not _is_active(customer.expires_at, now):
        return ""

    uris: list[str] = []
    for account in customer.proxy_accounts:
        if account.protocol != "vless" or not account.enabled or not _is_active(account.expires_at, now):
            continue
        for node in account.nodes:
            if node.enabled:
                uris.append(_vless_uri(account, node))

    raw = "\n".join(uris)
    if fmt == "raw":
        return raw

    return base64.b64encode(raw.encode()).decode()

