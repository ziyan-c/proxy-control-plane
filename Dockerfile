FROM golang:1.26-alpine AS build

WORKDIR /src

COPY go.mod go.sum* ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal

RUN CGO_ENABLED=0 GOOS=linux go build -o /out/proxy-control-plane ./cmd/server

FROM alpine:3.22

RUN addgroup -S app && adduser -S app -G app

WORKDIR /app
COPY --from=build /out/proxy-control-plane /app/proxy-control-plane

USER app
EXPOSE 8000

CMD ["/app/proxy-control-plane", "serve"]
