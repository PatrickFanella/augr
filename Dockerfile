ARG GO_VERSION=1.25.8
ARG ALPINE_VERSION=3.21
ARG AIR_VERSION=v1.66.0

FROM golang:${GO_VERSION}-alpine AS dev
ARG AIR_VERSION
RUN go install github.com/air-verse/air@${AIR_VERSION}
WORKDIR /app
EXPOSE 8080
CMD ["air", "-c", ".air.toml"]

FROM golang:${GO_VERSION}-alpine AS builder
ARG TARGETOS=linux
ARG TARGETARCH=amd64
ARG BUILD_VERSION=development
WORKDIR /src

RUN apk add --no-cache ca-certificates

COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags="-s -w -X main.version=${BUILD_VERSION}" -o /out/tradingagent ./cmd/tradingagent

FROM alpine:${ALPINE_VERSION} AS production
RUN addgroup -S app && \
    adduser -S -G app -h /app app

WORKDIR /app

COPY --from=builder /out/tradingagent ./tradingagent
COPY --from=builder /etc/ssl/certs/ca-certificates.crt ./ca-certificates.crt
COPY --chown=app:app migrations ./migrations
RUN chmod 444 ./ca-certificates.crt

ENV APP_ENV=production
ARG BUILD_VERSION=development
ARG BUILD_COMMIT=unknown
ARG BUILD_TIME=unknown
LABEL org.opencontainers.image.version="${BUILD_VERSION}" \
      org.opencontainers.image.revision="${BUILD_COMMIT}" \
      org.opencontainers.image.created="${BUILD_TIME}"
ENV APP_VERSION=${BUILD_VERSION}
ENV APP_BUILD_COMMIT=${BUILD_COMMIT}
ENV APP_BUILD_TIME=${BUILD_TIME}
ENV SSL_CERT_FILE=/app/ca-certificates.crt

USER app:app

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://127.0.0.1:${APP_PORT:-8080}/healthz || exit 1

ENTRYPOINT ["./tradingagent"]
CMD ["serve"]
