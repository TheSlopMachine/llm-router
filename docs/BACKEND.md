# Backend Architecture

Single-binary OpenAI-compatible routing gateway: Go backend, embedded Svelte
SPA, embedded SQLite DB. `docs/PLUGIN-API.md` is the binding plugin contract;
this file maps the Go core.

## System map

```
cmd/root.go              CLI entrypoint (environment + .env file; --version, --generate-config, interactive --bootstrap/--list-login/--remove-login/--reset-login/--create-login)
cmd/login.go             interactive admin login management (terminal gate, prompts, per-flag runners)
internal/server/         HTTP server: public listener (HOST, PORT) + optional loopback admin listener (127.0.0.1, ADMIN_PORT, API only, no auth); route tables in httpapi, source in requestsource
internal/services/
  token/                 router-token issue/validate
  router/                ModelId → backend, token allow-list gate, single pass, no repeats
  provider/              ProviderInstance CRUD (all types, one path) + schema/capability delegation
  credential/            credential storage + admin mutation + plugin lifecycle writes (no ordering, no usage)
  virtual/               virtual models (fall-through lists + instruction)
  responses/             router-side compat records (responses, conversations, assistants, threads, messages, runs)
  batches/               router-side Anthropic message batches (entries + JSONL-ready results)
  luaplugin/             Lua execution core: manifest, sandbox, HTTP+SSRF, storage+TTL, credentials/proxies tables, jobs
  pluginrepo/            plugin store: single-URL index repos (repo URL or direct index.json, files resolved against the index directory); code-defined built-in repos (`BuiltinRepos`, seeded on startup, protected from removal)
  modelinfo/             model metadata cache (1h TTL)
  metrics/               1m buckets with dimension children, 90d retention
  maintenance/           plugin job scheduler, model sync
  events/                operator telemetry: append-only records with metadata keys, retention prune, filtered queries
  videojobs/             router-side video job rows (local id → upstream job)
  healthcheck/           failure-triggered credential verification (check_health, cooldown, singleflight, disable on unhealthy only with automation on)
  admin/                 admin users (bootstrap, logins list/create/delete, password change/reset) + sessions
  config/                instance-wide router configuration
  datamanagement/        subsystem export/import/clear + provider export/import/purge
  doctor/                database inspection and repair
  proxypool/             free-pool hosting, Lua feed bridge, SQLite cache, custom pools (built-in auto row seeded), lane scheduler adapter
internal/httpkit/        shared transport helpers (SSE headers)
internal/logging/        bracket stderr renderer plus events handler (persist and display by LOG_LEVEL)
internal/requestsource/  RequestSource in request context (public vs private listener)
internal/httpapi/        shared route tables + public/private mux builders
internal/errors/         domain sentinels + ToAPIError + response header allow-list
internal/db/             SQLite open (database + cache + events files), DSN, migrations, record shapes, maintenance allowlist
internal/repository/     typed SQLite operations (generics API)
internal/dashboard/      admin REST API (providers, tokens, credentials, models, virtual-models, metrics, plugins, repos, config, data export/import/clear, doctor, proxy status/refresh/pools, provider jobs, events)
internal/api/v1/         OpenAI-compatible /v1/chat/completions, /v1/completions, /v1/messages (+count_tokens, +batches, /v1/complete), /v1/models list + retrieve (TokenRules-filtered, Anthropic dual shape), /v1/videos submit + poll + content + models, audio/transcriptions (+JSON variant)/translations/speech, images/generations/edits/variations, embeddings, moderations, responses (+cancel/input_items/compact/input_tokens) + conversations (+items), assistants, threads (+messages/runs/cancel/submit_tool_outputs)
internal/models/         shared wire types
internal/testutil/mockplugin/ deterministic Lua fixtures for Go tests (one source per capability)
internal/version/        Version struct (Parse/String, short X.Y when fix is 0); router label in main, plugin API contract in luaplugin
internal/config/         environment + .env loading and Config struct
internal/adapters/generic/ built-in custom backend (Go, single pass over gated pool)
providers/virtual/       built-in virtual-models backend (Go, list-order fall-through + first-byte gate)
web/                     Svelte SPA (web/openapi.yaml + src/lib/generated/ auto-generated — do not hand-edit)
scripts/                 separate Go module — build/dev helpers (never imported by main module)
scripts/smoke/           black-box smoke harness (live upstreams only)
docs/                    PLUGIN-API.md (binding plugin contract), BACKEND.md (core map), CHANGELOG.md
Makefile                 thin launcher for scripts/ — see AGENTS.md §3
```

Keep changes shallow. Touch service internals only when the task requires it.

## Request flow

`Parse → Resolve → Disabled → requireModelEnabled → checkEndpoint → capability
→ gateCredentials (token allow-list) → invoke → dropMissingModel(not_found) → metrics.`

- `router/route.go:resolveRequest` owns the shared prefix (parse, resolve,
  disabled gate, model-override gate, endpoint gate). Endpoint code in
  `router/service.go` adds virtual short-circuit, capability pre-check
  (`HasHandler` / `Transcriber` / `Speaker` / `ImageGenerator` / `Embedder` /
  `VideoGenerator` / `VideoSubmitter`), the credential gate and one backend
  call; metrics record in `api/v1/handler.go`. Video submits persist a
  `videojobs` row mapping the local job id to the upstream one; polls and
  content downloads resolve that row first and gate fresh credentials.
- `allowDisabled` probe paths bypass the disabled gate; virtual short-circuit
  runs before the credential gate.
- Virtual models fan out through the router re-entrantly in list order. The
  outer token rules travel in ctx (`router/service.go:withTokenRules`); inner
  member calls inherit them and recompute the gate per member. `nil` token
  with no snapshot stays unrestricted (admin probes only).
- The gate resolves to credential ID allow-lists carried in
  `luaplugin.HandlerMeta.AllowedCredentials` and enforced inside
  `credentials.list/get` (nil = unrestricted). Go adapters (`custom`,
  `virtual`) receive the gated credential slice directly.
- Token credential scopes (`models.CredentialScope`) union with the legacy
  flat `allowed_credentials`: scope `{provider, all}` covers future keys.

## Plugin execution

- `luaplugin/exec.go:handlerCallEx` is the single call site: fresh `LState`
  per call, source load, handler lookup (including `jobs.<name>.run` for
  `job:<name>` invocations), `PCall`, terminal-error decode. Lua error
  tables matching `{message, code?, param?, status?}` become
  `ProviderError`; everything else becomes `PluginInternalError` with crash
  accounting.
- Failure-triggered health verification runs detached: attempts carrying a
  credential identity mark it suspect, the healthcheck service bounds check
  frequency by cooldown and disables only on an explicit unhealthy verdict
  with the provider `disable_failed_credentials` switch on. Off reports the
  verdict without touching the credential. Traffic paths carry no credential
  identity and never trigger.
- Request handlers run `(ctx, request)` (`complete_stream` adds `emit`).
  `check_health` keeps `(ctx, credential)`; `check_model` takes
  `(ctx, model_ref)` with `provider_config` inside; `validate_credentials`
  keeps `(data)`; `get_model_infos` takes `(ctx)` with `provider_config` inside.
- Dashboard model test (`POST /dashboard/models/test`) delegates to
  `check_model`: resolve, `HasHandler` gate (`unsupported` with no live
  request, including Go backends), bounded invoke, verdict mapping
  (`healthy`/`quota_exceeded`/`unhealthy`/`unknown`; `unhealthy` surfaces as
  `model_unavailable` for explicit dashboard disables). Probes never mutate.
- Static schema tables (`credential_schema`, `config_schema`,
  `settings_schema`, `proxy_schema`) validate at install via `parseUINodes`
  and persist in the record; `Service.Schema` serves them without invoking
  Lua. Presence drives capability: credentials iff `credential_schema` or
  `auth_initiate` exists, proxies iff `proxy_schema` exists.
- Colocated jobs validate at install (`parseJobSpecs`: name pattern,
  `interval_seconds` 60..86400, `timeout_ms`, required `run`) and run via
  `Service.RunJob` with `run_reason` tick/startup/manual. Overlap skips;
  `credentials.update` exists in job sandboxes only.
- Proxy source keys qualify per plugin (`<recordID>/<name>`). The Lua feed
  bridge accepts unauthenticated HTTP entries into the free pool. The
  external proxypool library owns candidate ingestion, health checks,
  scoring and cache lifecycle. `proxypool.Service` owns the read-only
  `Query`, `Require` (demand-driven acquisition: waits on the free pool, never
  falls back to direct itself), custom-pool CRUD, `MarkDead` (proxy faults only, called from the
  plugin HTTP client: TLS/DNS faults ban, reset/refused/broken-pipe/truncated
  body mark the proxy suspect), lane scheduler adapter, adaptive limiter, foreground/
  background modes and network-health breaker. Only checked proxies persist.
- `providers/virtual/adapter.go:firstByteGate`: failover continues only
  before the first byte reaches the client. After that the stream belongs
  to one member.

## Error contract

- Domain errors live in `internal/errors`: sentinels (`ErrNotFound`,
  `ErrTimeout` → status `timeout`, `ErrRateLimited`, …) + simplified
  `models.ProviderError{StatusCode, Message, Code, Param}`. No
  `strings.Contains` matching anywhere.
- `ToAPIError(err)`: renders `ProviderError` verbatim (status sanitized to
  400..599, empty code defaults to `server_error`); sentinels map through the
  fixed table. `ErrorTypeForCode` maps wire codes to OpenAI error types.
- Plugin `not_found` (code `not_found`) evicts the model from the info cache
  on every routed path (`router/service.go:dropMissingModel`).
- Terminal tables validate strictly at the Go/Lua boundary: missing or
  empty `message`, out-of-range `status`, empty `code` and non-string
  `param` reject the table as `PluginInternalError`.
- Edge layers render `perr.Message` verbatim (`handleRouterError`,
  `handleCompatRouterError`, dashboard chat): the Go `Error()` prefix never
  reaches clients.

## Transactions

One public method = one SQLite transaction per handle when the operation
must be atomic: `token.Create/Delete/Regenerate` (token + join rows),
`provider.Delete` (provider row, credentials and model overrides cascade
from foreign keys; cached parks, health verdicts and model rows clear
explicitly after), metrics bucket persist (base row plus dimension
children). The two files share no transaction: cross-file writes sequence
primary then cache (plugin storage `set` targets the routed handle first,
then removes the key from the other, so an interrupted write duplicates
instead of losing). Each transaction runs on one pooled connection
(`MaxOpenConnections=16` per file, WAL, `_txlock=immediate`); writers
serialize on the database lock. Each migration runs in one transaction
together with its `schema_migrations` row. `repository` returns
`ErrNotFound` via `%w`; services map it with `errors.Is`, never by string.

## Lifecycle

- `db.OpenSQLite` connects both files, configures each pool, asserts open
  state (`AssertOpenState`: WAL, foreign keys, SQLite >=3.35.0), migrates
  the cache schema (`migrations.RegistryCache`), copies legacy cache tables
  out of single-file databases once, then migrates the primary schema
  (`migrations.RegistryDatabase`, `schema_migrations` history per file,
  forward-only). A missing cache file recreates empty; orphaned `-wal` /
  `-shm` sidecars clear before open. `server.New` seeds providers
  (`EnsureSeeded`), builtin repos (`EnsureBuiltinRepos`) and the builtin
  `auto` pool row; failures stop startup.
- `metrics.Stop`: workers join, `eventCh` drains, all in-memory buckets
  flush (`flushAll`, no age cutoff). Periodic aggregation persists only
  buckets older than 1h and evicts from memory after durable writes.
- `maintenance.Start(ctx)`: one goroutine, one 60s tick, independent work
  (due plugin jobs with overlap-skip and 4-worker bound, model sync).
  `RunStartupRefresh` runs `run_on_startup` jobs before listen under a 30s
  gate; failures log, never fail startup.
- `luaplugin` lifecycle hooks (`lifecycle.go`): optional `initialize` /
  `deinitialize` slots fan out concurrently (4 workers, 30s per call,
  failures log, never block). Install/update/delete/rollback dispatch
  detached after commit; startup runs in the server gate next to
  `RunStartupRefresh`; shutdown runs before metrics stop; provider disable
  transitions dispatch from the provider service only
  (`Update`/`SystemDisable`/`Delete` are the trigger sites); plugin
  enable/disable toggles dispatch from `SetDisabled` (`enable`/`disable`
  reasons). The plugin registry serves enabled types only; disabled rows
  stay inventoried and fail with `provider_disabled`.
- Shutdown enters one path (SIGINT/SIGTERM, listener error, or
  `POST /api/llm-router/shutdown` on the admin listener): background
  writers stop (maintenance, proxy flush, events prune), hooks, metrics
  flush, then both listeners `Shutdown` (in-flight requests complete, so
  the shutdown response is always delivered), then the events queue drains.
  `Close` vacuums all three files after checkpointing each write-ahead
  log; vacuum failures never skip the remaining closes. `make stop` waits
  up to 30s
  for the admin listener first (binds after the startup gate), uses the
  endpoint with a 15s wait and kills past it; the frontend is
  always killed.
- `proxypool.Service.Start(ctx)`: starts the proxypool library scheduler and
  independent 3s cache-flush loop. Library-owned lanes, foreground/background
  hysteresis, adaptive concurrency, suspect handling and network breaker drive
  checks; only validated proxies are persisted. `Stop` occurs via context
  cancellation rather than an adapter-owned adaptive refresh schedule.
- `proxypool.New` loads the persisted library cache and surfaces read errors
  during server construction.

## Storage

SQLite (`internal/db/sqlite.go`, `OpenSQLite`) owns three files:
`DB_PATH` (prod default `~/.local/share/llm-router/llm-router.sqlite3`)
holds durable configuration, `CACHE_PATH` (prod default
`~/.cache/llm-router/llm-router.cache.sqlite3`) holds rebuildable state,
`EVENTS_PATH` (prod default
`~/.local/state/llm-router/llm-router.events.sqlite3`) holds operator
telemetry with `EVENTS_RETENTION_DAYS` (default 3, minimum 1) pruning on
startup and hourly plus `auto_vacuum=INCREMENTAL` reclamation. Dev runs use
`.dev` suffixed files under the same XDG directories
(`llm-router.dev.sqlite3`, `llm-router.cache.dev.sqlite3`,
`llm-router.events.dev.sqlite3`, `.env.dev`); `make start` injects them
through `DB_PATH`/`CACHE_PATH`/`EVENTS_PATH`/`ENV_FILE` when unset. All
paths plus `ENV_FILE` expand a leading `~` (home directory, every OS),
`%NAME%` and `$VAR`/`${VAR}` references, then clean; a tilde elsewhere
stays literal. Managed XDG parents create on startup; custom parents must
exist (`ErrParentMissing`). `GET
/api/events` queries the events file by `severity` (>=), `id`, `start`/`end`
(RFC3339) and `filter.<key>` existence or `filter.<key>=<value>` match
(limit default 50, max 500). The schema
(`internal/db/migrations/0001_initial_schema.go` untouched history plus
`0002_split_storage.go`) owns every table; `internal/db/record/` pins the
row shapes. The database file keeps instance settings, admin users, proxy
pools plus entries, tokens plus rules, providers, credentials, model
overrides, virtual models, plugin repos, plugins, hosts, versions and
durable plugin storage. The cache file keeps sessions, proxy states, model
info snapshots plus rows, credential parks plus health, TTL plugin storage,
metric buckets plus dimensions, video jobs, response records and message
batches. Cache rows reference durable parents softly: no cross-file foreign
key exists, deletes clear related cache rows explicitly, and deleting the
cache file while stopped only logs operators out and refills in the
background. Reads and writes run through `gorm.G` with typed clauses (`scripts/sqlpolicy` enforces the policy);
`repository.GormRepository` owns the generics API (500-row upsert batches,
`Clear` is the one `AllowGlobalUpdate` path). DSN pins foreign keys, WAL,
synchronous NORMAL, busy timeout and immediate lock; `maintenance.go` owns
the only raw statements (checkpoint, vacuum, freelist count, optimize,
integrity check, backup, events prune).
Sessions store the token hash as primary key, never plaintext.
`virtual_models.name_key` carries the UNIQUE duplicate signal.
`plugin_storage.expires_at` carries the TTL index and routes the row: TTL
rows live in cache, durable rows in the database. Metrics persist one
`metric_buckets` row plus dimension children (`metric_bucket_providers`,
`metric_bucket_models`, `metric_bucket_tokens`,
`metric_bucket_error_types`); provider dimensions carry no duration columns.
`proxypool.gormCache` buffers states in memory; `Set` stages, `Flush` lands
500-row upserts inside one transaction per 20000 states, the 3s `flushLoop`
drives it. Provider `proxy.pool` references `proxy_pools` with RESTRICT.
Foreign keys carry the cascades the bucket scans used to perform by hand:
provider deletes cascade credentials, model overrides, managed
virtual models and virtual members; plugin deletes cascade hosts, versions
and durable storage; metric bucket deletes cascade dimension children.

## Smoke harness

`scripts/smoke/` drives the request paths black-box against a dev stack
(`make smoke-test` runs it against the running stack with no restart, then drives the admin listener): status → bootstrap →
plugin install → provider + dev-database credentials → model matrix
(one model per capability, first success closes it, plus fallback and
negative `endpoint_not_supported` paths) → mock `mock-limited` skip-path
self-check → cleanup of created credentials. Quota (`quota_exceeded`,
`insufficient_quota`), payment, rate, missing-model and fallback-path
outcomes skip with reason; anything else fails. Exit 0 means clean (skips
allowed).

## Adding an endpoint

1. `models`: `Endpoint*` constant + `SupportsEndpoint` coverage.
2. `provider`: capability interface (`Transcriber` / `Speaker` / `ImageGenerator` / `Embedder` / `Moderator` / `VideoGenerator`) for Go backends.
3. `luaplugin`: `Handler*` constant in `handler_names.go` (+ sandbox
   registration validation).
4. `router`: resolve + capability + gate + single call (see `route.go`).
5. `api/v1`: handler + `authorizeModel` + `recordRouteMetric`.
6. Plugin contract: extend `docs/PLUGIN-API.md` in the same change.

Edge translation: one plugin handler serves several edge routes. The edge
normalizes the wire shape, the router runs one pipeline, the plugin sees
one request table.

| Edge routes | Router pipeline | Plugin handler |
|---|---|---|
| `chat/completions`, `completions` (`ToChat`), `messages` (`ToChat`), `complete` (wraps as messages) | `Complete` / `CompleteStream` | `complete` / `complete_stream` |
| `responses` family, `threads` runs, `messages/batches` entries | chat pipeline + `responses` / `message_batches` rows | `complete` (`CreateResponse`, `CreateRun`, `CreateBatch` convert first) |
| `audio/transcriptions`, `audio/translations` | `Transcribe` | `transcribe` (edge marks the translation response) |
| `images/generations`, `images/edits`, `images/variations` | `GenerateImage` | `generate_image` (`image_b64` / `mask_b64`) |
| `audio/speech` | `Speech` | `speech` (edge serves the native `format` bytes, no transcoding) |
| `embeddings` | `Embed` | `embed` (edge renders `base64` from float vectors) |
| `moderations` | `Moderate` | `moderate` (request gate keys on `chat/completions`) |
| `videos` submit/poll/content | `SubmitVideo` / `PollVideo` / `VideoContent` | `generate_video` / `poll_video` / `video_content` (`videojobs` maps local ids to upstream ids) |

`messages/count_tokens` and `responses/input_tokens` run a local heuristic,
no upstream call. `models` list/retrieve read the cached catalog and
dual-serve the Anthropic shape. New wire shapes that reuse a pipeline skip
step 3 and persist async state in `responses` / `message_batches` (see
`responses/service.go`, `batches/service.go`). Auth accepts `Authorization: Bearer` and the
`x-api-key` alias on every `/v1` route; the `anthropic-version` header
selects the Anthropic success/error envelope via `writeCompatError` /
`handleCompatRouterError`.
