# =============================================================================
# Makefile - llm-router dev tasks  (launcher for scripts/)
# =============================================================================

VERSION    ?= dev

PUBLISH_PLATFORMS ?= windows/amd64 windows/386 windows/arm64 linux/amd64 linux/386 linux/arm64 linux/arm darwin/amd64 darwin/arm64 freebsd/amd64 freebsd/386 freebsd/arm64
WORKSPACE_REMOTE   ?= https

HOST      ?= localhost
WEB_PORT  ?= 8080
API_PORT  ?= 8081
VITE_PORT ?= 5173
URL       ?= http://$(HOST):$(VITE_PORT)

ifeq ($(OS),Windows_NT)
  DEV_DB  ?= $(subst \,/,$(USERPROFILE))/.local/llm-router/llm-router-dev.db
  DEV_KEY ?= $(subst \,/,$(USERPROFILE))/.local/llm-router/llm-router-dev.key
else
  DEV_DB  ?= $(HOME)/.local/llm-router/llm-router-dev.db
  DEV_KEY ?= $(HOME)/.local/llm-router/llm-router-dev.key
endif

BUN_MIN := 1.2
GO_MIN  := 1.25
BUN     := bun

.PHONY: help check-frontend-deps check-publish-deps go-tidy start stop restart status browser clean publish go-check go-test check-frontend

help:
	@cd scripts && GOWORK=off go run ./help --host "$(HOST)" --web-port "$(WEB_PORT)" --api-port "$(API_PORT)" --vite-port "$(VITE_PORT)" --url "$(URL)" --dev-db "$(DEV_DB)" --dev-key "$(DEV_KEY)" --platforms "$(PUBLISH_PLATFORMS)" --remote "$(WORKSPACE_REMOTE)"

check-frontend-deps:
	@printf '[>] Checking frontend deps (bun >=$(BUN_MIN))...\n'
	@if ! command -v $(BUN) >/dev/null 2>&1; then printf '[FAIL] bun not found (requires >=$(BUN_MIN) https://bun.sh)\n' >&2; exit 1; fi
	@_bun_ver=$$($(BUN) --version 2>/dev/null | sed -E 's/^v//'); \
	 if [ -z "$$_bun_ver" ]; then printf '[FAIL] cannot parse bun version (%s)\n' "$$($(BUN) --version 2>/dev/null)" >&2; exit 1; fi; \
	 if [ "$$(printf '%s\n%s\n' "$$_bun_ver" "$(BUN_MIN)" | sort -V | head -n1)" != "$(BUN_MIN)" ]; then printf '[FAIL] bun >=$(BUN_MIN) required, found %s\n' "$$_bun_ver" >&2; exit 1; fi; \
	 printf '[OK] bun v%s\n' "$$_bun_ver"
	@printf '[OK] Frontend deps OK\n'

check-publish-deps:
	@printf '[>] Checking publish deps (bun >=$(BUN_MIN), go >=$(GO_MIN))...\n'
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
	@printf '[OK] Publish deps OK\n'

go-tidy:
	@go mod tidy

start: check-frontend-deps
	@cd scripts && GOWORK=off go run ./start --host "$(HOST)" --web-port "$(WEB_PORT)" --api-port "$(API_PORT)" --vite-port "$(VITE_PORT)" --db "$(DEV_DB)" --testing-key "$(DEV_KEY)" --remote "$(WORKSPACE_REMOTE)"

stop:
	@cd scripts && GOWORK=off go run ./stop

restart:
	@cd scripts && GOWORK=off go run ./stop
	@cd scripts && GOWORK=off go run ./start --host "$(HOST)" --web-port "$(WEB_PORT)" --api-port "$(API_PORT)" --vite-port "$(VITE_PORT)" --db "$(DEV_DB)" --testing-key "$(DEV_KEY)" --remote "$(WORKSPACE_REMOTE)"

status:
	@cd scripts && GOWORK=off go run ./status

browser:
	@cd scripts && GOWORK=off go run ./browser --url "$(URL)"

clean:
	@cd scripts && GOWORK=off go run ./stop
	@git clean -fdX

publish: check-publish-deps
	@cd scripts && GOWORK=off go run ./publish --version "$(VERSION)" --platforms "$(PUBLISH_PLATFORMS)" --remote "$(WORKSPACE_REMOTE)"

go-check:
	@cd scripts && GOWORK=off go run ./vet

go-test:
	@cd scripts && GOWORK=off go run ./test

check-frontend: check-frontend-deps
	@cd scripts && GOWORK=off go run ./fcheck
