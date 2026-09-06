# scripts/

Separate Go module for build/dev helpers. Never imported by the parent module, never added to `go.work`. Every `make` recipe invokes via `GOWORK=off`.

```
module github.com/TheSlopMachine/llm-router/scripts
```

## Scripts

| Script | Flags | Called by |
|--------|-------|-----------|
| `workspace` | `--remote https\|ssh` | `start` / `publish` (implicit) |
| `frontend` | `--mode dev\|build --host --vite-port` | `start` (dev) / `publish` (build) |
| `start` | `--host --web-port --api-port --vite-port --db --testing-key --remote --pid-file` | `make start` / `make restart` |
| `stop` | `--pid-file` | `make stop` / `make restart` / `make clean` |
| `status` | `--pid-file` | `make status` |
| `browser` | `--url` | `make browser` |
| `publish` | `--version --platforms --remote` | `make publish` |
| `help` | `--host --web-port --api-port --vite-port --url --dev-db --dev-key --platforms --remote` | `make help` |
| `vet` / `test` / `fcheck` | — | `make go-check` / `make go-test` / `make check-frontend` |

## Dev vs publish

- **Dev** (`make start`): refuses to run if the pidfile shows a still-alive backend/frontend (run `make stop` first); a stale pidfile pointing at dead processes is cleared automatically. Then `frontend --mode dev` ensures `internal/dashboard/build/web/index.html` exists (placeholder stub only if missing, never overwrites real build; no `vite build`), and spawns `go run .` + `bun run dev` detached with JSON pidfile `{backend,frontend,vitePort}`. The backend is started with `--dev-ui-redirect http://HOST:VITE_PORT`, so any dashboard navigation that isn't proxied straight to Vite (see `web/vite.config.ts`) is 302'd there instead of serving the placeholder stub. Vite on `:VITE_PORT` proxies `/api/llm-router/*` → backend `:WEB_PORT`.
- **Publish** (`make publish`): `frontend --mode build` runs `vite build` into `internal/dashboard/build/web`, then multi-platform `go build` embeds it (without `--dev-ui-redirect`, so the redirect never fires), zips with `archive/zip`, hashes with `crypto/sha256`.

## Strictness

No fallbacks. Any `git` / `swag` / `api-types` / `vite build` failure is fatal. `stop` uses one graceful signal only (`SIGTERM` on unix, `CTRL_BREAK_EVENT` to the process group on Windows — not a `TerminateProcess` force-kill); survivors keep the pidfile and exit 1.
