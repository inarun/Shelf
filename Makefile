# Shelf — local dev targets. Assumes `go` is on PATH.
#
# Security policy per SKILL.md: every commit must pass `make lint-all` + `make test`.
# Linters are run via `go run <pkg>@<version>` to avoid a separate install step.
# Pinned versions help with supply-chain predictability; bump deliberately.

GO           ?= go
BIN          := shelf
BIN_EXE      := $(BIN).exe
PKGS         := ./...

STATICCHECK  := honnef.co/go/tools/cmd/staticcheck@v0.7.0
GOSEC        := github.com/securego/gosec/v2/cmd/gosec@latest
GOVULNCHECK  := golang.org/x/vuln/cmd/govulncheck@latest

.PHONY: all build test vet staticcheck gosec govulncheck lint-all fmt tidy clean

all: lint-all test build

build:
	$(GO) build -o $(BIN_EXE) ./cmd/shelf

# Default `test` skips -race because on Windows the race detector requires cgo
# and a C compiler (not bundled with Go). `test-race` enables both, which is
# worth running before concurrency-sensitive commits (e.g., internal/vault/atomic).
test:
	$(GO) test -count=1 $(PKGS)

test-race:
	CGO_ENABLED=1 $(GO) test -race -count=1 $(PKGS)

vet:
	$(GO) vet $(PKGS)

staticcheck:
	$(GO) run $(STATICCHECK) $(PKGS)

gosec:
	$(GO) run $(GOSEC) -quiet $(PKGS)

govulncheck:
	$(GO) run $(GOVULNCHECK) $(PKGS)

lint-all: vet staticcheck gosec govulncheck

fmt:
	$(GO) fmt $(PKGS)

tidy:
	$(GO) mod tidy

clean:
	rm -f $(BIN_EXE) coverage.*
