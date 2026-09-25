# AGENTS.md

Guidelines for AI agents in `llm-router/`. Read before any change. Treat every rule as absolute. No exceptions. No reinterpretation.

## 0. Wording

Apply this section to all agent output: commit messages, PR descriptions, code comments, log strings, UI copy, CLI help/output, error messages, and doc edits.

### 0.1 No filler phrases

Never pad a sentence without adding information.

DON'T:
- "In order to fix this issue, we need to update the router."
- "It's worth noting that the credential pool is LRU-based."
- "Please note that this change also updates the router."
- "As you can see, the test now passes."

DO:
- "Fix the issue by updating the router."
- "The credential pool is LRU-based."
- "Also update the router."
- "The test now passes."

### 0.2 No implementation-detail asides in UI or CLI

Surface actions and facts only in UI text, CLI output, log lines, and error messages. Never narrate internal conditions there.

DON'T:
- `"Skip validation... (because SKIP_VALIDATION was true)"`
- `"Load models... (isCacheValid == false)"`
- `"Retry request... (attempt < maxRetries)"`
- `"Save credential... (credential.Token != "")"`

DO:
- `"Skip validation"`
- `"Load models"`
- `"Retry request"`
- `"Save credential"`

Move the reasoning to a code comment, a commit message, or drop it. Log debuggable conditions at debug level with structured fields (`reason=rate_limited`).

### 0.3 No casual or vague wording

Use precise technical terms in code, comments, commits, logs, and docs. Banned examples: "blow up", "wire", "sweep", "spin up", "juice", "nuke", "hack", "magic".

DON'T:
- "This blows up if the token is empty."
- "Wire the new adapter into the router."
- "Sweep stale credentials on startup."
- "Spin up a goroutine to poll the queue."

DO:
- "This panics if the token is empty."
- "Register the new adapter with the router."
- "Remove stale credentials on startup."
- "Start a goroutine to poll the queue."

## 1. Project

`llm-router` — single-binary OpenAI-compatible LLM routing gateway. Go backend + embedded Svelte SPA + embedded bbolt DB.

- Routes `ModelId = provider/model` (e.g. `opencode-zen/gpt-5`, `virtual/my-model`) → backend + `CredentialPool`.
- Provider backends are single-file Lua plugins (installed store records in `BucketPlugins`, sourced from plugin store repositories). Built-in Go backends exist only for `custom` (OpenAI-compatible passthrough) and `virtual` (virtual models).

## 2. Structure

```
cmd/root.go              CLI entrypoint (--web/--api/--db)
internal/server/         HTTP server: dashboard (38080), /v1 API (38081)
internal/services/
  token/                 router-token issue/validate
  router/                ModelId → backend + CredentialPool, single pass, no repeats
  provider/              ProviderInstance CRUD (all types, one path)
  credential/            credential pool, usage stats
  virtual/               virtual models (fall-through lists + instruction)
  luaplugin/             Lua execution core: manifest, sandbox, HTTP+SSRF, storage
  pluginrepo/            plugin store: single-URL index repos (repo URL or direct index.json, files resolved against the index directory); code-defined built-in repos (`BuiltinRepos`, seeded on startup, protected from removal)
  modelinfo/             model metadata cache (1h TTL)
  metrics/               1m buckets, 90d retention
  maintenance/           refresh + cleanup (refresh, modelsync, proxy, auth jobs)
  exhausted/             joint limit keys (account/model/proxy), subset match, expiry auto-delete
internal/pool/           single-pass credential failover (unary + stream)
internal/streamgate/     first-byte gate: failover stops after first SSE byte
internal/httpkit/        shared transport helpers (SSE headers)
internal/errors/         domain sentinels + MapUpstream + ToAPIError
internal/repository/     bbolt buckets
internal/dashboard/      admin REST API
internal/api/v1/         OpenAI-compatible /v1/chat/completions, /v1/models, /v1/messages
internal/models/         shared wire types
internal/config/         Config struct
internal/adapters/generic/ built-in custom backend (Go)
providers/virtual/       built-in virtual-models backend (Go)
web/                     Svelte SPA (web/openapi.yaml + src/lib/generated/ auto-generated — do not hand-edit)
scripts/                 separate Go module — build/dev helpers (never imported by main module)
scripts/smoke/           black-box smoke harness + mock provider (testdata/mock.lua)
docs/                    PLUGIN-API.md (binding plugin contract), BACKEND.md (core map), CHANGELOG.md
Makefile                 thin launcher for scripts/ — see §3
```

Keep changes shallow. Do not touch service internals unless the task requires it.

## 3. Commands

### 3.1 Targets — agent runs the `agent` ones directly

| Command | Access | Purpose |
|---|---|---|
| `make check-frontend` | agent | Frontend compile/type-check |
| `make go-vet` | agent | Go static check (`PKG=...` scopes packages, default `./...`) |
| `make go-test` | agent | Go tests (`PKG=...` scopes packages, default `./...`) |
| `make go-fmt` | agent | Apply `gofmt` to both Go modules (`PATHS=...` scopes files/dirs, default whole tree) |
| `make go-fmt-check` | agent | Fail when `gofmt` would reformat anything (`PATHS=...` scopes files/dirs) |
| `make init` | agent | Project init/re-init: bun install, openapi.yaml, api-types, embed stub; skips fresh outputs unless `NO_SKIP=1` |
| `make log` | agent | Tail backend log (`LINES=...` sizes tail, default `100`; `FOLLOW=...` overrides TTY auto-follow) |
| `make log-frontend` | agent | Tail frontend log (same `LINES`/`FOLLOW` contract as `make log`) |
| `make start` | agent | Start dev backend + frontend (vite); `NO_AUTH=1` disables all authorization |
| `make stop` | agent | Stop dev processes |
| `make restart` | agent | Stop + start, keeping last start params |
| `make smoke` | agent | Restart with `NO_AUTH=1`, then run the smoke harness (`SMOKE_PLUGINS=...`, `SMOKE_TARGETS=...`) |
| `make status` | agent | Show dev server status |
| `make help` | agent | Print targets and variables |
| `make go-tidy` | agent | Tidy both Go modules |
| `make browser` | human | Open dashboard in browser |
| `make publish` | human | Build frontend + release binaries |
| `make clean` | human | Stop processes + clean ignored files |

`check-frontend-deps` and `check-publish-deps` are prerequisites, not direct targets. Never run them standalone.

### 3.2 Banned — raw equivalents of the above

| DO | DON'T |
|---|---|
| `make check-frontend` | `bunx svelte-check`, `bun run check`, `tsc --noEmit`, `eslint .` |
| `make go-vet` | `go vet ./...` |
| `make go-vet PKG=...` | `go vet ./internal/...` (scoped raw vet) |
| `make go-test` | `go test ./...` |
| `make go-test PKG=...` | `go test ./internal/...` (scoped raw test) |
| `make go-fmt` / `make go-fmt-check` | `gofmt -w .`, `gofmt -l .` |
| `make go-fmt-check PATHS=...` | `gofmt -l <paths>` (scoped raw gofmt) |

NEVER substitute a raw command for an allowed `make` target, even when the outcome matches.

When no target covers an operation, add one (`Makefile` + `scripts/help.txt` + `scripts/README.md` + this table) instead of running raw commands. A missing target is a gap to fix, never permission to bypass.

### 3.3 Human-only handoff

If the task needs `browser`, `publish`, or `clean`: STOP. Ask the human to run it and report back (terminal output, logs, curl/browser result).

| DO | DON'T |
|---|---|
| "Please run `make publish` and paste the output." | `go run .`, `go build -o ./llm-router.exe`, `bun run dev`, `Start-Process ...`, any script/wrapper invoking these |

## 4. Prohibitions

| # | Prohibition | Detail |
|---|---|---|
| 1 | Kill processes | No `kill`, `pkill`, `taskkill`, `Stop-Process`. Use `make stop` / `make restart` so pidfiles stay consistent. |
| 2 | Delete database files | Never remove `~/.local/llm-router/llm-router-dev.db` (or the Windows equivalent). If it is corrupt, ask the human to delete it. |
| 3 | Run destructive git commands | No `git push`, `git reset`, `git checkout -f`, `git clean`, force-push, amend. Commit only if explicitly instructed, for that exact commit only. |
| 4 | Litter the project | No `*.log`, `*.pid`, `*.tmp`, binaries, scratch files, notes inside the project tree. Use `/tmp` or an external scratch dir. Delete temporary verification files when done. |
| 5 | Avoid the Makefile | No raw command substitutes for an allowed target (§3.2). No manual invocation of a banned target's underlying steps (§3.3). |
| 6 | Build or launch the app outside make targets | `browser`, `publish`, `clean` are human-only. Debug live with `make start NO_AUTH=1` / `make restart` / `make stop` / `make status` — ask the human for anything else. See §3.3, §9. |
| 7 | Redirect output to nul or /dev/null | Breaks on Windows and hides diagnostics on every OS. |
| 8 | Truncate diagnostics output with tail/head | Diagnostics matter and truncating wastes time. NEVER truncate them. |
| 9 | Fall back silently | NEVER swallow a failure and continue on a fallback path. Surface every failure as an error — return it to the caller, log it, or both — or route it to an explicit, named on-fail branch. Never fall through unannounced. |
| 10 | Match errors by string | NEVER match error codes or kinds with `strings.Contains(err.Error(), ...)` or message substrings. Define sentinel errors and match with `errors.Is` / `errors.As`. |
| 11 | Patch a weak API contract in the frontend | Repetitive `??` / `?.` over backend data shapes means the contract is wrong. Fix the backend to return consistent shapes (arrays never null, objects never null when the schema promises them). Frontend guards stay only for genuinely optional local state. |
| 12 | Create stray files from the shell | NEVER let a shell command create a file: no `>` / `>>` / `Out-File` redirection, no heredocs, no `2>/dev/null`, no unquoted fragments that resolve to filenames (`nul`, `null`, command text as filename). Read command output from the tool result, never from disk. |
| 13 | Pass env vars with shell syntax | NEVER `VAR=value make <target>` (Unix-only) and NEVER `$env:VAR="value"; make <target>` (PowerShell-only, leaks state into the session). Use `make <target> VAR=value`: make syntax, works in every shell, scoped to one invocation. |

## 5. Edit discipline

- Re-read the region before every `edit` to the same file. Stale `oldString`
  either no-matches or deletes neighboring code.
- After deleting a function, grep the file's imports immediately (`net/url`,
  `regexp`, `errors`, `strings`, `slog` orphans are guaranteed); `go vet`
  catches them a cycle later.
- Diff every edited file before moving on.
- One simple shell command per call: no heredocs, no `VAR=x cmd`, no bare
  `echo`, no `2>/dev/null` (§4.#12, §4.#13).
- A predicted runtime-only risk ships with its test in the same change
  (route patterns validate only at startup — hence `mux_test.go`).
- Verify through execution whenever reasonable: run checks, tests, or smoke
  after implementing, fixing, or refactoring. `make go-vet`, `make go-test`,
  `make go-fmt-check` gate every Go change; `make smoke` gates wire-level
  changes against live upstreams.

## 6. Design defaults

- One knowledge, one owner. Duplicated logic drifts toward bugs; unify
  instead of patching instances.
- Fail closed. Unknown state denies with a surfaced error, never a default
  success.
- Transaction boundary belongs to the calling domain method: one public
  method, one `db.Update`.
- Fix the contract where data is born. Frontend guards over backend shapes,
  display-side parsing, and anonymous DTOs duplicating models are backend
  bugs postponed (§4.#11).
- Persisted derived values are frozen. Functions whose outputs live in
  storage never change semantics without a migration; lock them with
  stability tests.
- Migrate stored shapes explicitly: version the bucket (`proxies_v2`) or
  declare a named one-shot function (`migrateProxySourceKeys`). Never hide
  backward-compat inside the function needing the new shape.
- Fix the class, not the instance. Systemic fixes run smaller than
  instance patches.
- Names must not lie. Rename when semantics change; a lying name is worse
  than none.

## 7. Svelte 5 Reactivity

NEVER write to a `$state` variable from an `$effect` that reads it — directly, or via a function it calls. Guard flags and `untrack()` do not fix this — they hide it.

DON'T:
```svelte
$effect(() => {
  if (types.length && !typeKey) {
    untrack(() => { typeKey = types[0] })  // writes typeKey; effect exists because typeKey is read elsewhere → oscillation risk
  }
})
```

DO — computed value → `$derived`:
```svelte
let typeKey = $derived(types[0] ?? '')
```

DO — one-time default on data arrival → imperative, not reactive:
```svelte
onMount(async () => {
  types = await fetchTypes()
  if (types.length) typeKey = types[0]
})
```

DO — parent-notification effect (the only legitimate write-out pattern): list every dependency explicitly, `untrack()` only the outgoing call, never the effect's own state:
```svelte
$effect(() => {
  void a; void b; void c
  untrack(() => notifyParent(a, b, c))
})
```

Never stack a 2nd/3rd `$effect` with its own guard flag to patch the 1st. That means the 1st effect is wrong. Replace it, don't add to it.

Before finishing any `.svelte` change, re-check every `$effect` touched against this section.

## 8. Lua Plugins

- New provider backends are single-file Lua plugins: one `.lua` file with a `--- @` manifest header. Install via dashboard Plugins → Catalog tab or `POST /api/llm-router/dashboard/plugins/install-file`.
- Manifest: required tags `@plugin`, `@author`, `@version`, `@router_version`, one or more `@allow_host` (`*` marks the plugin unsafe). Routers serve no contract older than `0.1.1`. `internal/services/luaplugin/manifest.go` validates.
- API: `llm_router.register(type_key, {complete, ...})`, `llm_router.http_client`, `llm_router.classify_error`, `llm_router.multipart`, `llm_router.storage`, `llm_router.uuid_v5(namespace, name)` (RFC 4122), `llm_router.random_hex(nbytes)`, `json.encode/decode`. Error contract `{type=, message=, retry_after=, scope=}` with `account`/`model`/`proxy` scope words; empty scope on rate/quota marks the full combination. **docs/PLUGIN-API.md is the binding contract for plugin authors — keep it in sync with every handler/API change.**
- Error parallels: `classify_error(raw, default)` extension handles provider specifics; the core default stays safe (`auth`/`geo`/`quota` only on explicit signals, else `upstream`).
- Exhausted store: joint limit keys over plugin, provider type, account, model, proxy. Stored keys filter candidates by subset match; expired entries delete on read. `rate_limit`/`quota_exceeded` mark, everything else does not.
- UI trees for `config_schema`/`credential_schema`/`auth_initiate`/`auth_step` render through `DynamicForm.svelte`. Node kinds: leafs `text`, `input`, `select`, `checkbox`, `button`, `link`, `banner`, `secret`, `code`; containers `group`, `flow`, `grid`, `section`, `spacer`, `divider`. No raw HTML from plugins, ever — new widgets ship as first-class node kinds, not markup.
- Built-in Go backends exist only for `custom` (`internal/adapters/generic/`) and `virtual` (`providers/virtual/`), both implementing `provider.GoAdapter`.
- Verification: `make go-vet` for static checks, `make smoke` for wire-level checks against live upstreams.

## 9. Runtime, API Testing, Smoke

Debug live with `make start NO_AUTH=1` (authorization fully off: no bearer keys, no login), `make log` / `make log-frontend` for output, `make status` for state, `make stop` when done. Fresh DBs still open the bootstrap page once (account creation stays); `status.authenticated` reads true under no-auth. NEVER enable no-auth outside local dev.

To get runtime facts from a human-run instance instead:

1. State exactly what's needed: endpoint, log line, or behavior.
2. Ask human to run `make start` / `make restart` / `make init` and report back.
3. Interpret: dev dashboard = `http://HOST:WEB_PORT` proxying `/api/llm-router/*` → backend `HOST:38473` (dev-internal, hardcoded); publish dashboard = `:8080` (embedded). API = `:8081/v1` (dev values come from `WEB_PORT`/`API_PORT`, defaults `38080`/`38081`). Pidfile is JSON `{backend,frontend,vitePort}` at `%TEMP%/llm-router-dev.pid`.

Under `NO_AUTH=1` every call below needs no headers. With auth on, add `-H "Authorization: Bearer <token>"` to `/v1` and use a logged-in session (cookie) for `/api/llm-router/*`; `401` without them is expected, not a bug.

`/v1` (OpenAI-compatible, `:$API_PORT/v1`):
- `POST /v1/chat/completions` `{"model":"<provider>/<model>","messages":[{"role":"user","content":"hi"}]}` — core routing; add `"stream":true` for SSE
- `GET /v1/models` — routable model list, no token rules applied
- `POST /v1/messages` — Anthropic-compatible messages (translated to chat underneath)
- `POST /v1/audio/transcriptions` (multipart `file`+`model`), `/v1/audio/speech`, `/v1/images/generations`, `/v1/embeddings` — per-endpoint backends
- Diagnose: wrong `provider/` → `provider_not_found`; disabled model → `model_not_found`; no usable key → `no_credential`/`credential_not_allowed`; upstream failure → `upstream_error`/`timeout`/`rate_limit`/`payment_required` (see `errors.ToAPIError`)

Dashboard `/api/llm-router/*` (`:$WEB_PORT`, JSON `{"error":...}` on failure):
- `GET /api/llm-router/status` — `{bootstrapped, authenticated}`; boot gate for everything else
- `GET /dashboard/providers`, `/dashboard/tokens`, `/dashboard/credentials` — inventory
- `POST /dashboard/chat/completions` — same router through a session, `TokenID: "dashboard"` in metrics
- `POST /dashboard/credentials/{id}/test {"model":...}` — pinned single-credential probe (bare model name; the endpoint prepends the provider)
- `GET /dashboard/providers/{id}/models` — richest capability source (`endpoints` included)
- `GET /dashboard/proxy-sources`, `/dashboard/virtual-models` — pool and fan-out state

Runtime diagnostics: `make log LINES=...` (backend), `make log-frontend` (vite), `make status` (PIDs/ports), metrics overview/timeseries endpoints for usage.

Smoke harness (`scripts/smoke/`, `make smoke` restarts with `NO_AUTH=1` first):
- Default scope is the mock provider only; real plugins opt in via `SMOKE_PLUGINS=...`, heavy endpoints via `SMOKE_TARGETS=...`.
- Accounts come from the dev database, never from env. Missing credentials skip the plugin, never fail it.
- Matrix iterates models per capability until first success; `quota`/`payment`/`rate`/`not_found`/`invalid_request` move on, `auth` fails, exhaustion without success skips with reason.
- Mock provider ships in `testdata/mock.lua`; bump its `@version` on every edit (the harness skips reinstall on version match).

## 10. Plugin Store Repo (sibling checkout)

Provider plugins ship from plugin store repositories, not from the binary. Built-in repos live in `pluginrepo.BuiltinRepos` and seed on startup via `EnsureBuiltinRepos`; they cannot be removed (`ErrBuiltinRepoProtected`). To ship a plugin upgrade, bump `@version` in the store repository.

- Reissue checklist per plugin: `@version` bump (major on contract breaks), `@router_version` floor, classify through the helper, `(resp, err)` stream idiom with `on_response`, `scope` on rate/quota, `request.model_name` (never forward request tables verbatim upstream).
- Verify reissues without live keys: install dry-run plus classify extensions against synthetic `{status, headers, body}` inputs. Live streams and impersonation paths verify on `make start` with real accounts only.

## 11. Documentation

Edit docs in the same change as the code, never deferred. Short formulations: present simple for system state, past simple for history.

- `docs/PLUGIN-API.md` is the binding contract: every handler, API, or contract change updates it in the same change. Trace every claim to code; keep examples runnable with verbatim shapes; add a history row per contract feature.
- `docs/BACKEND.md` maps the current core only: a new service, bucket, or pipeline step adds a map line (plus a mini-section when needed). Never history here.
- `docs/CHANGELOG.md` is append-only: new entry `## Done (YYYY-MM-DD, vX.Y.Z)` with past-simple bullets, no numbering. Never rewrite old entries. Resolve the header version from git, never guess.
- `scripts/README.md`: a new script or env variable adds a table row with semantics.
- `scripts/help.txt`: a new target or variable adds a line; verify with a live `make help`.
- Root `README.md` is human-owned: never edit it.
- Plugin store reissues bump `@version` (major on contract breaks) and honor the `@router_version` floor.
- Move together: `CurrentVersion`, history rows, `minRouterVersion`. New make targets already ride the §3.2 rule (no duplication here).
- Verify doc edits by re-reading the whole file plus grepping stale markers (old API names, removed buckets or fields).
