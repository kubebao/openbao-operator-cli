.PHONY: install lint lint-fix test check build dist release-dist release-check verify-dist verify-homebrew vulncheck licenses check-licenses

GOLANGCI_LINT = go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT ?= $(shell git rev-parse --verify HEAD 2>/dev/null || echo unknown)
BUILDINFO = github.com/kubebao/openbao-operator-cli/internal/buildinfo
LDFLAGS = -s -w -X $(BUILDINFO).Version=$(VERSION) -X $(BUILDINFO).Commit=$(COMMIT)
DIST_VERSION ?= 0.0.0-dev
DIST_DIR ?= dist

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
	./scripts/dist.sh snapshot "$(DIST_VERSION)"

release-dist:
	./scripts/dist.sh release

release-check:
	./scripts/release-tool.sh goreleaser check
	python3 -m unittest discover -s scripts -p 'test_*.py'

verify-dist:
	python3 scripts/verify_dist.py "$(DIST_DIR)" --smoke --krew --source-build

verify-homebrew:
	python3 scripts/verify_homebrew.py "$(DIST_DIR)"

vulncheck:
	go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...

licenses:
	./scripts/third-party-licenses.sh

check-licenses:
	@scratch=$$(mktemp) && trap 'rm -f "$$scratch"' EXIT && \
		./scripts/third-party-licenses.sh "$$scratch" && \
		diff -u THIRD_PARTY_LICENSES "$$scratch"
