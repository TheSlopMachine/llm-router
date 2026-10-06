# Backend Architecture

Single-binary OpenAI-compatible routing gateway: Go backend, embedded Svelte
SPA, embedded bbolt DB. `docs/PLUGIN-API.md` is the binding plugin contract;
this file maps the Go core.

## System map

```
cmd/root.go              CLI entrypoint (--web/--api/--db; defaults 8080/8081, dev uses WEB_PORT/API_PORT 38080/38081)
internal/server/         HTTP server: dashboard + /v1 API (ports from CLI flags)
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
  metrics/               1m buckets, 90d retention
  maintenance/           plugin job scheduler, model sync, auth cleanup
  videojobs/             router-side video job rows (local id → upstream job)
  healthcheck/           failure-triggered credential verification (check_health, cooldown, singleflight, disable on unhealthy only with automation on)
  admin/                 admin password change
  config/                instance-wide router configuration
  datamanagement/        subsystem export/import/clear + provider export/import/purge
  doctor/                database inspection and repair
  proxypool/             free-pool hosting, Lua feed bridge, bbolt cache, custom pools, adaptive refresh schedule
internal/httpkit/        shared transport helpers (SSE headers)
internal/errors/         domain sentinels + ToAPIError
internal/repository/     bbolt buckets
internal/dashboard/      admin REST API (providers, tokens, credentials, models, virtual-models, metrics, plugins, repos, config, data export/import/clear, doctor, proxy status/refresh/pools, provider jobs)
internal/api/v1/         OpenAI-compatible /v1/chat/completions, /v1/completions, /v1/messages (+count_tokens, +batches, /v1/complete), /v1/models list + retrieve (TokenRules-filtered, Anthropic dual shape), /v1/videos submit + poll + content + models, audio/transcriptions (+JSON variant)/translations/speech, images/generations/edits/variations, embeddings, moderations, responses (+cancel/input_items/compact/input_tokens) + conversations (+items), assistants, threads (+messages/runs/cancel/submit_tool_outputs)
internal/models/         shared wire types
internal/config/         Config struct
internal/adapters/generic/ built-in custom backend (Go, single pass over gated pool)
providers/virtual/       built-in virtual-models backend (Go, list-order fall-through + first-byte gate)
web/                     Svelte SPA (web/openapi.yaml + src/lib/generated/ auto-generated — do not hand-edit)
scripts/                 separate Go module — build/dev helpers (never imported by main module)
scripts/smoke/           black-box smoke harness + mock provider (testdata/mock.lua)
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
  `check_health` keeps `(ctx, credential)`; `validate_credentials` keeps
  `(data)`; `get_model_infos` takes `(ctx)` with `provider_config` inside.
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
  `Query`, custom-pool CRUD, `MarkDead` (TLS faults only, called from the
  plugin HTTP client), lane scheduler adapter, adaptive limiter, foreground/
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

One public method = one `db.Update` when the operation must be atomic:
`token.Create/Delete/Regenerate` (token + index), `provider.Delete`
(provider + credentials), metrics batch persist. `repository` returns
`ErrNotFound` via `%w`; services map it with `errors.Is`, never by string.

## Lifecycle

- `metrics.Stop`: workers join, `eventCh` drains, all in-memory buckets
  flush (`flushAll`, no age cutoff). Periodic aggregation persists only
  buckets older than 1h and evicts from memory after durable writes.
- `maintenance.Start(ctx)`: one goroutine, one 60s tick, independent work
  (due plugin jobs with overlap-skip and 4-worker bound, model sync, auth
  cleanup). `RunStartupRefresh` runs `run_on_startup` jobs before listen
  under a 30s gate plus auth cleanup; failures log, never fail startup.
- `proxypool.Service.Start(ctx)`: starts the proxypool library scheduler and
  independent 3s cache-flush loop. Library-owned lanes, foreground/background
  hysteresis, adaptive concurrency, suspect handling and network breaker drive
  checks; only validated proxies are persisted. `Stop` occurs via context
  cancellation rather than an adapter-owned adaptive refresh schedule.
- `proxypool.New` loads the persisted library cache and surfaces read errors
  during server construction.

## Buckets

`internal/db` owns bucket names and creation. `initBuckets` drops legacy
`agents`, `exhausted`, `geo_bans`, `providers`, `custom_providers`,
`model_info`, `proxies`, `proxies_v2`, `active_regions`,
`proxy_source_meta` and `proxy_limits` on every open: pre-0.7.0 rows never
resurrect and no migration shims remain (`migrateClearCredentialQuota`
stays for ancient credential rows).

Live buckets (`internal/db/db.go`): `meta`, `admin`, `tokens`,
`token_index`, `provider_instances`, `credentials`, `plugins`,
`plugin_repos`, `plugin_storage`, `auth`, `sessions`, `metrics`,
`virtual_models`, `router_configuration`, `model_overrides`, `model_infos`,
`proxy_cache_v1`, `custom_pools`, `video_jobs`, `responses`,
`message_batches`, `credential_health`, `credential_parks`.

## Smoke harness

`scripts/smoke/` drives the request paths black-box against a dev stack
(`make smoke` restarts with `NO_AUTH=1` first): status → bootstrap →
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
