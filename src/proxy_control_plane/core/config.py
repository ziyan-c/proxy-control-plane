from functools import lru_cache

from pydantic import Field
from pydantic_settings import BaseSettings, SettingsConfigDict


class Settings(BaseSettings):
    model_config = SettingsConfigDict(env_prefix="PCP_", env_file=".env", extra="ignore")

    app_name: str = "proxy-control-plane"
    environment: str = "local"
    database_url: str = "sqlite:///./local.db"
    admin_email: str = "admin@example.com"
    admin_password: str = Field(default="change-me-admin-password", min_length=12)
    secret_key: str = Field(default="change-me-with-openssl-rand-base64-32", min_length=24)
    access_token_expire_minutes: int = 60


@lru_cache
def get_settings() -> Settings:
    return Settings()

