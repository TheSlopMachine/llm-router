# Changelog

## Done (2026-10-06, proxypool 1.1.0)

- Moved proxy-pool orchestration to the library-backed lane scheduler: foreground/background modes, liveness/revival lanes, adaptive concurrency and network-health breaker state are now exposed through the dashboard status API.
- Added `POST /api/llm-router/dashboard/proxy/recheck-banned` with optional reason/source filters and a queued-count response; proxy cache persistence now records only validated entries.


## Done (2026-10-04, v0.7.0)

- Bumped `proxypool` to v1.0.5: unified timeout configuration (separate handshake and probe phases), deadline-bounded SOCKS4/5 implementation, progress reporting every 5000 completions, removed `golang.org/x/net` dependency.

## Done (2026-10-04, v0.7.0)

- Accepted https, socks4 and socks5 proxy candidates (plus the socks4a alias and `user[:pass]@` credentials) in feed validation and per-request transport; bumped `proxypool` to v1.0.4 and delegated `TransportFor` to the library instead of the HTTP-only dialer.
- Documented the `fetch_proxies` row shapes and accepted protocol set in `PLUGIN-API.md`.
- Batched proxy cache persistence behind a write-behind flush: `Set` now buffers in memory and `Flush` lands each refresh cycle in 20K-entry transactions instead of one fsync'd write per proxy; manual marks flush synchronously.

## Done (2026-10-04, v0.7.0)

- Bumped the frontend toolchain to latest: Vite 8.3.2, vite-plugin-svelte 7.3.1, Svelte 5.57.1, TypeScript 6.0.3. Pinned openapi-typescript at 5.4.2 (last line reading the Swagger 2.0 spec) and skipped TypeScript 7 (its main export is version-only, which broke codegen). Migrated tsconfig paths to `./`-relative form after dropping the deprecated `baseUrl`. The `failed to load config from vite.config.ts` svelte-check warning disappeared under Vite 8.

## Done (2026-10-03, v0.7.0)

- Decentralized request orchestration to Lua plugins: credential selection, proxy selection, retry loops and rate-limit handling moved out of the core router into plugin code. Removed the credential failover pool, the stream first-byte gate package, router credential ordering and usage accounting, the exhausted joint-key store, geo bans, proxy rank/metadata/source policy, provider proxy mode/retry config and the `classify_error` slot plus helper.
- Shipped the 0.7.0 plugin runtime: explicit per-request `proxy_url` on the HTTP client with router-owned TLS-fault `MarkDead`, read-only `proxies.query` over the free pool and custom pools, `credentials.list/get` plus job-only `credentials.update`, KV `storage.set` with TTL, static schema tables (`credential_schema`, `config_schema`, `settings_schema`, `proxy_schema`) with presence-driven dashboard surfaces, and colocated `jobs` with interval/startup/timeout schedules. Terminal errors are OpenAI-shaped (`message`, `code`, `param`, `status`) and render verbatim.
- Replaced credential refresh with the plugin job scheduler: per-provider-instance tickers with overlap-skip and a 4-worker bound, `run_on_startup` jobs before listen, model autosync and auth cleanup retained, plus a dashboard manual job trigger.
- Purged legacy state without backward compatibility: dropped `exhausted`, `geo_bans`, `providers`, `custom_providers`, `model_info`, `proxies`, `proxies_v2`, `active_regions`, `proxy_source_meta` and `proxy_limits` buckets on open; added the `custom_pools` bucket with Proxies-page CRUD and a provider pool selector defaulting to direct.
- Rewrote all dashboard and `/v1` error paths around terminal plugin errors: no `MapUpstream`, no envelope parsing, no `Retry-After` synthesis, no per-route reclassification. Rewrote the mock provider to the 0.7.0 contract and extended the smoke harness skip codes with `insufficient_quota`.
- Made the provider `disable_failed_credentials` switch master every non-manual credential disable: single probes and detached health checks report `unhealthy` without disabling while off, and disable as before while on. Threaded the provider ID into discovery so `credentials.list()` sees the provider's rows; empty pools yield empty lists via `ListUsable` instead of errors.
- Unified the credential-disabled state across dashboard and plugins: `credentials.disable/enable` set the shared flag (`DisabledBy="plugin"`, first-wins, plugin restore clears only plugin causes), gated on the automation switch; KV parking stays for transient cooldowns while dead keys disable visibly.

## Done (2026-10-03, v0.5.3)

- Unified same-credential proxy retries under one provider `proxy_retry` policy (`fail_fast`/`next_proxy`, `max_attempts` 1..10, default 3): geo blocks, proxy-scoped rate/quota limits, transport failures and overloads now share a single attempt budget instead of the detached geo count plus the hardcoded route budget of three. Legacy `geo` sections migrated to `proxy_retry` at startup. Credential failover and stream first-byte rules stayed unchanged.
- Made fresh model discovery refresh known rows (modalities, endpoints, capabilities) instead of freezing the first-seen record; transiently omitted names are still retained until the backend reports them missing. The dashboard Test probe no longer writes modality overrides for rows that already report modalities, so plugin `model_specs` stopped being masked by stale probe data.

## Done (2026-10-03, v0.5.0)

- Made the gateway a drop-in superset for OpenAI-family and Anthropic clients. Added `POST /v1/completions` (prompt+suffix mapped onto the chat pipeline, Ollama/Mistral aliases translated), `POST /v1/images/edits` and `POST /v1/images/variations` (multipart, same images response, `image_b64`/`mask_b64` passed to the `generate_image` handler), `POST /v1/audio/translations` (transcription pipeline with `task: translate`), `POST /v1/moderations` (new optional `moderate` plugin handler plus `Moderator` Go capability), `POST /v1/messages/count_tokens` (documented 4-chars-per-token heuristic), `POST /v1/complete` (legacy Anthropic completions over the messages pipeline), the `POST /v1/responses` family (create/retrieve/cancel/input_items/compact/input_tokens plus `POST /v1/conversations` with items), prior-generation assistants/threads/messages/runs shims with `requires_action` tool continuations, and the six Anthropic `POST|GET /v1/messages/batches` operations with JSONL results. Responses-family state persists in the new `responses` and `message_batches` buckets.
- Hardened the shared surface: `GET /v1/models` and `GET /v1/models/{model}` require tokens, filter by TokenRules (denied ids 404 like missing ones), and dual-serialize the Anthropic models shape when `anthropic-version` is present; all `/v1` routes accept `x-api-key` as a Bearer alias; Anthropic-style requests get the `{type:error,error:{type,message}}` envelope (429/529 for rate/congestion) and echoed version header; Mistral `random_seed`/`output_dimension`, Ollama `options`/`format`, and OpenRouter `reasoning` envelopes translate at the edge; JSON STT (`input_audio` base64 or URL download) parses on transcriptions. No endpoint carries `deprecated:true`.
- Bumped the plugin contract to 0.5.0 (`moderate` slot, `image_b64`/`image_name`/`mask_b64` image fields); older plugins install unchanged.

## Done (2026-10-02, v0.4.0)

- Added the OpenRouter-compatible video generation endpoint: `POST /v1/videos` returns 202 with a router-local job id and polling URL, `GET /v1/videos/{jobId}` polls upstream status, `GET /v1/videos/{jobId}/content` streams the video bytes, and `GET /v1/videos/models` lists video-capable models. Job rows persist in the new `video_jobs` bucket (local id maps to the upstream job; polls re-enter the credential pool with fresh credentials). Lua plugins serve `generate_video`/`poll_video`/`video_content`, `custom` providers proxy the same paths upstream, and virtual models fan submits out over members. The mock provider serves a deterministic completed job with synthesized ftyp/mdat bytes, and the smoke harness covers submit, poll, content and the chat-only negative path under `SMOKE_TARGETS=video`.

## Done (2026-10-02, v0.3.10)

- Restricted manual provider creation to `custom` (OpenAI-compatible) rows: `POST /dashboard/providers` rejected other type keys, the Type picker and qualifier input left the New Provider wizard, and the `adapter-types` and `type-schemas` endpoints with their `TypeKeys`/`IsCreatableTypeKey` helpers were removed. Plugin type registration and core seeding stayed unchanged.

## Done (2026-10-01, v0.3.9)

- Added the request `cache_key`: a stable cross-turn prefix-cache partition (model, first message, sorted tool names) so consecutive turns of one conversation reuse the upstream prefix cache. Request tables carry it; old routers omit it.

## Done (2026-10-01, v0.3.8)

- Added manual proxy exclusion: structural proxy faults (DNS, address, TLS identity and handshake, refused, reset, early EOF, client-build failures) mark the proxy dead on an escalating 15m-4h schedule with 24h forgiveness, without touching probe scoring. Refresh write-back preserves newer manual marks over stale pipeline snapshots. Rotation stops at once on client cancellation instead of burning the pick list.

## Done (2026-09-30, v0.3.7)

- Added the `overloaded` error type for congested backends (exact `service_overloaded` code, wire `overloaded`, 503). Overloads retry the same credential on another proxy within the route budget with no marks or cooldown. Unary JSON errors now carry `Retry-After` when the router knows a wait time, and the contract documents the client backoff rules per wire code.

## Done (2026-09-30, v0.3.6)

- Added the plugin `model_specs` registration table: pinned per-model rows merged over discovered catalog rows in `GetModelInfos`. Specs win per field except the id, unknown ids are ignored, and unknown fields or mistyped values reject the plugin at install. Old routers ignore the unknown field.

## Done (2026-09-30, v0.3.5)

- Added the `transport` error type for connectivity failures (EOF, reset, broken tunnel, timeouts) with wire code `transport_error`. Transport failures carry no marks or disables and retry the same credential on another proxy within the three-attempt route budget. The HTTP client reports transport failures with the new type; plugins forward `req_err` tables as-is to preserve it.

## Done (2026-09-30, v0.3.4)

- Allowed `proxy` scope for `quota_exceeded` and retried proxy-scoped limits with the same credential on alternate routes, up to three attempts. Streaming retries stop after the first byte reaches the client, and requests return the original limit error when no alternate route is available.
- Reclassified OpenCode Free 429 responses as proxy-scoped rate limits or quotas instead of geo blocks. Classified the OpenCode-only 401 response as `upstream` so it does not geoban a proxy or disable a credential.
- Prevented proxy routing from falling through to direct when every ranked candidate was filtered. Updated the plugin contract and OpenCode Free plugin version.
- Fixed pool skip returning a silent empty success when every credential carried a live limit mark. An all-skipped pool now attempts in order as a last resort, matching the router drop-exhausted fallback.

## Done (2026-09-30, v0.3.3)

- **Library-backed proxy pool.** Added the external `proxypool` dependency, a Lua source adapter, and a bbolt `CacheSource`. Migrated supported HTTP proxies, selected provider IDs, proxy-scoped limits, and geo-ban references. Removed old pool settings from `router_configuration`, deleted the router-owned probe, health, ranking, rotation, CRUD, and per-source refresh code, and dropped the obsolete `proxies` bucket.
- **Adaptive refresh.** Started a refresh at startup. Refreshed every minute during proxy use, then backed off to five, ten, and fifteen minutes after eight idle minutes. Moved proxy-scoped limits to library metadata.
- **Proxy API and dashboard.** Removed manual proxy add/delete, per-source refresh/detail, and proxy export/import/clear controls. Kept read-only health, source diagnostics, status, and one global refresh action.
- **Proxy source contract.** Restricted candidates to unauthenticated HTTP proxies. Updated both Proxifly feeds to use ETag/Last-Modified conditional requests and handle `304 Not Modified` responses.

Tests: `make go-test`, `make go-vet`, and `make go-fmt-check` passed. `make check-frontend` completed with two existing accessibility warnings.

## Done (2026-09-28, v0.3.2)

- **Limit keys scoped to provider instance** — exhausted rate/quota/model marks and all read paths (credential filter, pool skip, proxy pick filter, virtual fan-out pre-checks) keyed by configured provider instance ID instead of the shared adapter type key; two instances of one type no longer shared account-less marks. Geo bans stayed keyed by adapter type (shared upstream region policy). Fixed the pool skip resolving the plugin namespace via lookup, the data-management purge clearing geobans under the resolved plugin ID, and the doctor flagging every geo ban as orphan.
- **Maintenance startup refresh** — server refreshed stale credentials synchronously before listening (30s cap, serves anyway on expiry) instead of racing the first tick behind the slow proxy rotation; refreshes ran through a bounded pool (4 workers) so many stale keys no longer serialize. Added regression tests for the gate, worker overlap, and the worker cap.
- **Smoke virtual-model collisions** — harness generated a unique virtual-model name per run and swept stale `smoke-vm*` rows at startup.
- **Doctor false orphans** — model-override inspection validated the stored provider value instead of splitting the key (keys join with `/`, the check split on `:`); plugin-storage inspection parsed the NUL-joined key via the owning `luaplugin` helper, with unparseable rows moved to an inspect-only corrupt category the fix button never deletes. Purge deleted overrides through the owning service instead of a dead `:`-prefix scan, imports remapped credentials and overrides to the created instance ID on collisions, and qualifier lookup slugified like ID creation.
- **Doctor missing backends** — inspection flagged enabled providers whose type had no installed plugin or built-in backend; fix disables them (manual re-enable clears), and maintenance stopped refreshing parked providers so disabled keys cost nothing and warn nothing — checked before backend resolution, so backend-less parked providers stay silent too.

Tests: `internal/services/maintenance/refresh_test.go`, `internal/services/luaplugin/exhausted_test.go`, `internal/services/router/exhausted_test.go`, `internal/services/doctor/service_test.go`, `internal/services/datamanagement/service_test.go`, `internal/services/luaplugin/storage_test.go`. `make go-test` green.

## Done (2026-09-27, v0.3.1)

- **Credential pool pre-skip** — pools skipped credentials with a live exhausted key before the attempt (unary and stream) and virtual fan-out applied the same filter to member calls.
- **Manual credential refresh** — added `POST /dashboard/credentials/{id}/refresh` with a dashboard button on the provider detail page.
- **Provider export/import/purge** — added `GET /dashboard/providers/{id}/export`, `POST /dashboard/providers/import`, `DELETE /dashboard/providers/{id}/purge`.
- **Data management and doctor** — added subsystem export/import/clear (`GET/POST /dashboard/data/...`, `GET /dashboard/data/stats`) and database doctor (`GET /dashboard/doctor/inspect`, `POST /dashboard/doctor/fix`); settings page gained security, data management and doctor sections with localized keys.
- **Admin password** — added `POST /dashboard/admin/password` for password change.
- **Smoke 0.3.0 codes** — smoke matrix classified `content_policy`, `model_unavailable` and `structural_fault` outcomes; mock plugin reissued.
- **UI kit consistency** — summarized header, table, button, empty-state, list, box, checkbox and container style updates in one pass.

Tests: `internal/pool/pool_test.go`, `internal/services/router/exhausted_test.go`, `providers/virtual/adapter_test.go`. `make go-test` green.

## Done (2026-09-27, v0.3.0)

- **Error contract rework** — removed `ErrorTypeTimeout` (merged into `upstream`); added `content_policy` (wire `400`), `model_unavailable` (wire `503`, fixed 2-minute model cooldown), `structural_fault` (wire `502`, stops the pool and disables the provider). Fixed `MapUpstream` order so code fallbacks run before the bare 400 default. Contract tables validated strictly at the boundary: unknown types, missing/past `retry_after` on rate/quota, and `scope`/`retry_after` where forbidden became `PluginInternalError`. Added `upstream_status`/`upstream_body` passthrough with 4KiB snippet cap and debug spill files in `upstream_dumps/`.
- **Geo bans** — added `geoban.Service` (bucket `geo_bans`): indefinite `(plugin, provider, proxy)` flags cleared on proxy delete or explicit admin clear. Proxy picks filter bans after ranking with other-region preference. Provider `config.geo` (`fail_fast` default, `retry_same_key` with `max_proxies` 1..10) drove same-key retries for unary pools; streams kept pool failover only.
- **Auto-disable** — `auth`/`payment_required` disabled the attempt credential and `structural_fault` disabled the provider instance (first cause won, `DisabledBy/Reason/At` on both models, manual re-enable cleared them). Direct-leg DNS/TLS/refused transport failures surfaced as `structural_fault`; timeouts, resets and proxy-leg failures stayed `upstream`.
- **Manifest floor 0.3.0** — older plugin contracts rejected at install; test fixtures and the smoke mock reissued.
- **Manual proxy without selection goes direct** — `manual` mode with empty `ids` no longer failed requests with `no usable proxy among 0 selected`; only an explicit selection resolving to nothing fails loudly.

Tests: `internal/errors/api_test.go`, `internal/models/geo_test.go`, `internal/services/geoban/service_test.go`, `internal/services/luaplugin/{contract_strict_test,service_test,exhausted_test}.go`, `internal/services/{credential/disable_test,provider/disable_test}.go`. `make go-test` green.

## Done (2026-09-26, v0.2.0)

- **DynamicForm rework** — normalized `flow`/`grid` `gap` and `spacer` `size` to Step int `0..8` (legacy `sm|md|lg` retained); `flow.justify` gained `around`/`evenly`; `input_type=secret` accepted; `link.url` restricted to `http`/`https`/`mailto` and relative paths; `provider_config` passed into `auth_initiate` and `auth_step` `ctx`. Router version rose to 0.2.0.
- **Exhausted model and proxy skip** — router skipped likely-exhausted models and joint proxy limits before selection; virtual fan-out applied the same filter.
- **Proxy log redaction** — pool and probe logs reported redacted hostport values.

Tests: `internal/services/luaplugin/{authctx_test,uinodes_test}.go`, `internal/services/router/exhausted_test.go`, `providers/virtual/adapter_test.go`. `make go-test` green.

## Done (2026-09-25, v0.1.2)

- **`payment_required` error type** — added `ErrorTypePaymentRequired` for upstream paywalls (status 402 mapped to status `402` + `payment_required`). Exhausted marking ignored it like `auth`; smoke harnesses treated it as a skip with reason, not a failure.

Tests: `internal/errors/api_test.go`, `internal/services/luaplugin/service_test.go`, `internal/services/luaplugin/classify_test.go`. `make go-test` green.

## Done (2026-09-25, v0.1.1)

- **Unified exhausted store** — added `internal/services/exhausted` (bucket `exhausted`): joint limit keys over plugin, provider type, account, model, proxy. Stored keys filtered candidates by subset match; expired entries deleted on read. Credential pools dropped matching combinations (full pool kept as last resort); proxy picks filtered after ranking.
- **Error `scope`** — added `ProviderError.Scope` (`account`/`model`/`proxy`, parsed from the contract table, unknown words failed closed). Empty scope on rate/quota marked the full combination. Marking happened once, in the exec defer, for rate/quota outcomes only.
- **`classify_error`** — added the handler slot plus `llm_router.classify_error` helper (default from status plus envelope code/type, quota wording, retry-after header/body hints; extension received `(raw, default)` and returned the final table or nil; pure, no recursion).
- **Removed old limit state** — deleted the credential `quota_reset_at` path and the `proxy_limits` bucket, with named startup migrations (`migrateDropProxyLimits`, `migrateClearCredentialQuota`). `geo` no longer persisted anything.
- **Streamlined pipeline** — threaded `HandlerMeta` through pool/handlers/exec; added `request.model_name` to every request table and `encoding_format` to embed; `client:stream` returned `(resp, err)` with an `on_response` hook; renamed the constructor to `llm_router.http_client`; `invalid_request` stopped pool failover; manifest floor rose to 0.1.1 (older contracts rejected at install); registry rebuild failed closed on duplicate type keys; rollback snapshots covered proxy source keys.

Tests: `internal/services/exhausted/store_test.go`,
`internal/services/luaplugin/{classify_test,exhausted_test,stream_api_test,meta_test}.go`,
`internal/services/router/exhausted_test.go`,
`internal/server/{migrate_test,proxy_wiring_test}.go`. `make go-test` green.

## Done (2026-09-24, v0.0.7)

- **Virtual metadata serve-time** — `GET /v1/models` and `GET /v1/models/{id}` folded virtual metadata from member views on every call (`virtual.FoldMembers` in `internal/services/virtual/fold.go`): capabilities/supported_parameters/modalities/reasoning intersected, context/max-tokens minimized. Unknown members skipped with a WARN log; a virtual model with no available members stayed hidden (list) + WARN / 404ed (single) + WARN. Disabled virtual models hid from both endpoints. Removed stored aggregates (`Capabilities/ContextLength/MaxCompletionTokens` on `models.VirtualModel`, `computeAggregates`, List backfill): old bbolt rows read fine, fields were ignored and dropped on next write.
- **Live managed members** — `virtual.Service.LiveMembers`: managed (`provider:<id>:<slug>`) virtual models resolved their endpoint group live from the provider list on every call; the stored member list served manual models only. The adapter snapshotted members once per request and walked the same fall-through queue.
- **Managed read-only** — `PUT /dashboard/virtual-models/{id}` rejected any change to a managed virtual model except the disabled toggle (`managedUpdateIsToggleOnly`); the marker was preserved server-side.
- **Upstream model-gone** — new plugin error type `not_found` (PLUGIN-API.md, `models.ErrorTypeNotFound`, generic adapter mapped HTTP 404). The router dropped the model from the info cache (`modelinfo.RemoveModel`, best-effort, WARN log) on all routed paths and returned the original error.
- **Rename agents → virtual-models** — dashboard routes became `/dashboard/virtual-models[/{id}]` (`virtual_models.go`, `apiVirtualModels*`); legacy `/dashboard/agents*` stayed as deprecated aliases on the same handlers until the frontend migrated. `POST /dashboard/providers/{id}/virtual-models/sync` ensured one managed record per non-empty endpoint group (idempotent, cache-only).
- **models/available parity** — `availableModelView` carried `description/input_modalities/output_modalities/reasoning/supported_parameters` from the already-fetched views (zero extra cost); virtual entries removed server-side (cycle safety). `agents/available-models` removed.
- **Sync robustness** — one bad group no longer aborted the whole sync (per-group WARN + continue); an unmanaged record squatting an auto-generated name was adopted instead of colliding (`virtual.Service` logger via `SetLogger`).
- **Per-provider token credential policy** — `TokenRules.AllowsCredential(providerID, ...)` evaluated provider-scoped rules in `router.Service.filterCredentials` alongside the globals.

Tests: `internal/services/virtual/fold_test.go`,
`internal/services/modelinfo/remove_model_test.go`,
`internal/dashboard/managed_guard_test.go`,
`internal/services/virtual/live_members_test.go`;
`TestAvailableModelsIncludesVirtualWithoutCredentials` asserted absence.
`make go-test` green.

## Done (2026-09-19, v0.0.7)

- **Proxy pool replaced** — two-stage probe (CONNECT tunnel ping, 1MB download speed), one live state per proxy-provider pair (rate limit, block), demand-driven fetch. Buckets `proxies_v2` + `proxy_limits` + `active_regions`; old buckets unread. Dead proxies deleted; slow ones (under `min_download_speed_kbps = 15000`) kept as fallback until their location filled, then displaced one-for-one by faster newcomers; rotation trimmed locations to the fastest `max_proxies_per_location`. Exit locations verified through the proxy on add. Demand accumulated request whitelists over defaults `US/DE/NL/GB/FR/CA` (observed entries expired after 48h); scheduled fetch paused while every demanded region held N fast proxies. Source fetch covered a rotating 1500-window in 150-chunks while shortfall persisted (zero probes on a full pool); manual refresh short-circuited on a full pool; 64 probe workers. Settings lived in `RouterConfiguration` with defaults `15000/10/15`, no UI editing yet.
- **Pair outcomes** — handler `rate_limit`/`quota_exceeded` limited the pair until `retry_after` (`+60s` default); `geo` blocked the pair with the message as reason. Blocks never expired; pairs died with the proxy. Handler geo-rotation removed; retries belonged to the credential retry engine, transport failover stayed inside one call.
- **Manifest** — repeatable `@proxy_location` whitelist (empty = any), new `@proxy_default_option disabled|auto|manual` (absent = disabled, applied in `EnsureSeeded`); `@proxy_force_on_mismatch` removed (tolerated on install); server `geoip` service and `RouterConfiguration.server_country` removed. Router version 0.0.7.

## Done (2026-09-12, v0.0.4)

- **Singleton plugin providers** — `provider.Service.Create` reused the seeded row (`id == type_key`) for unqualified plugin types instead of creating `type-2` duplicates. Regression test in `provider/service_test.go` (`TestCreateReusesSingletonRow`).
- **Orphaned plugin providers hidden** — dashboard provider list and management endpoints dropped rows whose type key had no installed plugin (`Handler.providerTypeKnown`; nil luaSvc = subsystem absent, keep visible). Data stayed in DB; reinstalling the plugin brought the row back. Test: `TestProvidersListHidesOrphanedPluginRows`.
- **Plugin error contract: `geo` type** — `models.ErrorTypeGeo`; `asProviderError` mapped `{type="geo"}` (400). Plugins returned it for location blocks (google.lua v1.2.0). When a handler failed geo through a proxy, `exec` reported proxy failure for that provider (the HTTP layer counted 400 as dial success) → `Select`/`SelectManual` skipped it next time. /v1 wire code: `geo_blocked` (400).
- **Proxy resolver hard-fails** — `SetProxyResolver` signature returned an error; handler aborted before Lua ran when manual mode had no usable selected proxy, or a `@proxy_force_on_mismatch` plugin needed a proxy but the pool was empty. No more silent direct fallback in those modes.
- **Provider `disabled` flag** — `ProviderInstance.Disabled`; PUT accepted `disabled` (also on readonly rows, alongside operational config keys `proxy`/`models_auto_sync`/`disable_failed_models`). Disabled providers skipped in `/v1/models`, dashboard available-models, and router `Complete`/`CompleteStream` (`ErrProviderDisabled` → 400 `provider_disabled`); settings/discovery kept working.
- **Model auto-sync** — `config.models_auto_sync` warmed the modelinfo cache via `MergedView` (TTL-throttled) on every maintenance tick; disabled providers skipped.
- **Available-models credential gate removed** — the dashboard models list no longer required routable credentials, so keyless providers (opencode-zen) showed up.
- **Multimodal message parts** — `ChatMessageContentPart` carried OpenAI `image_url` (`{url, detail}`) and `input_audio` (`{data, format}`) through unmarshal/marshal round-trips; text flattening ignored binary parts, so token counting and text views stayed unchanged.
- **Proxy egress overhaul** — one request resolved its route fresh and rotated through untried pooled proxies while attempts failed; proxy exhaustion surfaced the last error, never a silent direct fallback (the transport-build-failure direct fallback was removed). `force` locations resolved strictly (`SelectStrict`): a wrong-country exit no longer satisfied a region demand. Known-bad per-provider health demoted (`-1000` score) instead of excluding, in both `Select` and `SelectManual`. `Check` verified the real exit country through the proxy (`DetectExitCountry`, same ip-api technique as geoip) instead of trusting list metadata. Country codes normalized to alpha-2 everywhere (`NormalizeCountryCode`: manual adds, list syncs, manifest `@proxy_location`, geoip override, resolve-time). `ParseProxyMode` and `ResolveProxy` moved to `proxypool` with full matrix tests.
- **Lua `client:request` transport-error order fixed** — failure returned `(nil, errtable)` per the `(resp, err)` contract. Previously the error table arrived in the `resp` slot with `err == nil`, so plugins read a missing `status` off the error table instead of failing.
- **Provider stats never touch the network** — `/dashboard/providers/stats` read the model cache via `PeekModelInfos` (no fetch on miss) and dropped the per-provider day-long metrics scan with `RequestsToday`. Auto refresh stayed opt-in via `models_auto_sync` (maintenance loop); everything else synced on explicit user action. Opening the providers tab no longer pinged upstreams.
- **Proxy pool: sticky exits, soft force, parallel checks, geo rotation** — `Select` ranked the freshest known-good proxy for the provider first (+500), so repeat traffic kept one working exit instead of re-walking the pool. Forced exits (`@proxy_force_on_mismatch`) became a preference, not a gate: matching countries led, others fell through, and only an empty pool errored. `SelectStrict` removed. `CheckAll` ran 16-wide (per-proxy egress meant the geo endpoint's per-IP limit applied per proxy). Region-locked answers rotated silently through untried pooled proxies inside `Complete`/`CompleteStream` (stream only before the first chunk); pool exhaustion returned one synthesized geo error ("region-locked upstream: all N pooled proxies were rejected"), and dial-level exhaustion wrapped `proxypool.ErrProxyPoolExhausted`. `POST /dashboard/proxies/check-all` stayed synchronous and died with the request context, so a client abort cancelled the remaining checks.
- **Model cache persisted, startup warm, browse never fetches** — the model metadata cache lived in bbolt (`model_infos` bucket) and survived restarts; `PeekModelInfos` hydrated memory from it. `WarmMissing` ran in the background at startup and created caches for providers that had none, so first clicks never waited on upstream discovery. The available-models endpoints (`/dashboard/models/available`, agents variant) merged from the cache only (`PeekMergedView`): browsing the models tab no longer pinged upstreams. Refresh stayed opt-in (`models_auto_sync` maintenance) or manual (provider page import).
- **Audio transcription endpoint + plugin contract doc** — `POST /v1/audio/transcriptions` (multipart, 32 MB cap) routed through the same resolve → credential-pool → single backend pass as chat. Lua plugins declared an optional `transcribe` handler; Go adapters implemented the optional `provider.Transcriber` interface; a provider without either failed loudly with `endpoint_not_supported`. The plugin returned the normalized OpenAI `verbose_json` shape and the router rendered the client's `response_format` from it, including SRT/VTT from segments. `llm_router.multipart(parts)` built multipart bodies for plugins. `ModelInfo.endpoints` declared which endpoints a model served (empty = chat only); the router gated chat and transcription calls against it. Router version bumped to 0.0.5; PLUGIN-API.md became the binding plugin-author contract.

## Done (2026-09-11, v0.0.4)

### Base

- **Rename/update credential** — `PUT /api/llm-router/dashboard/credentials/{id}` (`label`, `disabled`, `data` with revalidation). `Credential.Disabled`, `Credential.Order` added to `internal/models`.
- **Enable/disable credential** — `Credential.Disabled`; `credential.Service.All` (the routing pool) excluded disabled and expired.
- **Manual credential ordering** — `Credential.Order` (1-based, 0 = unordered) + `PUT /api/llm-router/dashboard/credentials/reorder`. Pool sort: manual order, then computed priority, then LRU (`credential.SortPool`).
- **Test credential** — `POST /api/llm-router/dashboard/credentials/{id}/test` (`router.Service.TestCredential`, pinned to the credential, no rotation).
- **Model overrides** — bucket `model_overrides`, `models.ModelOverride`, `modelinfo.Service.MergedView/SetOverride/DeleteOverride/IsModelEnabled`. Disabled models disappeared from `GET /v1/models` and the router refused them (`ErrModelDisabled` → 404 `model_not_found`). Custom models appended by `MergedView`. Endpoints: GET/PUT/DELETE `.../providers/{id}/models[/{model...}]`.
- **Model capabilities** — `ModelInfo.Capabilities`; groq.lua v1.0.1 reported `supported_features` + vision/audio from modalities; `models/available` passed them through.
- **Force-refresh model list** — `POST .../providers/{id}/models/refresh` (used existing `modelinfo.InvalidateProvider`).
- **Test model** — `POST /api/llm-router/dashboard/models/test` (`router.Service.TestModel` through the normal routing path).

Tests: `internal/services/credential/ordering_test.go`,
`internal/services/modelinfo/overrides_test.go`. `make go-test` green.

### Second pass

- **Capabilities probe** — `POST /api/llm-router/dashboard/models/capabilities` (`router.Service.ProbeCapabilities`: live probes for `tools` and `json_mode`). UI stored the detected list as the model's override capabilities.
- **Maintenance sentinel errors** — `provider.ErrNotRefreshable` for Go adapters; maintenance matched `luaplugin.ErrHandlerNotFound` / `provider.ErrNotFound` via `errors.Is` instead of message substrings (killed the per-minute WARN spam for plugins without refresh handlers). `provider.Service.Get` returned `ErrNotFound`-wrapped errors.
- **API fallback is JSON 404** — the SPA fallback no longer dev-redirected unknown `/api/llm-router/*` paths (they looped through the Vite proxy).

### OpenAI-compat pass

- **Canonical `/v1/models`** — stable `created` (provider creation time, was `time.Now()` per request), plus extended fields: `display_name`, `context_window`, `max_completion_tokens`, `capabilities`. Same shape in `GET /v1/models/{model}` (powered by `MergedView`).
- **Usage details** — `prompt_tokens_details.cached_tokens` and `completion_tokens_details.reasoning_tokens` in `ChatCompletionUsage` (omitted when absent, per OpenAI spec).
- **Reasoning content** — `ChatMessage.ReasoningContent` (`reasoning_content`, DeepSeek-style; Groq's `reasoning` alias normalized on unmarshal). Worked in responses and stream deltas.
- **Stream usage chunk** — `emit` contract accepted chunks with empty choices when usage was present; `stream_options.include_usage` reached clients. groq.lua v1.0.2 forwarded it.
- **Empty-table contract fix** — Lua `{}` encoded as JSON object; wire arrays (`choices`, `tool_calls`) normalized recursively in the luaplugin boundary before schema validation.

Tests: `internal/models/wire_test.go`,
`internal/services/luaplugin/stream_test.go`. Harness scenarios
`streamusage`, `reasoning` passed live against Groq.

- **Canonical stream chunks** — `emit` wrote the re-marshaled `StreamChunk` struct, not raw plugin bytes: aliases normalized (`reasoning` → `reasoning_content`) and the stream shape matched non-stream responses.
- **Capability probes headroom** — probe requests used `max_tokens: 512` (was 16): reasoning models spent the whole budget on thinking and the forced tool call never arrived (`tool_use_failed`).
- **Dev start builds a real binary** — `scripts/start` stopped using `go run`: the pidfile tracked the wrapper while the compiled child could be orphaned holding the ports. The pidfile tracked the actual server after the change.

Live reference comparison `test_shit/compare_groq.py` (Groq direct vs router): 22 checks across /models, retrieve, basic, tools, JSON mode, streaming, errors.

### Proxy subsystem

- **Proxy pool** (`internal/services/proxypool`) — bucket `proxies`; deterministic IDs (sha256 of URL); manual + list-sourced entries; `Check` with immediate culling of dead list-sourced proxies; per-provider health memory (`RecordOutcome`/`Select` excluded only for the failing provider); preference-ordered `Select` (location match > provider-known-good > latency).
- **Protocols** — http/https/socks4(4a)/socks5 with optional auth; `proxypool.TransportFor`; socks4 implemented in-house, socks5 via `golang.org/x/net/proxy` (new dep).
- **Geo-IP** (`internal/services/geoip`) — ip-api.com, 1h cache; manual override `RouterConfiguration.server_country` (PUT config).
- **Plugin proxying** — execContext carried proxyID/proxyURL; plugin HTTP routed through the pool proxy; target allow-list still enforced, redirects validated; outcome feedback per provider. Manifest tags: `@proxy_location CC`, `@proxy_force_on_mismatch true`, `@proxy_source true`.
- **Proxy source plugins** — `llm_router.register_proxy_source(key, {fetch_proxies})`; `test_shit/proxifly.lua` v1.0.0 (2307 live proxies fetched in harness). Dashboard: GET/POST/DELETE `/dashboard/proxies`, `.../{id}/check`, `.../check-all`, GET `/dashboard/proxy-sources`, `.../{key}/refresh`, GET `/dashboard/proxy/status`.
- **Provider proxy mode** — `ProviderInstance.Config.proxy`: `disabled` (default; plugin force still applied on location mismatch), `auto` (pool select by plugin preference), `manual` (explicit proxy IDs). Resolution wired in server.go.
- **Maintenance** — hourly: refreshed all sources + `CheckAll`.

Tests: `internal/services/proxypool/service_test.go` (validation, sync idempotency, selection/health, live CONNECT proxy, socks4 handshake). Harness scenario `proxies` passed live.
