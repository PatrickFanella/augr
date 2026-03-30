package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestProductionDockerfileContainsRequiredStages(t *testing.T) {
	contents, err := os.ReadFile(productionDockerfilePath(t))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	dockerfile := string(contents)
	for _, want := range []string{
		"FROM golang:${GO_VERSION}-alpine AS builder",
		"COPY go.mod go.sum ./",
		"RUN go mod download",
		"RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags=\"-s -w\" -o /out/tradingagent ./cmd/tradingagent",
		"FROM alpine:${ALPINE_VERSION} AS production",
		"COPY --from=builder /out/tradingagent ./tradingagent",
		"COPY --from=builder /etc/ssl/certs/ca-certificates.crt ./ca-certificates.crt",
		"COPY --chown=app:app migrations ./migrations",
		"RUN chmod 444 ./ca-certificates.crt",
		"ENV SSL_CERT_FILE=/app/ca-certificates.crt",
		"EXPOSE 8080",
		"ENTRYPOINT [\"./tradingagent\", \"serve\"]",
	} {
		if !strings.Contains(dockerfile, want) {
			t.Fatalf("Dockerfile missing required content %q", want)
		}
	}
}

func productionDockerfilePath(t *testing.T) string {
	t.Helper()

	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to determine test file path")
	}

	return filepath.Join(filepath.Dir(filename), "..", "..", "Dockerfile")
}
