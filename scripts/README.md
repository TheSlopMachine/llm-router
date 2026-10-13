# scripts/

Separate Go module for build/dev helpers. Never imported by the parent module, never added to `go.work`. Every `make` recipe invokes via `GOWORK=off`.

```
module github.com/TheSlopMachine/llm-router/scripts
```

Configuration flows one way: `.env` file, then `Makefile` vars → env → scripts. `make VAR=value` wins over the file. Scripts take no CLI flags.

## Scripts

| Script | Env | Called by |
|--------|-----|-----------|
| `init` | `HOST`, `PORT`, `NO_SKIP` | `make init`; `make start` / `make build` (via dep) |
| `start` | `HOST`, `PORT`, `ADMIN_PORT` (default `38082`, empty disables), `SCOPE` (`all`/`backend`, default `all`), `LOG_LEVEL` (default `info`), `DB_PATH` (fallback `DEV_DB`, then default dev path), `CACHE_PATH` (fallback `DEV_CACHE`, then default dev cache path), `DEV_UI_REDIRECT`, `ENV_FILE`, `PID_FILE` | `make start` / `make restart` |
| `stop` | `PID_FILE`, `SCOPE` (`all`/`backend`, default `all`) | `make stop` / `make restart` / `make clean` (backend: graceful shutdown via admin API first, kill fallback; frontend: always killed) |
| `restart` | same as `start` | `make restart` (backend-only when the frontend lives, else full start) |
| `status` | `PID_FILE` | `make status` |
| `log` | `LINES` (default `100`), `FOLLOW` | `make log` |
| `logfrontend` | `LINES` (default `100`), `FOLLOW` | `make log-frontend` |
| `browser` | `URL` (default `http://HOST:PORT`) | `make browser` |
| `build` | `VERSION` (default: latest git tag, else `dev`), `PLATFORMS`* | `make build` |
| `publish` | `TYPE=fix\|minor\|major` xor `VERSION=X.Y[.Z]`, `NOTES` (optional tag annotation) | `make publish` (clean tree, HEAD synced to origin/main; CI builds on tag push) |
| `help` | static text (`help.txt`), no code | `make help` |
| `smoke` | `SMOKE_PLUGINS` (default `mock`), `SMOKE_TARGETS` (default `completions,messages`), `SMOKE_STORE_DIR`, `SMOKE_CLEANUP` (default `1`) | `make smoke` (running stack, no restart; fails fast when nothing answers) |
| `uploadplugins` | `UPLOAD_STORE_DIR` (default `../../llm-router-store/llm-router-plugins`) | `make upload-plugins` (running stack, targets the admin listener, no restart) |
| _(env)_ | `NO_SKIP` (`1`/`true`/`yes`/`on` disables caches) | `make init NO_SKIP=1` or `NO_SKIP=1 make init` |
| `vet` / `test` / `testrace` / `fcheck` | `PKG` (default `./...`; `scripts/`-relative patterns run inside the scripts module) / `PKG` (default `./...`) / `PKG` (default `./...`) / — | `make go-vet` / `make go-test` / `make go-test-race` / `make check-frontend` |
| `sqlpolicy` | — | `make sql-policy` |
| `fmt` | `FMT_WRITE=1` writes, otherwise checks; `PATHS` narrows to space-separated root-relative files/dirs (default: whole tree) | `make go-fmt` / `make go-fmt-check` |
| `go-tidy` | — | `make go-tidy` |

`make go-test PKG=./internal/services/router/` scopes the run; `make go-vet PKG=./internal/services/router/...` scopes vet; `make go-fmt PATHS="internal/server scripts/smoke"` formats only those trees. Unscoped `go vet` covers the root and `scripts` modules; unscoped `go test` covers the root module. Both skip `.workspace`, `node_modules`, `build`, `.git`.

`*` required (fatal when blank). The rest fall back to dev defaults (`localhost`, `38080`, `~/.local/llm-router/llm-router-dev.sqlite3`, temp pidfile). The dev backend listens on fixed `38081` (`shared.DevBackendPort`).

## Dev vs publish

- **Init** (`make init`): `bun install`, `swag` (`web/openapi.yaml` from `internal/**/*.go`), `generate:api-types` (`web/src/lib/generated/api-types.ts`), dev embed stub (`internal/dashboard/build/web/index.html`, created only when missing). Safe to re-run and safe for agents.
- **Dev** (`make start` → `init`, then `start`): refuses to run if the pidfile shows a still-alive backend/frontend (run `make stop` first); a stale pidfile pointing at dead processes clears automatically. Then spawns the backend + `bun run dev` detached with JSON pidfile `{backend,frontend,vitePort}`. Vite serves user `PORT` (dashboard SPA, proxied to the public backend `:38081`); the backend also serves the admin listener on `127.0.0.1:ADMIN_PORT` (default `38082`, same JSON API without authentication, no dashboard) with `DEV_UI_REDIRECT=http://HOST:PORT`, so any navigation hitting the backend directly redirects to vite instead of serving the placeholder stub. Vite proxies `/api` and `/v1` → public backend `:38081`. Legacy `WEB_PORT`/`API_PORT`/`NO_AUTH` are ignored.
- **Build** (`make build` → `init`, then `build`): `vite build` into `internal/dashboard/build/web`, then multi-platform `go build` embeds it (without `DEV_UI_REDIRECT`, so the redirect never fires), zips with `archive/zip`, hashes with `crypto/sha256`. `VERSION` prefers env, then the latest git tag, then `dev`; missing git degrades to `dev`/`unknown` commit with a warning, never a failure.
- **Publish** (`make publish` → `publish`, no build): verifies a clean tree with `HEAD == origin/main`, bumps the latest tag (`fix`: `Z+1`; `minor`: `Y+1`, short `vX.Y`; `major`: `X+1`, short `vX.0`) or takes forced `VERSION`, then creates and pushes the annotated tag. CI builds the release on tag push. Tags are the source of truth; without git or tags the command fails.

## Smoke (`make smoke`)

Drives the request paths black-box against the running dev stack through the admin listener, with no restart: fails fast when nothing answers (`make start` first, `make restart` after code changes). status → bootstrap → plugin install → provider + dev-database credentials → model matrix (one model per capability, first success closes it) → cleanup of created credentials. Missing credentials skip keyed plugins, never fail them; credential-less provider types run the matrix without accounts. Quota, payment, rate and missing-model outcomes skip with reason; anything else fails. Exit 0 means clean (skips allowed).

## Caching (`NO_SKIP`)

`init` caches two steps and skips them when up to date:

- `bun install` — skipped when `web/node_modules` is newer than `web/bun.lock` + `web/package.json`.
- OpenAPI — `swag` (`web/openapi.yaml` from `internal/**/*.go`) skipped when `openapi.yaml` is newer than all `internal/**/*.go`; `generate:api-types` (`web/src/lib/generated/api-types.ts`) skipped when newer than `openapi.yaml`.

`NO_SKIP=1` (also `true`/`yes`/`on`, case-insensitive) disables both caches and forces the full generation. Reads from env only. `export NO_SKIP` in `Makefile` makes `make init NO_SKIP=1` and `NO_SKIP=1 make init` equivalent; `NO_SKIP=0`/unset is the default (skipping allowed).

## Strictness

No fallbacks. Missing required env, any `git` / `swag` / `api-types` / `vite build` failure is fatal. `stop` waits up to 30s for the admin listener first (the backend binds only after its startup gate), then asks for graceful shutdown with a 15s wait, recorded `ADMIN_PORT` else env, skipped when disabled; then follows the kill path: one graceful `SIGTERM` on unix, direct force-kill on Windows (`TerminateJobObject`, fallback `TerminateProcess`); the frontend is always killed, never asked; survivors keep the pidfile and exit 1.
