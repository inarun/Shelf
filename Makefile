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

.PHONY: all build test vet staticcheck gosec govulncheck lint-all fmt tidy clean update update-check a11y

all: lint-all test build

build:
	$(GO) build -o $(BIN_EXE) ./cmd/shelf

# Update from origin/main: stop a running instance, fast-forward pull,
# rebuild. Safe to force-kill because every vault write goes through
# internal/vault/atomic and shelf.db uses SQLite WAL (see SKILL.md
# §Concurrent edit handling). Leading `-` and `2>/dev/null` swallow the
# "process not found" error when nothing is running.
update:
	-@taskkill /F /IM $(BIN_EXE) 2>/dev/null
	git pull --ff-only
	$(GO) build -o $(BIN_EXE) ./cmd/shelf
	@echo "Update complete. Run ./$(BIN_EXE) to launch."

# Peek at origin/main without modifying the working tree. Prints the
# commits on origin/main not yet in local HEAD.
update-check:
	@git fetch --quiet
	@git log HEAD..origin/main --oneline

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

# Dev-local contrast audit: parses app.css design tokens and reports
# WCAG 2.2 AA pass/fail for curated UI-used (fg, bg) pairs plus a
# full-matrix audit. Not wired into `all`; run manually when palette
# changes. `cmd/a11y-check/main.go` is //go:build ignore so lints skip it.
a11y:
	$(GO) run cmd/a11y-check/main.go
