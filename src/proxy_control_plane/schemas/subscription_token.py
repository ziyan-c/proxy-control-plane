from datetime import datetime

from pydantic import BaseModel, ConfigDict


class SubscriptionTokenCreate(BaseModel):
    customer_id: str
    name: str = "default"
    expires_at: datetime | None = None


class SubscriptionTokenRead(BaseModel):
    model_config = ConfigDict(from_attributes=True)

    id: str
    customer_id: str
    name: str
    enabled: bool
    expires_at: datetime | None
    created_at: datetime
    last_used_at: datetime | None
    plain_token: str | None = None

