# Changelog

## Done (2026-09-25, v0.1.2)

- **`payment_required` error type** — added `ErrorTypePaymentRequired` for upstream paywalls (status 402 mapped to wire `402` + `payment_required`). Exhausted marking ignored it like `auth`; smoke harnesses treated it as a skip with reason, not a failure.

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
