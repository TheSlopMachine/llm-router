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

- **Dev** (`make start`): `frontend --mode dev` ensures `internal/dashboard/build/web/index.html` exists (stub only if missing, never overwrites real build; no `vite build`), then spawns `go run .` + `bun run dev` detached with JSON pidfile `{backend,frontend,vitePort}`. Vite on `:VITE_PORT` proxies `/api/llm-router/*` → backend `:WEB_PORT`.
- **Publish** (`make publish`): `frontend --mode build` runs `vite build` into `internal/dashboard/build/web`, then multi-platform `go build` embeds it, zips with `archive/zip`, hashes with `crypto/sha256`.

## Strictness

No fallbacks. Any `git` / `swag` / `api-types` / `vite build` failure is fatal. `stop` uses one graceful signal only (`SIGTERM` / `taskkill /PID` without `/F` or `/T`); survivors keep the pidfile and exit 1.
