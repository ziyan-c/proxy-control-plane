from datetime import datetime

from pydantic import BaseModel, ConfigDict, EmailStr


class CustomerCreate(BaseModel):
    email: EmailStr
    display_name: str | None = None
    status: str = "active"
    expires_at: datetime | None = None


class CustomerRead(BaseModel):
    model_config = ConfigDict(from_attributes=True)

    id: str
    email: EmailStr
    display_name: str | None
    status: str
    expires_at: datetime | None
    created_at: datetime
    updated_at: datetime

