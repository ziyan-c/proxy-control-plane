FROM golang:1.26-alpine AS build

WORKDIR /src

COPY go.mod go.sum* ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal

RUN CGO_ENABLED=0 GOOS=linux go build -o /out/proxy-control-plane ./cmd/proxy-control-plane

FROM alpine:3.22

RUN addgroup -S app && adduser -S app -G app

WORKDIR /app
COPY --from=build /out/proxy-control-plane /app/proxy-control-plane
COPY migrations ./migrations

USER app
EXPOSE 9710
ENV PCP_LISTEN_ADDR=0.0.0.0:9710

CMD ["/app/proxy-control-plane", "server", "serve", "--no-local-config"]
