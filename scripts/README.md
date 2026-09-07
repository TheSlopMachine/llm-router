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
| `start` | `HOST`, `WEB_PORT`, `API_PORT`, `DEV_DB`*, `DEV_KEY`*, `PID_FILE` | `make start` / `make restart` |
| `stop` | `PID_FILE` | `make stop` / `make restart` / `make clean` |
| `status` | `PID_FILE` | `make status` |
| `browser` | `URL` (default `http://HOST:WEB_PORT`) | `make browser` |
| `publish` | `VERSION` (default `dev`), `PUBLISH_PLATFORMS`* | `make publish` |
| `help` | display only: `HOST`, `WEB_PORT`, `API_PORT`, `URL`, `DEV_DB`, `DEV_KEY` | `make help` |
| _(env)_ | `NO_SKIP` (`1`/`true`/`yes`/`on` disables caches) | `make init NO_SKIP=1` or `NO_SKIP=1 make init` |
| `vet` / `test` / `fcheck` | — | `make go-check` / `make go-test` / `make check-frontend` |

`*` required (fatal when blank). The rest fall back to dev defaults (`localhost`, `8080`, `8081`, `~/.local/llm-router/...`, temp pidfile).

## Dev vs publish

- **Init** (`make init`): `bun install`, `swag` (`web/openapi.yaml` from `internal/**/*.go`), `generate:api-types` (`web/src/lib/generated/api-types.ts`), dev embed stub (`internal/dashboard/build/web/index.html`, created only when missing). Safe to re-run and safe for agents.
- **Dev** (`make start` → `init`, then `start`): refuses to run if the pidfile shows a still-alive backend/frontend (run `make stop` first); a stale pidfile pointing at dead processes is cleared automatically. Then spawns `go run .` + `bun run dev` detached with JSON pidfile `{backend,frontend,vitePort}`. The backend is started with `--web 38473` (dev-internal, hardcoded) + `--dev-ui-redirect http://HOST:WEB_PORT`, so any navigation not proxied to `WEB_PORT` is 302'd there instead of serving the placeholder stub. `WEB_PORT` proxies `/api/llm-router/*` → backend `:38473`.
- **Publish** (`make publish` → `init`, then `publish`): `vite build` into `internal/dashboard/build/web`, then multi-platform `go build` embeds it (without `--dev-ui-redirect`, so the redirect never fires), zips with `archive/zip`, hashes with `crypto/sha256`.

## Caching (`NO_SKIP`)

`init` caches two steps and skips them when up to date:

- `bun install` — skipped when `web/node_modules` is newer than `web/bun.lock` + `web/package.json`.
- OpenAPI — `swag` (`web/openapi.yaml` from `internal/**/*.go`) skipped when `openapi.yaml` is newer than all `internal/**/*.go`; `generate:api-types` (`web/src/lib/generated/api-types.ts`) skipped when newer than `openapi.yaml`.

`NO_SKIP=1` (also `true`/`yes`/`on`, case-insensitive) disables both caches and forces the full generation. Read from env only. `export NO_SKIP` in `Makefile` makes `make init NO_SKIP=1` and `NO_SKIP=1 make init` equivalent; `NO_SKIP=0`/unset is the default (skipping allowed).

## Strictness

No fallbacks. Missing required env, any `git` / `swag` / `api-types` / `vite build` failure is fatal. `stop` uses one graceful signal only (`SIGTERM` on unix, `CTRL_BREAK_EVENT` to the process group on Windows — not a `TerminateProcess` force-kill); survivors keep the pidfile and exit 1.
