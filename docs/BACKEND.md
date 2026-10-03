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
  router/                ModelId → backend + CredentialPool, single pass, no repeats
  provider/              ProviderInstance CRUD (all types, one path)
  credential/            credential pool, usage stats
  virtual/               virtual models (fall-through lists + instruction)
  responses/             router-side compat records (responses, conversations, assistants, threads, messages, runs)
  batches/               router-side Anthropic message batches (entries + JSONL-ready results)
  luaplugin/             Lua execution core: manifest, sandbox, HTTP+SSRF, storage, model_specs merge
  pluginrepo/            plugin store: single-URL index repos (repo URL or direct index.json, files resolved against the index directory); code-defined built-in repos (`BuiltinRepos`, seeded on startup, protected from removal)
  modelinfo/             model metadata cache (1h TTL)
  metrics/               1m buckets, 90d retention
  maintenance/           credential refresh, model sync, auth cleanup
  videojobs/             router-side video job rows (local id → upstream job)
  exhausted/             joint limit keys (credential[`account`]/model/proxy), subset match, expiry auto-delete
  geoban/                indefinite (plugin, provider type, proxy) geo flags, no expiry, explicit clear
  healthcheck/           failure-triggered credential verification (check_health, cooldown, singleflight, disable on unhealthy only)
  admin/                 admin password change
  config/                instance-wide router configuration
  datamanagement/        subsystem export/import/clear + provider export/import/purge
  doctor/                database inspection and repair
  proxypool/             external library adapter, Lua source bridge, bbolt cache, provider policy and adaptive refresh schedule
internal/pool/           single-pass credential failover (unary + stream)
internal/streamgate/     first-byte gate: failover stops after first SSE byte
internal/httpkit/        shared transport helpers (SSE headers)
internal/errors/         domain sentinels + MapUpstream + ToAPIError
internal/repository/     bbolt buckets
internal/dashboard/      admin REST API (providers, tokens, credentials + refresh, models, virtual-models, metrics, plugins, repos, config, data export/import/clear, doctor, proxy status/refresh, geo bans)
internal/api/v1/         OpenAI-compatible /v1/chat/completions, /v1/completions, /v1/messages (+count_tokens, +batches, /v1/complete), /v1/models list + retrieve (TokenRules-filtered, Anthropic dual shape), /v1/videos submit + poll + content + models, audio/transcriptions (+JSON variant)/translations/speech, images/generations/edits/variations, embeddings, moderations, responses (+cancel/input_items/compact/input_tokens) + conversations (+items), assistants, threads (+messages/runs/cancel/submit_tool_outputs)
internal/models/         shared wire types
internal/config/         Config struct
internal/adapters/generic/ built-in custom backend (Go)
providers/virtual/       built-in virtual-models backend (Go)
web/                     Svelte SPA (web/openapi.yaml + src/lib/generated/ auto-generated — do not hand-edit)
scripts/                 separate Go module — build/dev helpers (never imported by main module)
scripts/smoke/           black-box smoke harness + mock provider (testdata/mock.lua)
docs/                    PLUGIN-API.md (binding plugin contract), BACKEND.md (core map), CHANGELOG.md
Makefile                 thin launcher for scripts/ — see AGENTS.md §3
```

Keep changes shallow. Touch service internals only when the task requires it.

## Request flow

`Parse → Resolve → Disabled → requireModelEnabled → checkEndpoint → capability
→ loadCredentials (dropExhausted → token filter) → invoke → dropMissingModel(not_found) → metrics.`

- `router/route.go:resolveRequest` owns the shared prefix (parse, resolve,
  disabled gate, model-override gate, endpoint gate). Endpoint code in
  `router/service.go` adds virtual short-circuit, capability pre-check
  (`HasHandler` / `Transcriber` / `Speaker` / `ImageGenerator` / `Embedder` /
  `VideoGenerator` / `VideoSubmitter`), credential load and pool call; metrics
  record in `api/v1/handler.go`. Video submits persist a `videojobs` row
  mapping the local job id to the upstream one; polls and content downloads
  resolve that row first and re-enter the pool with fresh credentials.
- `allowDisabled` probe paths bypass the disabled gate; virtual short-circuit
  runs before credential load.
- Virtual models fan out through the router re-entrantly. The outer token
  rules travel in ctx (`router/service.go:withTokenRules`); inner member
  calls inherit them. `nil` token with no snapshot stays unrestricted
  (admin probes only).
- Token credential scopes (`models.CredentialScope`) union with the legacy
  flat `allowed_credentials`: scope `{provider, all}` covers future keys.

## Pool invariants

- `pool.Run / pool.RunStream`: one credential attempt per key, pool order,
  no backoff. Fatal errors (`ErrHandlerNotFound`, `invalid_request`,
  `content_policy`, `structural_fault`, `geo` in `fail_fast` mode) stop
  immediately. Every retryable proxy failure (geo blocks in `next_proxy`
  mode, proxy-scoped rate/quota errors, `transport` connectivity
  failures, `overloaded` congestion) retries the same credential on an
  untried proxy up to the provider `proxy_retry.max_attempts`
  (`fail_fast` default, `next_proxy` default 3, cap 10); a full-combination
  error also retries when the request used a proxy. Streams allow this
  retry only before the first byte reaches the client. Geo errors do not
  retry with the same credential on streams.
  Per-attempt limit ordering moves credentials with a live exhausted key
  (`luaplugin/exhausted_skip.go`) to the tail ordered by earliest reset first.
  The router also reorders exhausted matches before the token filter: unlimited
  first, limited last as last resort.
- Proxy source keys qualify per plugin (`<recordID>/<name>`). The router
  bridge accepts unauthenticated HTTP entries from each Lua source. The external
  proxypool library owns candidate ingestion, health checks, scoring and
  cache lifecycle. `proxypool.Service` owns provider filters, proxy limit
  metadata and the adaptive refresh schedule.
- `streamgate.Writer`: failover continues only before the first byte reaches
  the client. After that the stream belongs to one upstream.
- Usage tracking is best-effort but never silent: failures log with the
  credential ID. Limit state lives in `exhausted.Service`: rate/quota
  outcomes mark the scoped joint key (or the full combination without
  scope) with the plugin-supplied TTL, `model_unavailable` marks
  `(provider, model)` for a fixed 2 minutes, all in the exec defer.
  `auth` / `payment_required` / `structural_fault` record no state: the pool
  fails over (`structural_fault` stops it) and surfaces the last error.
  `geo` records the indefinite geoban flag.

## Exhausted store and geo bans

- Stored keys act as filters over candidate dimensions (plugin, provider
  instance, credential [`account` scope word], model, proxy). A candidate matching every stored dimension
  is deprioritized until `ResetsAt` passes. Two instances of one adapter type never
  share a credential-less mark.
- Credential pools reorder matching combinations before the token filter: unlimited
  first, limited after ordered by earliest reset first
  (`router/service.go:dropExhausted`). Proxy picks deprioritize limited routes after ranking;
  joint limits stay in `exhausted.Service`, while proxy-scoped limits use
  library-managed metadata. A proxy-scoped rate/quota outcome cools the route
  and retries the same credential through another route. If no alternate
  route is usable, the router returns the original provider error. Limit-store
  errors stop routing. Manual mode with no IDs goes direct; selected IDs that
  resolve to no healthy proxy fail.
- Expired entries delete on read; `Prune` sweeps the rest. Content,
  malformed-request, missing-model and transient failures never mark.
- Geo bans (`geoban.Service`, bucket `geo_bans`) carry no expiry: proxy
  picks filter banned `(plugin, provider type, proxy)` triples after ranking,
  and unbanned picks from other regions sort above same-region picks.
  Flags clear through explicit admin clear
  (`DELETE /dashboard/providers/{id}/geo-bans[/{proxyId}]`). The pool does
  not expose manual proxy deletion; the library retains dead entries for
  health retries.

## Error contract

- Domain errors live in `internal/errors`: sentinels (`ErrNotFound`,
  `ErrTimeout` → status `timeout`, `ErrRateLimited`, …) + `models.ProviderError` with a typed
  `ErrorType` (13 plugin types plus `Unknown`; the removed `timeout` type merged into `upstream`). No
  `strings.Contains` matching anywhere.
- `MapUpstream(status, code, type, message)`: exact status/code matching
  from the upstream envelope with code fallbacks before the bare-status
  default, so quota/content/loading signals through 400 classify correctly;
  message text never decides (except quota wording on bare 429s in the Lua
  classify helper). 403 with moderation codes maps to `content_policy`,
  bare 403 stays `auth`.
- `ToAPIError(err)`: the single domain→wire table for both surfaces.
  `ErrorTypeForCode` maps wire codes to OpenAI error types.
- Provider 404 (`ErrorTypeNotFound`) → wire `not_found`, evicts the model
  from the info cache on every routed path.
- Contract tables validate strictly at the Go/Lua boundary: unknown types,
  missing or past `retry_after` on rate/quota, and `scope`/`retry_after`
  where forbidden reject the table as `PluginInternalError`.
- `ProviderError` carries `UpstreamStatus`/`UpstreamBody` for logs only;
  bodies over 4KiB spill to `upstream_dumps/` files in debug mode.

## Transactions

One public method = one `db.Update` when the operation must be atomic:
`token.Create/Delete/Regenerate` (token + index), `provider.Delete`
(provider + credentials), `credential.Reorder/DeleteByProvider`,
metrics batch persist. `repository` returns `ErrNotFound` via `%w`;
services map it with `errors.Is`, never by string.

## Lifecycle

- `metrics.Stop`: workers join, `eventCh` drains, all in-memory buckets
  flush (`flushAll`, no age cutoff). Periodic aggregation persists only
  buckets older than 1h and evicts from memory after durable writes.
- `maintenance.Start(ctx)`: one goroutine, one tick, independent jobs
  (credential refresh, model sync, auth cleanup). A credential-list failure
  never skips model sync.
- `proxypool.Service.Start(ctx)`: startup refresh and independent adaptive
  schedule. Refreshes run each minute during proxy use, then every five,
  ten and fifteen minutes while idle. A request after eight idle minutes
  starts a refresh immediately.
- `proxypool.New` loads the persisted library cache and surfaces read errors
  during server construction. Legacy migration runs in `EnsureSeeded` and
  `proxypool.MigrateLegacy`, where failures surface as errors.

## Buckets

`internal/db` owns bucket names and creation. The DB initializer drops the
legacy `agents` bucket. `proxypool.MigrateLegacy` drops `proxies`,
`proxies_v2`, `active_regions`, `proxy_source_meta`, and `proxy_limits`.
Supported unauthenticated HTTP proxy rows move to `proxy_cache_v1`; proxy limits move into library
metadata; geo-ban IDs are rewritten in `geo_bans`; and old pool settings are
removed from `router_configuration`. Unsupported rows and references are
counted or removed. Other legacy rows migrate
explicitly (`migrateLegacyCustom` inside `EnsureSeeded`,
`migrateClearCredentialQuota` in `server`).

Live buckets (`internal/db/db.go`): `meta`, `admin`, `tokens`,
`token_index`, `provider_instances`, `credentials`, `plugins`,
`plugin_repos`, `plugin_storage`, `auth`, `sessions`, `metrics`,
`virtual_models`, `router_configuration`, `model_overrides`, `model_infos`,
`proxy_cache_v1`, `exhausted`, `geo_bans`, `video_jobs`, `responses`,
`message_batches`. Legacy `providers`,
`custom_providers`, `model_info`
remain defined but are not created; `EnsureSeeded` migrates them.

## Smoke harness

`scripts/smoke/` drives the request paths black-box against a dev stack
(`make smoke` restarts with `NO_AUTH=1` first): status → bootstrap →
plugin install → provider + dev-database credentials → model matrix
(one model per capability, first success closes it, plus fallback and
negative `endpoint_not_supported` paths) → cleanup of created
credentials. Quota, payment, rate, missing-model and fallback-path outcomes skip with
reason; anything else fails. Exit 0 means clean (skips allowed).

## Adding an endpoint

1. `models`: `Endpoint*` constant + `SupportsEndpoint` coverage.
2. `provider`: capability interface (`Transcriber` / `Speaker` / `ImageGenerator` / `Embedder` / `Moderator` / `VideoGenerator`) for Go backends.
3. `luaplugin`: `Handler*` constant in `handler_names.go` (+ sandbox
   registration + `callAndDecode` wiring in `decode.go`).
4. `router`: resolve + capability + pool call (see `route.go`).
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
| `images/generations`, `images/edits`, `images/variations` | `GenerateImage` | `generate_image` (`image_b64` / `mask_b64`; old plugins ignore the extra fields) |
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
