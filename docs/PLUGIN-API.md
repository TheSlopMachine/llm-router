# Plugin Contract

The binding contract between the router core and Lua provider plugins. Every
handler, argument table, return shape and error form listed here is enforced
by the core: schema violations become `PluginInternalError` and are recorded
as plugin crashes.

Plugin API: **1.1** (`luaplugin.PluginAPIVersion`). A plugin declares the
`@plugin_api` contract it speaks; the router label (`main.RouterVersion`,
stamped from the release tag) carries no compatibility meaning. Install
compares majors only: plugin major above the served one fails with `too_new`,
below fails with `too_old`, a missing tag fails with `no_api_version`. Minor
is informational for plugin maintainers.

`@version` accepts `MAJOR.MINOR[.PATCH]` (missing patch means `.0`, one
leading `v` allowed); `@plugin_api` accepts `MAJOR.MINOR[.PATCH]` in the same
spelling. Anything else fails install.

## Version history

| Router | Adds |
|---|---|
| 0.0.4 | complete, complete_stream, validate_credentials, get_model_infos, needs_refresh, refresh_credential, config_schema, credential_schema, auth_initiate, auth_step; proxy sources; storage; uuid_v5; random_hex |
| 0.0.5 | `transcribe` handler, `llm_router.multipart`, `ModelInfo.endpoints` |
| 0.0.6 | `speech` handler, `generate_image` handler, `embed` handler, `llm_router.base64_encode/decode` |
| 0.0.7 | proxy pool rework: repeatable `@proxy_location` whitelist, `@proxy_default_option`, per-pair rate limits and blocks |
| 0.1.1 | unified exhausted store (`scope` on the error contract); `classify_error` handler slot + `llm_router.classify_error` helper; `request.model_name`; `embed.encoding_format`; `client:stream` returns `(resp, err)` with `on_response` hook; `llm_router.http_client` (renamed); `invalid_request` stops pool failover; manifest floor 0.1.1 |
| 0.1.2 | `payment_required` error type for upstream paywalls (status 402) |
| 0.2.0 | DynamicForm Step gaps (`flow`/`grid` `gap` and `spacer` `size` accept int Step `0..8` alongside legacy `sm\|md\|lg`), `input_type=secret`, `flow.justify` gains `around`/`evenly`, strict `link.url` schemes, `provider_config` in auth `ctx` |
| 0.3.0 | error contract rework: `timeout` merged into `upstream`; new types `content_policy` / `model_unavailable` / `structural_fault`; `rate_limit` and `quota_exceeded` require plugin-supplied future `retry_after`; `scope` strictly validated per type; `upstream_status` / `upstream_body` passthrough; geo becomes an indefinite per-provider proxy ban with same-key retry on another region (`config.geo`); `auth` / `payment_required` disable the credential, `structural_fault` disables the provider (`disabled_by/reason/at`); transport DNS/TLS/refused failures surface as `structural_fault`; manifest floor 0.3.0 |
| 0.3.1 | Proxy sources accept unauthenticated HTTP candidates only; the proxypool library owns health checks, scoring, cache lifecycle and revival |
| 0.3.4 | `quota_exceeded` accepts `proxy` scope; proxy-scoped rate/quota outcomes retry the same credential on another proxy, up to three total attempts and only before stream output reaches the client |
| 0.3.5 | `transport` error type for connectivity failures (wire `transport_error`, 502); transport failures retry the same credential on another proxy within the three-attempt route budget, carry no marks or disables, and forbid `scope`/`retry_after` |
| 0.3.6 | `model_specs` registration table: pinned per-model rows merged over `get_model_infos` rows, every field except the id; unknown ids ignored, unknown fields and mistyped values fail install |
| 0.3.7 | `overloaded` error type for congested backends (wire `overloaded`, 503); same-credential proxy retry within the route budget, no marks or cooldown; unary JSON errors carry `Retry-After` when the router knows a wait time |
| 0.3.9 | request tables carry `cache_key`: stable cross-turn prefix-cache partition (model, first message, sorted tool names); `"prefix-boot"` for empty histories; old routers omit it |
| 0.4.0 | `generate_video` / `poll_video` / `video_content` handlers serving `POST /v1/videos`, `GET /v1/videos/{jobId}`, `GET /v1/videos/{jobId}/content`; `videos` endpoint in `ModelInfo.endpoints`; router-side job rows map local IDs to upstream jobs |
| 0.5.0 | `moderate` handler serving `POST /v1/moderations`; `image_b64` / `image_name` / `mask_b64` `generate_image` request fields serving `POST /v1/images/edits` and `POST /v1/images/variations` (old plugins ignore the extra fields) |
| 0.5.1 | traffic-driven disables removed: `auth` / `payment_required` fail over without disabling the credential, `structural_fault` stops the pool without disabling the provider; disables come only from admin actions and doctor fix for backend-less types (old plugins unchanged) |
| 0.5.2 | `check_health` handler plus per-type `healthcheck_cooldown` registration value (whole seconds, 60..86400, default 300): failure-triggered detached credential verification, at most once per window; only an explicit `unhealthy` verdict disables (`disabled_by=healthcheck`) |
| 0.5.3 | unified `proxy_retry` provider policy (`mode` `fail_fast`/`next_proxy`, `max_attempts` 1..10, default 3) for every retryable proxy failure; legacy `geo` sections migrate at startup; fresh discovery refreshes known model rows |
| 0.7.0 | decentralized orchestration: plugins select credentials (`credentials.list/get`) and proxies (`proxies.query` + per-request `proxy_url`), retry internally, and return OpenAI-shaped terminal errors only when exhausted. Static schema tables (`credential_schema`, `config_schema`, `settings_schema`, `proxy_schema`); presence drives dashboard surfaces, no `_enabled` flags. Colocated `jobs` with per-job `run` (`interval_seconds` 60..86400, `run_on_startup`, `timeout_ms`); `credentials.update` exists in job contexts only. `storage.set` gains `{ttl}` seconds. `credentials.park/unpark/parked` shelve credentials transiently (`list()` skips parked rows); `credentials.disable/enable` set the shared flag under the provider `disable_failed_credentials` switch. Removed: `classify_error` slot + helper, `needs_refresh` / `refresh_credential`, error `type`/`scope`/`retry_after`/`upstream_*` contract, `@proxy_location` / `@proxy_default_option` / `@proxy_source` tags, router credential ordering, exhausted store, geo bans. Manifest floor 0.7.0. |
| 0.8.0 | `proxies.require({pool?, countries?, exclude?, limit?, want?, timeout_ms?, fallback?})`: demand-driven proxy acquisition. The free pool waits for a live proxy in the requested countries instead of returning an empty list; the plugin chooses `fallback` `direct` or `fail` explicitly. `proxies.query` is unchanged. Transport error tables from `http_client` gain `transport`, `reason`, `proxy_fault`, `retryable`; body-read failures on proxied legs suspect the proxy. |
| 1.0 | Plugin API split: `@plugin_api x.y` replaces `@router_version`. Install gates on the major only (`too_new` / `too_old` / `no_api_version`); minor is informational. All store plugins reissued under `1.0`. Router identity moves to the release tag (`main.RouterVersion`), short `X.Y` when the fix is 0. |
| 1.1 | Optional `headers` string table on the error contract, rendered through a strict allow-list (`Retry-After` today) on unary `/v1` errors in both envelopes; streams and dashboard excluded. |

## Responsibility split

The plugin owns wire translation and orchestration; the router owns routing,
authorization, hosting and rendering.

- Router: `ModelId` parse/resolve/disabled/model-override/endpoint gates,
  token allow-list gate over credential IDs, proxy-pool hosting (free pool,
  custom pools, adaptive refresh, TLS-fault `MarkDead`), model cache,
  metrics, sandboxing, version gating, crash accounting, job scheduling
  (trigger + bounds, never policy), OpenAI/Anthropic envelope rendering.
- Plugin: credential selection/rotation/parking, proxy selection and
  per-request assignment, backoff/retry loops, rate-limit interpretation,
  request payload building, response normalization, background refresh jobs.
  Limit state lives in `llm_router.storage` with TTL; the router holds no
  quota tables.

Endpoint translation stays in the router: one handler serves several edge
routes (`messages` / `completions` / `responses` / `batches` entries run
`complete`; `translations` runs `transcribe`; `edits` / `variations` run
`generate_image`). Plugins never see Anthropic shapes. `docs/BACKEND.md`
maps every route.

## Manifest reference

The header is the contiguous run of `---` lines at byte 0 of the file.
`@plugin_api` validates as semver; an unparsable value fails install before
any other field is examined. Unknown `@tags` fail install.

| Tag | Cardinality | Constraint |
|---|---|---|
| `@plugin` | required, once | non-empty display name |
| `@author` | required, once | non-empty |
| `@version` | required, once | valid semver |
| `@plugin_api` | required, once | valid `x.y[.z]`, major must equal the served contract |
| `@allow_host` | required, repeatable | bare hostname; `*` marks the plugin unsafe and stands alone |
| `@description` | optional, once | free text |
| `@license` | optional, once | free text |

## Registration

```lua
llm_router.register(type_key, {
  complete = function(ctx, request) ... end,  -- required
  complete_stream = function(ctx, request, emit) ... end,
  transcribe = function(ctx, request) ... end,
  credential_schema = {
    { type = "secret", name = "api_key", label = "API Key" },
  },
  proxy_schema = {},  -- present (even empty) enables the proxy tab
  jobs = {
    refresh = {
      interval_seconds = 300, run_on_startup = true, timeout_ms = 30000,
      run = function(ctx) ... end,
    },
  },
})

llm_router.register_proxy_source(name, {
  fetch_proxies = function() ... end,  -- required
})
```

- `type_key` and source names: start alnum, `[A-Za-z0-9_.-]`, 1–64 chars.
- `complete` is required. Undeclared optional handlers report
  `endpoint_not_supported` — loud, never a silent fallback. Unknown handler
  names are ignored.
- Schema tables are static: `credential_schema`, `config_schema`,
  `settings_schema`, `proxy_schema` must be tables when present; functions
  fail install. Presence drives surfaces: credentials iff
  `credential_schema` or `auth_initiate` exists, proxies iff `proxy_schema`
  exists, settings iff `settings_schema` exists. Missing tables hide the
  surface; the sandbox omits the matching `llm_router` tables.
- `jobs` keys match `^[a-z0-9_]{1,32}$`, max 8 per type. Each entry needs
  `run` (function) and `interval_seconds` (60..86400); `run_on_startup`
  defaults false, `timeout_ms` defaults 60000 (bounds 1000..300000).
- An optional `icon` string on the registration table selects the dashboard
  icon: `https://` URL or `data:image/` URI, 32KiB cap. Empty means no icon.
- An optional `model_specs` table pins per-model rows that clarify
  discovered catalog rows:
  ```lua
  model_specs = {
    ["glm-5"] = { context_window = 200000 },
  }
  ```
  Every field merges over the `get_model_infos` row except the id, which
  the entry key selects. Fields absent from the spec pass through;
  entries for undiscovered ids are ignored (specs never resurrect removed
  models). Unknown fields and mistyped values fail install — a typo'd key
  must never deploy as a silent no-op.
- An optional `healthcheck_cooldown` number sets the per-type credential
  health-check cooldown in whole seconds (60..86400, default 300).
  Mistyped and out-of-range values fail install.
- One bare source name may be claimed by several plugins; the runtime
  qualifies each as `<recordID>/<name>`.

## Handler slots

| Slot | Required | Args | Returns |
|---|---|---|---|
| `complete` | yes | `(ctx, request)` | `(result, err)` |
| `complete_stream` | no | `(ctx, request, emit)` | `(nil, err)` |
| `transcribe` | no | `(ctx, request)` | `(result, err)` |
| `speech` | no | `(ctx, request)` | `(result, err)` |
| `generate_image` | no | `(ctx, request)` | `(result, err)` |
| `embed` | no | `(ctx, request)` | `(result, err)` |
| `moderate` | no | `(ctx, request)` | `(result, err)` |
| `generate_video` | no | `(ctx, request)` | `(result, err)` |
| `poll_video` | no | `(ctx, request)` | `(result, err)` |
| `video_content` | no | `(ctx, request)` | `(result, err)` |
| `validate_credentials` | no | `(data)` | `(boolean, err?)` |
| `get_model_infos` | no | `(ctx)` | `(array, err)` |
| `check_health` | no | `(ctx, credential)` | `health table` |
| `auth_initiate` | no | `(ctx)` | `auth result` |
| `auth_step` | no | `(ctx, {action, values})` | `auth result` |
| `fetch_proxies` | no | `()` | `array?` |

### Common arguments

- `ctx` — `{ provider_config = {...} }` when the provider has config rows.
  Auth handlers also carry `flow_id`. Job `run` receives
  `{ job_name, run_reason = "tick"|"startup"|"manual", provider_config? }`.
- `credential` — `{ id = "...", data = {...} }`; `data` holds the fields
  saved through `credential_schema`. Only `check_health` receives one.
- Every `request` table carries `model` (full `provider/model` id),
  `model_name` (bare name for upstream payloads) and `cache_key` (opaque
  cross-turn prefix-cache partition: model, first message and sorted tool
  names hashed; `"prefix-boot"` for empty histories). `stream` is absent by
  contract: streaming is served by `complete_stream`, never by a flag.
  Never forward a request table verbatim upstream: `model_name` and
  `cache_key` are router-only and upstreams reject unknown properties.
- Request handlers return `(result, nil)` or `(nil, err)`; `nil` result is
  always a schema violation. Streams must not fail over after the first
  `emit` call: the stream belongs to that attempt.

### Error contract

`err` is `{ message = ..., code = ..., param = ..., status = ..., headers = ... }`:

- `message`: human string, required, non-empty. Missing or empty messages
  reject the table as a plugin crash.
- `code`: wire code string, defaults to `server_error`. Use OpenAI codes
  (`invalid_request_error`, `authentication_error`, `payment_required`,
  `not_found`, `rate_limit`, `insufficient_quota`, `server_error`).
- `param`: optional offending field name.
- `status`: optional HTTP status override 400..599, defaults to 502.
- `headers`: optional string-to-string table rendered as response headers on
  unary `/v1` errors (both envelopes). Names pass a strict allow-list
  (`Retry-After` today); anything else is dropped, never an error.
  `Retry-After` accepts non-negative integer seconds or an HTTP date.
  Mid-stream SSE failures carry no headers (status already 200). Non-string
  keys or values are dropped, never fatal.
- The router renders `message`/`code` verbatim into the OpenAI
  `{"error":{"message","type","code"}}` envelope (Anthropic envelope on
  Anthropic-style requests). No `type`/`scope`/`retry_after`/
  `upstream_status`/`upstream_body` fields exist: rate interpretation,
  parking and backoff live in plugin code backed by `storage` TTL.
- `not_found`: the requested model does not exist upstream. The router
  drops the model from the info cache (best-effort) and returns the error
  as-is. Emit it only when the upstream names the model as missing — never
  for bad endpoints or malformed requests.

## `llm_router` APIs

### `http_client({timeout_ms?})`

`timeout_ms` defaults 60000, clamped 1000..300000. Per-request tables
carry `proxy_url`: an explicit proxy endpoint (`""`/absent = direct).
The router tracks TLS faults on the assigned leg and marks the proxy dead
internally; plugins never mark proxies. Transport failures surface as
`(nil, {message, code="server_error"})`; the plugin selects the next
`proxy_url` itself.

Network-level failures add fields to that error table so the plugin can
decide whether another proxy is worth trying:

| Field | Meaning |
|---|---|
| `transport` | `true` for a network-level failure: connect, TLS, truncated or reset body, timeout. Absent for status errors and callback errors. |
| `reason` | `connection_refused`, `connection_reset`, `broken_pipe`, `early_eof`, `unexpected_eof`, `dns_resolution`, `address_parse`, `tls_certificate_verification`, `tls_handshake`, or `network` for any other network failure. |
| `proxy_fault` | `true` when the failure happened on a proxied leg (`proxy_url` set). |
| `retryable` | `proxy_fault` and no stream output has reached the client yet. Retry on another proxy only when this is `true`. |

The retry boundary is the first byte delivered to the client through `emit`.
After it, a failure is final: the plugin returns the error. Before it the
plugin may retry, but must discard anything it accumulated from the failed
attempt. A clean end of stream is not an error. A reset, broken pipe or
truncated body on a proxied leg marks the proxy as suspect: the pool
re-verifies it before any ban, so a reset caused by the upstream costs one
recheck. Cancelling the client request is never a transport failure.

```lua
local client = llm_router.http_client({ timeout_ms = 15000 })
local resp, err = client:request({
  method = "POST", url = "https://api.example.com/v1/chat/completions",
  headers = { ["Authorization"] = "Bearer " .. key },
  body = json.encode(payload),
  proxy_url = proxy_url,  -- explicit per-request assignment
})
-- resp = { status, headers, body }; err follows the error contract or nil
local resp, err = client:stream({
  method = "POST", url = url, headers = headers, body = body,
  proxy_url = proxy_url,
  on_response = function(r)  -- optional head classifier
    if r.status ~= 200 then
      return { message = "status " .. tostring(r.status), code = "server_error", status = r.status }
    end
  end,
  on_line = function(line) ... end,  -- or on_chunk = function(bytes) ... end
})
```

### `proxies.query({pool?, country?, limit?})`

Read-only snapshot of the router pool. `pool` defaults to no selection:
pass `"auto"` for the free pool or a custom pool ID/name (provider config
`proxy.pool`, default direct when absent). Installed only when the type
declares `proxy_schema`.

```lua
local proxies = llm_router.proxies.query({ pool = "auto", limit = 3 })
-- [{ id, url, country, pool }]
```

### `proxies.require({pool?, countries?, exclude?, limit?, want?, timeout_ms?, fallback?})`

Acquires live proxies for one attempt. Installed with `proxies.query`.

| Option | Meaning |
|---|---|
| `pool` | `""`/absent: direct. `"auto"`: free pool. Otherwise a custom pool ID/name. |
| `countries` | Array of two-letter ISO codes, OR semantics. Empty or absent accepts any country. Invalid codes raise an error. |
| `exclude` | Array of proxy URLs that must not be returned, for example exits the plugin already marked. |
| `limit` | Maximum number of distinct proxies returned, 1..50, default 1. |
| `want` | Free pool only: alive proxies the pool keeps in stock for these countries, 1..200. Default is the pool default. |
| `timeout_ms` | Free pool only: wait deadline, default 60000, at most 300000. |
| `fallback` | `"fail"` (default) or `"direct"`: what to do when no proxy is available. |

The free pool returns matching live proxies immediately. Without a match it
registers a demand, prioritizes finding the requested country, and waits until
a match is alive or the deadline passes. The wait deadline is shared by every
`require` call of one request: the first call fixes it. Cancelling the client
request stops only the wait; the pool keeps working on the demand for a while
so the next request finds a proxy. A custom pool answers from its own entries
and never waits. The router never falls back to a direct connection on its own.

Returns a table `{ proxies = { {id, url, country, pool}, ... }, timed_out, direct }`:

- `direct` is true when `pool` is empty. `proxies` is then empty.
- `timed_out` is true when the free pool had no match before the deadline and `fallback` is `"direct"`; `proxies` is empty and the plugin goes direct.

With `fallback = "fail"` an unavailable proxy returns `nil, code` instead:
`"timeout"` (free pool deadline passed), `"no_match"` (custom pool has no
matching entry) or `"canceled"` (request cancelled). The same `"canceled"`
return applies with `fallback = "direct"`. Malformed options raise an error.

```lua
local res, err = llm_router.proxies.require({
  pool = "auto", countries = { "US", "CA" }, exclude = marked_urls,
  limit = 3, fallback = "fail",
})
if not res then return nil, err end   -- plugin maps to its terminal error
local px = res.proxies[1]
local proxy_url = px and px.url or "" -- "" = direct
```

### `fetch_proxies()` (proxy sources)

Feeds return an array of rows in either shape:

```lua
{ url = "socks5://1.2.3.4:1080", country = "DE" }
{ protocol = "socks5", host = "1.2.3.4", port = 1080, country = "DE" }
```

`protocol` is one of `http`, `https`, `socks4`, `socks5` (`socks4a`
reads as `socks4`); omitted protocols default to `http`. Credentials
embed as `user[:pass]@` in `url`. Rows without an address are dropped;
other schemes count as unsupported. `nil` means no changes (304-style).
The router verifies every candidate through the pool before serving it.

### `credentials.list()` / `get(id)` / `update(id, data)` / `disable(id, reason?)` / `enable(id)` / `park(id, ttl_seconds, reason?)` / `unpark(id)` / `parked(id)`

Provider credential access. Installed only when the type serves
credentials. `list` returns enabled credentials `[{id, data}]` filtered by
the router-token allow-list; parked rows are skipped automatically, so
loops never test them twice. `get` returns one row or nil (expired and
disabled rows read as missing). `update` persists refreshed data and exists
in job contexts only — request paths calling it fail loudly. `disable` sets
the shared disabled flag (`DisabledBy="plugin"`, first-wins against other
causes) and `enable` clears plugin-caused disables only; both serve request
and job contexts but require the provider `disable_failed_credentials`
switch, failing loudly without it so plugins fall back to parking.
`park` shelves a credential for `ttl_seconds` with an optional `reason`
and returns true; `unpark` clears the entry early and returns true; both
serve request and job contexts with no switch. `parked` returns nil for a
live credential or `{reason, until}` (`until` is a unix timestamp) for a
parked one. Misuse (non-numeric ttl, non-string reason) raises a Lua error.

Park (router-owned entry with TTL, invisible, self-healing) is for transient
states: rate limits, quota windows. Disable (shared flag, dashboard-visible,
sticky until cleared) is for dead states: rejected keys. Prefer `park` over
hand-rolled KV cooldowns: parked rows skip `list()` on every path while KV
entries only hide rows the plugin remembers to check. The manual
dashboard toggle and switch-gated probe always win over plugin writes.

### `storage.set(scope, key, value, {ttl?})` / `get` / `delete`

Persistent per-plugin KV. `ttl` seconds bounds entry lifetime (max 90
days); expired rows read as missing and delete on read. Pre-0.7.0 bare
rows serve without expiry.

```lua
llm_router.storage.set("cooldown", cred_id, { until = os.time() + wait }, { ttl = wait })
```

### `multipart`, `base64_*`, `uuid_v5`, `random_hex`, `json`

Unchanged: `multipart(parts)→(body, ctype)` (1..64 parts),
`base64_encode/decode`, `uuid_v5(ns, name)` (RFC 4122), `random_hex(n)`
(1..1024 bytes), `json.encode/decode`.

### Sandbox limits

Fresh `LState` per call; `Base/Table/String/Math/Os(time/clock/date)`
only. No `io/debug/package/coroutine/require/load/dofile/print`
(`print` routes to the plugin log). Response bodies cap at 16 MiB;
stream error bodies at 64 KiB; redirects cap at 10 hops validated against
`@allow_host`; private/link-local dial targets are blocked even under
wildcard hosts.

## Worked example

```lua
complete = function(ctx, request)
  -- Unconfigured providers go direct: only an explicit pool selection
  -- (dashboard proxy switch) routes through pooled exits.
  local pool = nil
  if ctx.provider_config and ctx.provider_config.proxy
    and ctx.provider_config.proxy.pool ~= "" then
    pool = ctx.provider_config.proxy.pool
  end
  local client = llm_router.http_client({})
  local last_err = nil
  for _, cred in ipairs(llm_router.credentials.list()) do
    local key = (cred.data or {}).api_key or ""
    for _, px in ipairs(llm_router.proxies.query({ pool = pool, limit = 3 })) do
      local resp, err = client:request({
        method = "POST", url = "https://api.example.com/v1/chat/completions",
        headers = { ["Authorization"] = "Bearer " .. key },
        body = json.encode({ model = request.model_name, messages = request.messages }),
        proxy_url = px.url,
      })
      if err == nil and resp.status == 200 then
        local out = json.decode(resp.body)
        out.model = request.model
        return out
      elseif err ~= nil then
        last_err = err
      elseif resp.status == 401 then
        break  -- next credential
      elseif resp.status == 429 then
        llm_router.credentials.park(cred.id, 60, "rate limited")
        break  -- next credential
      elseif resp.status == 400 or resp.status == 404 then
        return nil, { message = "upstream rejected the request", code = "invalid_request_error", status = resp.status }
      else
        last_err = { message = "upstream status " .. tostring(resp.status), code = "server_error", status = resp.status }
      end
    end
  end
  return nil, last_err or { message = "all credentials exhausted", code = "server_error" }
end
```
