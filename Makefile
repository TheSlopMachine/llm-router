# =============================================================================
# Makefile - llm-router dev tasks  (launcher for scripts/)
# =============================================================================

# Project .env supplies defaults when present.
# Command-line VAR=value wins over the file.
ifneq (,$(wildcard .env))
include .env
endif

VERSION    ?= dev
NO_SKIP    ?= 0
export NO_SKIP

PLATFORMS ?= windows/amd64 windows/386 windows/arm64 linux/amd64 linux/386 linux/arm64 linux/arm darwin/amd64 darwin/arm64 freebsd/amd64 freebsd/386 freebsd/arm64

HOST      ?= localhost
PORT      ?= 38080
ADMIN_PORT ?= 38082
SCOPE     ?= all
URL       ?= http://$(HOST):$(PORT)
LOG_LEVEL ?= info
DB_PATH   ?= ~/.local/share/llm-router/llm-router.dev.sqlite3
CACHE_PATH ?= ~/.cache/llm-router/llm-router.cache.dev.sqlite3
EVENTS_PATH ?= ~/.local/state/llm-router/llm-router.events.dev.sqlite3
EVENTS_RETENTION_DAYS ?= 3
LUA_POOL_BUCKET_SIZE ?= 16
DEV_UI_REDIRECT ?=
ENV_FILE  ?= ~/.config/llm-router/.env.dev
PID_FILE  ?= ~/.local/state/llm-router/llm-router.dev.pid
PKG       ?= ./...
HARNESS   ?=
HARNESS_RUN_ROOT ?= ~/.cache/llm-router/harness
HARNESS_PHASE_TIMEOUT ?= 5m
export HOST PORT ADMIN_PORT SCOPE URL VERSION PLATFORMS LOG_LEVEL DB_PATH CACHE_PATH EVENTS_PATH EVENTS_RETENTION_DAYS LUA_POOL_BUCKET_SIZE DEV_UI_REDIRECT ENV_FILE PID_FILE PKG
export HARNESS HARNESS_RUN_ROOT HARNESS_PHASE_TIMEOUT

BUN_MIN := 1.2
GO_MIN  := 1.27.1
BUN     := bun

.PHONY: help check-frontend-deps check-build-deps go-tidy go-fmt go-fmt-check init start stop restart status browser log log-frontend clean build install publish go-vet go-test go-test-race sql-policy-test check-frontend smoke-test upload-plugins harness-test

help:
	@cat scripts/help.txt

check-frontend-deps:
	@printf '[>] Checking frontend deps (bun >=$(BUN_MIN))...\n'
	@if ! command -v $(BUN) >/dev/null 2>&1; then printf '[FAIL] bun not found (requires >=$(BUN_MIN) https://bun.sh)\n' >&2; exit 1; fi
	@_bun_ver=$$($(BUN) --version 2>/dev/null | sed -E 's/^v//'); \
	 if [ -z "$$_bun_ver" ]; then printf '[FAIL] cannot parse bun version (%s)\n' "$$($(BUN) --version 2>/dev/null)" >&2; exit 1; fi; \
	 if [ "$$(printf '%s\n%s\n' "$$_bun_ver" "$(BUN_MIN)" | sort -V | head -n1)" != "$(BUN_MIN)" ]; then printf '[FAIL] bun >=$(BUN_MIN) required, found %s\n' "$$_bun_ver" >&2; exit 1; fi; \
	 printf '[OK] bun v%s\n' "$$_bun_ver"
	@printf '[OK] Frontend deps OK\n'

check-build-deps:
	@printf '[>] Checking build deps (bun >=$(BUN_MIN), go >=$(GO_MIN))...\n'
	@if ! command -v $(BUN) >/dev/null 2>&1; then printf '[FAIL] bun not found (requires >=$(BUN_MIN) https://bun.sh)\n' >&2; exit 1; fi
	@_bun_ver=$$($(BUN) --version 2>/dev/null | sed -E 's/^v//'); \
	 if [ -z "$$_bun_ver" ]; then printf '[FAIL] cannot parse bun version (%s)\n' "$$($(BUN) --version 2>/dev/null)" >&2; exit 1; fi; \
	 if [ "$$(printf '%s\n%s\n' "$$_bun_ver" "$(BUN_MIN)" | sort -V | head -n1)" != "$(BUN_MIN)" ]; then printf '[FAIL] bun >=$(BUN_MIN) required, found %s\n' "$$_bun_ver" >&2; exit 1; fi; \
	 printf '[OK] bun v%s\n' "$$_bun_ver"
	@if ! command -v go >/dev/null 2>&1; then printf '[FAIL] go not found (requires >=$(GO_MIN) https://go.dev/dl/)\n' >&2; exit 1; fi
	@_go_ver=$$(go version 2>/dev/null | awk '{print $$3}' | sed 's/^go//'); \
	 if [ -z "$$_go_ver" ]; then printf '[FAIL] cannot parse go version (%s)\n' "$$(go version 2>/dev/null)" >&2; exit 1; fi; \
	 if [ "$$(printf '%s\n%s\n' "$$_go_ver" "$(GO_MIN)" | sort -V | head -n1)" != "$(GO_MIN)" ]; then printf '[FAIL] go >=$(GO_MIN) required, found %s\n' "$$_go_ver" >&2; exit 1; fi; \
	 printf '[OK] go %s\n' "$$_go_ver"
	@printf '[OK] Build deps OK\n'

go-tidy:
	@go mod tidy
	@cd scripts && GOWORK=off go mod tidy

go-fmt:
	@cd scripts && FMT_WRITE=1 GOWORK=off go run ./fmt

go-fmt-check:
	@cd scripts && GOWORK=off go run ./fmt

init: check-frontend-deps
	@cd scripts && GOWORK=off go run ./init

start: check-frontend-deps init
	@cd scripts && GOWORK=off go run ./start

stop:
	@cd scripts && GOWORK=off go run ./stop

restart:
	@cd scripts && GOWORK=off go run ./restart

# Smoke harness: run the request paths against the running dev stack
# (no restart; run `make start` first, `make restart` after code changes).
smoke-test:
	@cd scripts && GOWORK=off go run ./smoke

# Upload every .lua plugin in the store dir to the running dev stack via
# install-file (version matches skip). Targets the admin listener like smoke.
upload-plugins:
	@cd scripts && GOWORK=off go run ./uploadplugins

# Validate one CLI harness through the router against the running stack. HARNESS
# selects claude, codex or opencode. The harness-mock plugin must be uploaded first.
# CI-only: agents never run this (live CLIs, admin listener, long runtime).
harness-test:
	@cd scripts && GOWORK=off go run ./harness

status:
	@cd scripts && GOWORK=off go run ./status

log:
	@cd scripts && MSYS_NO_PATHCONV=1 GOWORK=off go run ./log

log-frontend:
	@cd scripts && GOWORK=off go run ./logfrontend

browser:
	@cd scripts && GOWORK=off go run ./browser

clean:
	@cd scripts && GOWORK=off go run ./stop
	@git clean -fdX -e .env -e .env.dev -e .env.local -e '*.sqlite*'

build: check-build-deps init
	@cd scripts && GOWORK=off go run ./build

install: check-build-deps init
	@cd scripts && GOWORK=off go run ./install

# Publish cuts and pushes the next release tag (TYPE=fix|minor|major or
# VERSION=X.Y[.Z]). Tags are the source of truth. CI builds on tag push.
# Requires a clean tree with HEAD synced to origin/main.
publish:
	@cd scripts && GOWORK=off go run ./publish

go-vet:
	@cd scripts && GOWORK=off go run ./vet

go-test:
	@cd scripts && GOWORK=off go run ./test

go-test-race:
	@cd scripts && GOWORK=off go run ./testrace

sql-policy-test:
	@cd scripts && GOWORK=off go run ./sqlpolicy

check-frontend: check-frontend-deps
	@cd scripts && GOWORK=off go run ./fcheck
