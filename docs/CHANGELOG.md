# Backend changes required by the provider detail page

## Done (0.1.2 plugin contract)

1. **`payment_required` error type** — `ErrorTypePaymentRequired` for
   upstream paywalls (status 402 → `402` + `payment_required` on the wire).
   Exhausted marking ignores it like `auth`; smoke harnesses treat it as a
   skip with reason, not a failure.

## Done (0.1.1 plugin contract)

1. **Unified exhausted store** — `internal/services/exhausted` (bucket
   `exhausted`): joint limit keys over plugin, provider type, account,
   model, proxy. Stored keys act as filters (subset match); expired entries
   delete on read. Credential pools drop matching combinations (full pool
   kept as last resort); proxy picks filter after ranking.
2. **Error `scope`** — `ProviderError.Scope` (`account`/`model`/`proxy`,
   parsed from the contract table, unknown words fail closed). Empty scope
   on rate/quota marks the full combination. Marking happens once, in the
   exec defer, for rate/quota outcomes only.
3. **`classify_error`** — handler slot plus `llm_router.classify_error`
   helper (default from status plus envelope code/type, quota wording,
   retry-after header/body hints; extension gets `(raw, default)` and
   returns the final table or nil; pure, no recursion).
4. **Removed old limit state** — credential `quota_reset_at` path and the
   `proxy_limits` bucket are gone, with named startup migrations
   (`migrateDropProxyLimits`, `migrateClearCredentialQuota`). `geo` no
   longer persists anything.
5. **Streamlined pipeline** — `HandlerMeta` through pool/handlers/exec;
   `request.model_name` everywhere, `embed.encoding_format`;
   `client:stream` returns `(resp, err)` with an `on_response` hook;
   `llm_router.http_client` (renamed); `invalid_request` stops pool
   failover; manifest floor 0.1.1 (older contracts rejected at install);
   registry rebuild fails closed on duplicate type keys; rollback
   snapshots cover proxy source keys.

Tests: `internal/services/exhausted/store_test.go`,
`internal/services/luaplugin/{classify_test,exhausted_test,stream_api_test,meta_test}.go`,
`internal/services/router/exhausted_test.go`,
`internal/server/{migrate_test,proxy_wiring_test}.go`. `make go-test` green.

## Done (2026-09-11)

1. **Rename/update credential** — `PUT /api/llm-router/dashboard/credentials/{id}`
   (`label`, `disabled`, `data` with revalidation). `Credential.Disabled`,
   `Credential.Order` added to `internal/models`.
2. **Enable/disable credential** — `Credential.Disabled`; `credential.Service.All`
   (the routing pool) excludes disabled and expired.
3. **Manual credential ordering** — `Credential.Order` (1-based, 0 = unordered) +
   `PUT /api/llm-router/dashboard/credentials/reorder`. Pool sort: manual order,
   then computed priority, then LRU (`credential.SortPool`).
4. **Test credential** — `POST /api/llm-router/dashboard/credentials/{id}/test`
   (`router.Service.TestCredential`, pinned to the credential, no rotation).
5. **Model overrides** — bucket `model_overrides`, `models.ModelOverride`,
   `modelinfo.Service.MergedView/SetOverride/DeleteOverride/IsModelEnabled`.
   Disabled models disappear from `GET /v1/models` and are refused by the router
   (`ErrModelDisabled` → 404 `model_not_found`). Custom models are appended by
   `MergedView`. Endpoints: GET/PUT/DELETE `.../providers/{id}/models[/{model...}]`.
6. **Model capabilities** — `ModelInfo.Capabilities`; groq.lua v1.0.1 reports
   `supported_features` + vision/audio from modalities; `models/available` passes
   them through.
7. **Force-refresh model list** — `POST .../providers/{id}/models/refresh`
   (uses existing `modelinfo.InvalidateProvider`).
8. **Test model** — `POST /api/llm-router/dashboard/models/test`
   (`router.Service.TestModel` through the normal routing path).

Tests: `internal/services/credential/ordering_test.go`,
`internal/services/modelinfo/overrides_test.go`. `make go-test` green.

## Done (2026-09-11, second pass)

9. **Capabilities probe** — `POST /api/llm-router/dashboard/models/capabilities`
   (`router.Service.ProbeCapabilities`: live probes for `tools` and `json_mode`).
   UI stores the detected list as the model's override capabilities.
10. **Maintenance sentinel errors** — `provider.ErrNotRefreshable` for Go
    adapters; maintenance matches `luaplugin.ErrHandlerNotFound` /
    `provider.ErrNotFound` via `errors.Is` instead of message substrings
    (kills the per-minute WARN spam for plugins without refresh handlers).
    `provider.Service.Get` now returns `ErrNotFound`-wrapped errors.
11. **API fallback is JSON 404** — the SPA fallback no longer dev-redirects
    unknown `/api/llm-router/*` paths (they looped through the Vite proxy).

## Done (2026-09-11, OpenAI-compat pass)

12. **Canonical `/v1/models`** — stable `created` (provider creation time, was
    `time.Now()` per request), plus extended fields: `display_name`,
    `context_window`, `max_completion_tokens`, `capabilities`. Same shape in
    `GET /v1/models/{model}` (now powered by `MergedView`).
13. **Usage details** — `prompt_tokens_details.cached_tokens` and
    `completion_tokens_details.reasoning_tokens` in `ChatCompletionUsage`
    (omitted when absent, per OpenAI spec).
14. **Reasoning content** — `ChatMessage.ReasoningContent`
    (`reasoning_content`, DeepSeek-style; Groq's `reasoning` alias normalized
    on unmarshal). Works in responses and stream deltas.
15. **Stream usage chunk** — `emit` contract now accepts chunks with empty
    choices when usage is present; `stream_options.include_usage` reaches
    clients. groq.lua v1.0.2 forwards it.
16. **Empty-table contract fix** — Lua `{}` encodes as JSON object; wire
    arrays (`choices`, `tool_calls`) are normalized recursively in the
    luaplugin boundary before schema validation.

Tests: `internal/models/wire_test.go`,
`internal/services/luaplugin/stream_test.go`. Harness scenarios
`streamusage`, `reasoning` pass live against Groq.

17. **Canonical stream chunks** — `emit` now writes the re-marshaled
    `StreamChunk` struct, not raw plugin bytes: aliases normalize
    (`reasoning` → `reasoning_content`) and the stream shape matches
    non-stream responses.
18. **Capability probes headroom** — probe requests use `max_tokens: 512`
    (was 16): reasoning models spent the whole budget on thinking and the
    forced tool call never arrived (`tool_use_failed`).
19. **Dev start builds a real binary** — `scripts/start` no longer uses
    `go run`: the pidfile tracked the wrapper while the compiled child could
    be orphaned holding the ports. Now the pidfile tracks the actual server.

Live reference comparison `test_shit/compare_groq.py` (Groq direct vs
router): 22 checks across /models, retrieve, basic, tools, JSON mode,
streaming, errors.

## Open

- Token rules: per-provider credential policy. The token wizard works
  provider by provider: each provider carries either "use all credentials
  (existing and future)" or an explicit credential id list, plus a virtual
  pseudo-provider (`id "virtual"`, no credentials). The current flat
  `TokenRules` cannot express per-provider "all": the frontend snapshots
  current ids into `allowed_credentials` (see
  `buildBackendTokenRules` in `web/src/lib/token-rules.ts`). Desired
  native shape (`buildDesiredTokenRules` in the same file):
  `{ full_access: bool, providers: [{ provider_id, credentials: "all" | [ids] }], models: "all" | [ids] }`.
  Backend work: accept per-provider credential policies (exact field
  layout negotiable, semantics above are binding), evaluate them in
  `router.Service.filterCredentials` alongside the existing globals, and
  treat provider id `"virtual"` in `allowed_providers` as the virtual
  models scope. Until then the snapshot adaptation stays.
- Google plugin (paused): when resumed, its manifest gets
  `@proxy_location US` + `@proxy_force_on_mismatch true` (region lock),
  and the harness should probe it through a pool proxy.

## Done (2026-09-11, proxy subsystem)

20. **Proxy pool** (`internal/services/proxypool`) — bucket `proxies`;
    deterministic IDs (sha256 of URL); manual + list-sourced entries;
    `Check` with immediate culling of dead list-sourced proxies;
    per-provider health memory (`RecordOutcome`/`Select` excludes only
    for the failing provider); preference-ordered `Select`
    (location match > provider-known-good > latency).
21. **Protocols** — http/https/socks4(4a)/socks5 with optional auth;
    `proxypool.TransportFor`; socks4 implemented in-house, socks5 via
    `golang.org/x/net/proxy` (new dep).
22. **Geo-IP** (`internal/services/geoip`) — ip-api.com, 1h cache;
    manual override `RouterConfiguration.server_country` (PUT config).
23. **Plugin proxying** — execContext carries proxyID/proxyURL; plugin
    HTTP routes through the pool proxy; target allow-list still enforced,
    redirects validated; outcome feedback per provider.
    Manifest tags: `@proxy_location CC`, `@proxy_force_on_mismatch true`,
    `@proxy_source true`.
24. **Proxy source plugins** — `llm_router.register_proxy_source(key,
    {fetch_proxies})`; `test_shit/proxifly.lua` v1.0.0 (2307 live proxies
    fetched in harness). Dashboard: GET/POST/DELETE `/dashboard/proxies`,
    `.../{id}/check`, `.../check-all`, GET `/dashboard/proxy-sources`,
    `.../{key}/refresh`, GET `/dashboard/proxy/status`.
25. **Provider proxy mode** — `ProviderInstance.Config.proxy`:
    `disabled` (default; plugin force still applies on location
    mismatch), `auto` (pool select by plugin preference),
    `manual` (explicit proxy IDs). Resolution wired in server.go.
26. **Maintenance** — hourly: refresh all sources + `CheckAll`.

Tests: `internal/services/proxypool/service_test.go` (validation, sync
idempotency, selection/health, live CONNECT proxy, socks4 handshake).
Harness scenario `proxies` passes live.


- Model test batch endpoint ("Test all" currently iterates client-side,
  sequentially; fine at this scale, revisit if providers list hundreds of models).
- Credential stats display could surface `request_count`/`success_count`/
  `quota_reset_at` — fields are already in the credential list response.

## Done (2026-09-12, provider lifecycle + proxy hardening)

27. **Singleton plugin providers** — `provider.Service.Create` reuses the
    seeded row (`id == type_key`) for unqualified plugin types instead of
    creating `type-2` duplicates. Regression test in
    `provider/service_test.go` (`TestCreateReusesSingletonRow`).
28. **Orphaned plugin providers hidden** — dashboard provider list and
    management endpoints drop rows whose type key has no installed plugin
    (`Handler.providerTypeKnown`; nil luaSvc = subsystem absent, keep
    visible). Data stays in DB; reinstalling the plugin brings the row back.
    Test: `TestProvidersListHidesOrphanedPluginRows`.
29. **Plugin error contract: `geo` type** — `models.ErrorTypeGeo`;
    `asProviderError` maps `{type="geo"}` (400). Plugins return it for
    location blocks (google.lua v1.2.0). When a handler fails geo through a
    proxy, `exec` reports proxy failure for that provider (the HTTP layer
    counts 400 as dial success) → `Select`/`SelectManual` skip it next time.
    /v1 wire code: `geo_blocked` (400).
30. **Proxy resolver hard-fails** — `SetProxyResolver` signature now returns
    an error; handler aborts before Lua runs when manual mode has no usable
    selected proxy, or a `@proxy_force_on_mismatch` plugin needs a proxy but
    the pool is empty. No more silent direct fallback in those modes.
31. **Provider `disabled` flag** — `ProviderInstance.Disabled`; PUT accepts
    `disabled` (also on readonly rows, alongside operational config keys
    `proxy`/`models_auto_sync`/`disable_failed_models`). Disabled providers
    are skipped in `/v1/models`, dashboard available-models, and router
    `Complete`/`CompleteStream` (`ErrProviderDisabled` → 400
    `provider_disabled`); settings/discovery keep working.
32. **Model auto-sync** — `config.models_auto_sync` warms the modelinfo cache
    via `MergedView` (TTL-throttled) on every maintenance tick; disabled
    providers skipped.
33. **Available-models credential gate removed** — the dashboard models list
    no longer requires routable credentials, so keyless providers
    (opencode-zen) show up.
34. **Multimodal message parts** — `ChatMessageContentPart` carries OpenAI
    `image_url` (`{url, detail}`) and `input_audio` (`{data, format}`)
    through unmarshal/marshal round-trips; text flattening ignores binary
    parts, so token counting and text views are unchanged.
35. **Proxy egress overhaul** — one request now resolves its route fresh
    and rotates through untried pooled proxies while attempts fail; proxy
    exhaustion surfaces the last error, never a silent direct fallback
    (the transport-build-failure direct fallback is removed). `force`
    locations resolve strictly (`SelectStrict`): a wrong-country exit no
    longer satisfies a region demand. Known-bad per-provider health demotes
    (`-1000` score) instead of excluding, in both `Select` and
    `SelectManual`. `Check` verifies the real exit country through the
    proxy (`DetectExitCountry`, same ip-api technique as geoip) instead of
    trusting list metadata. Country codes normalize to alpha-2 everywhere
    (`NormalizeCountryCode`: manual adds, list syncs, manifest
    `@proxy_location`, geoip override, resolve-time). `ParseProxyMode` and
    `ResolveProxy` moved to `proxypool` with full matrix tests.
36. **Lua `client:request` transport-error order fixed** — failure returns
    `(nil, errtable)` per the `(resp, err)` contract. Previously the error
    table arrived in the `resp` slot with `err == nil`, so plugins read a
    missing `status` off the error table instead of failing.
37. **Provider stats never touch the network** — `/dashboard/providers/stats`
    reads the model cache via `PeekModelInfos` (no fetch on miss) and drops
    the per-provider day-long metrics scan with `RequestsToday`. Auto refresh
    stays opt-in via `models_auto_sync` (maintenance loop); everything else
    syncs on explicit user action. Opening the providers tab no longer pings
    upstreams.
38. **Proxy pool: sticky exits, soft force, parallel checks, geo rotation** —
    `Select` ranks the freshest known-good proxy for the provider first
    (+500), so repeat traffic keeps one working exit instead of re-walking
    the pool. Forced exits (`@proxy_force_on_mismatch`) became a preference,
    not a gate: matching countries lead, others fall through, and only an
    empty pool errors. `SelectStrict` is gone. `CheckAll` runs 16-wide
    (per-proxy egress means the geo endpoint's per-IP limit applies per
    proxy). Region-locked answers now rotate silently through untried pooled
    proxies inside `Complete`/`CompleteStream` (stream only before the first
    chunk); pool exhaustion returns one synthesized geo error
    ("region-locked upstream: all N pooled proxies were rejected"), and
    dial-level exhaustion wraps `proxypool.ErrProxyPoolExhausted`.
    `POST /dashboard/proxies/check-all` is synchronous and dies with the
    request context, so a client abort cancels the remaining checks.
39. **Model cache persisted, startup warm, browse never fetches** — the model
    metadata cache lives in bbolt (`model_infos` bucket) and survives
    restarts; `PeekModelInfos` hydrates memory from it. `WarmMissing` runs in
    the background at startup and creates caches for providers that have
    none, so first clicks never wait on upstream discovery. The
    available-models endpoints (`/dashboard/models/available`, agents
    variant) now merge from the cache only (`PeekMergedView`): browsing the
    models tab no longer pings upstreams. Refresh stays opt-in
    (`models_auto_sync` maintenance) or manual (provider page import).
40. **Audio transcription endpoint + plugin contract doc** —
    `POST /v1/audio/transcriptions` (multipart, 32 MB cap) routes through the
    same resolve → credential-pool → single backend pass as chat. Lua plugins
    declare an optional `transcribe` handler; Go adapters implement the
    optional `provider.Transcriber` interface; a provider without either
    fails loudly with `endpoint_not_supported`. The plugin returns the
    normalized OpenAI `verbose_json` shape and the router renders the
    client's `response_format` from it, including SRT/VTT from segments.
    `llm_router.multipart(parts)` builds multipart bodies for plugins.
    `ModelInfo.endpoints` declares which endpoints a model serves (empty =
    chat only); the router gates chat and transcription calls against it.
    Router version bumped to 0.0.5; PLUGIN-API.md is now the binding
    plugin-author contract.

## Done (2026-09-19, proxy pool rework)

41. **Proxy pool replaced** — two-stage probe (CONNECT tunnel ping, 1MB
    download speed), one live state per proxy-provider pair (rate limit,
    block), demand-driven fetch. Buckets `proxies_v2` + `proxy_limits` +
    `active_regions`; old buckets unread. Dead proxies deleted; slow ones
    (under `min_download_speed_kbps = 15000`) kept as fallback until their
    location fills, then displaced one-for-one by faster newcomers;
    rotation trims locations to the fastest `max_proxies_per_location`.
    Exit locations verified through the proxy on add. Demand accumulates
    request whitelists over defaults `US/DE/NL/GB/FR/CA` (observed entries
    expire after 48h); scheduled fetch pauses while every demanded region
    holds N fast proxies. Source fetch covers a rotating 1500-window in
    150-chunks while shortfall persists (zero probes on a full pool);
    manual refresh short-circuits on a full pool; 64 probe workers.
    Settings live in `RouterConfiguration` with defaults
    `15000/10/15`, no UI editing yet.
42. **Pair outcomes** — handler `rate_limit`/`quota_exceeded` limits the
    pair until `retry_after` (`+60s` default); `geo` blocks the pair with
    the message as reason. Blocks never expire; pairs die with the proxy.
    Handler geo-rotation removed; retries belong to the credential retry
    engine, transport failover stays inside one call.
43. **Manifest** — repeatable `@proxy_location` whitelist (empty = any),
    new `@proxy_default_option disabled|auto|manual` (absent = disabled,
    applied in `EnsureSeeded`); `@proxy_force_on_mismatch` removed
    (tolerated on install); server `geoip` service and
    `RouterConfiguration.server_country` removed. Router version 0.0.7.

## Notes

- Model lists are NOT hardcoded in Lua plugins (except the documented fallback
  in `opencode-zen.lua`): plugins fetch live from upstream `/models`. Overrides
  live in the router core (bucket `model_overrides`), not in plugins.

## Done (2026-09-24, virtual models serve-time)

44. **Virtual metadata serve-time** — `GET /v1/models` and
    `GET /v1/models/{id}` fold virtual metadata from member views on every
    call (`virtual.FoldMembers` in `internal/services/virtual/fold.go`):
    capabilities/supported_parameters/modalities/reasoning = intersection,
    context/max-tokens = minima. Unknown members are skipped with a WARN log;
    a virtual model with no available members stays hidden (list) + WARN / 404s
    (single) + WARN. Disabled virtual models are hidden from both endpoints.
    Stored aggregates (`Capabilities/ContextLength/MaxCompletionTokens` on
    `models.VirtualModel`, `computeAggregates`, List backfill) removed: old
    bbolt rows read fine, fields are ignored and drop on next write.
45. **Live managed members** — `virtual.Service.LiveMembers`: managed
    (`provider:<id>:<slug>`) virtual models resolve their endpoint group live
    from the provider list on every call; the stored member list is used for
    manual models only. The adapter snapshots members once per request and
    walks the same fall-through queue.
46. **Managed read-only** — `PUT /dashboard/virtual-models/{id}` rejects any
    change to a managed virtual model except the disabled toggle
    (`managedUpdateIsToggleOnly`); the marker is preserved server-side.
47. **Upstream model-gone** — new plugin error type `not_found`
    (PLUGIN-API.md, `models.ErrorTypeNotFound`, generic adapter maps
    HTTP 404). The router drops the model from the info cache
    (`modelinfo.RemoveModel`, best-effort, WARN log) on all routed paths and
    returns the original error.
48. **Rename agents → virtual-models** — dashboard routes
    `/dashboard/virtual-models[/{id}]` (`virtual_models.go`,
    `apiVirtualModels*`); legacy `/dashboard/agents*` kept as deprecated
    aliases on the same handlers until the frontend migrates.
    `POST /dashboard/providers/{id}/virtual-models/sync` ensures one managed
    record per non-empty endpoint group (idempotent, cache-only).
49. **models/available parity** — `availableModelView` now carries
    `description/input_modalities/output_modalities/reasoning/
    supported_parameters` from the already-fetched views (zero extra cost);
    virtual entries removed server-side (cycle safety).
    `agents/available-models` removed.
50. **Sync robustness** — one bad group no longer aborts the whole sync
    (per-group WARN + continue); an unmanaged record squatting an
    auto-generated name is adopted instead of colliding
    (`virtual.Service` logger via `SetLogger`).

Tests: `internal/services/virtual/fold_test.go`,
`internal/services/modelinfo/remove_model_test.go`,
`internal/dashboard/managed_guard_test.go`,
`internal/services/virtual/live_members_test.go`;
`TestAvailableModelsIncludesVirtualWithoutCredentials` now asserts absence.
`make go-test` green.
