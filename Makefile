.PHONY: init-local test run migrate build tidy fmt docker-build docker-up

LOCAL_API_ENV := .local/api.local.env
DOCKER_API_ENV := .local/api.docker.env
POSTGRES_ENV := .local/postgres.env

init-local:
	test -f $(LOCAL_API_ENV) || cp .local/api.local.env.example $(LOCAL_API_ENV)
	test -f $(DOCKER_API_ENV) || cp .local/api.docker.env.example $(DOCKER_API_ENV)
	test -f $(POSTGRES_ENV) || cp .local/postgres.env.example $(POSTGRES_ENV)

build:
	go build ./cmd/server

test:
	go test ./...

run: init-local
	set -a; . $(LOCAL_API_ENV); set +a; go run ./cmd/server serve

migrate: init-local
	set -a; . $(LOCAL_API_ENV); set +a; go run ./cmd/server migrate

tidy:
	go mod tidy

fmt:
	gofmt -w ./cmd ./internal

docker-build:
	docker build -t proxy-control-plane:local .

docker-up: init-local
	docker compose up --build
