from datetime import datetime

from pydantic import BaseModel, ConfigDict, Field


class ProxyNodeCreate(BaseModel):
    name: str
    hostname: str
    public_host: str | None = None
    region: str | None = None
    protocol: str = "vless"
    port: int = Field(default=443, ge=1, le=65535)
    transport: str = "tcp"
    security: str = "none"
    enabled: bool = True


class ProxyNodeRead(BaseModel):
    model_config = ConfigDict(from_attributes=True)

    id: str
    name: str
    hostname: str
    public_host: str | None
    region: str | None
    protocol: str
    port: int
    transport: str
    security: str
    enabled: bool
    created_at: datetime
    updated_at: datetime

