.PHONY: install lint lint-fix test check build dist vulncheck licenses check-licenses

GOLANGCI_LINT = go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT ?= $(shell git rev-parse --verify HEAD 2>/dev/null || echo unknown)
BUILDINFO = github.com/kubebao/openbao-operator-cli/internal/buildinfo
LDFLAGS = -s -w -X $(BUILDINFO).Version=$(VERSION) -X $(BUILDINFO).Commit=$(COMMIT)
DIST_PLATFORMS ?= linux/amd64 linux/arm64 darwin/amd64 darwin/arm64

install:
	go mod download

lint:
	$(GOLANGCI_LINT) run ./...

lint-fix:
	$(GOLANGCI_LINT) fmt ./...
	$(GOLANGCI_LINT) run --fix ./...

test:
	go test -race ./...

check: lint test build

build:
	mkdir -p bin
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/kubebao ./cmd/kubebao

dist:
	./scripts/dist.sh "$(VERSION)" "$(COMMIT)" $(DIST_PLATFORMS)

vulncheck:
	go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...

licenses:
	./scripts/third-party-licenses.sh

check-licenses:
	@scratch=$$(mktemp) && trap 'rm -f "$$scratch"' EXIT && \
		./scripts/third-party-licenses.sh "$$scratch" && \
		diff -u THIRD_PARTY_LICENSES "$$scratch"
