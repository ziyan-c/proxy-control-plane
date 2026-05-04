.PHONY: test run migrate build tidy fmt docker-build

build:
	go build ./cmd/server

test:
	go test ./...

run:
	go run ./cmd/server serve

migrate:
	go run ./cmd/server migrate

tidy:
	go mod tidy

fmt:
	gofmt -w ./cmd ./internal

docker-build:
	docker build -t proxy-control-plane:local .
