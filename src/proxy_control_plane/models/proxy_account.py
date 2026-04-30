from datetime import datetime
from uuid import uuid4

from sqlalchemy import BigInteger, ForeignKey, Table, Column
from sqlalchemy.orm import Mapped, mapped_column, relationship

from proxy_control_plane.models.base import Base, IdMixin, TimestampMixin

proxy_account_nodes = Table(
    "proxy_account_nodes",
    Base.metadata,
    Column("proxy_account_id", ForeignKey("proxy_accounts.id", ondelete="CASCADE"), primary_key=True),
    Column("proxy_node_id", ForeignKey("proxy_nodes.id", ondelete="CASCADE"), primary_key=True),
)


class ProxyAccount(IdMixin, TimestampMixin, Base):
    __tablename__ = "proxy_accounts"

    customer_id: Mapped[str] = mapped_column(ForeignKey("customers.id", ondelete="CASCADE"), index=True)
    protocol: Mapped[str] = mapped_column(default="vless")
    uuid: Mapped[str] = mapped_column(default=lambda: str(uuid4()), unique=True)
    email_tag: Mapped[str] = mapped_column()
    flow: Mapped[str | None] = mapped_column(nullable=True)
    enabled: Mapped[bool] = mapped_column(default=True, index=True)
    expires_at: Mapped[datetime | None] = mapped_column(nullable=True)
    traffic_limit_bytes: Mapped[int | None] = mapped_column(BigInteger, nullable=True)

    customer: Mapped["Customer"] = relationship(back_populates="proxy_accounts")
    nodes: Mapped[list["ProxyNode"]] = relationship(
        secondary=proxy_account_nodes,
        back_populates="proxy_accounts",
    )

