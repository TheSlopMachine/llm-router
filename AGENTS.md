# AGENTS.md

Guidelines for AI agents in `llm-router/`. Read before any change. Rules are absolute. No exceptions. No reinterpretation.

## 0. Priority Rules

1. NEVER write to `$state` from an `$effect` that reads it. → §4
2. NEVER run, start, build, or live-test the app. Only 4 `make` targets allowed. → §3
3. NEVER run destructive git commands. → §5
4. NEVER delete the dev DB. NEVER kill processes. NEVER litter the repo. → §5

## 1. Project

`llm-router` — single-binary OpenAI-compatible LLM routing gateway. Go backend + embedded Svelte SPA + embedded bbolt DB.

- Routes `ModelId = provider/model` (e.g. `openai/gpt-4o`, `agents/my-agent`) → `Adapter` + `CredentialPool`.
- External adapters registered via `adapters.conf`.

## 2. Structure

```
cmd/root.go              CLI entrypoint (--web/--api/--db)
internal/server/         HTTP server: dashboard (8080), /v1 API (8081)
internal/services/
  token/                 router-token issue/validate
  router/                ModelId → Adapter + Credential, LRU + retry
  provider/              Provider CRUD
  credential/            credential pool, usage stats
  agent/                 agents/* virtual provider
  modelinfo/             model metadata cache (1h TTL)
  metrics/               1m buckets, 90d retention
  maintenance/           refresh + cleanup
  tokencount/            slop-tokenizer wrapper
internal/repository/     bbolt buckets
internal/dashboard/      admin REST API
internal/api/v1/         OpenAI-compatible /v1/chat/completions, /v1/models
internal/models/         shared wire types
internal/config/         Config struct
providers/agents/        built-in agents adapter
web/                     Svelte SPA (src/lib/generated/ is generated — do not hand-edit)
adapters.go              generated — DO NOT EDIT
adapters.conf            external adapter registry, one module per line
.workspace/              adapter dev workspace (gitignored) — ONLY place to write adapter code
Makefile                 dev tasks — see §3
```

Rule: keep changes shallow. Do not touch service internals unless the task requires it.

## 3. Commands

### 3.1 Allowed — agent runs these directly

| Command | Purpose |
|---|---|
| `make check-frontend` | Frontend compile/type-check |
| `make prepare-frontend` | Install frontend deps + build frontend assets |
| `make go-check` | Go static check (`go vet`) |
| `make go-test` | Go tests |

### 3.2 Banned — raw equivalents of the above

| DO | DON'T |
|---|---|
| `make check-frontend` | `npx svelte-check`, `tsc --noEmit`, `eslint .` |
| `make prepare-frontend` | `npm install`, `npm run build` |
| `make go-check` | `go vet ./...` |
| `make go-test` | `go test ./...` |

Rule: a raw command duplicating a `make` target is banned even if the outcome would be identical.

### 3.3 Banned — every other Makefile target

`start`, `stop`, `restart`, `status`, `browser`, `prepare`, `prepare-workspace`, `publish`, `clean`.

Agent NEVER runs these. No exception for "just to check", "isolated test", "custom port", or any other framing.

If the task needs one of these: STOP. Ask the human to run it and report back (terminal output, logs, curl/browser result).

| DO | DON'T |
|---|---|
| "Please run `make start` and paste the output." | `go run .`, `go build -o ./llm-router.exe`, `npm run dev`, `Start-Process ...`, any script/wrapper invoking these |

## 4. Svelte 5 Reactivity

Rule: `$effect` NEVER writes to a `$state` variable that the same effect (or a function it calls) also reads. Guard flags and `untrack()` do not fix this — they hide it.

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

## 5. Absolute Prohibitions

| # | Prohibition | Detail |
|---|---|---|
| 1 | Kill processes | No `kill`, `pkill`, `taskkill`, `Stop-Process`. Ask human to run `make stop`. |
| 2 | Delete database files | Never remove `~/.local/llm-router/llm-router-dev.db` (or Windows equivalent). Human recreates it via `make start`. |
| 3 | Destructive git commands | No `git commit`, `git push`, `git reset`, `git checkout -f`, `git clean`, force-push, amend. Commit only if explicitly instructed, for that exact commit only. |
| 4 | Litter the project | No `*.log`, `*.pid`, `*.tmp`, binaries, scratch files, notes inside the project tree. Use `/tmp` or an external scratch dir. |
| 5 | Avoid the Makefile | No raw command substitutes for an allowed target (§3.2). No manual invocation of a banned target's underlying steps (§3.3). |
| 6 | Run, start, or live-test the app | Human-only task. Agent is not proficient at runtime debugging. See §3.3, §7. |

## 6. Adapters

- Location: new adapter code goes ONLY in `.workspace/<adapter-name>/`. Never in `providers/`, repo root, or elsewhere.
- Module: separate Go module `github.com/TheSlopMachine/llm-router-adapter-<name>`, `sdk.Register` in `init()`.
- Required files: `go.mod`, `adapter.go`, `client.go`, `models.go`, `transform.go`, `errors.go`, `README.md`, `.gitignore`.
- Registration: agent edits `adapters.conf` (add `<module>` line). Agent does NOT run `make prepare-workspace` (banned, §3.3) — ask human to run it and confirm `adapters.go` + `go.work` regenerated.
- Verification: `make go-check` only. Runtime check (e.g. `opencode-zen/model`) requires `make start` — ask human.

## 7. When Runtime Info Is Needed

Agent cannot run the app (§3.3, §5.6). To get runtime facts:

1. State exactly what's needed: endpoint, log line, or behavior.
2. Ask human to run `make start` / `make restart` and report back.
3. Interpret: dashboard = `:8080`, API = `:8081/v1`, bearer key printed on start and stored at `~/.local/llm-router/llm-router-dev.key`. `401` without that key header is expected, not a bug.
