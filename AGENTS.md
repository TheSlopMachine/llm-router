# AGENTS.md

Rules for AI agents in `llm-router/`. Read them before any change. Every rule is absolute.

This file holds behavior rules only. Facts about the project live in one place each:

| Need | Owner |
|---|---|
| Code map, request flow, where a change goes | `docs/BACKEND.md` |
| Plugin contract (complete reference) | `docs/PLUGIN-API.md` |
| Event log keys | `docs/EVENT-KEYS.md` |
| Make targets and variables | `make help` (`scripts/help.txt`) |
| Script variables | `scripts/README.md` |
| UI text keys | `web/src/lib/i18n/README.md` |
| Release history | `docs/CHANGELOG.md` |

## 0. Principles

### 0.1 Quality over diff size

Pick the correct design, even when it takes a large refactor. A short workaround that leaves the defect class in place is wrong.

- BAD: a 5-line guard in one caller because the service returns `null` for empty lists.
- GOOD: the service returns `[]`. Every caller drops its guard.

### 0.2 Check the net size

A change meant to simplify must leave less code, or a clear reason for more. Read the diff stat before finishing.

- BAD: "unifying" two helpers with a 300-line adapter layer.
- GOOD: one helper, both callers shorter.

### 0.3 One knowledge, one owner

Every fact, rule and helper has one owner, in code and in docs. Before writing a helper, search for an existing one and extend it.

- BAD: a second latency formatter inside a component.
- GOOD: import `formatLatency` from `web/src/lib/format.ts`, extend it there if needed.
- BAD: the same route list in `AGENTS.md` and `docs/BACKEND.md`.
- GOOD: one copy in `docs/BACKEND.md`, a link everywhere else.

### 0.4 Fix the class, not the instance

- BAD: patching the one call site where the bug showed up.
- GOOD: fixing the shared cause so no call site can hit it.

### 0.5 Fail closed

Unknown or failed state denies with a surfaced error. Never default to success, never continue silently.

- BAD: `catch {}` and an empty list on screen.
- GOOD: the error reaches the log or the UI, and the caller stops.

### 0.6 Ask, do not guess

Understand the concept before the code. On ambiguity, ask. A question from the user is not permission to act: answer it with words and change nothing.

## 1. Writing

Applies to everything an agent writes: chat replies, commit messages, PR descriptions, code comments, log strings, UI text, CLI output, errors, docs.

### 1.1 Plain and dense

One idea per sentence. Short sentences. Do not chain clauses with dashes, semicolons or parentheses.

- BAD: "The pool, which refreshes hourly (unless discovery is off), keeps only proxies that passed a check; failed ones are dropped, and imported ones wait for the next ingest."
- GOOD: "The pool refreshes hourly. It keeps proxies that pass the check."

### 1.2 No filler

Delete words that carry no information. Banned: "It's worth noting", "Please note", "Note that", "In order to", "As you can see", "basically", "simply", "High-level description:", "comprehensive", "robust".

- BAD: "In order to fix this issue, we need to update the router."
- GOOD: "Fix the issue by updating the router."

### 1.3 No redundancy

Each word adds distinct information. No synonym chains, no stacked modifiers.

- BAD: "View, browse, and explore available plugins."
- GOOD: "Browse available plugins."
- BAD: "Create a new plugin to add an additional capability."
- GOOD: "Create a plugin to add a capability."

### 1.4 No casual or vague words

Banned in code, comments, commits, logs and docs: "blow up", "wire", "sweep", "spin up", "juice", "nuke", "hack", "magic".

- BAD: "Spin up a goroutine to poll the queue."
- GOOD: "Start a goroutine to poll the queue."

### 1.5 Only what this reader needs here

Each sentence must matter to the reader at that place. Mechanics, history and reasoning belong in code comments or `docs/BACKEND.md`, not in UI text or the README.

User-facing text (UI, CLI, errors, README, release notes) answers one of three questions: what is this, what do I do, what happened.

- BAD (UI): `"Retrying... (3 seconds passed already)"`
- GOOD (UI): `"Retrying..."`
- BAD (UI): `"Access the dedicated plugin management interface to review, inspect, configure, and otherwise manage plugins that have already been installed."`
- GOOD (UI): `"Manage plugins and install more plugins."`
- BAD (UI): `"Press 'Done' to close."`
- GOOD: UI that guides visually, without text.
- BAD (README): `"This starts one public listener on PORT (default 8080) for the dashboard and /v1. The dashboard asks for the password only."`
- GOOD (README): `"This starts the server on the configured port (default is 8080)."`
- BAD (`help` table): `` `logs` | Print server events (`-f`, `--subsystem`, ...). ``
- GOOD (`help` table): `` `logs` | Print server events. `` Flags live in `help`.

A constraint the binary enforces with a clear error needs no paragraph in the docs. A fact the user must know before an irreversible mistake gets one line at the point of action: `"Back up credentials.key with the database: provider keys cannot be decrypted without it."`

Short product claims in the README pitch are fine: "Never hit rate limits."

### 1.6 Change propagation

When a feature's behavior changes, update only the places where the fact matters to that reader. Elsewhere, delete or simplify the old mention. Never add the new detail to every place a grep finds.

- BAD: managed virtual models now resync on import, so "Members refresh on import" goes into the model description, the provider page hint and the README.
- GOOD: the sync rule is one line in `docs/BACKEND.md`. The description says "Fall-through across Groq chat models."

### 1.7 Naming: no arbitrary abbreviations

Use full words in every part of an identifier, in code, tests, scripts, configuration, docs, logs and errors. All new and modified code must comply.

- BAD: `repo`, `args`, `params`, `opts`, `deps`, `impl`, `auth`, `cnfg`, `rqst`, `rsps`, `hndlReq`, `getUsrCfg`
- GOOD: `repository`, `arguments`, `parameters`, `options`, `dependencies`, `implementation`, `authentication`/`authorization`, `configuration`, `request`, `response`, `handleRequest`, `getUserConfiguration`

Accepted as is: `id`, `url`, `http`, `json`, `uuid`, `min`, `max`, `avg`, `i`/`j` in short loops, `x`/`y` in math, `stdin`/`stdout`/`stderr`, `ctx`.

Established terms, language-mandated names and wire-compatible external fields keep their spelling. New project-owned names use full words.

## 2. Session start

Query the OS, shell and working directory first. Training defaults do not apply.

On win32 the shell is PowerShell: chain with `;`, quote paths with `"..."`, no Unix-isms.

- BAD: `head file`, `cat file`, `VAR=x make go-test`.
- GOOD: `make go-test PKG=./internal/...`.

## 3. Project

`llm-router` is a single-binary OpenAI-compatible LLM routing gateway: Go backend, embedded Svelte SPA, embedded SQLite.

- `ModelId = provider/model` (`opencode-zen/gpt-5`, `virtual/my-model`) routes to a backend. Plugins pick credentials under the token allow-list gate.
- Provider backends are single-file Lua plugins, installed from plugin store repositories into the `plugins` table. Go backends exist only for `custom` (OpenAI-compatible passthrough) and `virtual`.

The code map is `docs/BACKEND.md`. Change the shallowest layer that owns the behavior.

## 4. Commands

The `Makefile` is the entry point. Never run a raw command when a target covers the operation. Direct commands are fine where no target exists. `make help` lists targets and variables.

### 4.1 Agent targets

| Target | Use | Replaces |
|---|---|---|
| `make go-vet` | Static check, both modules (`PKG=`) | `go vet` |
| `make go-test` | Tests (`PKG=` while iterating, then one full run) | `go test` |
| `make go-test-race` | Tests with the race detector | `go test -race` |
| `make go-fmt` / `make go-fmt-check` | Apply or check `gofmt` (`PATHS=`) | `gofmt -w`, `gofmt -l` |
| `make sql-policy-test` | Typed GORM conditions only | grep for SQL strings |
| `make go-tidy` | Tidy both modules | `go mod tidy` |
| `make check-frontend` | Svelte and TypeScript check | `bunx svelte-check`, `tsc` |
| `make init` | Install deps, regenerate OpenAPI and API types (`NO_SKIP=1` forces) | `bun install` + codegen |
| `make start` / `make restart` / `make stop` | Dev stack lifecycle | `go run .`, `bun run dev`, `kill` |
| `make status` | Dev process state | `Get-Process`, `netstat`, pidfiles |
| `make log` | Backend events (`EVENT_FILTERS`, `EVENTS_SEVERITY`, `EVENTS_START`/`EVENTS_END`, `EVENTS_LIMIT`, `LINES`, `FOLLOW`) | reading log files |
| `make log-frontend` | Frontend log | reading vite logs |
| `make smoke-test` | Smoke harness on the running stack (`SMOKE_PLUGINS`, `SMOKE_TARGETS`) | running `scripts/smoke` |
| `make upload-plugins` | Upload store `.lua` plugins through the admin listener (`UPLOAD_STORE_DIR`) | copying plugin files |

- Scope `PKG=` only to iterate on one failing package. Finish with one full `make go-test`, never a chain of scoped runs.
- `check-frontend-deps` and `check-build-deps` are prerequisites, not direct targets.
- Filter events natively, never pipe `make log` into `grep` or `Select-String`. Filter keys are event metadata, listed in `docs/EVENT-KEYS.md`. Message text is not filterable.
  - GOOD: `make log EVENT_FILTERS="code=rate_limit" EVENTS_LIMIT=500`
- `make harness-test` runs only in CI. Read its artifacts from CI logs.

### 4.2 Human-only targets

`browser`, `build`, `install`, `publish`, `clean` are human-only. When a task needs one, stop and ask the human to run it and paste the output.

- BAD: agent runs `make build` or `go build -o ./llm-router.exe`.
- GOOD: "Please run `make build` and paste the output."

### 4.3 Git

Read-only git is allowed: `git status`, `git diff`, `git log`. Commit only when instructed, for that commit only. Commits and PR descriptions carry no AI attribution (`Co-Authored-By`, `Generated with`).

`git stash` is allowed only for a baseline run: check for a clean status first and pop right after.

## 5. Prohibitions

1. Killing processes. Use `make stop` / `make restart`. Warn before `make restart` while work is in flight.
2. Deleting database files. Ask the human to delete a corrupt database.
3. Destructive git: `push`, `reset`, `checkout -f`, `clean`, force-push, amend.
4. Littering the project: `*.log`, `*.pid`, `*.tmp`, binaries, scratch notes. Use an external scratch directory and delete verification files when done.
5. Building or launching the app outside make targets (`go run .`, `go build`, `bun run dev`, `Start-Process`, wrappers around them).
6. Discarding output: `> nul`, `2>/dev/null`, `Out-File` sinks. Read output from the tool result.
7. Creating files from the shell (`>`, `>>`, heredocs, `Out-File`) or through executed scripts (`python -c` writers, patch scripts). Use the file tools so every change stays reviewable.
8. Passing env vars with shell syntax (`VAR=value make`, `$env:VAR=...`). Use `make <target> VAR=value`.
9. Silent fallback. Surface every failure, or route it to an explicit named branch.
10. Matching errors by string. Use sentinels with `errors.Is` / `errors.As`.
11. Patching a weak API contract in the frontend with repeated `??` / `?.`. Fix the backend shape: arrays never null, objects never null when the schema promises them.
12. Dead code. Delete unused, legacy and unreachable code in the change that makes it unused: no commented code, unused constants or shims.

## 6. Edit discipline

- Re-read a region before every edit of it. A stale `oldString` misses or deletes neighbors.
- Batch edits per file from a fresh read. Verify each result before the next batch.
- After deleting a function, check the file's imports at once.
- Review the diff of every edited file before moving on.
- Leave no scaffolding: no draft notes, `TODO` placeholders or reasoning asides in the result.
- A predicted runtime-only risk ships with its test in the same change (route change plus `mux_test.go`).
- Verify by running:
  - every Go change: `make go-vet`, `make go-test`, `make go-fmt-check`
  - every frontend change: `make check-frontend`
  - request-path changes (`router`, `proxypool`, `virtual`, `luaplugin`, `api/v1`, `errors`): `make smoke-test`.
- When smoke fails after a change, reproduce on the clean tree first (scoped run, stash baseline or a separate worktree), then fix.

## 7. Design defaults

- One public domain method owns one GORM transaction (`Handle.Transaction`). Never nest transactions across callee layers.
- Fix a data contract where the data is born, not in the frontend or at display time.
- Persisted derived values are frozen. A function whose output is stored changes semantics only with a migration and a stability test.
- Migrate stored shapes explicitly: a numbered migration in the owning registry (`RegistryDatabase`, `RegistryCache`, `RegistryEvents`). Versions are never reused, `Up` is forward-only, history goes to `schema_migrations`.
- Names must not lie. Rename when the meaning changes.

## 8. Svelte 5 reactivity

Never write to a `$state` variable from an `$effect` that reads it, directly or through a called function. Guard flags and `untrack()` do not fix this.

DON'T:

```svelte
$effect(() => {
  if (types.length && !typeKey) {
    untrack(() => { typeKey = types[0] })
  }
})
```

DO, computed value:

```svelte
let typeKey = $derived(types[0] ?? '')
```

DO, one-time default when data arrives:

```svelte
onMount(async () => {
  types = await fetchTypes()
  if (types.length) typeKey = types[0]
})
```

DO, parent notification (the only legitimate write-out): list every dependency, `untrack()` only the outgoing call.

```svelte
$effect(() => {
  void a; void b; void c
  untrack(() => notifyParent(a, b, c))
})
```

Never stack a second guarded `$effect` to stabilize the first. Replace the first with `$derived` or imperative setup. Re-check every touched `$effect` before finishing a `.svelte` change.

## 9. UI patterns

- Adding an item to a list opens a modal with the item's fields. The page shows only an Add button next to the list. Never place an inline form under the list.
  - BAD: Host, Username and Secret fields with an Add button under the registry table.
  - GOOD: an Add button that opens a modal with those fields (`openFormModal`).
- Action buttons sit at the right end. A section with a header puts them in the header (`Header primary`, `SectionHeading actions`, `CardGroup actions`). Otherwise they go in an `HStack halign="end"` under the content. Add and Save rows follow the same rule.
  - BAD: Add on the left under the registry table, Save on the right above it.
  - GOOD: both in `HStack halign="end"`.
- A masked value uses `TextField secure`: browsers do not autofill it. Only the login form uses `type="password"`.

## 10. UI text (i18n)

UI text uses robotic keys from `web/src/lib/i18n/`: `types.ts` (the `TranslationKey` union), `en.ts`, `ru.ts`, `index.ts` (`t`, `n`, `tb`). Full guide: `web/src/lib/i18n/README.md`.

- Add a key to `types.ts`, `en.ts` and `ru.ts` together, then run `make check-frontend`.
- `t('common.actions.save')` for static text, `n(count, ...)` for counts, `tb(summary)` for backend summaries. Never pass English or dynamic strings to `t`.
- Key format `<domain>.<subdomain>.<entity>.<suffix>`: 2-4 levels, `snake_case`, full words.
- Domains: `common`, `auth`, `nav`, `providers`, `credentials`, `models`, `tokens`, `virtual`, `plugins`, `proxy`, `settings`, `data`, `metrics`, `upload`, `misc`, `time`, `units`.
- Suffixes: `.title`, `.label`, `.placeholder`, `.hint`, `.action`, `.status`, `.error`, `.empty`, `.desc`, `.one`/`.many` (English), `.one`/`.few`/`.many` (Russian).
- ASCII only (`...`, `-`). Title case for titles and buttons, sentence case for descriptions.
- Delete a key in all three files when its last use goes.

## 11. Lua plugins

`docs/PLUGIN-API.md` is the binding contract. Every handler or API change updates it in the same change. `TestPluginContract*` in `luaplugin` fails when a sandbox function or handler slot is missing from it, or when its quickstart plugin stops installing.

- A new provider is one `.lua` file with a `--- @` manifest. Never a new Go backend.
- Plugins never mark proxies dead and never return HTML. New widgets become first-class UI node kinds in `DynamicForm.svelte`.
- Rate limits and quota windows park a credential. Rejected keys disable it (under `disable_failed_credentials`).
- `luaplugin.PluginAPIVersion` and the contract doc header change together.

Store plugins live in the sibling store repository, not in this repository. Reissue loop for every content edit:

1. `luac -p` when available.
2. Bump `@version` (major on contract breaks). `make upload-plugins` skips an unchanged version, so content under an old version never deploys.
3. `@plugin_api` major equals the served contract.
4. `make upload-plugins`: expect `installed`, never `already current`.
5. Reproduce the fixed path with a direct `/v1` call, then `make smoke-test SMOKE_PLUGINS=<name>`.

Synthetic checks (install dry-run, classify on fake `{status, headers, body}`) do not replace live checks for stream paths.

## 12. Runtime and smoke

Debug in a closed loop: `make status`, filtered `make log` around the incident window, one direct call per affected path, the fix, the same calls, scoped smoke.

- Order: state, then logs, then lifecycle.
- `make start` runs the public listener (`PORT`, needs the dashboard password) and the loopback admin listener (`127.0.0.1:ADMIN_PORT`, API only, no auth). A fresh dev database prints a generated password once. Never enable `ADMIN_PORT` outside local dev.
- The dev dashboard is `http://HOST:PORT`: vite serves the SPA and proxies `/api/*` and `/v1/*` to the backend on `HOST:38081`.
- `/v1` failure codes (`internal/errors`, `errors.ToAPIError`):
  - wrong provider: `provider_not_found`
  - disabled model: `model_not_found`
  - no usable key: `no_credential`, `credential_not_allowed`
  - upstream failure: `upstream_error`, `timeout`, `rate_limit`, `payment_required`.
- Through the admin listener no auth headers are needed. On the public listener `/v1` needs `Authorization: Bearer <token>` and `/api/llm-router/*` needs a session cookie. A `401` without them is expected.
- When a fact needs the human's instance, name exactly what you need: "Please run `make status` and `make log LINES=200`, paste both outputs."
- `make smoke-test` drives the running stack and never restarts it. Run `make restart` after code changes first.
- Smoke accounts come from the dev database, never from env. Missing credentials skip a plugin.
- Matrix policy: `quota`, `payment`, `rate`, `not_found` and `invalid_request` move to the next model. `auth` fails the plugin. Exhaustion skips it with a reason.
- Streams assert delivery, dashboard chat asserts non-empty `choices`, unary asserts content. A unary-only failure with passing streams means empty upstream text.
- Diagnose from the debug log first: `make smoke-test SMOKE_PLUGINS=... LOG_LEVEL=debug`. Entries below `LOG_LEVEL` never persist, so a missing `debug` line proves nothing.

## 13. Documentation

Docs change in the same change as the code. Present simple for current state, past simple for history.

| File | Holds | Never holds |
|---|---|---|
| `README.md` | What the product is, install, first run | Internals, flags covered by `help` |
| `docs/BACKEND.md` | Current map of the code and where each kind of change goes | History, plugin contract details |
| `docs/PLUGIN-API.md` | Complete, self-contained plugin reference: enough to write a plugin from this file alone | Version history, removed features |
| `docs/EVENT-KEYS.md` | Every event key with meaning and a filter example | Message texts |
| `docs/CHANGELOG.md` | Dated release entries, past simple | Current-state docs |
| `scripts/README.md`, `scripts/help.txt` | Every script variable and target | Implementation notes |

- A new service, table, pipeline step or kind of change adds a line to `docs/BACKEND.md`.
- A new, renamed or removed event key adds or changes its row in `docs/EVENT-KEYS.md`.
- A new target or variable adds a line to `scripts/help.txt` (check with `make help`) and a row to `scripts/README.md`.
- `docs/CHANGELOG.md` is append-only for released versions. Add one `## Done (YYYY-MM-DD, vX.Y.Z)` entry with the version resolved from git tags. An entry whose tag does not exist yet may still be edited.
- `docs/PLUGIN-API.md` describes only the current contract. Every claim traces to code. Examples run as written, with verbatim shapes.
- Before finishing a doc edit, re-read the whole file and grep for stale names (old APIs, removed fields, renamed keys).
