from datetime import datetime

from sqlalchemy import BigInteger, DateTime, ForeignKey
from sqlalchemy.orm import Mapped, mapped_column

from proxy_control_plane.models.base import Base, IdMixin, utc_now


class TrafficUsage(IdMixin, Base):
    __tablename__ = "traffic_usage"

    proxy_account_id: Mapped[str] = mapped_column(ForeignKey("proxy_accounts.id", ondelete="CASCADE"), index=True)
    proxy_node_id: Mapped[str] = mapped_column(ForeignKey("proxy_nodes.id", ondelete="CASCADE"), index=True)
    upload_bytes: Mapped[int] = mapped_column(BigInteger, default=0)
    download_bytes: Mapped[int] = mapped_column(BigInteger, default=0)
    recorded_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utc_now, index=True)

