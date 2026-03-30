ARG GO_VERSION=1.25.8
ARG ALPINE_VERSION=3.21

FROM golang:${GO_VERSION}-alpine AS dev
RUN go install github.com/air-verse/air@latest
WORKDIR /app
EXPOSE 8080
CMD ["air", "-c", ".air.toml"]

FROM golang:${GO_VERSION}-alpine AS builder
ARG TARGETOS=linux
ARG TARGETARCH=amd64
WORKDIR /src

RUN apk add --no-cache ca-certificates

COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags="-s -w" -o /out/tradingagent ./cmd/tradingagent

FROM alpine:${ALPINE_VERSION} AS production
RUN addgroup -S app && \
    adduser -S -G app -h /app app

WORKDIR /app

COPY --from=builder /out/tradingagent ./tradingagent
COPY --from=builder /etc/ssl/certs/ca-certificates.crt ./ca-certificates.crt
COPY --chown=app:app migrations ./migrations
RUN chmod 444 ./ca-certificates.crt

ENV APP_ENV=production
ENV SSL_CERT_FILE=/app/ca-certificates.crt

USER app:app

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://127.0.0.1:${APP_PORT:-8080}/healthz || exit 1

ENTRYPOINT ["./tradingagent", "serve"]
