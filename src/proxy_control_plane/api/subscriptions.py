from datetime import UTC, datetime

from fastapi import APIRouter, Depends, HTTPException, Query, status
from fastapi.responses import PlainTextResponse
from sqlalchemy import select
from sqlalchemy.orm import Session, selectinload

from proxy_control_plane.api.deps import get_db
from proxy_control_plane.models.subscription_token import SubscriptionToken
from proxy_control_plane.services.security import token_digest
from proxy_control_plane.services.subscriptions import build_subscription

router = APIRouter(tags=["subscriptions"])


@router.get("/sub/{token}", response_class=PlainTextResponse)
def subscription(
    token: str,
    fmt: str = Query(default="v2ray", pattern="^(v2ray|raw)$"),
    db: Session = Depends(get_db),
) -> PlainTextResponse:
    token_row = db.scalar(
        select(SubscriptionToken)
        .options(selectinload(SubscriptionToken.customer))
        .where(SubscriptionToken.token_hash == token_digest(token))
    )
    if token_row is None or not token_row.enabled:
        raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail="Subscription not found")

    now = datetime.now(UTC)
    if token_row.expires_at is not None and token_row.expires_at < now:
        raise HTTPException(status_code=status.HTTP_403_FORBIDDEN, detail="Subscription expired")

    token_row.last_used_at = now
    body = build_subscription(token_row.customer, fmt=fmt, now=now)
    db.commit()
    return PlainTextResponse(body)

