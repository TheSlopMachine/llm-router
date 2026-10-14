# Plugin Contract

The binding contract between the router core and Lua provider plugins. Every
handler, argument table, return shape and error form listed here is enforced
by the core: schema violations become `PluginInternalError` and are recorded
as plugin crashes.

Plugin API: **1.6** (`luaplugin.PluginAPIVersion`). A plugin declares the
`@plugin_api` contract it speaks; the router label (`main.RouterVersion`,
stamped from the release tag) carries no compatibility meaning. Install,
rollback, and the startup registry compare majors only: plugin major above
the served one fails with `too_new`, below fails with `too_old`, a missing
tag fails with `no_api_version`. A rejected install or rollback writes
nothing; startup skips incompatible rows with a warning while keeping them
inventoried. Minor is informational for plugin maintainers.

`@version` accepts `MAJOR.MINOR[.PATCH]` (missing patch means `.0`, one
leading `v` allowed); `@plugin_api` accepts `MAJOR.MINOR[.PATCH]` in the same
spelling. Anything else fails install.

## Table of Contents

- [Version history](#version-history)
- [Responsibility split](#responsibility-split)
- [Manifest reference](#manifest-reference)
- [Registration](#registration)
- [Handler slots](#handler-slots)
- [Common arguments](#common-arguments)
- [Chat handlers](#chat-handlers)
- [Audio image embedding moderation video handlers](#audio-image-embedding-moderation-video-handlers)
- [Metadata health authentication handlers](#metadata-health-authentication-handlers)
- [Lifecycle hooks and jobs](#lifecycle-hooks-and-jobs)
- [Error contract](#error-contract)
- [llm_router APIs](#llm_router-apis)
- [proxies.query and proxies.require](#proxiesquery-and-proxiesrequire)
- [fetch_proxies](#fetch_proxies)
- [credentials](#credentials)
- [storage](#storage)
- [log and print](#log-and-print)
- [Logging convention](#logging-convention)
- [Utilities](#utilities)
- [Schemas UI nodes locales static metadata](#schemas-ui-nodes-locales-static-metadata)
- [Sandbox limits](#sandbox-limits)
- [Worked example](#worked-example)

## Version history

Contract `0.x` rows predate the Plugin API split; `@plugin_api 1.x` rows
follow it. Router identity ships via the release tag
(`main.RouterVersion`), never via these numbers.

| Contract | Adds |
|---|---|
| 0.0.4 | complete, complete_stream, validate_credentials, get_model_infos, needs_refresh, refresh_credential, config_schema, credential_schema, auth_initiate, auth_step; proxy sources; storage; uuid_v5; random_hex |
| 0.0.5 | `transcribe` handler, `llm_router.multipart`, `ModelInfo.endpoints` |
| 0.0.6 | `speech` handler, `generate_image` handler, `embed` handler, `llm_router.base64_encode/decode` |
| 0.0.7 | proxy pool rework: repeatable `@proxy_location` whitelist, `@proxy_default_option`, per-pair rate limits and blocks |
| 0.1.1 | unified exhausted store (`scope` on the error contract); `classify_error` handler slot + `llm_router.classify_error` helper; `request.model_name`; `embed.encoding_format`; `client:stream` returns `(resp, err)` with `on_response` hook; `llm_router.http_client` (renamed); `invalid_request` stops pool failover; manifest floor 0.1.1 |
| 0.1.2 | `payment_required` error type for upstream paywalls (status 402) |
| 0.2.0 | DynamicForm Step gaps (`flow`/`grid` `gap` and `spacer` `size` accept int Step `0..8` alongside legacy `sm\|md\|lg`), `input_type=secret`, `flow.justify` gains `around`/`evenly`, strict `link.url` schemes, `provider_config` in authentication `ctx` |
| 0.3.0 | error contract rework: `timeout` merged into `upstream`; new types `content_policy` / `model_unavailable` / `structural_fault`; `rate_limit` and `quota_exceeded` require plugin-supplied future `retry_after`; `scope` strictly validated per type; `upstream_status` / `upstream_body` passthrough; geo becomes an indefinite per-provider proxy ban with same-key retry on another region (`config.geo`); `auth` / `payment_required` disable the credential, `structural_fault` disables the provider (`disabled_by/reason/at`); transport DNS/TLS/refused failures surface as `structural_fault`; manifest floor 0.3.0 |
| 0.3.1 | Proxy sources accept unauthenticated HTTP candidates only; the proxypool library owns health checks, scoring, cache lifecycle and revival |
| 0.3.4 | `quota_exceeded` accepts `proxy` scope; proxy-scoped rate/quota outcomes retry the same credential on another proxy, up to three total attempts and only before stream output reaches the client |
| 0.3.5 | `transport` error type for connectivity failures (transport `transport_error`, 502); transport failures retry the same credential on another proxy within the three-attempt route budget, carry no marks or disables, and forbid `scope`/`retry_after` |
| 0.3.6 | `model_specs` registration table: pinned per-model rows merged over `get_model_infos` rows, every field except the id; unknown ids ignored, unknown fields and mistyped values fail install |
| 0.3.7 | `overloaded` error type for congested backends (protocol `overloaded`, 503); same-credential proxy retry within the route budget, no marks or cooldown; unary JSON errors carry `Retry-After` when the router knows a wait time |
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
| 1.2 | Optional `initialize` / `deinitialize` lifecycle slots: one-time `ctx`-table hooks with `ctx.reason` (`install` / `update` / `startup` and `delete` / `update` / `shutdown` / `disable`); detached concurrent fan-out, 30s bound, failures log. |
| 1.3 | Optional per-type `locales` table (`en`/`ru`): widget keys in schema and authentication trees resolve per dashboard locale with English fallback. |
| 1.4 | `llm_router.log(severity, message, keys?)`: structured plugin logging into the events database (`debug`/`info`/`warn`/`error`/`fatal`, router-owned `plugin`/`type` keys, `print` shims info severity); per-plugin log reads serve from events with severity and keys. |
| 1.5 | `check_model` handler: plugin-owned model liveness for every endpoint (`ctx`, `model_ref` with `model`/`model_name`/`endpoint`); returns `{status, message?, response?}` with `healthy`/`quota_exceeded`/`unhealthy`/`unknown`. Dashboard model test delegates; types without the handler report `unsupported`. |
| 1.6 | Plugin disable state: operator-owned `disabled` flag on the stored plugin row (`PUT /dashboard/plugins/{id}`); disabled types leave the registry and fail with `provider_disabled`. `initialize` gains `ctx.reason = "enable"`, `deinitialize` reason `"disable"` also covers the plugin toggle (reason only, no provider identity). |

## Responsibility split

The plugin owns protocol translation and orchestration; the router owns routing,
authorization, hosting and rendering.

- Router: `ModelId` parse/resolve/disabled/model-override/endpoint gates,
  token allow-list gate over credential identifiers, proxy-pool hosting (free pool,
  custom pools, adaptive refresh, TLS-fault `MarkDead`), model cache,
  metrics, sandboxing, version gating, crash accounting, job scheduling
  (trigger plus bounds, never policy), OpenAI/Anthropic envelope rendering.
- Plugin: credential selection/rotation/parking, proxy selection and
  per-request assignment, backoff/retry loops, rate-limit interpretation,
  request payload building, response normalization, background refresh jobs,
  model liveness probing (`check_model` for every endpoint).
  Limit state lives in `llm_router.storage` with time to live; the router holds no
  quota tables.

Endpoint translation stays in the router: one handler serves several edge
routes (`messages` / `completions` / `responses` / `batches` entries run
`complete`; `translations` runs `transcribe`; `edits` / `variations` run
`generate_image`). Plugins never see Anthropic shapes. `docs/BACKEND.md`
maps every route.

## Manifest reference

High-level description: the manifest is the leading contiguous run of `---`
lines at byte zero of the file. The parser reads `--- @tag value` lines.
The block ends at the first non-`---` line. Carriage returns normalize to
line feeds. Unknown tags fail install. Duplicate single-value tags fail
install.

API signature:

```lua
--- @plugin <display name>
--- @author <author name>
--- @version <MAJOR.MINOR[.PATCH]>
--- @plugin_api <x.y[.z]>
--- @allow_host <hostname>
--- @description <free text>
--- @license <free text>
```

Parameters:

| Tag | Cardinality | Description |
|---|---|---|
| `@plugin` | required, once | Display name. Non-empty string. |
| `@author` | required, once | Author name. Non-empty string. |
| `@version` | required, once | Plugin version. Format `MAJOR.MINOR[.PATCH]`, leading `v` allowed, missing patch means `.0`. Every content edit bumps this value. Version-match reinstall skips deployment. |
| `@plugin_api` | required, once | Contract declaration. Format `x.y[.z]`. Major must equal served major `1`. |
| `@allow_host` | required, repeatable, minimum one | Bare hostname without `/`, space, or `:`. Value `*` marks the plugin unsafe and must stand alone. |
| `@description` | optional, once | Free text description. |
| `@license` | optional, once | Free text license. |

Return values: install returns `installed` on success. Compatibility
failures report `no_api_version` (missing tag), `too_new` (plugin major
above `1`), `too_old` (plugin major below `1`). Rejected install writes
nothing. Startup skips incompatible rows with a warning.

Example:

```lua
--- @plugin Harness Mock
--- @author llm-router
--- @version 1.0.1
--- @plugin_api 1.6
--- @allow_host example.com
--- @description Mock backend for harness runs.
```

## Registration

High-level description: top-level code runs once at install in dry-run mode
and again per worker at load. Top-level code calls exactly one registration
function. Handler bodies do not run during dry-run. Dry-run validates
handlers, icon, model specifications, health cooldown, schemas, locales, and
jobs.

API signatures:

```lua
--- Register a provider backend.
-- @param type_key string: backend key, pattern ^[A-Za-z0-9][A-Za-z0-9_.-]*$, length 1-64.
-- @param handlers table: handler functions plus static tables.
llm_router.register(type_key, handlers)

--- Register a proxy source.
-- @param source_key string: source key, same pattern as type_key, unique across registrations and sources.
-- @param source table: table with required fetch_proxies function.
llm_router.register_proxy_source(source_key, source)
```

Parameters:

| Parameter | Description |
|---|---|
| `type_key` | Backend identifier. Must match `^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`. Must be unique. |
| `handlers.complete` | Required function for provider backends. |
| Optional handlers | `complete_stream`, `transcribe`, `speech`, `generate_image`, `embed`, `moderate`, `generate_video`, `poll_video`, `video_content`, `validate_credentials`, `get_model_infos`, `check_health`, `check_model`, `auth_initiate`, `auth_step`, `initialize`, `deinitialize`. Each must be a function when present. Unknown handler names are ignored. |
| Static tables | `credential_schema`, `config_schema`, `settings_schema`, `proxy_schema`, `locales`, `jobs`, `model_specs`. Each must be a table when present. |
| Optional values | `icon`, `healthcheck_cooldown`. See static metadata section. |
| `source.fetch_proxies` | Required function for proxy sources. |

Return values: registration returns no value. Duplicate keys raise an error.
Invalid shapes fail dry-run.

Example:

```lua
llm_router.register("example", {
  complete = function(ctx, request)
    return { choices = { { message = { role = "assistant", content = "hi" } } } }, nil
  end,
  credential_schema = {
    { type = "secret", name = "api_key", label = "API Key" },
  },
  proxy_schema = {},
  jobs = {
    refresh = {
      interval_seconds = 300, run_on_startup = true, timeout_ms = 30000,
      run = function(ctx) return true end,
    },
  },
})
```

## Handler slots

| Slot | Required | Arguments | Returns |
|---|---|---|---|
| `complete` | yes | `(ctx, request)` | `(result, call_error)` |
| `complete_stream` | no | `(ctx, request, emit)` | `(nil, call_error)` |
| `transcribe` | no | `(ctx, request)` | `(result, call_error)` |
| `speech` | no | `(ctx, request)` | `(result, call_error)` |
| `generate_image` | no | `(ctx, request)` | `(result, call_error)` |
| `embed` | no | `(ctx, request)` | `(result, call_error)` |
| `moderate` | no | `(ctx, request)` | `(result, call_error)` |
| `generate_video` | no | `(ctx, request)` | `(result, call_error)` |
| `poll_video` | no | `(ctx, request)` | `(result, call_error)` |
| `video_content` | no | `(ctx, request)` | `(result, call_error)` |
| `validate_credentials` | no | `(data)` | `(boolean, call_error?)` |
| `get_model_infos` | no | `(ctx)` | `(array, call_error)` |
| `check_health` | no | `(ctx, credential)` | `health table` |
| `check_model` | no | `(ctx, model_ref)` | `model health table` |
| `auth_initiate` | no | `(ctx)` | `authentication result` |
| `auth_step` | no | `(ctx, {action, values})` | `authentication result` |
| `fetch_proxies` | no | `()` | `array?` |
| `initialize` | no | `(ctx)` | `nil`/`true`, or terminal error table |
| `deinitialize` | no | `(ctx)` | `nil`/`true`, or terminal error table |

Undeclared optional handlers report `endpoint_not_supported`. Unknown handler
names are ignored.

## Common arguments

### `ctx`

High-level description: first argument to every handler except
`validate_credentials`, `fetch_proxies`, and job variants.

API signature:

```lua
--- @param ctx table: execution context.
complete = function(ctx, request) ... end
```

Parameters:

| Field | Description |
|---|---|
| `provider_config` | Table with provider configuration. Present when configured. |
| `flow_id` | String. Present for authentication flows only. |
| `reason` | String. Present for jobs and lifecycle hooks. Job values: `tick`, `startup`, `manual`. Lifecycle values: `install`, `update`, `startup`, `enable`, `delete`, `shutdown`, `disable`. |
| `provider_id` | String. Present on provider disable deinitialization only. |
| `job_name` | String. Present in job execution. |
| `run_reason` | String. Alias for job reason. |

### `credential`

High-level description: credential record passed to health checks.

API signature:

```lua
--- @param credential table: credential record.
check_health = function(ctx, credential) ... end
```

Parameters:

| Field | Description |
|---|---|
| `id` | Credential identifier string. |
| `data` | Credential payload table. Empty table when no payload exists. |

### `model_ref`

High-level description: model reference passed to model checks.

API signature:

```lua
--- @param model_ref table
check_model = function(ctx, model_ref) ... end
```

Parameters:

| Field | Description |
|---|---|
| `model` | Full routing identifier, format `provider/model`. |
| `model_name` | Bare upstream model name. Use this value in upstream payloads. |
| `endpoint` | Capability name: `chat`, `vision`, `speech`, `transcription`, `image`, `embeddings`, `video`, `moderate`, future values. Empty normalizes to `chat` in the router. |

### `request`

High-level description: every request table carries `model` (full
`provider/model` identifier), `model_name` (bare name for upstream payloads)
and `cache_key` (opaque cross-turn prefix-cache partition derived from model,
first message and sorted tool names; `"prefix-boot"` for empty histories).
`stream` is absent by contract: streaming is served by `complete_stream`,
never by a flag. Never forward a request table verbatim upstream:
`model_name` and `cache_key` are router-only and upstreams reject unknown
properties.

### Return idiom

High-level description: request handlers return `(result, nil)` or
`(nil, call_error)`; `nil` result is always a schema violation. Streams must
not fail over after the first `emit` call: the stream belongs to that attempt.

## Chat handlers

### `complete`

High-level description: serves unary chat completions. Required handler.

API signature:

```lua
--- Serve unary chat completion.
-- @param ctx table: execution context with optional provider_config.
-- @param request table: chat completion request without stream flag, with model, model_name, cache_key, messages, tools.
-- @return table: ChatCompletionResponse with non-empty choices.
-- @return nil: error slot, nil on success.
complete = function(ctx, request) return result, nil end
```

Parameters:

| Parameter | Description |
|---|---|
| `ctx` | Execution context with optional `provider_config`. |
| `request.model` | Full routing identifier. |
| `request.model_name` | Bare model name for upstream payload. |
| `request.cache_key` | Cache partition string. |
| `request.messages` | Message array. |
| `request.tools` | Tool array when present. |

Return values: `ChatCompletionResponse`. The `choices` array must be
non-empty. Second slot is `nil` on success, terminal error table on failure.

Example:

```lua
complete = function(ctx, request)
  local client = llm_router.http_client({ timeout_ms = 15000 })
  local response, call_error = client:request({
    method = "POST",
    url = "https://api.example.com/chat",
    headers = { ["content-type"] = "application/json" },
    body = json.encode({ model = request.model_name, messages = request.messages })
  })
  if call_error ~= nil then return nil, call_error end
  if response.status ~= 200 then
    return nil, { message = "upstream status " .. tostring(response.status), code = "server_error", status = response.status }
  end
  local payload = json.decode(response.body)
  return { choices = { { message = { role = "assistant", content = payload.text } } } }, nil
end
```

### `complete_stream`

High-level description: serves streaming chat completions. Optional. Missing
handler emulates streaming from `complete` with one chunk plus `[DONE]`.

API signature:

```lua
--- Serve streaming chat completion.
-- @param ctx table: execution context.
-- @param request table: chat completion request.
-- @param emit function: chunk writer, function(chunk) validates choices or usage present.
-- @return nil: no result table, router appends [DONE].
complete_stream = function(ctx, request, emit) emit({...}) end
```

Parameters:

| Parameter | Description |
|---|---|
| `ctx` | Execution context. |
| `request` | Same shape as `complete`. |
| `emit` | Function accepting one chunk table. Each chunk must contain non-empty `choices` or `usage`. Each call writes `data: <canonical-json>\n\n` and sets delivery flag. |

Return values: no result table. Returns terminal error table on failure
before first `emit`. After first `emit`, failure is final.

Example:

```lua
complete_stream = function(ctx, request, emit)
  emit({
    id = "chatcmpl-1",
    object = "chat.completion.chunk",
    created = os.time(),
    model = request.model,
    choices = { { index = 0, delta = { role = "assistant", content = "hi" }, finish_reason = nil } }
  })
  emit({
    id = "chatcmpl-1",
    object = "chat.completion.chunk",
    created = os.time(),
    model = request.model,
    choices = { { index = 0, delta = {}, finish_reason = "stop" } }
  })
end
```

## Audio image embedding moderation video handlers

### `transcribe`

High-level description: serves transcription.

API signature:

```lua
--- Transcribe audio.
-- @param ctx table: execution context.
-- @param request table: model, model_name, file raw string, file_name, content_type, language, prompt, response_format, temperature, timestamp_granularities array, needs_segments boolean.
-- @return table: TranscriptionResponse.
transcribe = function(ctx, request) return result, nil end
```

Parameters: `model` full identifier, `model_name` bare name, `file` raw
bytes string, `file_name` string, `content_type` string, `language` optional
string, `prompt` optional string, `response_format` optional string,
`temperature` optional number, `timestamp_granularities` optional string
array, `needs_segments` boolean (true when `response_format` is `srt` or
`vtt`).

Return values: transcription response table.

Example:

```lua
transcribe = function(ctx, request)
  local body, content_type = llm_router.multipart({
    { name = "file", data = request.file, filename = request.file_name, content_type = request.content_type },
    { name = "model", value = request.model_name },
  })
  local client = llm_router.http_client({})
  local response, call_error = client:request({
    method = "POST",
    url = "https://api.example.com/audio/transcriptions",
    headers = { ["content-type"] = content_type },
    body = body,
  })
  if call_error ~= nil then return nil, call_error end
  return { text = json.decode(response.body).text }, nil
end
```

### `speech`

High-level description: serves text to speech.

API signature:

```lua
--- Synthesize speech.
-- @param ctx table: execution context.
-- @param request table: model, model_name, input, voice, response_format, speed, instructions.
-- @return table: {audio_b64, format} mapped to SpeechResponse.
speech = function(ctx, request) return result, nil end
```

Parameters: `model`, `model_name`, `input` string, `voice` optional string,
`response_format` optional string, `speed` optional number, `instructions`
optional string.

Return values: table with `audio_b64` base64 audio and `format` string.

Example:

```lua
speech = function(ctx, request)
  local client = llm_router.http_client({})
  local response, call_error = client:request({
    method = "POST",
    url = "https://api.example.com/audio/speech",
    headers = { ["content-type"] = "application/json" },
    body = json.encode({ model = request.model_name, input = request.input, voice = request.voice })
  })
  if call_error ~= nil then return nil, call_error end
  return { audio_b64 = llm_router.base64_encode(response.body), format = "mp3" }, nil
end
```

### `generate_image`

High-level description: serves image generation, edits, and variations.

API signature:

```lua
--- Generate image.
-- @param ctx table: execution context.
-- @param request table: model, model_name, prompt, n, size, quality, style, response_format, image_b64, image_name, mask_b64.
-- @return table: ImageGenerationResponse with non-empty data.
generate_image = function(ctx, request) return result, nil end
```

Parameters: `model`, `model_name`, `prompt` string, `n` optional number,
`size` optional string, `quality` optional string, `style` optional string,
`response_format` optional string, `image_b64` optional base64 input for
edits/variations, `image_name` optional string, `mask_b64` optional base64
mask.

Return values: image response with non-empty `data`.

Example:

```lua
generate_image = function(ctx, request)
  local client = llm_router.http_client({})
  local response, call_error = client:request({
    method = "POST",
    url = "https://api.example.com/images/generations",
    headers = { ["content-type"] = "application/json" },
    body = json.encode({ model = request.model_name, prompt = request.prompt })
  })
  if call_error ~= nil then return nil, call_error end
  return { data = json.decode(response.body).data }, nil
end
```

### `embed`

High-level description: serves embeddings.

API signature:

```lua
--- Create embeddings.
-- @param ctx table: execution context.
-- @param request table: model, model_name, input string array, encoding_format, dimensions.
-- @return table: EmbeddingsResponse where length of data equals length of input and each vector is non-empty.
embed = function(ctx, request) return result, nil end
```

Parameters: `model`, `model_name`, `input` string array,
`encoding_format` optional string, `dimensions` optional number.

Return values: embeddings response where length of `data` equals length of
`input`.

Example:

```lua
embed = function(ctx, request)
  local client = llm_router.http_client({})
  local response, call_error = client:request({
    method = "POST",
    url = "https://api.example.com/embeddings",
    headers = { ["content-type"] = "application/json" },
    body = json.encode({ model = request.model_name, input = request.input })
  })
  if call_error ~= nil then return nil, call_error end
  return json.decode(response.body), nil
end
```

### `moderate`

High-level description: serves moderation.

API signature:

```lua
--- Moderate text.
-- @param ctx table: execution context.
-- @param request table: model, model_name, input string array.
-- @return table: ModerationResponse where length of results equals length of input.
moderate = function(ctx, request) return result, nil end
```

Parameters: `model`, `model_name`, `input` string array.

Return values: moderation response where length of `results` equals length of
`input`.

Example:

```lua
moderate = function(ctx, request)
  return { results = { { flagged = false } } }, nil
end
```

### `generate_video`, `poll_video`, `video_content`

High-level description: serves asynchronous video generation.

API signatures:

```lua
--- Submit video job.
-- @param ctx table: execution context.
-- @param request table: model, model_name, prompt, duration, resolution, aspect_ratio, size, seed, generate_audio, frame_images array, input_references array, previous_job_id, callback_url storage only, provider options table, user, session_id.
-- @return table: VideoGenerationResponse with id and status.
generate_video = function(ctx, request) return result, nil end

--- Poll video job.
-- @param ctx table: execution context.
-- @param request table: model, model_name, job_id.
-- @return table: job status table.
poll_video = function(ctx, request) return result, nil end

--- Fetch video content.
-- @param ctx table: execution context.
-- @param request table: model, model_name, job_id, index.
-- @return table: {video_b64, content_type}.
video_content = function(ctx, request) return result, nil end
```

Parameters for `generate_video`: `model`, `model_name`, `prompt` optional
string, `duration` optional, `resolution` optional, `aspect_ratio` optional,
`size` optional, `seed` optional, `generate_audio` optional boolean,
`frame_images` optional array of `{type, image_url={url}, frame_type}`,
`input_references` optional array of `{type, image_url, audio_url,
video_url}`, `previous_job_id` optional string, `callback_url` storage only,
`provider.options` optional table, `user` optional string, `session_id`
optional string. `poll_video` takes `job_id`. `video_content` takes `job_id`
and `index`.

Return values: submit returns `{id, status}`; poll returns status table;
content returns `{video_b64, content_type}`.

Example:

```lua
generate_video = function(ctx, request)
  local client = llm_router.http_client({})
  local response, call_error = client:request({
    method = "POST",
    url = "https://api.example.com/videos",
    headers = { ["content-type"] = "application/json" },
    body = json.encode({ model = request.model_name, prompt = request.prompt })
  })
  if call_error ~= nil then return nil, call_error end
  local payload = json.decode(response.body)
  return { id = payload.id, status = payload.status }, nil
end
```

## Metadata health authentication handlers

### `validate_credentials`

High-level description: validates one credential payload. Missing handler
accepts all payloads.

API signature:

```lua
--- Validate credential payload.
-- @param data table: credential fields.
-- @return boolean: true accepts, false rejects with 400 invalid_request_error when error slot is nil.
-- @return table|nil: terminal error table or nil.
validate_credentials = function(data) return true end
```

Parameters: `data` table with credential fields.

Return values: boolean plus optional error table. `false` plus `nil` maps to
`400 invalid_request_error`.

Example:

```lua
validate_credentials = function(data)
  if data.api_key == nil or data.api_key == "" then
    return false, { message = "missing api key", code = "invalid_request_error", status = 400 }
  end
  return true
end
```

### `get_model_infos`

High-level description: returns provider model inventory. Runs with
provider-scoped credential list. Static `model_specs` overlay the result.

API signature:

```lua
--- List upstream models.
-- @param ctx table: execution context.
-- @return table: array of {name, display_name, endpoints array}.
get_model_infos = function(ctx) return result, nil end
```

Parameters: `ctx` execution context.

Return values: array of `{name, display_name, endpoints}` where `endpoints`
lists capability names.

Example:

```lua
get_model_infos = function(ctx)
  return { { name = "model-1", display_name = "Model 1", endpoints = { "chat" } } }, nil
end
```

### `check_health`

High-level description: checks one credential. Only `unhealthy` disables the
credential, and only when provider automation is on. Cooldown comes from
`healthcheck_cooldown`.

API signature:

```lua
--- Check credential health.
-- @param ctx table: execution context.
-- @param credential table: {id, data}.
-- @return table: {status="healthy|unhealthy|unknown", message=optional string}.
check_health = function(ctx, credential) return result end
```

Parameters: `ctx` execution context, `credential` with `id` and `data`.

Return values: table with `status` one of `healthy`, `unhealthy`, `unknown`
and optional `message` string. Invalid shapes are crashes and change nothing.

Example:

```lua
check_health = function(ctx, credential)
  local client = llm_router.http_client({})
  local response, call_error = client:request({ method = "GET", url = "https://api.example.com/health" })
  if call_error ~= nil then return { status = "unknown", message = "probe failed" } end
  if response.status == 200 then return { status = "healthy" } end
  if response.status == 401 then return { status = "unhealthy", message = "rejected key" } end
  return { status = "unknown", message = "status " .. tostring(response.status) }
end
```

### `check_model`

High-level description: checks one model plus endpoint.

API signature:

```lua
--- Check model health.
-- @param ctx table: execution context.
-- @param model_ref table: {model, model_name, endpoint}.
-- @return table: {status="healthy|quota_exceeded|unhealthy|unknown", message=optional, response=optional}.
check_model = function(ctx, model_ref) return result end
```

Parameters: `ctx` execution context, `model_ref` with `model`, `model_name`,
`endpoint`.

Return values: `healthy` keeps routing, `quota_exceeded` skips without
disable, `unhealthy` marks dead model, `unknown` leaves state unchanged.
`response` holds a short success excerpt. Unknown endpoints return
`{status="unknown", message="endpoint not supported ..."}`. Types without the
handler report `unsupported` with no live request.

Example:

```lua
check_model = function(ctx, model_ref)
  if model_ref.endpoint ~= "chat" then
    return { status = "unknown", message = "endpoint not supported" }
  end
  return { status = "healthy", response = "ok" }
end
```

### `auth_initiate` and `auth_step`

High-level description: implements multi-step credential capture. Missing
`auth_initiate` uses single-step `credential_schema` form.

API signatures:

```lua
--- Start authentication flow.
-- @param ctx table: {flow_id, provider_config optional}.
-- @return table: exactly one of render, redirect_url, credentials.
auth_initiate = function(ctx) return result end

--- Handle authentication step.
-- @param ctx table: execution context.
-- @param input table: {action, values}.
-- @return table: exactly one of render, redirect_url, credentials.
auth_step = function(ctx, input) return result end
```

Parameters: `ctx` carries `flow_id` plus optional `provider_config`;
`input` carries `action` string and `values` table.

Return values: exactly one field must be present. `render` is a UI node tree,
`redirect_url` is a non-empty string, `credentials` is a string-keyed table.
Any other shape fails.

Example:

```lua
auth_initiate = function(ctx)
  return { render = { { type = "secret", name = "api_key", label = "API Key" } } }
end
auth_step = function(ctx, input)
  return { credentials = { api_key = input.values.api_key } }
end
```

## Lifecycle hooks and jobs

### `initialize` and `deinitialize`

High-level description: optional one-time slots. Declare them only when the
plugin needs setup or teardown; most plugins omit both.

API signatures:

```lua
--- Run initialization.
-- @param ctx table: {reason="install|update|startup|enable", provider_id on provider disable only, provider_config on provider disable only}.
-- @return nil|boolean|table: nil or true on success, terminal error table on failure.
initialize = function(ctx) return true end

--- Run deinitialization.
-- @param ctx table: {reason="delete|update|shutdown|disable"}.
-- @return nil|boolean|table.
deinitialize = function(ctx) return true end
```

Parameters: `initialize` reason one of `install` (first install), `update`
(reinstall or rollback), `startup` (router boot with the plugin enabled),
`enable` (operator re-enabled the plugin). `deinitialize` reason one of
`delete` (plugin removal), `update` (source replacement, old source runs),
`shutdown` (router exit), `disable` (provider instance left enabled state or
operator disabled the plugin). Provider `disable` carries `provider_id` and
`provider_config`; plugin-toggle `disable` and every other lifecycle context
carry `reason` only.

Return values: `nil` or `true` on success, terminal error table on failure.
Any other value is a crash.

Sandbox: `storage`, `http_client`, `proxies` when the type declares
`proxy_schema`, `print`, `llm_router.log`, `json`, `uuid_v5`, `random_hex`,
`multipart`, `base64_*`. Credential tables stay out of plugin-scoped hooks;
the `disable` hook reads `credentials.list`/`get`/`parked` for its provider
only. No lifecycle hook writes credentials. Each call runs in fresh state
with 30 second bound. Calls fan out concurrently across types; failures log
and never block install, delete, disable, enable, startup or shutdown. States
stay per call: top-level file code reruns on every request, so it stays
side-effect free. One-time state persists through `llm_router.storage`.

Example:

```lua
initialize = function(ctx)
  if ctx.reason == "install" then
    llm_router.storage.set("flags", "seeded", { version = 1 }, { ttl = 7776000 })
  end
end,
deinitialize = function(ctx)
  if ctx.reason == "disable" then
    llm_router.storage.delete("flags", "seeded")
  end
end,
```

### `jobs`

High-level description: colocated periodic tasks.

API signature:

```lua
--- Job specification.
-- @param jobs table: map from job name to specification {interval_seconds, run_on_startup, timeout_ms, run}.
jobs = {
  refresh = {
    interval_seconds = 300,
    run_on_startup = true,
    timeout_ms = 30000,
    run = function(ctx) return true end,
  },
}
```

Parameters:

| Field | Description |
|---|---|
| Name | Pattern `^[a-z0-9_]{1,32}$`. Maximum 8 jobs per type. |
| `interval_seconds` | Interval in seconds. Range 60 to 86400. |
| `run_on_startup` | Boolean. Runs once at startup when true. Defaults to false. |
| `timeout_ms` | Timeout in milliseconds. Defaults to 60000. Bounds 1000 to 300000. Values outside bounds clamp to bounds. |
| `run` | Function `function(ctx)` where `ctx` carries `job_name`, `run_reason` (`tick`, `startup`, `manual`), and optional `provider_config`. Returns `true`, `nil`, or terminal error table. |

Return values: `true` or `nil` marks success. Terminal error table marks
failure. Any other value is a crash.

Example:

```lua
jobs = {
  refresh = {
    interval_seconds = 3600,
    run_on_startup = false,
    timeout_ms = 30000,
    run = function(ctx)
      for _, item in ipairs(llm_router.credentials.list()) do
        llm_router.log("debug", "refreshing credential", { cred_id = item.id, job = ctx.job_name })
      end
      return true
    end,
  },
}
```

## Error contract

High-level description: terminal errors are OpenAI-shaped tables and render
verbatim. Extra fields are ignored.

API signature:

```lua
--- Terminal error table.
return nil, { message = "...", code = "rate_limit", param = "...", status = 429, headers = {...} }
```

Parameters:

| Field | Description |
|---|---|
| `message` | Required non-empty string. Missing or empty value rejects the table as a crash. |
| `code` | Optional string. Default `server_error`. Use `invalid_request_error`, `authentication_error`, `payment_required`, `not_found`, `rate_limit`, `insufficient_quota`, `server_error`. |
| `param` | Optional offending field name string. |
| `status` | Optional HTTP status override 400 to 599. Default 502. |
| `headers` | Optional string-to-string table rendered as response headers on unary `/v1` errors in both envelopes. Names pass a strict allow-list (`Retry-After` today); anything else is dropped, never an error. `Retry-After` accepts non-negative integer seconds or an HTTP date. Mid-stream server-sent event failures carry no headers because status is already 200. Non-string keys or values are dropped, never fatal. |

Return behavior: the router renders `message`/`code` verbatim into the
OpenAI `{"error":{"message","type","code"}}` envelope (Anthropic envelope on
Anthropic-style requests). No `type`/`scope`/`retry_after`/`upstream_status`/
`upstream_body` fields exist: rate interpretation, parking and backoff live in
plugin code backed by `storage` time to live. `not_found` means the requested
model does not exist upstream. The router drops the model from the model cache
(best-effort) and returns the error as-is. Emit it only when the upstream
names the model as missing, never for bad endpoints or malformed requests.
Calling Lua `error()` with a terminal table on the stack maps to provider
error. Any other Lua error maps to internal error plus crash record. Branch on
`response.status` directly: `401` tries next credential, `429` parks plus
next, `400` or `404` returns terminal `invalid_request_error`.

Example:

```lua
return nil, { message = "upstream rejected request", code = "invalid_request_error", status = 400 }
```

## `llm_router` APIs

### `http_client({timeout_ms?})`

High-level description: creates an HTTP client bound to `@allow_host` plus
server-side request forgery guard. The router tracks TLS faults on the
assigned leg and marks the proxy dead internally; plugins never mark proxies.

API signature:

```lua
--- Create HTTP client.
-- @param options table|nil: {timeout_ms=number}.
-- @return userdata: client with request and stream methods.
local client = llm_router.http_client({ timeout_ms = 15000 })
```

Parameters:

| Parameter | Description |
|---|---|
| `timeout_ms` | Timeout in milliseconds. Default 60000. Range 1000 to 300000. Values outside range clamp to range. |

Return values: client userdata with `request` and `stream` methods.

#### `client:request`

High-level description: sends one unary request.

API signature:

```lua
--- Send unary request.
-- @param arguments table: {method, url required http/https string, proxy_url optional non-empty string, headers table string to string, body string}.
-- @return table|nil: response {status=number, headers=table lowercased keys to string, body=string} on success, nil on failure.
-- @return table|nil: nil on success, fault table on failure.
local response, call_error = client:request({ method = "POST", url = "https://...", headers = {...}, body = "..." })
```

Parameters:

| Parameter | Description |
|---|---|
| `method` | HTTP method string. Default `GET`. |
| `url` | Required `http` or `https` string. Must match allow-list. |
| `proxy_url` | Optional non-empty string. Empty or absent means direct connection. |
| `headers` | Table string to string. Custom `Host` header is revalidated against allow-list. |
| `body` | String payload. |

Return values: success returns `(response, nil)` where `response` holds
`status` number, `headers` table with lowercased keys, `body` string capped at
16 MiB. Failure returns `(nil, fault)` where `fault` follows the error
contract plus transport fields below. Redirects recheck allow-list, maximum 10
hops. Private and link-local dials are blocked even under `*`.

Transport fault fields:

| Field | Meaning |
|---|---|
| `transport` | `true` for a network-level failure: connect, TLS, truncated or reset body, timeout. Absent for status errors and callback errors. |
| `reason` | `connection_refused`, `connection_reset`, `broken_pipe`, `early_eof`, `unexpected_eof`, `dns_resolution`, `address_parse`, `tls_certificate_verification`, `tls_handshake`, or `network` for any other network failure. |
| `proxy_fault` | `true` when the failure happened on a proxied leg (`proxy_url` set). |
| `retryable` | `proxy_fault` and no stream output has reached the client yet. Retry on another proxy only when this is `true`. |

The retry boundary is the first byte delivered to the client through `emit`.
After it, a failure is final. Before it the plugin may retry, but must discard
anything it accumulated from the failed attempt. A clean end of stream is not
an error. A reset, broken pipe or truncated body on a proxied leg marks the
proxy as suspect: the pool re-verifies it before any ban. Cancelling the
client request is never a transport failure.

Example:

```lua
local client = llm_router.http_client({ timeout_ms = 15000 })
local response, call_error = client:request({
  method = "POST", url = "https://api.example.com/v1/chat/completions",
  headers = { ["content-type"] = "application/json" },
  body = json.encode({ model = request.model_name }),
  proxy_url = proxy_url,
})
```

#### `client:stream`

High-level description: sends one streaming request with line or chunk
callbacks plus optional response gate.

API signature:

```lua
--- Send streaming request.
-- @param arguments table: request fields plus on_line or on_chunk plus optional on_response.
-- @return table|nil: head {status, headers} on success.
-- @return table|nil: error table on failure.
local head, call_error = client:stream({ method = "POST", url = url, headers = headers, body = body, on_line = function(line) ... end })
```

Parameters:

| Parameter | Description |
|---|---|
| `method`, `url`, `proxy_url`, `headers`, `body` | Same semantics as `request`. Exactly one of `on_line` or `on_chunk` is required. |
| `on_line` | Function `function(line)` called per server-sent event line. Scanner buffer 64 KiB to 1 MiB. |
| `on_chunk` | Function `function(bytes)` called per byte chunk. Buffer 32 KiB. Exclusive with `on_line`. |
| `on_response` | Function `function(head)` where `head` is `{status, headers}` plus `body` on non-2xx. Non-nil table return aborts with that error. Non-table return raises. `nil` accepts the response. |

Return values: non-2xx without hook returns
`(nil, {message="unexpected status N: <body up to 64 KiB>",
code="server_error"})`. Success returns `(head, nil)`.

Example:

```lua
local client = llm_router.http_client({ timeout_ms = 15000 })
local head, call_error = client:stream({
  method = "POST",
  url = "https://api.example.com/stream",
  headers = { ["content-type"] = "application/json" },
  body = json.encode({ model = request.model_name }),
  on_response = function(head)
    if head.status ~= 200 then
      return { message = "status " .. tostring(head.status), code = "server_error", status = head.status }
    end
  end,
  on_line = function(line) end,
})
```

## `proxies.query` and `proxies.require`

Availability: installed only when the type declares `proxy_schema`.

### `proxies.query`

High-level description: reads a read-only snapshot of the router pool.

API signature:

```lua
--- Query proxy snapshot.
-- @param options table|nil: {pool=string, country=string singular, limit=number}.
-- @return table: array of {id, url, country, pool}.
local list = llm_router.proxies.query({ pool = "auto", country = "US", limit = 3 })
```

Parameters:

| Parameter | Description |
|---|---|
| `pool` | Pool selector. Empty or absent means direct. `"auto"` means free pool. Otherwise a custom pool identifier or name. |
| `country` | Singular country filter string. |
| `limit` | Maximum rows. Number. |

Return values: array of `{id, url, country, pool}`.

Example:

```lua
local list = llm_router.proxies.query({ pool = "auto", limit = 3 })
-- [{ id, url, country, pool }]
```

### `proxies.require`

High-level description: acquires live proxies for one attempt.

API signature:

```lua
--- Require proxies.
-- @param options table|nil: {pool, countries array plural, exclude array, limit, want, timeout_ms, fallback}.
-- @return table|nil: {proxies=array, timed_out=boolean, direct=boolean} or nil plus code.
local result, code = llm_router.proxies.require({ pool = "auto", countries = { "US" }, limit = 1 })
```

Parameters:

| Option | Meaning |
|---|---|
| `pool` | `""`/absent: direct. `"auto"`: free pool. Otherwise a custom pool identifier or name. |
| `countries` | Array of two-letter ISO codes, OR semantics. Empty or absent accepts any country. Invalid codes raise an error. Plural form, unlike `query`. |
| `exclude` | Array of proxy URLs that must not be returned. |
| `limit` | Maximum number of distinct proxies returned, 1 to 50, default 1. Whole numbers only. |
| `want` | Free pool only: alive proxies the pool keeps in stock for these countries, 1 to 200. Default is the pool default. |
| `timeout_ms` | Free pool only: wait deadline, default 60000, at most 300000. |
| `fallback` | `"fail"` (default) or `"direct"`: behavior when no proxy is available. |

Return values: table `{proxies={{id,url,country,pool},...}, timed_out,
direct}`. `direct` is true when `pool` is empty; `proxies` is then empty.
`timed_out` is true when the free pool had no match before the deadline and
`fallback` is `"direct"`; `proxies` is empty and the plugin goes direct. With
`fallback="fail"` an unavailable proxy returns `nil, code` instead:
`"timeout"` (free pool deadline passed), `"no_match"` (custom pool has no
matching entry) or `"canceled"` (request cancelled). The same `"canceled"`
return applies with `fallback="direct"`. Malformed options raise an error. The
free pool returns matching live proxies immediately. Without a match it
registers a demand, prioritizes finding the requested country, and waits until
a match is alive or the deadline passes. The wait deadline is shared by every
`require` call of one request: the first call fixes it. Cancelling the client
request stops only the wait; the pool keeps working on the demand so the next
request finds a proxy. A custom pool answers from its own entries and never
waits. The router never falls back to a direct connection on its own.

Example:

```lua
local result, code = llm_router.proxies.require({
  pool = "auto", countries = { "US", "CA" }, limit = 1, fallback = "fail",
})
if result == nil then
  return nil, { message = "no proxy: " .. tostring(code), code = "server_error" }
end
local proxy_url = result.proxies[1].url
```

## `fetch_proxies`

High-level description: proxy source feed. Source-only plugins use this slot.
One bare source name may be claimed by several plugins; the runtime qualifies
each as `<recordID>/<name>`.

API signature:

```lua
--- Fetch proxy list.
-- @return table|nil: array of {url, country} or {protocol, host, port, country}; nil means no change.
fetch_proxies = function() return result end
```

Parameters: none.

Return values: array of rows in either shape below, or `nil` for no changes
(304-style). Rows without an address are dropped; other schemes count as
unsupported. The router verifies every candidate through the pool before
serving it.

Example:

```lua
fetch_proxies = function()
  return {
    { url = "socks5://1.2.3.4:1080", country = "DE" },
    { protocol = "socks5", host = "5.6.7.8", port = 1080, country = "DE" },
  }
end
```

`protocol` is one of `http`, `https`, `socks4`, `socks5` (`socks4a` reads as
`socks4`); omitted protocols default to `http`. Credentials embed as
`user[:pass]@` in `url`.

## `credentials`

Availability: installed only when the type serves credentials, that is when
`credential_schema` exists or `auth_initiate` is declared.

### `list`

High-level description: lists usable credentials.

API signature:

```lua
--- List usable credentials.
-- @return table: array of {id, data}.
local list = llm_router.credentials.list()
```

Parameters: none.

Return values: array `[{id, data}]` filtered by the router-token allow-list;
parked rows are skipped automatically, disabled and denied rows are skipped.
Empty allow-list means unrestricted for admin probes.

Example:

```lua
for _, credential_entry in ipairs(llm_router.credentials.list()) do
  llm_router.log("debug", "trying credential", { cred_id = credential_entry.id })
end
```

### `get`

High-level description: fetches one credential.

API signature:

```lua
--- Fetch one credential.
-- @param id string: credential identifier.
-- @return table|nil: {id, data} or nil when missing, disabled, or denied. No error is raised.
local item = llm_router.credentials.get(id)
```

Parameters: `id` credential identifier string.

Return values: `{id, data}` table or `nil`.

Example:

```lua
local item = llm_router.credentials.get("cred-1")
if item == nil then return nil, { message = "missing credential", code = "server_error" } end
```

### `update`

High-level description: persists refreshed credential payload.

API signature:

```lua
--- Update credential payload.
-- @param id string: credential identifier.
-- @param data table: new payload.
-- @return boolean: true on success, raises otherwise. Job contexts only.
llm_router.credentials.update(id, { key = "next" })
```

Parameters: `id` string, `data` table.

Return values: `true` on success. Request paths calling it fail loudly.

Example:

```lua
run = function(ctx)
  llm_router.credentials.update("cred-1", { api_key = "next" })
  return true
end
```

### `disable` and `enable`

High-level description: sets or clears the shared disabled flag.

API signatures:

```lua
--- Disable credential.
-- @param id string: credential identifier.
-- @param reason string|nil: optional reason.
-- @return boolean: true on success. Requires provider disable_failed_credentials switch, else raises.
llm_router.credentials.disable(id, "rejected key")

--- Enable credential.
-- @param id string: credential identifier.
-- @return boolean: true on success. Same switch gate as disable.
llm_router.credentials.enable(id)
```

Parameters: `id` string, `reason` optional string.

Return values: `true` on success. Both serve request and job contexts but
require the provider `disable_failed_credentials` switch, failing loudly
without it so plugins fall back to parking. `disable` sets the shared flag
(first-wins against other causes). `enable` clears plugin-caused disables
only.

Example:

```lua
llm_router.credentials.disable(credential_entry.id, "rejected key")
```

### `park`, `unpark`, `parked`

High-level description: shelves credentials transiently with time to live.

API signatures:

```lua
--- Park credential for transient failure.
-- @param id string: credential identifier.
-- @param ttl number: seconds.
-- @param reason string|nil: optional reason.
-- @return boolean: true on success. Ungated.
llm_router.credentials.park(id, 60, "rate_limit")

--- Unpark credential.
-- @param id string: credential identifier.
-- @return boolean: true on success. Ungated.
llm_router.credentials.unpark(id)

--- Read park state.
-- @param id string: credential identifier.
-- @return table|nil: {reason=string, ["until"]=unix seconds} or nil when live.
local state = llm_router.credentials.parked(id)
```

Parameters: `id` string, `ttl` number seconds, `reason` optional string.

Return values: `park` and `unpark` return `true`. `parked` returns `nil` for
a live credential or `{reason, until}` for a parked one. Misuse (non-numeric
time to live, non-string reason) raises a Lua error. Both serve request and
job contexts with no switch.

Guidance: park (router-owned entry with time to live, invisible,
self-healing) is for transient states: rate limits, quota windows. Disable
(shared flag, dashboard-visible, sticky until cleared) is for dead states:
rejected keys. Prefer `park` over hand-rolled key-value cooldowns: parked rows
skip `list()` on every path while key-value entries only hide rows the plugin
remembers to check. The manual dashboard toggle and switch-gated probe always
win over plugin writes.

Example:

```lua
llm_router.credentials.park(credential_entry.id, 60, "rate_limit")
local state = llm_router.credentials.parked(credential_entry.id)
if state ~= nil then
  llm_router.log("debug", "credential parked", { cred_id = credential_entry.id, wait = state["until"] })
end
```

## `storage`

High-level description: per-plugin key-value store shared by request
handlers, jobs, and lifecycle hooks. Use it for quota state and cooldowns when
`park` is insufficient. Prefer `credentials.park` over hand-rolled cooldowns.

API signatures:

```lua
--- Store value.
-- @param scope string: non-empty namespace.
-- @param key string: non-empty key.
-- @param value any: JSON-compatible value.
-- @param options table|nil: {ttl=seconds number}.
-- @return boolean: true on success, raises otherwise.
llm_router.storage.set("cooldown", cred_id, { ["until"] = os.time() + 60 }, { ttl = 60 })

--- Read value.
-- @param scope string: non-empty namespace.
-- @param key string: non-empty key.
-- @return any|nil: stored value or nil on miss or expiry. Expired rows prune on read.
local value = llm_router.storage.get("cooldown", cred_id)

--- Delete value.
-- @param scope string: non-empty namespace.
-- @param key string: non-empty key.
-- @return boolean: true. Missing keys are not errors.
llm_router.storage.delete("cooldown", cred_id)
```

Parameters:

| Parameter | Description |
|---|---|
| `scope` | Non-empty namespace string. |
| `key` | Non-empty key string. |
| `value` | Any JSON-compatible Lua value. |
| `ttl` | Seconds as number. `0` means durable database. Greater than `0` means cache database with expiry. Negative fails. Greater than 90 days fails. |

Return values: `set` and `delete` return `true`. `get` returns stored value
or `nil`. Reads check durable storage first, then cache with expiry check.
`set` migrates rows between files when time-to-live class changes. Bare rows
written before contract 0.7.0 serve without expiry. Entries with a time to
live live in the disposable cache file: deleting it while the router is
stopped drops them. Durable entries stay in the database file.

Example:

```lua
llm_router.storage.set("cooldown", credential_entry.id, { ["until"] = os.time() + 60 }, { ttl = 60 })
local saved = llm_router.storage.get("cooldown", credential_entry.id)
llm_router.storage.delete("cooldown", credential_entry.id)
```

## `log` and `print`

### `log`

High-level description: writes one structured event. Message holds a stable
human sentence without embedded values and without provider name. Type
identifies source. Keys carry facts.

API signature:

```lua
--- Write structured log.
-- @param severity string: debug, info, warn, warning alias, error, fatal, critical alias to fatal.
-- @param message any: required message, tables encode as JSON, truncated to 2048 characters.
-- @param keys table|nil: optional string keys to string, number, boolean, or JSON-encoded table. Maximum 32 keys. Reserved plugin and type rejected.
llm_router.log("warn", "rate limited", { code = "rate_limit", cred_id = id })
```

Parameters:

| Parameter | Description |
|---|---|
| `severity` | `debug`, `info`, `warn`, `error`, `fatal`. `warning` aliases `warn`. `critical` aliases `fatal`. |
| `message` | Required message. Tables encode as JSON. Truncated to 2048 characters. |
| `keys` | Optional table. Maximum 32 entries. Values truncate at 2048 characters. Reserved `plugin` and `type` rejected. |

Return values: none. Entries below configured `LOG_LEVEL` never persist. The
router attaches `plugin` (record identifier) and `type` (type key) itself.

Example:

```lua
llm_router.log("warn", "rate limited", { code = "rate_limit", status = 429, wait = 30 })
```

### `print`

High-level description: print shim.

API signature:

```lua
--- Print shim.
-- @param ... any: values joined with tab and logged at info severity.
print("hello", 1)
```

Parameters: variadic values.

Return values: none.

## Logging convention

Messages carry one stable human sentence per code site: the same site always
emits identical text, so lines group. Sentence case, present tense, no
trailing period, no embedded values, no provider or plugin name (the `type`
key and the display line already identify the source).

Severity tiers: `debug` for verbose flow and troubleshooting (attempts,
payloads, dumps, completions); `info` for concise lifecycle progress
(selection results, completions, background runs); `warn` for handled
failures with failover (bench, park, next credential or exit); `error`
for terminal failures (the request fails); `fatal` for unrecoverable
states (the plugin cannot continue). `critical` aliases `fatal`.
`info` marks expected progress, `debug` marks diagnostic detail;
`error` marks a failed request, `fatal` marks a dead plugin.

Keys use `snake_case` with full words. Standard vocabulary, required
whenever the data exists. Use `cred_id` (never `credential_id`),
`error` (never `err`), `provider_id` (never `provider`), `endpoint`
(never `method` or `path`):

| Key | Meaning |
|---|---|
| `status` | upstream HTTP status (number) |
| `code` | protocol error code |
| `cred_id` | credential attempting the call |
| `provider_id` | provider instance serving the call |
| `token_id` | router token authorizing the call |
| `plugin_id` | plugin record emitting the line |
| `model` | router model identifier |
| `endpoint` | request path |
| `event` | category for high-volume debug sites (see below) |
| `count` | result cardinality |
| `job` | background job name |
| `operation` | handler or lifecycle slot name |
| `attempt` | attempt number within the retry loop |
| `reason` | variable failure text that cannot stabilize into a message |
| `detail` | variable upstream-supplied detail |
| `wait` | park or backoff seconds (number) |
| `latency_ms` | upstream round-trip time (number) |
| `exit` | proxy exit key |
| `proxy_url` | proxy endpoint assigned to the attempt |
| `body` | capped upstream text (1000 characters) |
| `line` | full server-sent event line |
| `body_len`, `input_tokens`, `cached_tokens`, `tools`, `messages` | numeric payload facts |

Arbitrary extra keys stay allowed; machine data never lives in message
text. The `event` values are fixed per family: `exits`,
`rate_limit`, `forbidden`, `request_dump`, `response_dump`,
`reasoning_replay`, `payload`, `cache`, `request_start`, `http`,
`http_error`, `stream`, `stream_response`, `stream_error`,
`stream_done`, `sse`.

Example and anti-example:

```lua
-- DO: stable message, facts in keys
llm_router.log("debug", "selecting exits", { usable = 3, skipped = 1, event = "exits" })
-- DON'T: values in text
llm_router.log("debug", "usable=3 skipped_rate_limited=1", { tag = "EXITS" })
```

## Utilities

### `multipart`

High-level description: builds multipart bodies.

API signature:

```lua
--- Build multipart body.
-- @param parts table: array length 1-64. Each part is {name=string, value=string} or {name=string, data=string, filename=optional string, content_type=optional string default application/octet-stream}.
-- @return string: body.
-- @return string: content type with random 16-byte boundary.
local body, content_type = llm_router.multipart({ { name = "file", data = raw } })
```

Parameters: `parts` array 1 to 64 entries. Each part holds `name` required
string plus either `value` string for plain fields or `data` string for file
fields with optional `filename` string and optional `content_type` string
(default `application/octet-stream`).

Return values: body string plus content type string with random 16-byte
boundary.

Example:

```lua
local body, content_type = llm_router.multipart({
  { name = "file", data = request.file, filename = "audio.wav", content_type = "audio/wav" },
  { name = "model", value = request.model_name },
})
```

### `base64_encode` and `base64_decode`

High-level description: standard encoding helpers.

API signatures:

```lua
--- Encode base64.
-- @param value string: raw bytes.
-- @return string: encoded text.
local text = llm_router.base64_encode(raw)

--- Decode base64.
-- @param value string: encoded text.
-- @return string: decoded bytes. Raises on invalid input.
local raw = llm_router.base64_decode(text)
```

Parameters: `value` string.

Return values: encoded or decoded string.

### `uuid_v5` and `random_hex`

High-level description: identifier helpers.

API signatures:

```lua
--- Generate RFC 4122 version 5 UUID.
-- @param namespace string: UUID string.
-- @param name string: name string.
-- @return string: UUID in 8-4-4-4-12 format.
local id = llm_router.uuid_v5(namespace, name)

--- Generate random hex.
-- @param nbytes number: range 1-1024.
-- @return string: hex string from cryptographic random source.
local token = llm_router.random_hex(16)
```

Parameters: `namespace` must itself be a UUID string; `name` is any string.
`nbytes` range 1 to 1024.

Return values: UUID string or hex string.

Example:

```lua
local id = llm_router.uuid_v5("6ba7b810-9dad-11d1-80b4-00c04fd430c8", request.model_name)
local nonce = llm_router.random_hex(16)
```

### `json`

High-level description: global JSON table, not under `llm_router`.

API signatures:

```lua
--- Encode JSON.
-- @param value any: Lua value.
-- @return string: JSON text.
local text = json.encode({ hello = "world" })

--- Decode JSON.
-- @param text string: JSON text.
-- @return any: Lua value. JSON null maps to nil.
local value = json.decode(text)
```

Parameters: value or text.

Return values: encoded text or decoded Lua value.

## Schemas UI nodes locales static metadata

### Schema kinds

High-level description: static tables validated at install and served through
the dashboard.

API signature:

```lua
credential_schema = { ... },
config_schema = { ... },
settings_schema = { ... },
proxy_schema = {},
```

Parameters: each kind must be a table when present; functions fail install.

Surfaces: credentials exist when `credential_schema` or `auth_initiate`
exists, proxies exist when `proxy_schema` exists (even empty enables the
proxy tab), settings exist when `settings_schema` exists. Missing tables hide
the surface; the sandbox omits the matching `llm_router` tables. Capability
gating resolves from the stored record.

Example:

```lua
credential_schema = {
  { type = "secret", name = "api_key", label = "API Key" },
},
proxy_schema = {},
```

### UI node kinds

High-level description: leaf kinds `text`, `input`, `select`, `checkbox`,
`button`, `link`, `banner`, `secret`, `code`. Container kinds `group`,
`flow`, `grid`, `section`, `spacer`, `divider`. No raw HTML from plugins.

API signature:

```lua
--- UI tree.
-- @param nodes table: array table, maximum 200 nodes, depth maximum 8.
local tree = { { type = "secret", name = "api_key", label = "API Key" } }
```

Parameters:

| Kind | Rules |
|---|---|
| `input`, `select`, `checkbox`, `secret` | Require `name`. `input.input_type` is `text`, `password`, `number`, or `secret`. `select.options` is a string array, `option_labels` keys must be a subset of `options`. |
| `link` | Requires `url` with `http`, `https`, `mailto`, or relative form. Rejects `javascript:` and `data:`. |
| `button` | `form_action` defaults to `submit`. Variant is `primary`, `secondary`, or `danger`. Buttons render in host footer. |
| `banner` | Variant is `info`, `error`, or `success`. |
| `code` | Requires `text`. |
| `group`, `flow`, `grid`, `section` | Require non-empty `content`. `flow.direction` is `horizontal` or `vertical`, default `vertical`. `gap` is step 0 to 8 or `sm`, `md`, `lg`, default `md`. `align` is `start`, `center`, `end`, or `stretch`. `justify` is `start`, `center`, `end`, `between`, `around`, or `evenly`. `grid.columns` range 1 to 6. `section.title` required, maximum 120 characters, `subtitle` maximum 240 characters. |
| `spacer`, `divider` | No content. `spacer.size` is step value, `grow` defaults to true. |

Return values: validated node array served through `Schema` plus
`ResolveUINodes` per locale.

Example:

```lua
credential_schema = {
  { type = "secret", name = "api_key", label = "API Key" },
  { type = "select", name = "region", label = "Region", options = { "us", "eu" } },
  { type = "flow", direction = "horizontal", gap = 4, content = {
    { type = "button", text = "Save", form_action = "submit", variant = "primary" },
  } },
}
```

### Locales

High-level description: per-type widget translations.

API signature:

```lua
--- Locales table.
-- @param locales table: {en={key=text}, ru={key=text}}. Maximum 1000 entries per locale. Key maximum 128 characters. Value maximum 1000 characters.
locales = {
  en = { ["API Key"] = "API Key" },
  ru = { ["API Key"] = "API Key RU" },
}
```

Parameters: locale codes `en` and `ru` only. Entry keys 1 to 128 characters,
values 1 to 1000 characters, maximum 1000 entries per locale. Values must be
non-empty strings.

Return behavior: widget fields (`text`, `label`, `placeholder`, `title`,
`subtitle`, `option_labels` values, including nested `content`) match by exact
string: a match serves the requested locale, a miss falls back to the `en`
entry, a final miss serves the stored text. Stored text stays the fallback, so
plugins without `locales` serve English unchanged. Authentication render trees
resolve the same way. Dashboard schema endpoints accept `?locale=` with
`Accept-Language` fallback. Russian tags resolve to `ru`, everything else
resolves to `en`.

Example:

```lua
locales = {
  en = { ["API Key"] = "API Key" },
  ru = { ["API Key"] = "API key RU" },
}
```

### `icon`

High-level description: dashboard icon selector.

API signature:

```lua
--- @param icon string: empty string, https:// URL, or data:image/ URI. Maximum 32 KiB.
icon = "https://example.com/icon.png"
```

Parameters: `icon` string.

Return behavior: empty means no icon. Other schemes fail install.

### `model_specs`

High-level description: pinned per-model rows that clarify discovered catalog
rows.

API signature:

```lua
--- @param model_specs table: map from model identifier to field table merged over get_model_infos row except identifier.
model_specs = {
  ["model-1"] = { context_window = 200000 },
}
```

Parameters: map keys select model identifiers. Every field merges over the
`get_model_infos` row except the identifier.

Return behavior: fields absent from the specification pass through; entries
for undiscovered identifiers are ignored (specifications never resurrect
removed models). Unknown fields and mistyped values fail install.

### `healthcheck_cooldown`

High-level description: per-type credential health-check cooldown.

API signature:

```lua
--- @param healthcheck_cooldown number: whole seconds, range 60 to 86400, default 300.
healthcheck_cooldown = 300
```

Parameters: whole seconds number.

Return behavior: mistyped and out-of-range values fail install. Absent means
5 minute default.

## Sandbox limits

High-level description: fresh Lua state per call, pooled and rebound.

API surface:

```lua
-- Open libraries: base (pairs, ipairs, next, select, type, tostring, tonumber, error, pcall, xpcall, setmetatable, getmetatable, rawget, rawset, rawequal, assert), table, string, math, os (time, clock, date only).
-- Globals: print shim, json, llm_router.
```

Parameters: none.

Limits: no `io`, `debug`, `package`, `coroutine`, `require`, `load`,
`dofile`, `print` (shims `llm_router.log` at info severity). Response bodies
cap at 16 MiB; stream error bodies at 64 KiB; redirects cap at 10 hops
validated against `@allow_host`; private and link-local dial targets are
blocked even under wildcard hosts. `on_chunk` buffer 32 KiB. `on_line`
scanner 64 KiB to 1 MiB. `multipart` 1 to 64 parts. `random_hex` 1 to 1024
bytes. Log values cap at 2048 characters and 32 keys. UI trees cap at depth 8
and 200 nodes. Locales cap at 1000 entries. Jobs cap at 8 per type.

## Worked example

High-level description: credential plus proxy loop with park, authentication
break, rate-limit park, and terminal invalid request.

```lua
complete = function(ctx, request)
  local pool = nil
  if ctx.provider_config and ctx.provider_config.proxy
    and ctx.provider_config.proxy.pool ~= "" then
    pool = ctx.provider_config.proxy.pool
  end
  local client = llm_router.http_client({})
  local last_error = nil
  for _, credential_entry in ipairs(llm_router.credentials.list()) do
    local key = (credential_entry.data or {}).api_key or ""
    for _, proxy_entry in ipairs(llm_router.proxies.query({ pool = pool, limit = 3 })) do
      local response, call_error = client:request({
        method = "POST", url = "https://api.example.com/v1/chat/completions",
        headers = { ["content-type"] = "application/json" },
        body = json.encode({ model = request.model_name, messages = request.messages }),
        proxy_url = proxy_entry.url,
      })
      if call_error == nil and response.status == 200 then
        local out = json.decode(response.body)
        out.model = request.model
        return out, nil
      elseif call_error ~= nil then
        last_error = call_error
      elseif response.status == 401 then
        break
      elseif response.status == 429 then
        llm_router.credentials.park(credential_entry.id, 60, "rate limited")
        break
      elseif response.status == 400 or response.status == 404 then
        return nil, { message = "upstream rejected the request", code = "invalid_request_error", status = response.status }
      else
        last_error = { message = "upstream status " .. tostring(response.status), code = "server_error", status = response.status }
      end
    end
  end
  return nil, last_error or { message = "all credentials exhausted", code = "server_error" }
end
```
