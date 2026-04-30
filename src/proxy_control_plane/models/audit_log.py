from datetime import datetime

from sqlalchemy import DateTime, Text
from sqlalchemy.orm import Mapped, mapped_column

from proxy_control_plane.models.base import Base, IdMixin, utc_now


class AuditLog(IdMixin, Base):
    __tablename__ = "audit_logs"

    actor: Mapped[str | None] = mapped_column(nullable=True)
    action: Mapped[str] = mapped_column(nullable=False)
    metadata_json: Mapped[str | None] = mapped_column(Text, nullable=True)
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utc_now)

