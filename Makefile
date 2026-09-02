# =============================================================================
# Makefile  -  cross-platform replacement for proj.cmd
# Requires: go, npm, zip, git
#           sha256sum (Linux / Git Bash) or shasum (macOS)
# Works on: Linux, macOS, Windows (via make.ps1 / make.cmd, Git Bash, MSYS2, WSL)
# =============================================================================

# -- Shell ---------------------------------------------------------------------
# On Windows use make.ps1 which prepends Git's usr/bin to PATH so bash and
# coreutils (printf, mkdir ...) are visible to CreateProcess.
# NOTE: $(shell ...) at parse time still bypasses SHELL on Windows32 Make, so
# the OS blocks below only call native executables (go, git, powershell).
SHELL      := bash
.SHELLFLAGS := -c

VERSION    ?= dev
BINARY     := llm-router
UI_DIR     := web
PUBLISH    := build/release
ADAPTERS   := adapters.conf
PLUGINS    := plugins/plugins.go
DASHBOARD_PORT ?= 8080
API_PORT       ?= 8081
PORT           ?= $(DASHBOARD_PORT)
URL            ?= http://localhost:$(DASHBOARD_PORT)
QUICK      ?= 0
ifeq ($(OS),Windows_NT)
  DEV_DB ?= $(subst \,/,$(USERPROFILE))/.local/llm-router/llm-router-dev.db
else
  DEV_DB ?= $(HOME)/.local/llm-router/llm-router-dev.db
endif

# -- OS-specific settings ------------------------------------------------------
ifeq ($(OS),Windows_NT)
  # --- Windows: parse-time calls use only native Windows executables ----------
  LOCAL_BIN  := $(BINARY).exe
  NPM        := npm.cmd
  # go.exe is a native Windows binary - CreateProcess can invoke it directly.
  # $(subst) converts backslashes (C:\Users\...) to forward slashes so the
  # resulting path is usable inside Git Bash recipes.
  GOPATH     := $(subst \,/,$(shell go env GOPATH))
  # git.exe is also natively callable.  If the repo check fails the var is empty.
  _GIT_RAW   := $(shell git rev-parse --short HEAD)
  GIT_COMMIT := $(if $(_GIT_RAW),$(_GIT_RAW),unknown)
  # powershell.exe is natively callable.  'Z' is not a .NET format specifier
  # so it is emitted as a literal character in the output.
  BUILD_TIME := $(shell powershell -NoProfile -Command \
    "[System.DateTime]::UtcNow.ToString('yyyy-MM-ddTHH:mm:ssZ')")
  # sha256sum ships with Git for Windows coreutils; used in bash recipes below.
  SHA256     := sha256sum
  # powershell.exe is natively callable and can launch the default browser.
  OPEN_CMD   := powershell -NoProfile -Command Start-Process
else
  # --- Unix (Linux / macOS / MSYS2 / WSL) ------------------------------------
  SHELL      := bash
  NPM        := npm
  UNAME_S    := $(shell uname -s)
  ifneq ($(filter MINGW% CYGWIN%,$(UNAME_S)),)
    LOCAL_BIN := $(BINARY).exe
    OPEN_CMD  := powershell -NoProfile -Command Start-Process
  else ifeq ($(UNAME_S),Darwin)
    LOCAL_BIN := $(BINARY)
    OPEN_CMD  := open
  else ifneq ($(shell grep -qi microsoft /proc/version 2>/dev/null && echo wsl),)
    # WSL: plain xdg-open usually has nothing to hand off to. Prefer wslview
    # (from the 'wslu' package) if installed, else fall back to cmd.exe.
    LOCAL_BIN := $(BINARY)
    OPEN_CMD  := $(if $(shell command -v wslview 2>/dev/null),wslview,cmd.exe /c start "")
  else
    LOCAL_BIN := $(BINARY)
    OPEN_CMD  := xdg-open
  endif
  GOPATH     := $(shell go env GOPATH 2>/dev/null)
  GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
  BUILD_TIME := $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
  SHA256     := $(shell command -v sha256sum 2>/dev/null || echo "shasum -a 256")
endif

LDFLAGS     = -s -w \
              -X main.Version=$(VERSION) \
              -X main.GitCommit=$(GIT_COMMIT) \
              -X main.BuildTime=$(BUILD_TIME)

# -- Release target platforms --------------------------------------------------
PLATFORMS := \
  windows/amd64 windows/386   windows/arm64 \
  linux/amd64   linux/386     linux/arm64   linux/arm \
  darwin/amd64  darwin/arm64 \
  freebsd/amd64 freebsd/386   freebsd/arm64

PLATFORM_TARGETS := $(addprefix _build_,$(subst /,_,$(PLATFORMS)))

.PHONY: help run build release clean prepare-frontend generate-plugins FORCE

# -- Default target ------------------------------------------------------------
help:
	@printf '\nUsage: make <target>\n\n'
	@printf 'Targets:\n'
	@printf '  run      Build frontend, then run the server\n'
	@printf '  build    Build frontend, compile for current platform\n'
	@printf '  release  Build frontend once, compile all platforms in parallel\n'
	@printf '  clean    Remove $(PUBLISH)/ and local binaries\n\n'
	@printf 'Variables:\n'
	@printf '  VERSION  Release version tag  (default: dev)\n'
	@printf '  QUICK    Skip adapters/svelte for run (QUICK=1)\n\n'
	@printf 'Examples:\n'
	@printf '  make run\n'
	@printf '  make run QUICK=1\n'
	@printf '  make build\n'
	@printf '  VERSION=1.2.0 make release\n\n'
	@printf 'Adapters:\n'
	@printf '  Edit %s directly - format: <module-path> <full-commit-hash>\n\n' "$(ADAPTERS)"

# -- generate-plugins ----------------------------------------------------------
ifeq ($(filter 1 true,$(QUICK)),)
generate-plugins:
	@printf '\n== Plugin Generation ==\n'
	@mkdir -p plugins
	@if [ ! -f "$(ADAPTERS)" ]; then \
		printf '# External adapter registry\n# Format: <module-path> <full-commit-hash>\n# Example: github.com/user/adapter 01286aaf5620fb7b4a0f108f96ac7751ae3d7040\n' > "$(ADAPTERS)"; \
	fi
	@printf '// Code generated by make generate-plugins. DO NOT EDIT.\n\npackage plugins\n\n' > "$(PLUGINS)"
	@if [ -s "$(ADAPTERS)" ] && grep -q '^[^#]' "$(ADAPTERS)" 2>/dev/null; then \
		printf 'import (\n' >> "$(PLUGINS)"; \
		grep -v '^#' "$(ADAPTERS)" | grep -v '^$$' | while read -r mod query; do \
			if ! printf '%s' "$$query" | grep -qE '^[0-9a-f]{40}$$'; then \
				printf '[ERROR] %s: "%s" is not a full 40-char commit hash\n' "$$mod" "$$query"; \
				exit 1; \
			fi; \
			printf '[>] Adding adapter via git: %s@%s\n' "$$mod" "$$query"; \
			GOPROXY=direct go get "$$mod@$$query" || exit 1; \
			printf '\t_ "%s"\n' "$$mod" >> "$(PLUGINS)"; \
		done || exit 1; \
		printf ')\n' >> "$(PLUGINS)"; \
		printf '[>] Tidying module...\n'; \
		go mod tidy || exit 1; \
	fi
	@printf '[OK] Plugins generated.\n'
else
generate-plugins:
	@printf '[>] QUICK mode - skipping adapter loading.\n'
endif

# -- prepare-frontend ----------------------------------------------------------
ifeq ($(filter 1 true,$(QUICK)),)
prepare-frontend: generate-plugins
	@printf '\n== Frontend ==\n'
	mkdir -p "$(UI_DIR)/src/lib/generated"
	cd "$(UI_DIR)" && $(NPM) install
	cd "$(UI_DIR)" && $(NPM) run build
	@printf '[OK] Frontend ready.\n'
else
prepare-frontend: generate-plugins
	@printf '[>] QUICK mode - skipping frontend build.\n'
endif

# -- run -----------------------------------------------------------------------
# QUICK=1 skips adapters and svelte build — goes straight to `go run .`
run: prepare-frontend
	@if [ "$(QUICK)" = "1" ] || [ "$(QUICK)" = "true" ]; then printf '[>] QUICK mode enabled - adapters and svelte build skipped.\n'; fi
	@printf '[>] Starting dashboard %s and API %s, will open %s once it responds...\n' "http://localhost:$(DASHBOARD_PORT)" "http://localhost:$(API_PORT)" "$(URL)"
	@printf '[>] Using dev DB: %s\n' "$(DEV_DB)"
	@mkdir -p "$(dir $(DEV_DB))"
	@( \
		i=0; \
		while [ $$i -lt 60 ]; do \
			if (exec 3<>"/dev/tcp/localhost/$(DASHBOARD_PORT)") 2>/dev/null; then \
				exec 3>&- 3<&-; \
				if command -v "$(firstword $(OPEN_CMD))" >/dev/null 2>&1; then \
					$(OPEN_CMD) "$(URL)" || printf '[WARN] Browser launch command failed. Open %s manually.\n' "$(URL)"; \
				else \
					printf '[WARN] "$(firstword $(OPEN_CMD))" not found on PATH. Open %s manually.\n' "$(URL)"; \
				fi; \
				exit 0; \
			fi; \
			sleep 0.5; i=$$((i+1)); \
		done; \
		printf '[WARN] Timed out waiting for %s to come up - open it manually.\n' "$(URL)" \
	) &
	go run . --dashboard-port $(DASHBOARD_PORT) --api-port $(API_PORT) --db "$(DEV_DB)"

# -- build (current platform only) --------------------------------------------
build: prepare-frontend
	@printf '[>] Compiling for current platform...\n'
	go build -o "$(LOCAL_BIN)" .
	@printf '[OK] Binary: $(LOCAL_BIN)\n'

# -- Per-platform build (pattern rule) ----------------------------------------
#
# A single pattern rule replaces the old define/call/eval template.
# On GNU Make "Built for Windows32", $$ escaping in define-block recipes is
# broken: $$_goos is parsed as Make-variable $_ (empty) + literal "goos",
# not as a deferred shell variable.  A pattern rule avoids that entirely:
# $* is a proper Make automatic variable (the stem, e.g. "linux_amd64"),
# expanded by Make before the shell sees the recipe.  $(subst _,/,$*) turns
# the stem back into a GOOS/GOARCH pair without any space-as-replacement
# tricks.  All values are baked in as literals before the shell runs.

FORCE:

_build_%: FORCE
	@set -e; \
	_goos="$(patsubst %/,%,$(dir $(subst _,/,$*)))"; \
	_goarch="$(notdir $(subst _,/,$*))"; \
	if [ "$$_goos" = "windows" ]; then _bin="$(BINARY).exe"; else _bin="$(BINARY)"; fi; \
	mkdir -p "$(PUBLISH)/$${_goos}_$${_goarch}"; \
	printf '[>] Building %s/%s...\n' "$$_goos" "$$_goarch"; \
	GOOS=$$_goos GOARCH=$$_goarch go build -ldflags="$(LDFLAGS)" \
	    -o "$(PUBLISH)/$${_goos}_$${_goarch}/$$_bin" . \
	    || { printf '[FAIL] Build failed: %s/%s\n' "$$_goos" "$$_goarch"; exit 1; }; \
	(cd "$(PUBLISH)/$${_goos}_$${_goarch}" && zip -q "$(BINARY)_$${_goos}_$${_goarch}.zip" "$$_bin") \
	    || { printf '[FAIL] Archive failed: %s/%s\n' "$$_goos" "$$_goarch"; exit 1; }; \
	$(SHA256) "$(PUBLISH)/$${_goos}_$${_goarch}/$(BINARY)_$${_goos}_$${_goarch}.zip" \
	    > "$(PUBLISH)/_cksum_$${_goos}_$${_goarch}.tmp"; \
	printf '[OK] Done: %s/%s\n' "$$_goos" "$$_goarch"

# -- release -------------------------------------------------------------------
#
# 1. Prepares frontend once.
# 2. Spawns all platform builds in parallel via $(MAKE) -j.
#    make already tracks failures: any failing _build_* target causes make to
#    report an error and exit non-zero after parallel jobs finish, which is
#    equivalent to the original's fail-marker / counter approach.
# 3. Merges and sorts the isolated per-build checksum temp files into one
#    manifest, then removes the temp files.

release: prepare-frontend
	@printf '\n== Release - $(VERSION) ==\n'
	@printf '  Commit:     $(GIT_COMMIT)\n'
	@printf '  Build time: $(BUILD_TIME)\n\n'
	rm -rf "$(PUBLISH)"
	mkdir -p "$(PUBLISH)"
	@printf '[>] Building $(words $(PLATFORMS)) platforms in parallel...\n'
	$(MAKE) -j $(PLATFORM_TARGETS)
	@# Merge per-build checksum temp files into one sorted manifest
	@cat $(PUBLISH)/_cksum_*.tmp 2>/dev/null | sort > "$(PUBLISH)/checksums.txt"
	@rm -f $(PUBLISH)/_cksum_*.tmp
	@printf '\n[OK] Artifacts in  $(PUBLISH)/\n'
	@printf '[OK] Checksums in  $(PUBLISH)/checksums.txt\n'

# -- clean ---------------------------------------------------------------------
clean:
	@printf '[>] Cleaning ignored build artifacts...\n'
	git clean -fdX
	@printf '[OK] Clean.\n'