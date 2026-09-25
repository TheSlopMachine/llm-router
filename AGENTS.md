# AGENTS.md

Guidelines for AI agents in `llm-router/`. Read before any change. Treat every rule as absolute. No exceptions. No reinterpretation.

## 0. Banned Wording

Apply this section to all agent output: commit messages, PR descriptions, code comments, log strings, UI copy, CLI help/output, error messages, and doc edits.

### 0.1 No Filler Phrases

Rule: NEVER write filler phrases that pad a sentence without adding information.

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

### 0.2 No Implementation-Detail Asides in UI or CLI

Rule: NEVER append a parenthetical (or dash/comma aside) that explains an internal condition, branch, or implementation reason inside UI text, CLI output, log lines, or error messages. State the action or fact only. Put the reasoning in a code comment or commit message, or leave it out entirely — never surface it to the user.

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

If the condition is genuinely useful for debugging, log it at debug level with structured fields (`reason=rate_limited`). Never narrate it in prose inside the user-facing string.

### 0.3 No Unprofessional or Non-Informative Wording

Rule: NEVER use casual, slang, or vague verbs in place of precise technical terms, in code, comments, commits, logs, or docs. Banned examples: "blow up", "wire", "sweep", "spin up", "juice", "nuke", "hack", "magic".

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

## 1. Priority Rules

1. NEVER write to `$state` from an `$effect` that reads it. → §5
2. NEVER build the app binary by hand or debug outside `make start`. Use the allowed `make` targets. → §4
3. NEVER run destructive git commands. → §6
4. NEVER delete the dev DB. NEVER kill processes. NEVER litter the repo. → §6

## 2. Project

`llm-router` — single-binary OpenAI-compatible LLM routing gateway. Go backend + embedded Svelte SPA + embedded bbolt DB.

- Routes `ModelId = provider/model` (e.g. `opencode-zen/gpt-5`, `virtual/my-model`) → backend + `CredentialPool`.
- Provider backends are single-file Lua plugins (installed store records in `BucketPlugins`, sourced from plugin store repositories). Built-in Go backends exist only for `custom` (OpenAI-compatible passthrough) and `virtual` (virtual models).

## 3. Structure

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
internal/pool/           single-pass credential failover (unary + stream)
internal/streamgate/     first-byte gate: failover stops after first SSE byte
internal/httpkit/        shared transport helpers (SSE headers)
internal/errors/         domain sentinels + MapUpstream + ToAPIError
internal/repository/     bbolt buckets
internal/dashboard/      admin REST API
internal/api/v1/         OpenAI-compatible /v1/chat/completions, /v1/models
internal/models/         shared wire types
internal/config/         Config struct
internal/adapters/generic/ built-in custom backend (Go)
providers/virtual/       built-in virtual-models backend (Go)
web/                     Svelte SPA (web/openapi.yaml + src/lib/generated/ auto-generated — do not hand-edit)
scripts/                 separate Go module — build/dev helpers (never imported by main module)
Makefile                 thin launcher for scripts/ — see §4
```

Rule: keep changes shallow. Do not touch service internals unless the task requires it.

## 4. Commands

### 4.1 Allowed — agent runs these directly

Rule: run checks, tests and formatting ONLY through these targets. No raw
tool invocations, even for a single file.

| Command | Purpose |
|---|---|
| `make check-frontend` | Frontend compile/type-check |
| `make go-vet` | Go static check (`go vet`; `PKG=...` scopes packages, default `./...`) |
| `make go-test` | Go tests (`PKG=...` scopes packages, default `./...`) |
| `make go-fmt` | Apply `gofmt` to both Go modules (`PATHS=...` scopes files/dirs, default whole tree) |
| `make go-fmt-check` | Fail when `gofmt` would reformat anything (`PATHS=...` scopes files/dirs) |
| `make init` | Project init/re-init: bun install, openapi.yaml, api-types, embed stub; skips fresh outputs unless `NO_SKIP=1` |
| `make log` | Tail backend log (`LINES=...` sizes tail, default `100`; `FOLLOW=...` overrides TTY auto-follow) |
| `make log-frontend` | Tail frontend log (same `LINES`/`FOLLOW` contract as `make log`) |
| `make start` | Start dev backend + frontend (vite); `NO_AUTH=1` disables all authorization |
| `make stop` | Stop dev processes |
| `make restart` | Stop + start, keeping last start params |
| `make smoke` | Restart with `NO_AUTH=1`, then run the smoke harness (`SMOKE_PLUGINS=...`, `SMOKE_TARGETS=...`) |
| `make status` | Show dev server status |
| `make log` | Tail backend log (`LINES=...` sizes tail, default `100`; `FOLLOW=...` overrides TTY auto-follow) |
| `make log-frontend` | Tail frontend log (same `LINES`/`FOLLOW` contract as `make log`) |

### 4.2 Banned — raw equivalents of the above

| DO | DON'T |
|---|---|
| `make check-frontend` | `bunx svelte-check`, `bun run check`, `tsc --noEmit`, `eslint .` |
| `make go-vet` | `go vet ./...` |
| `make go-vet PKG=...` | `go vet ./internal/...` (scoped raw vet) |
| `make go-test` | `go test ./...` |
| `make go-test PKG=...` | `go test ./internal/...` (scoped raw test) |
| `make go-fmt` / `make go-fmt-check` | `gofmt -w .`, `gofmt -l .` |
| `make go-fmt-check PATHS=...` | `gofmt -l <paths>` (scoped raw gofmt) |

Rule: NEVER substitute a raw command for an allowed `make` target, even when the outcome matches.

Rule: when no target covers an operation, add one (`Makefile` + `scripts/help.txt` + `scripts/README.md` + this table) instead of running raw commands. A missing target is a gap to fix, never permission to bypass.

### 4.3 Banned — every other Makefile target

`browser`, `publish`, `clean`.

NEVER run these. No exception for "just to check", "isolated test", "custom port", or any other framing.

If the task needs one of these: STOP. Ask the human to run it and report back (terminal output, logs, curl/browser result).

| DO | DON'T |
|---|---|
| "Please run `make publish` and paste the output." | `go run .`, `go build -o ./llm-router.exe`, `bun run dev`, `Start-Process ...`, any script/wrapper invoking these |

## 5. Svelte 5 Reactivity

Rule: NEVER write to a `$state` variable from an `$effect` that reads it — directly, or via a function it calls. Guard flags and `untrack()` do not fix this — they hide it.

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

Rule: never stack a 2nd/3rd `$effect` with its own guard flag to patch the 1st. That means the 1st effect is wrong. Replace it, don't add to it.

Rule: before finishing any `.svelte` change, re-check every `$effect` touched against this section.

## 6. Absolute Prohibitions

| # | Prohibition | Detail |
|---|---|---|
| 1 | Kill processes | No `kill`, `pkill`, `taskkill`, `Stop-Process`. Ask human to run `make stop`. |
| 2 | Delete database files | Never remove `~/.local/llm-router/llm-router-dev.db` (or the Windows equivalent). Ask the human to recreate it via `make start`. |
| 3 | Run destructive git commands | No `git commit`, `git push`, `git reset`, `git checkout -f`, `git clean`, force-push, amend. Commit only if explicitly instructed, for that exact commit only. |
| 4 | Litter the project | No `*.log`, `*.pid`, `*.tmp`, binaries, scratch files, notes inside the project tree. Use `/tmp` or an external scratch dir. |
| 5 | Avoid the Makefile | No raw command substitutes for an allowed target (§4.2). No manual invocation of a banned target's underlying steps (§4.3). |
| 6 | Run, start, or live-test the app | Human-only task: `browser`, `publish`, `clean`. Debug live with `make start NO_AUTH=1` / `make restart` / `make stop` / `make status` — ask the human for anything else. See §4.3, §8. |
| 7 | Redirect output to nul or /dev/null | Breaks on Windows, and hides diagnostics on every OS. Every such redirect earns the offending agent 800 lashes. |
| 8 | Truncate diagnostics output with tail/head | Diagnostics matter and truncating wastes time. NEVER truncate them. |
| 9 | Fall back silently | NEVER swallow a failure and continue on a fallback path. Surface every failure as an error — return it to the caller, log it, or both — or route it to an explicit, named on-fail branch. Never fall through unannounced. |
| 10 | Match errors by string | NEVER match error codes or kinds with `strings.Contains(err.Error(), ...)` or message substrings. Define sentinel errors and match with `errors.Is` / `errors.As`. |
| 11 | Patch a weak API contract in the frontend | Repetitive `??` / `?.` over backend data shapes means the contract is wrong. Fix the backend to return consistent shapes (arrays never null, objects never null when the schema promises them). Frontend guards stay only for genuinely optional local state. |
| 12 | Create stray files from the shell | NEVER let a shell command create a file: no `>` / `>>` / `Out-File` redirection, no heredocs, no `2>/dev/null`, no unquoted fragments that resolve to filenames (`nul`, `null`, command text as filename). Read command output from the tool result, never from disk. Every such file earns the offending agent 20 lashes. |
| 13 | Pass env vars with shell syntax | NEVER `VAR=value make <target>` (Unix-only) and NEVER `$env:VAR="value"; make <target>` (PowerShell-only, leaks state into the session). Use `make <target> VAR=value`: make syntax, works in every shell, scoped to one invocation. |

## 7. Lua Plugins

- Location: new provider backends are single-file Lua plugins. Develop them anywhere as one `.lua` file with a `--- @` manifest header; install via dashboard Plugins → Catalog tab or `POST /api/llm-router/dashboard/plugins/install-file`.
- Manifest: required tags `@plugin`, `@author`, `@version`, `@router_version`, one or more `@allow_host` (`*` marks the plugin unsafe). `internal/services/luaplugin/manifest.go` validates.
- API: `llm_router.register(type_key, {complete, ...})`, `llm_router.http_client`, `llm_router.classify_error`, `llm_router.multipart`, `llm_router.storage`, `llm_router.uuid_v5(namespace, name)` (RFC 4122), `llm_router.random_hex(nbytes)`, `json.encode/decode`. Error contract `{type=, message=, retry_after=, scope=}`. Endpoint handlers beyond chat: `transcribe` (POST /v1/audio/transcriptions). **docs/PLUGIN-API.md is the binding contract for plugin authors — keep it in sync with every handler/API change.** UI trees for `config_schema`/`credential_schema`/`auth_initiate`/`auth_step` render through `DynamicForm.svelte`. Node kinds: leafs `text`, `input`, `select`, `checkbox`, `button`, `link`, `banner`, `secret`, `code`; containers `group`, `flow`, `grid`, `section`, `spacer`, `divider`. No raw HTML from plugins, ever — new widgets ship as first-class node kinds, not markup.
- Store: provider plugins ship from plugin store repositories, not from the binary. Built-in repos live in `pluginrepo.BuiltinRepos` and seed on startup via `EnsureBuiltinRepos`; they cannot be removed (`ErrBuiltinRepoProtected`). To ship a plugin upgrade, bump `@version` in the store repository.
- Built-in Go backends exist only for `custom` (`internal/adapters/generic/`) and `virtual` (`providers/virtual/`), both implementing `provider.GoAdapter`.
- Verification: run `make go-vet` only. For runtime checks, ask the human to run `make start`.

## 8. When Runtime Info Is Needed

Debug live with `make start NO_AUTH=1` (authorization fully off: no bearer keys, no login), `make log` / `make log-frontend` for output, `make status` for state, `make stop` when done. Fresh DBs still open the bootstrap page once (account creation stays); `status.authenticated` reads true under no-auth. NEVER enable no-auth outside local dev.

To get runtime facts from a human-run instance instead:

1. State exactly what's needed: endpoint, log line, or behavior.
2. Ask human to run `make start` / `make restart` / `make init` and report back.
3. Interpret: dev dashboard = `http://HOST:WEB_PORT` proxying `/api/llm-router/*` → backend `HOST:38473` (dev-internal, hardcoded); publish dashboard = `:8080` (embedded). API = `:8081/v1` (dev values come from `WEB_PORT`/`API_PORT`, defaults `38080`/`38081`). Pidfile is JSON `{backend,frontend,vitePort}` at `%TEMP%/llm-router-dev.pid`.

## 9. API Testing

Under `NO_AUTH=1` every call below needs no headers. With auth on, add `-H "Authorization: Bearer <token>"` to `/v1` and use a logged-in session (cookie) for `/api/llm-router/*`; `401` without them is expected, not a bug.

`/v1` (OpenAI-compatible, `:$API_PORT/v1`):
- `POST /v1/chat/completions` `{"model":"<provider>/<model>","messages":[{"role":"user","content":"hi"}]}` — core routing; add `"stream":true` for SSE
- `GET /v1/models` — routable model list, no token rules applied
- `POST /v1/audio/transcriptions` (multipart `file`+`model`), `/v1/audio/speech`, `/v1/images/generations`, `/v1/embeddings` — per-endpoint backends
- Diagnose: wrong `provider/` → `provider_not_found`; disabled model → `model_not_found`; no usable key → `no_credential`/`credential_not_allowed`; upstream failure → `upstream_error`/`timeout`/`rate_limit` (see `errors.ToAPIError`)

Dashboard `/api/llm-router/*` (`:$WEB_PORT`, JSON `{"error":...}` on failure):
- `GET /api/llm-router/status` — `{bootstrapped, authenticated}`; boot gate for everything else
- `GET /dashboard/providers`, `/dashboard/tokens`, `/dashboard/credentials` — inventory
- `POST /dashboard/chat/completions` — same router through a session, `TokenID: "dashboard"` in metrics
- `GET /dashboard/proxy-sources`, `/dashboard/virtual-models` — pool and fan-out state

Runtime diagnostics: `make log LINES=...` (backend), `make log-frontend` (vite), `make status` (PIDs/ports), metrics overview/timeseries endpoints for usage.

## 10. Agent Field Manual

### 10.1 Edit discipline

- Re-read the region before every `edit` to the same file. Stale `oldString`
  either no-matches or deletes neighboring code.
- After deleting a function, grep the file's imports immediately (`net/url`,
  `regexp`, `errors`, `strings`, `slog` orphans are guaranteed); `go vet`
  catches them a cycle later.
- Diff every edited file before moving on.
- One simple shell command per call: no heredocs, no `VAR=x cmd`, no bare
  `echo`, no `2>/dev/null` (§12, §13).
- A predicted runtime-only risk ships with its test in the same change
  (route patterns validate only at startup — hence `mux_test.go`).

### 10.2 Design defaults

- One knowledge, one owner. Duplicated logic drifts toward bugs; unify
  instead of patching instances.
- Fail closed. Unknown state denies with a surfaced error, never a default
  success (§9).
- Transaction boundary belongs to the calling domain method: one public
  method, one `db.Update`.
- Fix the contract where data is born. Frontend guards over backend shapes,
  display-side parsing, and anonymous DTOs duplicating models are backend
  bugs postponed (§11).
- Persisted derived values are frozen. Functions whose outputs live in
  storage must never change semantics without a migration; lock them with
  stability tests.
- Migrations are explicit or versioned, never inline recovery hacks.
  Never smuggle backward-compat recovery into the function that needs
  the new shape: the next reader sees dead weight and deletes it,
  re-breaking old data silently. If new code breaks stored shapes,
  either version the bucket (`proxies_v2`, `metrics_v2`, …) or declare
  a named migration function (`migrateLegacyCustom`, `migrateProxySourceKeys`)
  that runs once at startup and stays greppable.
- Fix the class, not the instance. Systemic fixes run smaller than
  instance patches.
- Names must not lie. Rename when semantics change; a lying name is worse
  than none.
