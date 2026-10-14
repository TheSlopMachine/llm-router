# AGENTS.md

Guidelines for AI agents in `llm-router/`. Read before any change. Treat every rule as absolute. No exceptions. No reinterpretation.

## 0. Wording

Applies to all agent output: commit messages, PR descriptions, code comments, log strings, UI copy, CLI help/output, error messages, doc edits.

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

### 0.2 Over-explaining in UI is forbidden

Do not explain internals. Surface intent only:

- BAD: `"Retrying... (3 seconds passed already)"`
- GOOD: `"Retrying..."`

Do not over-explain functionality:

- BAD: `"Access the dedicated plugin management interface to review, inspect, configure, and otherwise manage plugins that have already been installed and are currently available within your environment."`
- GOOD: `"Manage plugins and install more plugins."`

Do not guide the UX with text:

- BAD: `"Press 'Done' to close."`
- GOOD: UI that guides visually, without text.

### 0.3 No redundant wording

Do not repeat one idea with synonyms or modifiers. Every word contributes distinct information.

DON'T:

- "View, browse, and explore available plugins."
- "Completely remove all existing entries."
- "Create a new plugin to add an additional capability."

DO:

- "Browse available plugins."
- "Remove all entries."
- "Create a plugin to add a capability."

### 0.4 No casual or vague wording

Use precise technical terms in code, comments, commits, logs, docs. Banned: "blow up", "wire", "sweep", "spin up", "juice", "nuke", "hack", "magic".

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

### 0.5 Naming policy: no arbitrary abbreviations

Prefer complete, descriptive names. Less conventional abbreviations are more severe violations. Single-letter names can be critical violations.

- BAD: `repo`, `args`, `params`, `opts`, `deps`, `impl`, `auth`, `hndl`, `prc`, `mthd`, `vl`, `itm`, `cnfg`, `rqst`, `rsps`, `hndlReq`, `getUsrCfg`
- GOOD: `repository`, `arguments`, `parameters`, `options`, `dependencies`, `implementation`, `authentication`/`authorization`, `handler`, `process`, `method`, `value`, `item`, `configuration`, `request`, `response`, `handleRequest`, `getUserConfiguration`

Accept without change:

- GOOD: `id`, `url`, `http`, `json`, `uuid`, `min`, `max`, `avg`, `i`/`j` in short loops, `x`/`y` in mathematical contexts, `stdin`/`stdout`/`stderr`, conventional `ctx`.

Applies to every component of compound identifiers and all project-authored code, tests, scripts, configuration, documentation, comments, logs, error messages.

- BAD: abbreviated components inside compound names (`getUsrCfg`, `hndlReq`).
- GOOD: full words in every component (`getUserConfiguration`, `handleRequest`).

Established technical terms, conventional notation, language-mandated names, and compatibility-sensitive external interfaces are exempt where necessary.

- BAD: renaming a wire-compatible external field to satisfy this policy.
- GOOD: keeping the external name, using full words in new project-owned names.

All new and modified code MUST comply.

- BAD: new code introduces `cnfg`, `rqst`, `rsps`.
- GOOD: new code uses `configuration`, `request`, `response`.

## 1. Session start

Open every session by identifying platform and shell. Never assume them.

Query runtime environment first (OS, shell, working directory); training defaults do not apply.

- BAD: assuming Unix shell, running `head`, `cat`, `VAR=x cmd`.
- GOOD: query OS/shell/workdir first, then use shell-correct syntax.

On win32 the `bash` tool runs PowerShell: chain with `;`, quote paths with `"..."`, never use Unix-isms.

- BAD: `head file`, `cat file`, `VAR=x make go-test`.
- GOOD: `make go-test PKG=./internal/...` with `;`-chained PowerShell.

## 2. Project

`llm-router` — single-binary OpenAI-compatible LLM routing gateway. Go backend + embedded Svelte SPA + embedded SQLite DB.

- Routes `ModelId = provider/model` (e.g. `opencode-zen/gpt-5`, `virtual/my-model`) → backend; plugins select credentials under the token allow-list gate.
- Provider backends are single-file Lua plugins (installed store records in the `plugins` table, sourced from plugin store repositories). Built-in Go backends exist only for `custom` (OpenAI-compatible passthrough) and `virtual` (virtual models).

## 3. Structure

```
cmd/root.go              CLI entrypoint (environment + .env; --version, --generate-config, interactive --bootstrap/--list-login/--remove-login/--reset-login/--create-login)
cmd/login.go             interactive admin login management (terminal gate, prompts, per-flag runners)
internal/server/         HTTP server: public listener (dashboard SPA + /v1 + /api on PORT) + optional loopback admin listener (API only, no auth, on 127.0.0.1:ADMIN_PORT)
internal/services/
  token/                 router-token issue/validate
  router/                ModelId → backend, token allow-list gate, single pass, no repeats
  provider/              ProviderInstance CRUD (all types, one path)
  credential/            credential storage + admin mutation + plugin lifecycle writes
  virtual/               virtual models (fall-through lists + instruction)
  responses/             router-side compat records (responses, conversations, assistants, threads, messages, runs)
  batches/               router-side Anthropic message batches
  luaplugin/             Lua execution core: manifest, sandbox, HTTP+SSRF, storage+TTL, credentials/proxies tables, jobs
  pluginrepo/            plugin store: single-URL index repos (repo URL or direct index.json, files resolved against the index directory); code-defined built-in repos (`BuiltinRepos`, seeded on startup, protected from removal)
  modelinfo/             model metadata cache (1h TTL)
  metrics/               1m buckets, 90d retention
  maintenance/           plugin job scheduler, model sync, auth cleanup
  videojobs/             router-side video job rows (local id → upstream job)
  healthcheck/           failure-triggered credential verification (check_health, cooldown, disable on unhealthy only with automation on)
  admin/                 admin password change
  config/                instance-wide router configuration
  datamanagement/        subsystem export/import/clear + provider export/import/purge
  doctor/                database inspection and repair
  proxypool/             free-pool hosting, Lua feed bridge, SQLite cache, custom pools, lane scheduler adapter
  events/                event log storage (events SQLite store)
internal/httpkit/        shared transport helpers (SSE headers)
internal/requestsource/  RequestSource in request context (public vs private listener)
internal/httpapi/        shared route tables + public/private mux builders
internal/errors/         domain sentinels + ToAPIError
internal/repository/     typed SQLite operations
internal/dashboard/      admin REST API
internal/api/v1/         OpenAI-compatible /v1/chat/completions, /v1/models, /v1/messages
internal/models/         shared wire types
internal/config/         Config struct
internal/adapters/generic/ built-in custom backend (Go)
providers/virtual/       built-in virtual-models backend (Go)
web/                     Svelte SPA (web/openapi.yaml + src/lib/generated/ auto-generated — do not hand-edit)
scripts/                 separate Go module — build/dev helpers (never imported by main module)
scripts/smoke/           black-box smoke harness (live upstreams only)
docs/                    PLUGIN-API.md (binding plugin contract), BACKEND.md (core map), CHANGELOG.md
Makefile                 thin launcher for scripts/ — see §4
```

Keep changes shallow. Do not touch service internals unless the task requires it.

- BAD: refactoring a service interior for a caller-side fix.
- GOOD: changing the shallowest layer that owns the behavior.

## 4. Commands

`Makefile` is the entry point for covered operations. NEVER substitute a raw command where a `make` target covers the operation, even when the outcome matches.

- BAD: `go vet ./...`, `go test ./...`, `gofmt -l .`, `bunx svelte-check`.
- GOOD: `make go-vet`, `make go-test`, `make go-fmt-check`, `make check-frontend`.

Operations with no covering target may use direct commands.

- BAD: hand-rolling frontend checks when `make check-frontend` covers them.
- GOOD: direct commands only where no target covers the operation.

### 4.1 Agent targets — agent runs these directly

Frontend compile/type-check:

- BAD: `bunx svelte-check`, `bun run check`, `tsc --noEmit`, `eslint .`
- GOOD: `make check-frontend`

Go static check (`PKG=...` scopes packages, default `./...`):

- BAD: `go vet ./...`, `go vet ./internal/...`
- GOOD: `make go-vet`, `make go-vet PKG=./internal/...`

Go tests (`PKG=...` scopes packages, default `./...`):

- BAD: `go test ./...`, `go test ./internal/...`
- GOOD: `make go-test`, `make go-test PKG=./internal/...`

Go tests with race detector (`PKG=...` scopes packages, default `./...`):

- BAD: `go test -race ./...`
- GOOD: `make go-test-race`

Typed GORM conditions (no raw SQL outside `internal/db/maintenance.go`):

- BAD: manual `grep` for SQL strings as policy.
- GOOD: `make sql-policy-test`

Apply `gofmt` to both Go modules (`PATHS=...` scopes files/dirs, default whole tree):

- BAD: `gofmt -w .`
- GOOD: `make go-fmt`, `make go-fmt PATHS=./internal/...`

Fail when `gofmt` would reformat anything (`PATHS=...` scopes files/dirs):

- BAD: `gofmt -l .`, `gofmt -l <paths>`
- GOOD: `make go-fmt-check`, `make go-fmt-check PATHS=./internal/...`

Project init/re-init (`NO_SKIP=1` forces fresh outputs):

- BAD: running `bun install` plus codegen steps by hand.
- GOOD: `make init`, `make init NO_SKIP=1`

Tail backend log (`LINES=...` overrides the `EVENTS_LIMIT=50` default tail; `FOLLOW=...` overrides TTY auto-follow). Exclusive backend-log access:

- BAD: `type "$env:TEMP\llm-router-backend.log"`, `Get-Content` on log paths, `Get-ChildItem` temp-dir log hunting.
- GOOD: `make log`, `make log LINES=200`, `make log FOLLOW=1`

Query persisted events with native filters (`EVENT_FILTERS="k=v bare-key"`, `EVENTS_SEVERITY=0-4`, `EVENTS_LIMIT=...`, `EVENTS_START=`/`EVENTS_END=` RFC3339, `EVENTS_ID=...`). The server clamps `limit` to 500; page with time windows instead of larger limits. Filter keys are event metadata (`code`, `cred_id`, `model`, `type`, `reason`); message text is not filterable. Full key inventory: `docs/EVENT-KEYS.md`.

- BAD: piping `make log` into `Select-String`/`grep`, `Get-Content` on log paths.
- GOOD: `make log EVENT_FILTERS="code=rate_limit" EVENTS_LIMIT=500`, `make log EVENT_FILTERS="type=google" EVENTS_START="2026-10-13T17:38:00Z" EVENTS_END="2026-10-13T17:43:00Z"`, `make log EVENTS_SEVERITY=2`

Tail frontend log (same `LINES`/`FOLLOW` contract). Exclusive frontend-log access:

- BAD: `type` on vite logs, reading pid-dir `*.log` files directly.
- GOOD: `make log-frontend`, `make log-frontend LINES=200`

Start dev backend + frontend (vite); admin listener on 127.0.0.1:ADMIN_PORT answers without auth:

- BAD: `go run .`, `bun run dev`, `Start-Process ...`.
- GOOD: `make start`

Stop dev processes (backend graceful via admin API with kill fallback; frontend killed):

- BAD: `kill`, `pkill`, `taskkill`, `Stop-Process`.
- GOOD: `make stop`

Rebuild and restart backend only, keeping live frontend and last start params (full start when frontend is down):

- BAD: killing the backend process, restarting the binary by hand.
- GOOD: `make restart`

Run smoke harness against the running dev stack, never restarting it (`SMOKE_PLUGINS=...`, `SMOKE_TARGETS=...`):

- BAD: restarting the stack for smoke, running `scripts/smoke` directly.
- GOOD: `make smoke-test`, `make smoke-test SMOKE_PLUGINS=google SMOKE_TARGETS=stream`

CI harness validation (`HARNESS=...` selects the CLI; CI matrix only, agents never run it):

- BAD: agent runs `make harness-test` (live CLIs, admin listener, long runtime).
- GOOD: reading the `harness-<name>` run artifacts from CI logs.

Upload every store `.lua` plugin via admin listener (`UPLOAD_STORE_DIR=...`, no restart):

- BAD: copying `.lua` files into the project, calling install scripts directly.
- GOOD: `make upload-plugins`, `make upload-plugins UPLOAD_STORE_DIR=C:\devel\llm-router-store\llm-router-plugins`

Show dev server status. Exclusive process-state access:

- BAD: `Get-Process llm-router-dev-backend`, `netstat -ano`, reading `*.pid` files with `type`/`Get-Content`.
- GOOD: `make status`

Print targets and variables:

- BAD: reading `Makefile` to guess targets.
- GOOD: `make help`

Tidy both Go modules:

- BAD: `go mod tidy` in each module by hand.
- GOOD: `make go-tidy`

`check-frontend-deps` and `check-build-deps` are prerequisites, not direct targets.

- BAD: running `make check-frontend-deps` standalone.
- GOOD: letting `make init`/`make start`/`make build` invoke them.

`make go-test` runs the full suite by default. Scope with `PKG=...` only to iterate on one failing package. Never chain scoped runs to cover the tree.

- BAD: `make go-test PKG=A` then `make go-test PKG=B` as full coverage.
- GOOD: one global `make go-test` pass after scoped iteration.

### 4.2 Human-only targets — agent MUST NOT run these

`browser`, `build`, `install`, `publish`, `clean` are human-only. Task needing one: STOP. Ask the human to run it and report back (terminal output, logs, curl/browser result).

Open dashboard in browser:

- BAD: agent runs `make browser`.
- GOOD: "Please run `make browser` and paste the result."

Build frontend + release binaries (`PLATFORMS=...`, `VERSION=...`):

- BAD: agent runs `make build`, `go build -o ./llm-router.exe`.
- GOOD: "Please run `make build` and paste the output."

Install host release binary into `~/.local/bin` (`VERSION=...`; refuses when running):

- BAD: agent runs `make install`.
- GOOD: "Please run `make install VERSION=...` and paste the output."

Cut and push next release tag (`TYPE=fix|minor|major` or `VERSION=X.Y[.Z]`); CI builds:

- BAD: agent runs `make publish`.
- GOOD: "Please run `make publish` and paste the output."

Stop processes + clean ignored files except `.env` files and SQLite databases:

- BAD: agent runs `make clean`, `git clean -fdX`.
- GOOD: "Please run `make clean` and paste the output."

Never launch the app outside make targets:

- BAD: `go run .`, `go build -o ./llm-router.exe`, `bun run dev`, `Start-Process ...`, any script/wrapper invoking these.
- GOOD: `make start` / `make restart` / `make stop` for live debug; human-run request otherwise.

### 4.3 Git commands

Read-only git is allowed; destructive git is banned (see §5.3).

- BAD: `git push`, `git reset`, `git checkout -f`, `git clean`, force-push, amend; stashing without a clean-status check and immediate pop.
- GOOD: `git status`, `git diff`, `git log`; `git stash` only with clean status first and immediate pop after the baseline run.

## 5. Prohibitions

1. Kill processes. `make stop` / `make restart` keep pidfiles consistent.

- BAD: `kill`, `pkill`, `taskkill`, `Stop-Process`.
- GOOD: `make stop`, `make restart` (warn before `make restart` when work is in flight).

2. Delete database files.

- BAD: removing `~/.local/share/llm-router/llm-router.dev.sqlite3`, `~/.cache/llm-router/llm-router.cache.dev.sqlite3`.
- GOOD: asking the human to delete a corrupt database file.

3. Run destructive git commands (see §4.3).

- BAD: `git push`, `git reset`, `git checkout -f`, `git clean`, force-push, amend.
- GOOD: committing only when explicitly instructed, for that exact commit only.

4. Litter the project.

- BAD: `*.log`, `*.pid`, `*.tmp`, binaries, scratch files, notes inside the project tree.
- GOOD: `/tmp` or external scratch dir; delete temporary verification files when done.

5. Avoid the Makefile.

- BAD: raw substitutes for allowed targets; manual invocation of banned target internals.
- GOOD: `make <target>` where a target covers the operation; direct commands otherwise.

6. Build or launch the app outside make targets.

- BAD: human-only or raw launch commands from §4.2.
- GOOD: `make start` / `make restart` / `make stop` / `make status`; human handoff for the rest.

7. Redirect output to nul or /dev/null.

- BAD: `2>/dev/null`, `> nul`, `Out-File` discards.
- GOOD: returning full command output in the tool result.

8. Fall back silently.

- BAD: swallowing a failure, continuing on a fallback path unannounced.
- GOOD: surfacing every failure (return, log, or both) or routing to an explicit named on-fail branch.

9. Match errors by string.

- BAD: `strings.Contains(err.Error(), ...)`, message-substring checks.
- GOOD: sentinel errors matched with `errors.Is` / `errors.As`.

10. Patch a weak API contract in the frontend.

- BAD: repetitive `??` / `?.` over backend data shapes.
- GOOD: fixing the backend to return consistent shapes (arrays never null, objects never null when the schema promises them); frontend guards only for genuinely optional local state.

11. Create stray files from the shell.

- BAD: `>` / `>>` / `Out-File` redirection, heredocs, `2>/dev/null`, unquoted fragments resolving to filenames (`nul`, `null`, command text).
- GOOD: reading command output from the tool result, never from disk.

12. Pass env vars with shell syntax.

- BAD: `VAR=value make <target>` (Unix-only), `$env:VAR="value"; make <target>` (PowerShell-only, leaks session state).
- GOOD: `make <target> VAR=value` (make syntax, every shell, one invocation).

13. Act on questions.

- BAD: treating a question as authorization for edits, runs, installs.
- GOOD: answering with words; zero side effects.

14. Write files through executed scripts.

- BAD: `python -c` writers, heredoc generators, `sed` equivalents, throwaway file-patching scripts.
- GOOD: dedicated file tools so every change stays reviewable.

## 6. Edit discipline

Grasp the concept before the code. On ambiguity, ask instead of inferring literally.

- BAD: editing from a literal code guess while the concept is unclear.
- GOOD: asking for clarification, then editing.

Re-read the region before every `edit` to one file. Stale `oldString` either no-matches or deletes neighbors.

- BAD: reusing an earlier read across multiple edit batches.
- GOOD: fresh read of every region before each batch; verify each result before the next batch.

Batch edits to one file only from fresh reads; verify each result before the next batch.

- BAD: batching edits across files from stale reads.
- GOOD: one file per batch, verified before continuing.

After deleting a function, grep the file imports immediately (`net/url`, `regexp`, `errors`, `strings`, `slog` orphans are guaranteed).

- BAD: leaving orphan imports for `go vet` to catch a cycle later.
- GOOD: immediate import grep plus cleanup in one change.

Diff every edited file before moving on.

- BAD: moving to the next file with an unverified diff.
- GOOD: reviewing the diff of the edited file first.

Leave no scaffolding: delete draft notes, TODO-placeholders, reasoning asides from the edit itself.

- BAD: committed `TODO` placeholders describing the edit process.
- GOOD: clean tree containing only the intended change.

A predicted runtime-only risk ships with its test in one change.

- BAD: route-pattern change without startup validation coverage.
- GOOD: route change plus `mux_test.go` in one change.

Verify through execution whenever reasonable: `make go-vet`, `make go-test`, `make go-fmt-check` gate every Go change; `make smoke-test` gates request-path changes (`router`, `proxypool`, `virtual`, `luaplugin`/`PLUGIN-API`, `api/v1`, `errors` per `docs/BACKEND.md`).

- BAD: shipping a request-path change on static checks alone.
- GOOD: `make go-vet` + `make go-test` + `make go-fmt-check`, plus `make smoke-test` for request paths; scoped plugin runs only to debug that plugin.

Attribute failures before fixing: reproduce on the clean tree first with a scoped run when smoke fails after the change. `git stash` is permitted for baselines but dangerous.

- BAD: fixing forward without a clean-tree baseline.
- GOOD: confirming clean `git status`, stashing, baseline run, immediate pop; preferring a separate worktree when unrelated work is in flight.

## 7. Design defaults

One knowledge, one owner. Duplicated logic drifts toward bugs.

- BAD: patching each duplicate instance.
- GOOD: unifying the logic in one owner.

Fail closed. Unknown state denies with a surfaced error, never default success.

- BAD: defaulting unknown state to success.
- GOOD: denying unknown state with a surfaced error.

Transaction boundary belongs to the calling domain method: one public method, one GORM transaction (`Handle.Transaction`).

- BAD: nesting transactions across callee layers.
- GOOD: one public domain method owning one transaction.

Fix the contract where data is born (§5.10).

- BAD: frontend guards over backend shapes, display-side parsing, anonymous DTOs duplicating models.
- GOOD: backend returning consistent shapes at creation.

Persisted derived values are frozen. Functions whose outputs live in storage never change semantics without a migration; lock them with stability tests.

- BAD: changing a stored-output function silently.
- GOOD: migration plus stability test in one change.

Migrate stored shapes explicitly: numbered migration in the owning store registry (`RegistryDatabase`/`RegistryCache`/`RegistryEvents`) (versions never reused, forward-only `Up`, history row in `schema_migrations`).

- BAD: hiding backward-compat inside the function needing the new shape.
- GOOD: explicit numbered migration with history row.

Fix the class, not the instance.

- BAD: instance patch for a systemic defect.
- GOOD: smaller systemic fix covering the class.

Names MUST NOT lie. Rename when semantics change; a lying name is worse than none.

- BAD: keeping an old name after a semantic change.
- GOOD: renaming with the semantic change.

## 8. Svelte 5 Reactivity

NEVER write to a `$state` variable from an `$effect` that reads it — directly, or via a function it calls. Guard flags and `untrack()` do not fix this.

DON'T:

```svelte
$effect(() => {
  if (types.length && !typeKey) {
    untrack(() => { typeKey = types[0] })
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

DO — parent-notification effect (only legitimate write-out pattern): list every dependency explicitly, `untrack()` only the outgoing call, never the effect's own state:

```svelte
$effect(() => {
  void a; void b; void c
  untrack(() => notifyParent(a, b, c))
})
```

Never stack a 2nd/3rd `$effect` with guard flags to patch the 1st. A patched 1st effect is a wrong 1st effect.

- BAD: adding another guarded `$effect` to stabilize the first.
- GOOD: replacing the first effect with `$derived` or imperative setup.

Before finishing any `.svelte` change, re-check every touched `$effect` against this section.

- BAD: submitting a `.svelte` change without effect review.
- GOOD: explicit re-check of every touched `$effect`.

## 9. i18n Robotic Keys

UI text uses categorized robotic keys. English text is the default output. A key shows only when a translation is missing.

Key location: `web/src/lib/i18n/` with `types.ts` (`TranslationKey` union), `en.ts` (source), `ru.ts` (translation), `index.ts` (`t`, `n`, `tb`).

- BAD: adding UI text outside `web/src/lib/i18n/`.
- GOOD: adding the key in `types.ts`, `en.ts`, `ru.ts`, then using it.

API: `t('common.actions.save')` for static text; `n(count, ...)` for counts with automatic Russian forms; `tb(summary)` for backend probe summaries.

- BAD: `t('Save')`, `t(dynamicString)`.
- GOOD: `t('common.actions.save')`, `tb(backendSummary)`.

Key format: `<domain>.<subdomain>.<entity>.<suffix>`, 2-4 levels, `snake_case`, full words, no abbreviations.

- BAD: `t('cmn.sv')`.
- GOOD: `t('common.actions.save')`.

Domains: `common`, `auth`, `nav`, `providers`, `credentials`, `models`, `tokens`, `virtual`, `plugins`, `proxy`, `settings`, `data`, `metrics`, `upload`, `misc`, `time`, `units`.

- BAD: inventing a top-level domain per screen.
- GOOD: reusing the listed domains.

Suffixes: `.title`, `.label`, `.placeholder`, `.hint`, `.action`, `.status`, `.error`, `.empty`, `.desc`, `.one`/`.many` (English), `.one`/`.few`/`.many` (Russian).

- BAD: `.text`, `.msg`, `.string` suffixes.
- GOOD: listed suffixes only.

Text rules: ASCII only (`...` not Unicode ellipsis, `-` not em dash), short sentences, title case for titles/buttons, sentence case for descriptions.

- BAD: `"Manage plugins…"` with Unicode ellipsis.
- GOOD: `"Manage Plugins..."` with ASCII.

Add a key: extend `TranslationKey`, add English to `en.ts`, add Russian to `ru.ts`, use `t('domain.entity.suffix')`, run `make check-frontend`.

- BAD: adding `en.ts` text without `ru.ts` plus type-check.
- GOOD: union + `en.ts` + `ru.ts` + `t(...)` + `make check-frontend`.

Never pass English strings to `t`. Never pass dynamic strings to `t`; use `tb` for backend summaries.

- BAD: `t('Save changes')`, `t(statusMessage)`.
- GOOD: `t('common.actions.save')`, `tb(statusMessage)`.

Full guide: `web/src/lib/i18n/README.md`.

- BAD: inventing i18n rules from component code.
- GOOD: following `web/src/lib/i18n/README.md`.

## 10. Lua Plugins

New provider backends are single-file Lua plugins: one `.lua` file with a `--- @` manifest header.

- BAD: multi-file backend, Go backend for a new provider.
- GOOD: one `.lua` file installed via Plugins → Catalog or `POST /api/llm-router/dashboard/plugins/install-file`.

Manifest: required `@plugin`, `@author`, `@version`, `@plugin_api`, one or more `@allow_host` (`*` marks unsafe). API format `x.y`: `x` breaks, `y` extends. Install compares majors: newer reports `too_new`, older reports `too_old`, missing reports `no_api_version`. `internal/services/luaplugin/manifest.go` validates; `luaplugin.PluginAPIVersion` (`1.4`) owns the served contract.

- BAD: missing `@allow_host`, undeclared API major.
- GOOD: complete manifest with matching `@plugin_api` major.

API surface: `llm_router.register(type_key, {complete, ...})`, `llm_router.http_client({timeout_ms?})` with per-request `proxy_url` (router owns TLS-fault `MarkDead`, plugins never mark proxies), `llm_router.proxies.query`/`require`, `llm_router.credentials` (`list`/`get` plus job-only `update`, switch-gated `disable`/`enable`, ungated `park`/`unpark`/`parked`), `llm_router.storage` with TTL, colocated `jobs`, `llm_router.multipart`, `llm_router.uuid_v5(namespace, name)` (RFC 4122), `llm_router.random_hex(nbytes)`, `json.encode/decode`. Terminal errors are OpenAI-shaped `{message, code?, param?, status?, headers?}` and render verbatim; `type`/`scope`/`retry_after` fields are not honored (extras are ignored).

- BAD: plugin marking proxies dead, returning `type`/`scope`/`retry_after` error fields.
- GOOD: per-request `proxy_url` only, OpenAI-shaped errors only.

**docs/PLUGIN-API.md is the binding contract — keep it in sync with every handler/API change.**

- BAD: handler change without `docs/PLUGIN-API.md` update.
- GOOD: handler plus contract doc plus history row in one change.

Park (transient, TTL, self-healing, invisible; `list()` auto-excludes parked) vs disable (dead, sticky, dashboard-visible): rate limits and quota windows park, rejected keys disable under `disable_failed_credentials`. Manual dashboard toggle and switch-gated probe win over plugin writes.

- BAD: disabling a credential for a rate limit; parking a rejected key.
- GOOD: parking rate limits/quota windows, disabling rejected keys under the switch.

UI trees for `config_schema`/`credential_schema`/`auth_initiate`/`auth_step` render through `DynamicForm.svelte`. Node kinds: leafs `text`, `input`, `select`, `checkbox`, `button`, `link`, `banner`, `secret`, `code`; containers `group`, `flow`, `grid`, `section`, `spacer`, `divider`. No raw HTML from plugins, ever.

- BAD: plugin returning HTML markup for a new widget.
- GOOD: new widgets as first-class node kinds.

Built-in Go backends exist only for `custom` (`internal/adapters/generic/`) and `virtual` (`providers/virtual/`), both implementing `provider.GoAdapter`.

- BAD: adding a third Go backend for a new provider.
- GOOD: Lua plugin for new providers; Go only for `custom`/`virtual`.

Logging convention (`llm_router.log`, contract 1.4): one stable human sentence per site (sentence case, no embedded values, no provider name — `type` identifies source); `debug` for flow, `info` for progress, `warn` for handled failover, `error` for terminal, `fatal` for unrecoverable (`critical` aliases `fatal`); `snake_case` keys from standard vocabulary (`status`, `code`, `cred_id`, `provider_id`, `token_id`, `plugin_id`, `model`, `endpoint`, `event`, `count`, `job`, `operation`, `attempt`, `reason`, `detail`, `wait`, `latency_ms`, `exit`, `proxy_url`, `body`, `line`, payload facts), `plugin`/`type` reserved.

- BAD: `llm_router.log("warn", "google rate limited key XYZ", { CredID = id })`.
- GOOD: `llm_router.log("warn", "rate limited", { code = "rate_limit", cred_id = id })`.

Verification: static checks plus live black-box checks.

- BAD: `go vet ./...` plus manual curl as plugin verification.
- GOOD: `make go-vet` plus `make smoke-test`.

## 11. Runtime, API Testing, Smoke

Live-debug through make targets plus direct endpoint calls via the admin listener: `make status`, `make log`, `make log-frontend`, `make start`, `make restart`, `make stop`, `make smoke-test`, direct `/v1` and `/api` calls against 127.0.0.1:ADMIN_PORT.

- BAD: `type` on log files, `Get-Content` on pidfiles, `Get-Process`, `netstat`, `Get-ChildItem` log hunting.
- GOOD: `make status` for state, `make log` for backend, `make log-frontend` for frontend, `make smoke-test` and direct admin-listener calls for request paths.

Troubleshoot in a closed loop: state, filtered events, direct reproduction, fix, re-verify, smoke.

- BAD: editing from an unfiltered tail, re-running the full matrix to confirm a single-path fix.
- GOOD: `make status`, native-filtered `make log` around the incident window, one direct `/v1` call per affected path against 127.0.0.1:ADMIN_PORT, the fix, the same direct calls, `make smoke-test SMOKE_PLUGINS=<name>`.

Order: state first, then logs, then lifecycle.

- BAD: `make log` before knowing whether anything runs.
- GOOD: `make status`, then `make log LINES=200`, then `make log-frontend LINES=200`.

`make start` runs public listener (needs login) plus loopback admin listener (API only, no auth, no dashboard) on 127.0.0.1:ADMIN_PORT. Fresh DBs still open bootstrap once on public; admin `status` reads bootstrapped and authenticated without an account. NEVER enable admin listener outside local dev (empty `ADMIN_PORT` disables it).

- BAD: enabling `ADMIN_PORT` outside local dev.
- GOOD: empty `ADMIN_PORT` outside local dev.

Human-run instance facts: state exactly what is needed (endpoint, log line, behavior), ask human to run `make start` / `make restart` / `make init`, interpret the report.

- BAD: "Check whether the server works."
- GOOD: "Please run `make status` and `make log LINES=200`, paste both outputs."

Dev dashboard = `http://HOST:PORT` (vite proxies `/api/*` and `/v1/*` → backend `HOST:38081`, dev-internal fixed); release = `:PORT` (embedded SPA + both APIs on one listener). Pidfile is JSON `{backend,frontend,vitePort}` at `~/.local/state/llm-router/llm-router.dev.pid`.

- BAD: reading the pidfile directly for state.
- GOOD: `make status`; pidfile path is documentation only.

Auth: under admin listener no headers needed. With auth on (public), `/v1` needs `-H "Authorization: Bearer <token>"`, `/api/llm-router/*` needs logged-in session cookie; `401` without them is expected, not a bug.

- BAD: reporting `401` without credentials as a backend defect.
- GOOD: attaching the token/session first, expecting `401` without them.

`/v1` (OpenAI-compatible, same `:PORT` as dashboard):

- `POST /v1/chat/completions` `{"model":"<provider>/<model>","messages":[{"role":"user","content":"hi"}]}` — core routing; `"stream":true` for SSE.
- `GET /v1/models` — routable model list, no token rules applied.
- `POST /v1/messages` — Anthropic-compatible messages (translated to chat underneath).
- `POST /v1/audio/transcriptions` (multipart `file`+`model`), `POST /v1/audio/speech` `{"model":"<provider>/<tts-model>","input":"...","voice":"Kore"}`, `POST /v1/images/generations` `{"model":"<provider>/<image-model>","prompt":"..."}`, `POST /v1/embeddings` `{"model":"<provider>/<embedding-model>","input":"..."}` — per-endpoint backends.
- Diagnose: wrong `provider/` → `provider_not_found`; disabled model → `model_not_found`; no usable key → `no_credential`/`credential_not_allowed`; upstream failure → `upstream_error`/`timeout`/`rate_limit`/`payment_required` (codes in `internal/errors/`; see `errors.ToAPIError`).

Dashboard `/api/llm-router/*` (same `:PORT`, JSON `{"error":...}` on failure):

- `GET /api/llm-router/status` — `{bootstrapped, authenticated}`; boot gate for everything else.
- `GET /dashboard/providers`, `/dashboard/tokens`, `/dashboard/credentials` — inventory.
- `POST /dashboard/chat/completions` — same router through a session, `TokenID: "dashboard"` in metrics.
- `POST /dashboard/credentials/{id}/test {"model":...}` — pinned single-credential probe (bare model name; endpoint prepends provider).
- `GET /dashboard/providers/{id}/models` — richest capability source (`endpoints` included).
- `GET /dashboard/proxy-sources`, `/dashboard/virtual-models` — pool and fan-out state.

`GET /api/events` (same `:PORT`; backs `make log`): `severity` (0-4 minimum), `id`, `start`/`end` (RFC3339), `limit` (server clamps to 500), plus `filter.<key>[=<value>]` predicates over event metadata (`code`, `cred_id`, `model`, `type`, `reason`, ...). Bare `filter.<key>` matches presence; message text is not filterable. Full key inventory: `docs/EVENT-KEYS.md`.

Runtime diagnostics come ONLY from make targets plus metrics endpoints:

- BAD: ad-hoc process/log-file inspection for usage.
- GOOD: `make log LINES=...`, `make log-frontend`, `make status`, metrics overview/timeseries endpoints.

Smoke harness (`scripts/smoke/`, `make smoke-test` drives the running stack through the admin listener without restarting it):

- Plugin types discover from `SMOKE_STORE_DIR` (`llm_router.register` keys, filename fallback); default scope is all discovered, opt in with `SMOKE_PLUGINS=...`, heavy endpoints via `SMOKE_TARGETS=...`.
- BAD: `make smoke-test SMOKE_PLUGINS=typo-name` then debugging the typo as a failure.
- GOOD: unknown names fail listing available; correct `SMOKE_PLUGINS` to a listed name.
- Accounts come from the dev database, never from env. Missing credentials skip the plugin, never fail it.
- BAD: exporting live keys via env for smoke.
- GOOD: relying on dev-database accounts; accepting skips for missing credentials.
- Matrix iterates models per capability until first success; `quota`/`payment`/`rate`/`not_found`/`invalid_request` move on, `auth` fails, exhaustion without success skips with reason.
- BAD: treating `quota`/`rate`/`not_found` as hard failures.
- GOOD: moving on per matrix policy; failing only on `auth`.
- Exercises the running build without restarting it.
- BAD: restarting inside the smoke flow.
- GOOD: `make restart` after code changes first, `make start` when nothing answers first (it fails fast).
- Check semantics differ per target: streams assert delivery (`[DONE]`/`message_stop`), dashboard-chat asserts non-empty `choices`, unary asserts content.
- BAD: treating unary-only empty text as transport failure when streams pass.
- GOOD: reading unary-only failure with passing streams as empty upstream text.
- Diagnose from the log first: failing calls' full upstream bodies are already in the debug log.
- BAD: re-running the matrix blindly on failure.
- GOOD: `make smoke-test SMOKE_PLUGINS=... LOG_LEVEL=debug`, debug-log inspection, reproducing single-model failures with direct `/v1` calls first.
- Entries below the configured `LOG_LEVEL` never persist, so missing `debug` plugin lines (`trying credential`, cooldown skips) on an `info` stack are not evidence the path never ran.
- BAD: reading absent `debug` lines as proof a code path never ran.
- GOOD: reproducing with direct `/v1` calls, or restarting with `LOG_LEVEL=debug` when flow tracing is required.

## 12. Plugin Store Repo (sibling checkout)

Provider plugins ship from plugin store repositories, not from the binary. Built-in repos live in `pluginrepo.BuiltinRepos`, seeded on startup via `EnsureBuiltinRepos`; they cannot be removed (`ErrBuiltinRepoProtected`).

- BAD: editing a shipped plugin inside the binary repo.
- GOOD: bumping `@version` in the store repository.

Reissue checklist per plugin: `@version` bump on every content edit (major on contract breaks) — version-match reinstall skips same-version sources, so rewritten content under a fixed version never deploys; `@plugin_api` major MUST equal the served contract; classify through the helper; `(resp, err)` stream idiom with `on_response`; `scope` on rate/quota; `request.model_name` (never forward request tables verbatim upstream).

- BAD: rewriting plugin content without a `@version` bump.
- GOOD: every content edit bumps `@version` (major on contract breaks).

Verify reissues without live keys: install dry-run plus classify extensions against synthetic `{status, headers, body}` inputs. Live streams and impersonation paths verify on `make start` with real accounts only.

- BAD: shipping a reissue on synthetic checks alone when it touches streams.
- GOOD: synthetic checks plus `make start` live verification for stream paths.

Store reissue loop after every edit: `luac -p` when available, `@version` bump, `make upload-plugins` (expect `installed`, never `already current`), direct `/v1` reproduction of the fixed path, `make smoke-test SMOKE_PLUGINS=<name>`.

- BAD: uploading without a version bump and reading `already current` as deployed.
- GOOD: version bump plus `installed` plus direct-call verification plus scoped smoke.

## 13. Documentation

Edit docs in one change with the code, never deferred. Short formulations: present simple for system state, past simple for history.

- BAD: code change now, doc update later.
- GOOD: code plus docs in one change.

`docs/PLUGIN-API.md` is the binding contract: every handler, API, or contract change updates it in one change. Trace every claim to code; keep examples runnable with verbatim shapes; add a history row per contract feature.

- BAD: contract change with prose-only doc edit.
- GOOD: traced claim, runnable verbatim example, history row.

`docs/EVENT-KEYS.md` is the single source of truth for event keys: a change that adds, renames, or removes an event key adds a table row here in one change.

- BAD: new log key without a table row.
- GOOD: key plus meaning plus runnable example filter in one change.

`docs/BACKEND.md` maps the current core only: a new service, bucket, or pipeline step adds a map line (plus mini-section when needed). Never history here.

- BAD: appending changelog-style history to `docs/BACKEND.md`.
- GOOD: current-core map line only.

`docs/CHANGELOG.md` is append-only: new entry `## Done (YYYY-MM-DD, vX.Y.Z)` with past-simple bullets, no numbering. Never rewrite old entries. Resolve header version from git, never guess.

- BAD: editing old changelog entries, inventing the header version.
- GOOD: appending one dated entry with git-resolved version.

`scripts/README.md`: a new script or env variable adds a table row with semantics.

- BAD: adding a script variable without a `scripts/README.md` row.
- GOOD: variable plus row in one change.

`scripts/help.txt`: a new target or variable adds a line; verify with live `make help`.

- BAD: adding a target without a help line.
- GOOD: target plus help line, verified by `make help`.

Root `README.md` is human-owned: never edit it.

- BAD: agent edits to root `README.md`.
- GOOD: leaving root `README.md` to the human.

Plugin store reissues bump `@version` (major on contract breaks) and declare the served `@plugin_api` major.

- BAD: reissue without version/API bump.
- GOOD: version bump plus matching `@plugin_api` major.

Move together: `luaplugin.PluginAPIVersion`, history rows. Document new make targets in §4.

- BAD: bumping `luaplugin.PluginAPIVersion` without history rows.
- GOOD: version bump plus history rows plus §4 entry.

Verify doc edits by re-reading the whole file plus grepping stale markers (old API names, removed buckets or fields).

- BAD: submitting doc edits without re-read plus stale-marker grep.
- GOOD: full re-read plus stale-marker grep before finishing.
