FROM golang:1.24-alpine

# Install build tools and air for hot-reload
RUN apk add --no-cache git curl && \
    go install github.com/air-verse/air@latest

WORKDIR /app

EXPOSE 8080

CMD ["air", "-c", ".air.toml"]
