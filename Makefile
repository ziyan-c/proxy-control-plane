.PHONY: install test run migrate revision docker-build

install:
	python -m pip install -e ".[dev]"

test:
	pytest

run:
	uvicorn proxy_control_plane.main:app --reload

migrate:
	alembic upgrade head

revision:
	alembic revision --autogenerate -m "$(m)"

docker-build:
	docker build -t proxy-control-plane:local .

