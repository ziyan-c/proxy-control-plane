from datetime import datetime

from pydantic import BaseModel, ConfigDict, Field


class ProxyAccountCreate(BaseModel):
    customer_id: str
    protocol: str = "vless"
    uuid: str | None = None
    email_tag: str
    flow: str | None = None
    enabled: bool = True
    expires_at: datetime | None = None
    traffic_limit_bytes: int | None = Field(default=None, ge=0)
    node_ids: list[str] = Field(default_factory=list)


class ProxyAccountRead(BaseModel):
    model_config = ConfigDict(from_attributes=True)

    id: str
    customer_id: str
    protocol: str
    uuid: str
    email_tag: str
    flow: str | None
    enabled: bool
    expires_at: datetime | None
    traffic_limit_bytes: int | None
    created_at: datetime
    updated_at: datetime

