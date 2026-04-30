from datetime import datetime

from sqlalchemy import ForeignKey
from sqlalchemy.orm import Mapped, mapped_column, relationship

from proxy_control_plane.models.base import Base, IdMixin, utc_now


class SubscriptionToken(IdMixin, Base):
    __tablename__ = "subscription_tokens"

    customer_id: Mapped[str] = mapped_column(ForeignKey("customers.id", ondelete="CASCADE"), index=True)
    name: Mapped[str] = mapped_column(default="default")
    token_hash: Mapped[str] = mapped_column(unique=True)
    enabled: Mapped[bool] = mapped_column(default=True, index=True)
    expires_at: Mapped[datetime | None] = mapped_column(nullable=True)
    created_at: Mapped[datetime] = mapped_column(nullable=False, default=utc_now)
    last_used_at: Mapped[datetime | None] = mapped_column(nullable=True)

    customer: Mapped["Customer"] = relationship(back_populates="subscription_tokens")

    @property
    def plain_token(self) -> str | None:
        return getattr(self, "_plain_token", None)

    @plain_token.setter
    def plain_token(self, value: str) -> None:
        self._plain_token = value
