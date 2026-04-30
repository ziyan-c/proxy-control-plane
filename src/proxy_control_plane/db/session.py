from sqlalchemy import create_engine
from sqlalchemy.orm import sessionmaker
from sqlalchemy.pool import StaticPool

from proxy_control_plane.core.config import get_settings


def make_engine(database_url: str):
    if database_url.startswith("sqlite"):
        kwargs = {"connect_args": {"check_same_thread": False}}
        if database_url == "sqlite:///:memory:":
            kwargs["poolclass"] = StaticPool
        return create_engine(database_url, **kwargs)
    return create_engine(database_url, pool_pre_ping=True)


engine = make_engine(get_settings().database_url)
SessionLocal = sessionmaker(bind=engine, autoflush=False, autocommit=False)
