from fastapi import APIRouter, Depends, HTTPException, Response, status
from sqlalchemy import select
from sqlalchemy.orm import Session

from proxy_control_plane.api.deps import get_db, require_admin
from proxy_control_plane.core.config import get_settings
from proxy_control_plane.models.audit_log import AuditLog
from proxy_control_plane.models.customer import Customer
from proxy_control_plane.models.proxy_account import ProxyAccount
from proxy_control_plane.models.proxy_node import ProxyNode
from proxy_control_plane.models.subscription_token import SubscriptionToken
from proxy_control_plane.schemas.auth import AdminLogin, AccessToken
from proxy_control_plane.schemas.customer import CustomerCreate, CustomerRead
from proxy_control_plane.schemas.proxy_account import ProxyAccountCreate, ProxyAccountRead
from proxy_control_plane.schemas.proxy_node import ProxyNodeCreate, ProxyNodeRead
from proxy_control_plane.schemas.subscription_token import (
    SubscriptionTokenCreate,
    SubscriptionTokenRead,
)
from proxy_control_plane.services.security import (
    create_access_token,
    new_subscription_token,
    token_digest,
    verify_password,
)

router = APIRouter(prefix="/admin", tags=["admin"])


def write_audit(db: Session, actor: str | None, action: str, metadata_json: str | None = None) -> None:
    db.add(AuditLog(actor=actor, action=action, metadata_json=metadata_json))


@router.post("/login", response_model=AccessToken)
def login(payload: AdminLogin) -> AccessToken:
    settings = get_settings()
    if payload.email != settings.admin_email or not verify_password(
        payload.password,
        settings.admin_password,
    ):
        raise HTTPException(
            status_code=status.HTTP_401_UNAUTHORIZED,
            detail="Invalid credentials",
        )

    return AccessToken(
        access_token=create_access_token(
            subject=payload.email,
            secret_key=settings.secret_key,
            expires_minutes=settings.access_token_expire_minutes,
        )
    )


@router.post("/customers", response_model=CustomerRead, status_code=status.HTTP_201_CREATED)
def create_customer(
    payload: CustomerCreate,
    db: Session = Depends(get_db),
    actor: str = Depends(require_admin),
) -> Customer:
    existing = db.scalar(select(Customer).where(Customer.email == payload.email))
    if existing is not None:
        raise HTTPException(status_code=status.HTTP_409_CONFLICT, detail="Customer already exists")

    customer = Customer(**payload.model_dump())
    db.add(customer)
    write_audit(db, actor, "customer.created", customer.id)
    db.commit()
    db.refresh(customer)
    return customer


@router.get("/customers", response_model=list[CustomerRead])
def list_customers(
    db: Session = Depends(get_db),
    _: str = Depends(require_admin),
) -> list[Customer]:
    return list(db.scalars(select(Customer).order_by(Customer.created_at.desc())))


@router.post("/nodes", response_model=ProxyNodeRead, status_code=status.HTTP_201_CREATED)
def create_node(
    payload: ProxyNodeCreate,
    db: Session = Depends(get_db),
    actor: str = Depends(require_admin),
) -> ProxyNode:
    existing = db.scalar(select(ProxyNode).where(ProxyNode.name == payload.name))
    if existing is not None:
        raise HTTPException(status_code=status.HTTP_409_CONFLICT, detail="Node already exists")

    node = ProxyNode(**payload.model_dump())
    db.add(node)
    write_audit(db, actor, "proxy_node.created", node.name)
    db.commit()
    db.refresh(node)
    return node


@router.get("/nodes", response_model=list[ProxyNodeRead])
def list_nodes(
    db: Session = Depends(get_db),
    _: str = Depends(require_admin),
) -> list[ProxyNode]:
    return list(db.scalars(select(ProxyNode).order_by(ProxyNode.name)))


@router.post("/proxy-accounts", response_model=ProxyAccountRead, status_code=status.HTTP_201_CREATED)
def create_proxy_account(
    payload: ProxyAccountCreate,
    db: Session = Depends(get_db),
    actor: str = Depends(require_admin),
) -> ProxyAccount:
    customer = db.get(Customer, payload.customer_id)
    if customer is None:
        raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail="Customer not found")

    nodes = []
    if payload.node_ids:
        nodes = list(db.scalars(select(ProxyNode).where(ProxyNode.id.in_(payload.node_ids))))
        if len(nodes) != len(set(payload.node_ids)):
            raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail="One or more nodes not found")

    data = payload.model_dump(exclude={"node_ids"}, exclude_none=True)
    account = ProxyAccount(**data)
    account.nodes = nodes
    db.add(account)
    write_audit(db, actor, "proxy_account.created", account.uuid)
    db.commit()
    db.refresh(account)
    return account


@router.get("/proxy-accounts", response_model=list[ProxyAccountRead])
def list_proxy_accounts(
    db: Session = Depends(get_db),
    _: str = Depends(require_admin),
) -> list[ProxyAccount]:
    return list(db.scalars(select(ProxyAccount).order_by(ProxyAccount.created_at.desc())))


@router.post("/subscription-tokens", response_model=SubscriptionTokenRead, status_code=status.HTTP_201_CREATED)
def create_subscription_token(
    payload: SubscriptionTokenCreate,
    response: Response,
    db: Session = Depends(get_db),
    actor: str = Depends(require_admin),
) -> SubscriptionToken:
    customer = db.get(Customer, payload.customer_id)
    if customer is None:
        raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail="Customer not found")

    raw_token = new_subscription_token()
    token = SubscriptionToken(
        customer_id=payload.customer_id,
        name=payload.name,
        expires_at=payload.expires_at,
        token_hash=token_digest(raw_token),
    )
    db.add(token)
    write_audit(db, actor, "subscription_token.created", token.name)
    db.commit()
    db.refresh(token)
    response.headers["X-Subscription-Token"] = raw_token
    token.plain_token = raw_token
    return token
