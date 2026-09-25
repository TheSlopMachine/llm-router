# scripts/

Separate Go module for build/dev helpers. Never imported by the parent module, never added to `go.work`. Every `make` recipe invokes via `GOWORK=off`.

```
module github.com/TheSlopMachine/llm-router/scripts
```

Configuration flows one way: `Makefile` vars → env → scripts. Scripts take no CLI flags.

## Scripts

| Script | Env | Called by |
|--------|-----|-----------|
| `init` | `HOST`, `WEB_PORT`, `NO_SKIP` | `make init`; `make start` / `make publish` (via dep) |
| `start` | `HOST`, `WEB_PORT`, `API_PORT`, `LOG_LEVEL` (default `info`), `NO_AUTH` (default `0`), `DEV_DB`*, `PID_FILE` | `make start` / `make restart` |
| `stop` | `PID_FILE` | `make stop` / `make restart` / `make clean` |
| `status` | `PID_FILE` | `make status` |
| `browser` | `URL` (default `http://HOST:WEB_PORT`) | `make browser` |
| `publish` | `VERSION` (default `dev`), `PUBLISH_PLATFORMS`* | `make publish` |
| `help` | static text (`help.txt`), no code | `make help` |
| `smoke` | `SMOKE_PLUGINS` (default `mock`), `SMOKE_TARGETS` (default `completions,messages`), `SMOKE_STORE_DIR`, `SMOKE_CLEANUP` (default `1`) | `make smoke` (restarts with `NO_AUTH=1` first) |
| _(env)_ | `NO_SKIP` (`1`/`true`/`yes`/`on` disables caches) | `make init NO_SKIP=1` or `NO_SKIP=1 make init` |
| `vet` / `test` / `fcheck` | `PKG` (default `./...`; `scripts/`-relative patterns run inside the scripts module) / `PKG` (default `./...`) / — | `make go-vet` / `make go-test` / `make go-fmt-check` |
| `fmt` | `FMT_WRITE=1` writes, otherwise checks; `PATHS` narrows to space-separated root-relative files/dirs (default: whole tree) | `make go-fmt` / `make go-fmt-check` |

`make go-test PKG=./internal/services/router/` scopes the run; `make go-vet PKG=./internal/services/router/...` scopes vet; `make go-fmt PATHS="internal/server scripts/smoke"` formats only those trees. Unscoped `go vet` covers the root and `scripts` modules; unscoped `go test` covers the root module. Both skip `.workspace`, `node_modules`, `build`, `.git`.

`*` required (fatal when blank). The rest fall back to dev defaults (`localhost`, `38080`, `38081`, `~/.local/llm-router/...`, temp pidfile).

## Dev vs publish

- **Init** (`make init`): `bun install`, `swag` (`web/openapi.yaml` from `internal/**/*.go`), `generate:api-types` (`web/src/lib/generated/api-types.ts`), dev embed stub (`internal/dashboard/build/web/index.html`, created only when missing). Safe to re-run and safe for agents.
- **Dev** (`make start` → `init`, then `start`): refuses to run if the pidfile shows a still-alive backend/frontend (run `make stop` first); a stale pidfile pointing at dead processes clears automatically. Then spawns `go run .` + `bun run dev` detached with JSON pidfile `{backend,frontend,vitePort}`. The backend starts with `--web 38473` (dev-internal, hardcoded) + `--dev-ui-redirect http://HOST:WEB_PORT`, so any navigation not proxied to `WEB_PORT` redirects there instead of serving the placeholder stub. `WEB_PORT` proxies `/api/llm-router/*` → backend `:38473`.
- **Publish** (`make publish` → `init`, then `publish`): `vite build` into `internal/dashboard/build/web`, then multi-platform `go build` embeds it (without `--dev-ui-redirect`, so the redirect never fires), zips with `archive/zip`, hashes with `crypto/sha256`.

## Smoke (`make smoke`)

Restarts the dev stack with `NO_AUTH=1`, waits for readiness, then drives the wire surfaces black-box: status → bootstrap → plugin install → provider + dev-database credentials → model matrix (one model per capability, first success closes it) → cleanup of created credentials. Missing credentials skip the plugin, never fail it. Quota, payment, rate and missing-model outcomes skip with reason; anything else fails. Exit 0 means clean (skips allowed).

## Caching (`NO_SKIP`)

`init` caches two steps and skips them when up to date:

- `bun install` — skipped when `web/node_modules` is newer than `web/bun.lock` + `web/package.json`.
- OpenAPI — `swag` (`web/openapi.yaml` from `internal/**/*.go`) skipped when `openapi.yaml` is newer than all `internal/**/*.go`; `generate:api-types` (`web/src/lib/generated/api-types.ts`) skipped when newer than `openapi.yaml`.

`NO_SKIP=1` (also `true`/`yes`/`on`, case-insensitive) disables both caches and forces the full generation. Reads from env only. `export NO_SKIP` in `Makefile` makes `make init NO_SKIP=1` and `NO_SKIP=1 make init` equivalent; `NO_SKIP=0`/unset is the default (skipping allowed).

## Strictness

No fallbacks. Missing required env, any `git` / `swag` / `api-types` / `vite build` failure is fatal. `stop` uses one graceful signal only (`SIGTERM` on unix, `CTRL_BREAK_EVENT` to the process group on Windows — not a `TerminateProcess` force-kill); survivors keep the pidfile and exit 1.
