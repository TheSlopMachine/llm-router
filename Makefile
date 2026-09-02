# =============================================================================
# Makefile - llm-router dev tasks
# =============================================================================
SHELL      := bash
.SHELLFLAGS := -c

VERSION    ?= dev
BINARY     := llm-router
UI_DIR     := web
PUBLISH    := build/release
ADAPTERS   := adapters.conf
PLUGINS    := plugins/plugins.go

PUBLISH_PLATFORMS ?= windows/amd64 windows/386 windows/arm64 linux/amd64 linux/386 linux/arm64 linux/arm darwin/amd64 darwin/arm64 freebsd/amd64 freebsd/386 freebsd/arm64

WORKSPACE_DIR := .workspace
GO_WORK       := go.work

WORKSPACE_REMOTE ?= https

URL ?= http://localhost:8080

ifeq ($(OS),Windows_NT)
  DEV_DB ?= $(subst \,/,$(USERPROFILE))/.local/llm-router/llm-router-dev.db
else
  DEV_DB ?= $(HOME)/.local/llm-router/llm-router-dev.db
endif

ifeq ($(OS),Windows_NT)
  ifneq ($(TEMP),)
    _TMP_DIR := $(subst \,/,$(TEMP))
  else ifneq ($(TMP),)
    _TMP_DIR := $(subst \,/,$(TMP))
  else
    _TMP_DIR := $(subst \,/,$(USERPROFILE))/AppData/Local/Temp
  endif
  PID_FILE ?= $(_TMP_DIR)/llm-router-dev.pid
  LOG_FILE ?= $(_TMP_DIR)/llm-router-dev.log
  TMP_BIN  ?= $(_TMP_DIR)/llm-router-dev.exe
else
  _TMP_DIR := $(if $(TMPDIR),$(TMPDIR),/tmp)
  PID_FILE ?= $(_TMP_DIR)/llm-router-dev.pid
  LOG_FILE ?= $(_TMP_DIR)/llm-router-dev.log
  TMP_BIN  ?= $(_TMP_DIR)/llm-router-dev
endif

ifeq ($(OS),Windows_NT)
  LOCAL_BIN := $(BINARY).exe
  NPM       := npm.cmd
  GOPATH    := $(subst \,/,$(shell go env GOPATH))
  _GIT_RAW  := $(shell git rev-parse --short HEAD)
  GIT_COMMIT := $(if $(_GIT_RAW),$(_GIT_RAW),unknown)
  BUILD_TIME := $(shell powershell -NoProfile -Command "[System.DateTime]::UtcNow.ToString('yyyy-MM-ddTHH:mm:ssZ')")
  SHA256    := sha256sum
  OPEN_CMD  := powershell -NoProfile -Command Start-Process
else
  NPM       := npm
  GOPATH    := $(shell go env GOPATH 2>/dev/null)
  GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
  BUILD_TIME := $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
  SHA256    := $(shell command -v sha256sum 2>/dev/null || echo "shasum -a 256")
  UNAME_S   := $(shell uname -s)
  ifneq ($(filter MINGW% CYGWIN%,$(UNAME_S)),)
    OPEN_CMD := powershell -NoProfile -Command Start-Process
  else ifeq ($(UNAME_S),Darwin)
    OPEN_CMD := open
  else ifneq ($(shell grep -qi microsoft /proc/version 2>/dev/null && echo wsl),)
    OPEN_CMD := $(if $(shell command -v wslview 2>/dev/null),wslview,cmd.exe /c start "")
  else
    OPEN_CMD := xdg-open
  endif
endif

LDFLAGS = -s -w -X main.Version=$(VERSION) -X main.GitCommit=$(GIT_COMMIT) -X main.BuildTime=$(BUILD_TIME)

.PHONY: help init-workspace prepare-plugins prepare-frontend prepare start stop restart status browser clean publish go-check go-test

help:
	@printf '\nUsage: make <target>\n\n'
	@printf 'Targets:\n'
	@printf '  init-workspace    Clone adapters from %s to %s and generate %s\n' "$(ADAPTERS)" "$(WORKSPACE_DIR)" "$(GO_WORK)"
	@printf '  prepare-plugins   Install adapters from %s\n' "$(ADAPTERS)"
	@printf '  prepare-frontend  npm install && build frontend\n'
	@printf '  prepare           prepare-frontend + prepare-plugins\n'
	@printf '  start             Build dev binary to temp and start daemon (db %s)\n' "$(DEV_DB)"
	@printf '  stop              Stop daemon\n'
	@printf '  restart           Stop + start\n'
	@printf '  status            Show dev server status (pid/log)\n'
	@printf '  browser           Open dashboard in browser (%s)\n' "$(URL)"
	@printf '  clean             Stop + git clean -fdx\n'
	@printf '  publish           prepare-frontend + prepare-plugins + build all PUBLISH_PLATFORMS\n'
	@printf '  go-check          Run go vet\n'
	@printf '  go-test           Run go test ./...\n\n'
	@printf 'Variables:\n'
	@printf '  PUBLISH_PLATFORMS  Platforms for publish. Default: "windows/amd64 windows/386 windows/arm64 linux/amd64 linux/386 linux/arm64 linux/arm darwin/amd64 darwin/arm64 freebsd/amd64 freebsd/386 freebsd/arm64"\n'
	@printf '  WORKSPACE_REMOTE   Clone protocol for %s. Default: "https" (https|ssh)\n\n' "$(WORKSPACE_DIR)"
	@printf 'Examples:\n'
	@printf '  make start\n'
	@printf '  make restart\n'
	@printf '  make publish\n'
	@printf '  make publish PUBLISH_PLATFORMS="linux/amd64 darwin/arm64"\n\n'

init-workspace:
	@mkdir -p "$(WORKSPACE_DIR)"
	@grep -v '^#' "$(ADAPTERS)" 2>/dev/null | grep -v '^$$' | while read -r mod _hash; do \
		mod=$$(printf '%s' "$$mod" | tr -d '\r' | xargs); \
		[ -z "$$mod" ] && continue; \
		dir="$(WORKSPACE_DIR)/$$(basename $$mod)"; \
		if [ "$(WORKSPACE_REMOTE)" = "ssh" ]; then \
			host=$$(printf '%s' "$$mod" | cut -d/ -f1); \
			path=$$(printf '%s' "$$mod" | cut -d/ -f2-); \
			url="git@$$host:$$path.git"; \
		else \
			url="https://$$mod.git"; \
		fi; \
		if [ ! -d "$$dir/.git" ]; then \
			printf '[>] Cloning %s (%s) -> %s\n' "$$mod" "$(WORKSPACE_REMOTE)" "$$dir"; \
			git clone "$$url" "$$dir" || exit 1; \
		else \
			cur_url=$$(git -C "$$dir" remote get-url origin 2>/dev/null || echo ""); \
			if [ "$$cur_url" != "$$url" ]; then \
				printf '[>] Updating remote %s: %s -> %s\n' "$$dir" "$$cur_url" "$$url"; \
				git -C "$$dir" remote set-url origin "$$url" || true; \
			else \
				printf '[>] Exists %s, skip\n' "$$dir"; \
			fi; \
		fi; \
	done
	@printf '[>] Generating %s...\n' "$(GO_WORK)"
	@printf 'go 1.25.0\n\nuse (\n  .\n' > "$(GO_WORK)"
	@for d in $(WORKSPACE_DIR)/*; do [ -d "$$d" ] && printf '  ./%s\n' "$$d" >> "$(GO_WORK)"; done
	@printf ')\n' >> "$(GO_WORK)"
	@go work sync 2>/dev/null || true
	@printf '[OK] Workspace ready (%s + %s)\n' "$(WORKSPACE_DIR)" "$(GO_WORK)"

prepare-plugins:
	@$(MAKE) stop
	@$(MAKE) init-workspace
	@printf '\n== Plugin Generation ==\n'
	@mkdir -p plugins
	@if [ ! -f "$(ADAPTERS)" ]; then \
		printf '# External adapter registry\n# Format: <module-path> <full-commit-hash>\n' > "$(ADAPTERS)"; \
	fi
	@printf '// Code generated by make generate-plugins. DO NOT EDIT.\n\npackage plugins\n\n' > "$(PLUGINS)"
	@if [ -s "$(ADAPTERS)" ] && grep -q '^[^#]' "$(ADAPTERS)" 2>/dev/null; then \
		printf 'import (\n' >> "$(PLUGINS)"; \
		grep -v '^#' "$(ADAPTERS)" | grep -v '^$$' | while read -r mod query; do \
			if ! printf '%s' "$$query" | grep -qE '^[0-9a-f]{40}$$'; then \
				printf '[ERROR] %s: "%s" is not a full 40-char commit hash\n' "$$mod" "$$query"; exit 1; \
			fi; \
			printf '[>] Adding adapter via git: %s@%s\n' "$$mod" "$$query"; \
			GOPROXY=direct go get "$$mod@$$query" || exit 1; \
			printf '\t_ "%s"\n' "$$mod" >> "$(PLUGINS)"; \
		done || exit 1; \
		printf ')\n' >> "$(PLUGINS)"; \
		printf '[>] Tidying module...\n'; go mod tidy || exit 1; \
	fi
	@printf '[OK] Plugins generated.\n'

prepare-frontend:
	@$(MAKE) stop
	@printf '\n== Frontend ==\n'
	@mkdir -p "$(UI_DIR)/src/lib/generated"
	@cd "$(UI_DIR)" && $(NPM) install --no-audit --no-fund
	@cd "$(UI_DIR)" && $(NPM) run build
	@printf '[OK] Frontend ready.\n'

prepare: prepare-frontend prepare-plugins
	@printf '[OK] Prepare done\n'

start:
	@mkdir -p "$(dir $(DEV_DB))" "$(dir $(PID_FILE))"
	@if [ -f "$(PID_FILE)" ] && kill -0 $$(cat "$(PID_FILE)") 2>/dev/null; then \
		printf '[>] Already running PID %s (log %s)\n' "$$(cat $(PID_FILE))" "$(LOG_FILE)"; \
		printf 'Dashboard: http://localhost:8080\n'; \
		printf 'API: http://localhost:8081/v1\n'; \
		exit 0; \
	fi; \
	rm -f "$(PID_FILE)"; \
	printf '[>] Building dev binary to %s...\n' "$(TMP_BIN)"; \
	go build -o "$(TMP_BIN)" . || exit 1; \
	printf '[>] Starting llm-router --web 8080 --api 8081 --db %s (pid %s, log %s)...\n' "$(DEV_DB)" "$(PID_FILE)" "$(LOG_FILE)"; \
	if command -v nohup >/dev/null 2>&1; then \
		nohup "$(TMP_BIN)" --web 8080 --api 8081 --db "$(DEV_DB)" > "$(LOG_FILE)" 2>&1 & echo $$! > "$(PID_FILE)"; \
	else \
		"$(TMP_BIN)" --web 8080 --api 8081 --db "$(DEV_DB)" > "$(LOG_FILE)" 2>&1 & echo $$! > "$(PID_FILE)"; \
	fi; \
	sleep 0.3; \
	if kill -0 $$(cat "$(PID_FILE)") 2>/dev/null; then \
		printf '[OK] Started PID %s\n' "$$(cat $(PID_FILE))"; \
		printf 'Dashboard: http://localhost:8080\n'; \
		printf 'API: http://localhost:8081/v1\n'; \
	else \
		printf '[FAIL] Start failed, log:\n'; cat "$(LOG_FILE)" 2>/dev/null || true; rm -f "$(PID_FILE)"; exit 1; \
	fi

stop:
	@if [ ! -f "$(PID_FILE)" ]; then printf '[>] Not running (no pid file %s)\n' "$(PID_FILE)"; exit 0; fi; \
	pid=$$(cat "$(PID_FILE)"); printf '[>] Stopping PID %s...\n' "$$pid"; \
	if kill -0 $$pid 2>/dev/null; then \
		kill $$pid 2>/dev/null || true; \
		sleep 1; \
		if kill -0 $$pid 2>/dev/null; then \
			kill -9 $$pid 2>/dev/null || true; \
			taskkill //F //PID $$pid 2>/dev/null || true; \
			powershell -NoProfile -Command "try { Stop-Process -Id $$pid -Force -ErrorAction Stop } catch {}" 2>/dev/null || true; \
		fi; \
	fi; \
	rm -f "$(PID_FILE)"; printf '[OK] Stopped\n'

restart:
	@$(MAKE) stop
	@$(MAKE) start

status:
	@if [ -f "$(PID_FILE)" ] && kill -0 $$(cat "$(PID_FILE)") 2>/dev/null; then \
		printf 'llm-router dev server is running as PID %s\n' "$$(cat $(PID_FILE))"; \
	else \
		printf 'llm-router dev server is not running\n'; \
	fi; \
	printf 'Log: %s\n' "$(LOG_FILE)"

browser:
	@if command -v "$(firstword $(OPEN_CMD))" >/dev/null 2>&1; then \
		$(OPEN_CMD) "$(URL)" || printf '[WARN] Browser launch failed. Open %s manually.\n' "$(URL)"; \
	else \
		printf '[WARN] "$(firstword $(OPEN_CMD))" not found. Open %s manually.\n' "$(URL)"; \
	fi

clean:
	@$(MAKE) stop
	@printf '[>] Cleaning git-ignored files (git clean -fdx)...\n'
	@git clean -fdx
	@printf '[OK] Clean.\n'

publish: prepare-frontend prepare-plugins
	@printf '\n== Publish - $(VERSION) ==\n'
	@printf '  Commit:     $(GIT_COMMIT)\n'
	@printf '  Build time: $(BUILD_TIME)\n\n'
	@rm -rf "$(PUBLISH)"
	@mkdir -p "$(PUBLISH)"
	@printf '[>] Building %s platforms...\n' "$(words $(PUBLISH_PLATFORMS))"
	@for plat in $(PUBLISH_PLATFORMS); do \
		_goos=$$(echo $$plat | cut -d/ -f1); \
		_goarch=$$(echo $$plat | cut -d/ -f2); \
		if [ "$$_goos" = "windows" ]; then _bin="$(BINARY).exe"; else _bin="$(BINARY)"; fi; \
		mkdir -p "$(PUBLISH)/$${_goos}_$${_goarch}"; \
		printf '[>] Building %s/%s...\n' "$$_goos" "$$_goarch"; \
		GOOS=$$_goos GOARCH=$$_goarch go build -ldflags="$(LDFLAGS)" -o "$(PUBLISH)/$${_goos}_$${_goarch}/$$_bin" . || { printf '[FAIL] Build failed: %s/%s\n' "$$_goos" "$$_goarch"; exit 1; }; \
		(cd "$(PUBLISH)/$${_goos}_$${_goarch}" && zip -q "$(BINARY)_$${_goos}_$${_goarch}.zip" "$$_bin") || { printf '[FAIL] Archive failed: %s/%s\n' "$$_goos" "$$_goarch"; exit 1; }; \
		$(SHA256) "$(PUBLISH)/$${_goos}_$${_goarch}/$(BINARY)_$${_goos}_$${_goarch}.zip" > "$(PUBLISH)/_cksum_$${_goos}_$${_goarch}.tmp"; \
		printf '[OK] Done: %s/%s\n' "$$_goos" "$$_goarch"; \
	done
	@cat $(PUBLISH)/_cksum_*.tmp 2>/dev/null | sort > "$(PUBLISH)/checksums.txt"
	@rm -f $(PUBLISH)/_cksum_*.tmp
	@printf '\n[OK] Artifacts in  $(PUBLISH)/\n'
	@printf '[OK] Checksums in  $(PUBLISH)/checksums.txt\n'

go-check:
	@printf '[>] Running go vet...\n'
	@go vet ./...
	@printf '[OK] go vet passed\n'

go-test:
	@printf '[>] Running go test...\n'
	@go test ./...
	@printf '[OK] go test passed\n'
