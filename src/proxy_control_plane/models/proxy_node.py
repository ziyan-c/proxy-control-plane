from sqlalchemy.orm import Mapped, mapped_column, relationship

from proxy_control_plane.models.base import Base, IdMixin, TimestampMixin


class ProxyNode(IdMixin, TimestampMixin, Base):
    __tablename__ = "proxy_nodes"

    name: Mapped[str] = mapped_column(unique=True, index=True)
    hostname: Mapped[str] = mapped_column()
    public_host: Mapped[str | None] = mapped_column(nullable=True)
    region: Mapped[str | None] = mapped_column(nullable=True)
    protocol: Mapped[str] = mapped_column(default="vless")
    port: Mapped[int] = mapped_column(default=443)
    transport: Mapped[str] = mapped_column(default="tcp")
    security: Mapped[str] = mapped_column(default="none")
    enabled: Mapped[bool] = mapped_column(default=True, index=True)

    proxy_accounts: Mapped[list["ProxyAccount"]] = relationship(
        secondary="proxy_account_nodes",
        back_populates="nodes",
    )

