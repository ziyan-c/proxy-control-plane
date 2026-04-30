from datetime import datetime

from sqlalchemy.orm import Mapped, mapped_column, relationship

from proxy_control_plane.models.base import Base, IdMixin, TimestampMixin


class Customer(IdMixin, TimestampMixin, Base):
    __tablename__ = "customers"

    email: Mapped[str] = mapped_column(unique=True, index=True)
    display_name: Mapped[str | None] = mapped_column(nullable=True)
    status: Mapped[str] = mapped_column(default="active", index=True)
    expires_at: Mapped[datetime | None] = mapped_column(nullable=True)

    proxy_accounts: Mapped[list["ProxyAccount"]] = relationship(
        back_populates="customer",
        cascade="all, delete-orphan",
    )
    subscription_tokens: Mapped[list["SubscriptionToken"]] = relationship(
        back_populates="customer",
        cascade="all, delete-orphan",
    )

