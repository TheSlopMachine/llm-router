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
ADAPTERS_GO := adapters.go

PUBLISH_PLATFORMS ?= windows/amd64 windows/386 windows/arm64 linux/amd64 linux/386 linux/arm64 linux/arm darwin/amd64 darwin/arm64 freebsd/amd64 freebsd/386 freebsd/arm64

WORKSPACE_DIR := .workspace
GO_WORK       := go.work

WORKSPACE_REMOTE ?= https

URL ?= http://localhost:8080

ifeq ($(OS),Windows_NT)
  DEV_DB ?= $(subst \,/,$(USERPROFILE))/.local/llm-router/llm-router-dev.db
  DEV_KEY ?= $(subst \,/,$(USERPROFILE))/.local/llm-router/llm-router-dev.key
else
  DEV_DB ?= $(HOME)/.local/llm-router/llm-router-dev.db
  DEV_KEY ?= $(HOME)/.local/llm-router/llm-router-dev.key
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

# file:// URI for LOG_FILE display (RFC 8089) — preserves LOG_FILE for FS ops
empty :=
space := $(empty) $(empty)
_LOG_URI_ESC := $(subst $(space),%20,$(LOG_FILE))
ifeq ($(OS),Windows_NT)
  LOG_URI := file:///$(_LOG_URI_ESC)
else
  LOG_URI := file://$(_LOG_URI_ESC)
endif

LDFLAGS = -s -w -X main.Version=$(VERSION) -X main.GitCommit=$(GIT_COMMIT) -X main.BuildTime=$(BUILD_TIME)

.PHONY: help prepare-workspace prepare-frontend prepare start stop restart status browser clean publish go-check go-test

help:
	@printf '\nUsage: make <target>\n\n'
	@printf 'Targets:\n'
	@printf '  prepare-workspace Clone adapters from %s to %s and generate %s + %s\n' "$(ADAPTERS)" "$(WORKSPACE_DIR)" "$(GO_WORK)" "$(ADAPTERS_GO)"
	@printf '  prepare-frontend  npm install && build frontend\n'
	@printf '  prepare           prepare-frontend + prepare-workspace\n'
	@printf '  start             Build dev binary to temp and start daemon (db %s)\n' "$(DEV_DB)"
	@printf '  stop              Stop daemon\n'
	@printf '  restart           Stop + start\n'
	@printf '  status            Show dev server status (pid/log)\n'
	@printf '  browser           Open dashboard in browser (%s)\n' "$(URL)"
	@printf '  clean             Stop + git clean -fdx\n'
	@printf '  publish           prepare-frontend + prepare-workspace + build all PUBLISH_PLATFORMS\n'
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

prepare-workspace:
	@$(MAKE) stop
	@mkdir -p "$(WORKSPACE_DIR)"
	@if [ ! -f "$(ADAPTERS)" ]; then \
		printf '# External adapter registry\n# Format: <module-path>\n' > "$(ADAPTERS)"; \
	fi
	@grep -v '^#' "$(ADAPTERS)" 2>/dev/null | grep -v '^$$' | while read -r mod; do \
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
		if [ -d "$$dir" ]; then \
			if [ ! -d "$$dir/.git" ]; then \
				printf '[!] Local dir %s exists without .git — skip clone (local dev adapter)\n' "$$dir"; \
			else \
				cur_url=$$(git -C "$$dir" remote get-url origin 2>/dev/null || echo ""); \
				if [ -z "$$cur_url" ]; then \
					printf '[>] Exists %s (no remote), skip\n' "$$dir"; \
				elif [ "$$cur_url" != "$$url" ]; then \
					printf '[>] Updating remote %s: %s -> %s\n' "$$dir" "$$cur_url" "$$url"; \
					git -C "$$dir" remote set-url origin "$$url" || true; \
				else \
					printf '[>] Exists %s, skip\n' "$$dir"; \
				fi; \
			fi; \
		else \
			printf '[>] Cloning %s (%s) -> %s\n' "$$mod" "$(WORKSPACE_REMOTE)" "$$dir"; \
			git clone "$$url" "$$dir" || { printf '[WARN] clone failed for %s (repo may not exist yet) — skip\n' "$$mod"; continue; }; \
		fi; \
	done
	@printf '[>] Generating %s...\n' "$(GO_WORK)"
	@printf 'go 1.25.0\n\nuse (\n  .\n' > "$(GO_WORK)"
	@for d in $(WORKSPACE_DIR)/*; do [ -d "$$d" ] && printf '  ./%s\n' "$$d" >> "$(GO_WORK)"; done
	@printf ')\n' >> "$(GO_WORK)"
	@printf '[>] Generating %s...\n' "$(ADAPTERS_GO)"
	@printf '// Code generated by make prepare-workspace. DO NOT EDIT.\n\npackage main\n\n' > "$(ADAPTERS_GO)"
	@if grep -q '^[^#]' "$(ADAPTERS)" 2>/dev/null; then \
		printf 'import (\n' >> "$(ADAPTERS_GO)"; \
		grep -v '^#' "$(ADAPTERS)" | grep -v '^$$' | while read -r mod; do \
			mod=$$(printf '%s' "$$mod" | tr -d '\r' | xargs); \
			[ -z "$$mod" ] && continue; \
			printf '\t_ "%s"\n' "$$mod" >> "$(ADAPTERS_GO)"; \
		done; \
		printf ')\n' >> "$(ADAPTERS_GO)"; \
	fi
	@go work sync 2>/dev/null || true
	@printf '[OK] Workspace ready (%s + %s + %s)\n' "$(WORKSPACE_DIR)" "$(GO_WORK)" "$(ADAPTERS_GO)"

prepare-frontend:
	@$(MAKE) stop
	@printf '\n== Frontend ==\n'
	@mkdir -p "$(UI_DIR)/src/lib/generated"
	@cd "$(UI_DIR)" && $(NPM) install --no-audit --no-fund
	@cd "$(UI_DIR)" && $(NPM) run build
	@printf '[OK] Frontend ready.\n'

prepare: prepare-frontend prepare-workspace
	@printf '[OK] Prepare done\n'

start:
	@mkdir -p "$(dir $(DEV_DB))" "$(dir $(PID_FILE))"
	@if [ -f "$(PID_FILE)" ] && kill -0 $$(cat "$(PID_FILE)") 2>/dev/null; then \
		printf '[>] Already running PID %s (log %s)\n' "$$(cat $(PID_FILE))" "$(LOG_URI)"; \
		printf 'Dashboard: http://localhost:8080\n'; \
		printf 'API: http://localhost:8081/v1\n'; \
		if [ -f "$(DEV_KEY)" ]; then printf 'API Key: %s\n' "$$(cat $(DEV_KEY))"; fi; \
		exit 0; \
	fi; \
	rm -f "$(PID_FILE)"; \
	printf '[>] Building dev binary to %s...\n' "$(TMP_BIN)"; \
	go build -o "$(TMP_BIN)" . || exit 1; \
	printf '[>] Starting llm-router --web 8080 --api 8081 --db %s --testing-key %s (pid %s, log %s)...\n' "$(DEV_DB)" "$(DEV_KEY)" "$(PID_FILE)" "$(LOG_URI)"; \
	if command -v nohup >/dev/null 2>&1; then \
		nohup "$(TMP_BIN)" --web 8080 --api 8081 --db "$(DEV_DB)" --testing-key "$(DEV_KEY)" > "$(LOG_FILE)" 2>&1 & echo $$! > "$(PID_FILE)"; \
	else \
		"$(TMP_BIN)" --web 8080 --api 8081 --db "$(DEV_DB)" --testing-key "$(DEV_KEY)" > "$(LOG_FILE)" 2>&1 & echo $$! > "$(PID_FILE)"; \
	fi; \
	sleep 0.3; \
	if kill -0 $$(cat "$(PID_FILE)") 2>/dev/null; then \
		printf '[OK] Started PID %s\n' "$$(cat $(PID_FILE))"; \
		printf 'Dashboard: http://localhost:8080\n'; \
		printf 'API: http://localhost:8081/v1\n'; \
		if [ -f "$(DEV_KEY)" ]; then printf 'API Key: %s\n' "$$(cat $(DEV_KEY))"; fi; \
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
	printf 'Log: %s\n' "$(LOG_URI)"

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

publish: prepare-frontend prepare-workspace
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
