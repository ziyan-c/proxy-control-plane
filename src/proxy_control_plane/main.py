from fastapi import FastAPI

from proxy_control_plane.api import admin, health, subscriptions
from proxy_control_plane.core.config import get_settings


def create_app() -> FastAPI:
    settings = get_settings()
    app = FastAPI(title=settings.app_name)
    app.include_router(health.router)
    app.include_router(admin.router)
    app.include_router(subscriptions.router)
    return app


app = create_app()

