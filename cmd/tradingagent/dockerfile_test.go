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
		"RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags=\"-s -w -X main.version=${BUILD_VERSION}\" -o /out/tradingagent ./cmd/tradingagent",
		"FROM alpine:${ALPINE_VERSION} AS production",
		"COPY --from=builder /out/tradingagent ./tradingagent",
		"COPY --from=builder /etc/ssl/certs/ca-certificates.crt ./ca-certificates.crt",
		"COPY --chown=app:app migrations ./migrations",
		"RUN chmod 444 ./ca-certificates.crt",
		"ENV SSL_CERT_FILE=/app/ca-certificates.crt",
		"EXPOSE 8080",
		"ENTRYPOINT [\"./tradingagent\"]",
		"CMD [\"serve\"]",
	} {
		if !strings.Contains(dockerfile, want) {
			t.Fatalf("Dockerfile missing required content %q", want)
		}
	}

	builderStart := strings.Index(dockerfile, "FROM golang:${GO_VERSION}-alpine AS builder")
	productionStart := strings.Index(dockerfile, "FROM alpine:${ALPINE_VERSION} AS production")
	if builderStart == -1 || productionStart == -1 || builderStart >= productionStart {
		t.Fatal("Dockerfile builder and production stages are not ordered")
	}
	builderStage := dockerfile[builderStart:productionStart]
	for _, metadataArg := range []string{"ARG BUILD_COMMIT", "ARG BUILD_TIME"} {
		if strings.Contains(builderStage, metadataArg) {
			t.Fatalf("builder stage unexpectedly contains cache-busting metadata %q", metadataArg)
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
