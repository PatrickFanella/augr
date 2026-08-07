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
		"org.opencontainers.image.revision=\"${BUILD_COMMIT}\"",
		"org.opencontainers.image.version=\"${BUILD_VERSION}\"",
		"org.opencontainers.image.created=\"${BUILD_TIME}\"",
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

func TestProductionWebDockerfileAndNUCBuildCarryCommitMetadata(t *testing.T) {
	repoRoot := filepath.Join(filepath.Dir(productionDockerfilePath(t)), ".")
	contents, err := os.ReadFile(filepath.Join(repoRoot, "Dockerfile.web"))
	if err != nil {
		t.Fatalf("ReadFile(Dockerfile.web) error = %v", err)
	}
	webDockerfile := string(contents)
	for _, want := range []string{
		"ARG BUILD_VERSION=development",
		"ARG BUILD_COMMIT=unknown",
		"ARG BUILD_TIME=unknown",
		"org.opencontainers.image.revision=\"${BUILD_COMMIT}\"",
		"org.opencontainers.image.version=\"${BUILD_VERSION}\"",
		"org.opencontainers.image.created=\"${BUILD_TIME}\"",
	} {
		if !strings.Contains(webDockerfile, want) {
			t.Fatalf("Dockerfile.web missing required content %q", want)
		}
	}

	nucContents, err := os.ReadFile(filepath.Join(repoRoot, "docker-compose.nuc.yml"))
	if err != nil {
		t.Fatalf("ReadFile(docker-compose.nuc.yml) error = %v", err)
	}
	nuc := string(nucContents)
	webIdx := strings.Index(nuc, "\n  web:")
	if webIdx == -1 {
		t.Fatal("docker-compose.nuc.yml missing web service")
	}
	webService := nuc[webIdx:]
	for _, want := range []string{
		"BUILD_VERSION: ${BUILD_VERSION:-development}",
		"BUILD_COMMIT: ${BUILD_COMMIT:-unknown}",
		"BUILD_TIME: ${BUILD_TIME:-unknown}",
	} {
		if !strings.Contains(webService, want) {
			t.Fatalf("NUC web build missing required content %q", want)
		}
	}
}

func TestDockerContextExcludesDocumentationAndDatabaseDumps(t *testing.T) {
	repoRoot := filepath.Join(filepath.Dir(productionDockerfilePath(t)), ".")
	contents, err := os.ReadFile(filepath.Join(repoRoot, ".dockerignore"))
	if err != nil {
		t.Fatalf("ReadFile(.dockerignore) error = %v", err)
	}
	ignore := string(contents)
	for _, want := range []string{"docs", "backups", "*.dump", "**/*.dump", "**/*_test.go"} {
		if !lineExists(ignore, want) {
			t.Fatalf(".dockerignore missing exact entry %q", want)
		}
	}
}

func lineExists(contents, want string) bool {
	for _, line := range strings.Split(contents, "\n") {
		if strings.TrimSpace(line) == want {
			return true
		}
	}
	return false
}

func productionDockerfilePath(t *testing.T) string {
	t.Helper()

	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to determine test file path")
	}

	return filepath.Join(filepath.Dir(filename), "..", "..", "Dockerfile")
}
