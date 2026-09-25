# Plugin Contract

The binding contract between the router core and Lua provider plugins. Every
handler, argument table, return shape and error form listed here is enforced
by the core: schema violations become `PluginInternalError` and are recorded
as plugin crashes.

Router version: **0.1.2** (`models.CurrentVersion`). A plugin using a feature
must declare the `@router_version` that introduced it; older routers refuse
to install it. Routers serve no contract older than **0.1.1**: plugins
declaring `0.0.x` are rejected at install and must be reissued.

Plugin `@version` and `@router_version` accept `MAJOR.MINOR[.PATCH]`
(a missing patch means `.0`); anything else is rejected at install.

## Version history

| Router | Adds |
|---|---|
| 0.0.4 | complete, complete_stream, validate_credentials, get_model_infos, needs_refresh, refresh_credential, config_schema, credential_schema, auth_initiate, auth_step; proxy sources; storage; uuid_v5; random_hex |
| 0.0.5 | `transcribe` handler, `llm_router.multipart`, `ModelInfo.endpoints` |
| 0.0.6 | `speech` handler, `generate_image` handler, `embed` handler, `llm_router.base64_encode/decode` |
| 0.0.7 | proxy pool rework: repeatable `@proxy_location` whitelist, `@proxy_default_option`, per-pair rate limits and blocks |
| 0.1.1 | unified exhausted store (`scope` on the error contract); `classify_error` handler slot + `llm_router.classify_error` helper; `request.model_name`; `embed.encoding_format`; `client:stream` returns `(resp, err)` with `on_response` hook; `llm_router.http_client` (renamed from `create_http_client`); `invalid_request` stops pool failover; manifest floor 0.1.1 |
| 0.1.2 | `payment_required` error type for upstream paywalls (status 402) |

## Responsibility split

The router owns orchestration; the plugin owns wire translation. Concretely:

- Router: credential pool order and single-pass iteration, token filtering,
  proxy pick order and rotation, exhausted skip filtering, stream first-byte
  gate, model cache, metrics, sandboxing, version gating, crash accounting.
- Plugin: request payload building, response normalization, error
  classification (via the helper below), model catalog mapping, and the
  limit semantics of its upstream expressed as error `scope`.

Plugins keep no limit state of their own: no quota tables in
`llm_router.storage`, no retry parsing duplicated per method. All of that
lives in `classify_error` plus `scope`.

## Registration

```lua
llm_router.register(type_key, {
  complete = function(ctx, credential, request) ... end,  -- required
  classify_error = function(raw, default_err) ... end,    -- optional
  transcribe = function(ctx, credential, request) ... end, -- optional
  ...
})
```

`complete` is required today. Optional handlers a plugin does not declare are
reported to the caller as "endpoint not supported" — loud, never a silent
fallback. `classify_error` absent means the core default decides alone.

### Common arguments

- `ctx` — `{ provider_config = {...} }` when the provider has config rows.
- `credential` — `{ id = "...", data = {...} }`; `data` holds the credential
  fields the user saved through `credential_schema`.
- Every `request` table carries `model` (full `provider/model` id) and
  `model_name` (bare name without the provider prefix, for upstream
  payloads). `stream` is absent by contract: streaming is served by
  `complete_stream`, never by a flag. Never forward a request table
  verbatim upstream: `model_name` is router-only and upstreams reject
  unknown properties.
- Handlers return two values: `(result, nil)` on success or `(nil, err)` on
  failure. `err` is the contract table
  `{ type = ..., message = ..., retry_after = ..., scope = ... }`:
  - `type`: `rate_limit` | `quota_exceeded` | `auth` | `upstream` | `timeout` | `invalid_request` | `geo` | `not_found` | `payment_required`
  - `message`: human string, required.
  - `payment_required`: the upstream paywall (subscription, credits, 402).
    Skip, never mark: the exhausted store ignores it like `auth`. Smoke
    harnesses treat it as a skip with reason, not a failure.
  - `retry_after`: optional unix timestamp; mandatory for `quota_exceeded`
    (defaults to now+60s when absent). A bare `rate_limit` without a hint
    also cools down for 60s.
  - `scope`: optional array naming the exhausted dimensions the error
    limits: any combination of `account`, `model`, `proxy`. The provider
    is always part of the key and needs no naming. Only `rate_limit` and
    `quota_exceeded` read scope; other types ignore it. Without scope a
    rate/quota error marks the full combination of the request. Unknown
    words reject the whole table (`PluginInternalError`).
  - `not_found`: the requested model does not exist upstream. The router
    drops the model from the info cache (best-effort) and returns the error
    as-is. Emit it only when the upstream names the model as missing — never
    for bad endpoints or malformed requests.
- The router calls once per key and never repeats: there are no repeat
  passes or backoff pauses in the request path. Key iteration lives inside
  the backend: the core tries the sorted credential pool in order, at most
  once per key, and returns the first success or the last error.
  `invalid_request` stops the pool after the first key: a malformed request
  is identical for every key. Streaming stops failover after the first byte
  reaches the client.
- Any other error form (raised errors, wrong shapes) becomes
  `PluginInternalError` and counts as a plugin crash.

## Handlers

### complete (required)

`complete(ctx, credential, request)` → ChatCompletionResponse table.

`request` mirrors the OpenAI chat completion body plus `model_name`. The
response must contain a non-empty `choices` array.

### complete_stream (optional)

`complete_stream(ctx, credential, request, emit)` → `nil, err`.

Calls `emit(chunk)` per SSE chunk; chunks follow the OpenAI stream shape. A
chunk carries `choices` or `usage` (usage-only final chunks are accepted);
anything else is rejected. When absent, the core emulates streaming from
`complete` output. Native streaming is required for non-SSE upstreams
(binary protocols), gate-compliant anonymous paths, and faithful
thought/tool deltas — the emulator only splits plain text.

### classify_error (optional)

`classify_error(raw, default_err)` → final error table or `nil`.

- `raw` — the untouched upstream failure: `{ status, headers, body }`
  (body is the raw string as received).
- `default_err` — the core verdict built from status plus the structured
  envelope code/type, or `nil` when the core does not take the case.
- Return the final contract table to override, or `nil` to accept the
  default. Returning anything else is a schema violation (plugin crash).
- The handler must be pure: no network, no storage. Calling
  `llm_router.classify_error` from inside the extension is rejected
  (no recursion).
- Put limit semantics here, once, instead of repeating them per method:
  adjust `type` and set `scope` where the upstream needs it. All request
  handlers then collapse to one line:
  `return nil, llm_router.classify_error({ status, headers, body })`.

### transcribe (optional, 0.0.5)

Serves `POST /v1/audio/transcriptions`.

`transcribe(ctx, credential, request)` → normalized transcription table.

Request table:

| Field | Type | Notes |
|---|---|---|
| `model` | string | full ModelId |
| `model_name` | string | bare name, for upstream payloads |
| `file` | string | raw audio bytes (Lua strings are byte arrays) |
| `file_name` | string | original upload filename |
| `content_type` | string | upload MIME type, `application/octet-stream` fallback |
| `language` | string? | ISO-639-1 |
| `prompt` | string? | style/vocabulary hint |
| `response_format` | string? | client's: `json` (default), `text`, `srt`, `verbose_json`, `vtt` |
| `temperature` | number? | |
| `timestamp_granularities` | array? | `word`, `segment` |
| `needs_segments` | boolean | true when the client asked for `srt`/`vtt` |

Return table (OpenAI `verbose_json` shape):

```lua
{
  text = "full transcript",          -- required key, may be empty
  language = "en",                   -- optional
  duration = 12.4,                   -- optional, seconds
  segments = {                       -- required when request.needs_segments
    { id = 0, start = 0.0, ["end"] = 1.5, text = "..." },
  },
  words = { { word = "hi", start = 0.0, ["end"] = 0.4 } }, -- optional
}
```

Rules:

- Always fetch the most detailed upstream format (`verbose_json` or native
  equivalent) and return the normalized table. The router renders the
  client's `response_format` from it: `json` → `{text}`, `text` → plain body,
  `srt`/`vtt` → rendered from `segments`, `verbose_json` → the table as-is.
- When `needs_segments` is true and the model cannot return segments, fail
  with `invalid_request` — the router refuses to emit an empty subtitle file.
- Upload size is capped at 32 MB at the router edge.

### speech (optional, 0.0.6)

Serves `POST /v1/audio/speech`.

`speech(ctx, credential, request)` → speech result table.

Request table:

| Field | Type | Notes |
|---|---|---|
| `model` | string | full ModelId |
| `model_name` | string | bare name, for upstream payloads |
| `input` | string | text to speak, non-empty |
| `voice` | string? | client's voice name |
| `response_format` | string? | client's: `mp3` (default), `opus`, `aac`, `flac`, `wav`, `pcm` |
| `speed` | number? | 0.25..4.0 |
| `instructions` | string? | style hint (OpenAI gpt-4o-mini-tts field) |

Return table:

```lua
{
  audio_b64 = "...",   -- required: base64-encoded audio bytes. The JSON
                       -- return path cannot carry raw bytes, so audio
                       -- crosses the boundary base64-encoded.
  format = "wav",      -- required: actual encoding of the bytes; one of the
                       -- response_format identifiers above.
}
```

Rules:

- The router does not transcode. When the upstream cannot produce the
  client's `response_format`, return the native format in `format` — the
  response Content-Type follows the returned format, not the request.
- Empty audio and invalid base64 are schema violations (plugin crash).

### generate_image (optional, 0.0.6)

Serves `POST /v1/images/generations`.

`generate_image(ctx, credential, request)` → OpenAI images table.

Request table:

| Field | Type | Notes |
|---|---|---|
| `model` | string | full ModelId |
| `model_name` | string | bare name, for upstream payloads |
| `prompt` | string | required, non-empty |
| `n` | number? | requested image count, 1..10 |
| `size` | string? | client's, e.g. `1024x1024` |
| `quality` | string? | client's, e.g. `standard`, `hd` |
| `style` | string? | client's, e.g. `vivid`, `natural` |
| `response_format` | string? | client's: `url` or `b64_json` |

Return table (OpenAI images shape):

```lua
{
  created = 1700000001,  -- optional, router fills now() when absent
  data = {               -- required, non-empty
    { b64_json = "...", revised_prompt = "..." },  -- or
    { url = "https://..." },
  },
}
```

Rules:

- Each `data` entry carries `b64_json` or `url` (or both). `revised_prompt`
  is optional.
- The client's `response_format` is a preference: an upstream that only
  yields base64 may return `b64_json` even when `url` was requested.

### embed (optional, 0.0.6)

Serves `POST /v1/embeddings`.

`embed(ctx, credential, request)` → embeddings table.

Request table:

| Field | Type | Notes |
|---|---|---|
| `model` | string | full ModelId |
| `model_name` | string | bare name, for upstream payloads |
| `input` | array of strings | one embedding per entry, order preserved |
| `encoding_format` | string? | client's: `float` (default) or `base64` |
| `dimensions` | number? | client's requested output dimensionality |

Return table:

```lua
{
  data = {
    { embedding = { 0.012, -0.34, ... } },  -- one entry per input string,
    { embedding = { ... } },                -- same order; float vectors
  },
  usage = { prompt_tokens = 7, total_tokens = 7 },  -- optional
}
```

Rules:

- Plugins always return float vectors. `encoding_format=base64` is rendered
  at the router edge (base64 float32-LE, the OpenAI wire form).
- `data` length must equal `input` length; indexes are assigned by the
  router. A short or empty result is a schema violation (plugin crash).
- `dimensions` is a request, not a guarantee: an upstream with fixed
  dimensionality returns its native size.

### get_model_infos (optional)

Returns an array of model cards. Endpoint routing uses the `endpoints` field:

```lua
{ name = "whisper-large-v3", display_name = "Whisper Large V3",
  endpoints = { "audio/transcriptions" }, ... }
{ name = "llama-3.3-70b", endpoints = { "chat/completions" }, ... }
```

Endpoint identifiers: `chat/completions`, `audio/transcriptions`,
`audio/speech`, `images/generations`, `embeddings`. A model
with no `endpoints` field serves chat/completions only. The router rejects
requests against a declared-but-absent endpoint with `endpoint_not_supported`.

Listing policy is the plugin's choice; all three deployed patterns are
acceptable: live fetch failing closed, live fetch with a hardcoded fallback
list, or a static list. Document which one a plugin uses in its
`@description`.

### Credential and UI handlers

`validate_credentials(data)` → `boolean, err?`. `needs_refresh(credential)` →
boolean. `refresh_credential(ctx, credential)` → new data table.
`config_schema()` / `credential_schema()` / `auth_initiate(ctx)` /
`auth_step(ctx, {action, values})` → UI node trees (see AGENTS.md §7 for node
kinds).

## llm_router API

| Function | Purpose |
|---|---|
| `llm_router.register(type_key, handlers)` | claim a provider type key |
| `llm_router.register_proxy_source(key, {fetch_proxies})` | claim a proxy list source |
| `llm_router.http_client({timeout_ms})` (0.1.1, renamed) | SSRF-guarded client: `request({...})` → `(resp, err)`, `stream({...})` → `(resp, err)` |
| `llm_router.classify_error({status, headers, body})` (0.1.1) | default classification, then the `classify_error` extension when declared; returns the final contract table |
| `llm_router.multipart(parts)` (0.0.5) | build a multipart/form-data body |
| `llm_router.base64_encode(s)` / `llm_router.base64_decode(s)` (0.0.6) | binary-safe base64 codec |
| `llm_router.storage` | per-plugin key-value storage |
| `llm_router.uuid_v5(namespace, name)` | RFC 4122 UUIDv5 |
| `llm_router.random_hex(nbytes)` | random hex string |
| `json.encode` / `json.decode` | JSON codec |

### llm_router.http_client({timeout_ms}) → client

Timeout in milliseconds, clamped to 1000..300000 (default 60000). One
timeout budget covers one attempt; every key attempt gets a fresh budget.

`client:request({method, url, headers, body})` → `(resp, err)`:

- `resp` — `{ status, headers, body }` with lowercased header names.
- `err` — transport failure only, `{ type = "upstream", message = ... }`.
  HTTP statuses arrive as data: the plugin classifies them itself.

`client:stream({method, url, headers, body, on_response?, on_line?, on_chunk?})`
→ `(resp, err)`:

- Exactly one of `on_line` / `on_chunk` is required.
- `resp` — `{ status, headers }` on success.
- `on_response(resp)` (optional) runs on the response head before the body
  streams: return an error table to abort with it, `nil` to continue. On
  error statuses the bounded body is buffered first, so the hook sees full
  context — `{status, headers, body}` — and classifies once, with no
  string re-parsing downstream. On success the hook sees `{status,
  headers}`. The canonical use is early classification:
  `on_response = function(r) if r.status ~= 200 then return
  llm_router.classify_error({ status = r.status, headers = r.headers,
  body = r.body or "" }) end end`.
- Without a hook decision, a non-2xx surfaces as `upstream`, never as a
  silent stream. Callback failures surface as `upstream` with the cause in
  the message. `on_response` must return a contract table or `nil`;
  anything else is a plugin crash.

### llm_router.classify_error({status, headers, body}) → err

Default classification, then the plugin `classify_error` extension when the
plugin declares one. `status` is required; `headers` is a lowercase-keyed
map; `body` is the raw string.

The default maps status plus the structured envelope code/type
(`{"error":{"code","type","message"}}`); message text only feeds quota
wording on bare 429s and the human message. `retry_after` resolves from the
`retry-after` header (delta seconds) or a `retry in N` hint in the body;
quota errors without any hint default to now+60s. Unknown shapes degrade to
`upstream` — the default never asserts `auth`, `geo` or `quota_exceeded` on
weak signals.

### llm_router.multipart(parts) → body, content_type

```lua
local body, ctype = llm_router.multipart({
  { name = "model", value = "whisper-large-v3" },                -- plain field
  { name = "file", filename = "a.wav",                            -- file field
    content_type = "audio/wav", data = request.file },
})
-- ctype = "multipart/form-data; boundary=..."; send body as the request body
```

Max 64 parts. `content_type` defaults to `application/octet-stream`. Header
metacharacters in names/filenames are stripped.

## Exhausted store (0.1.1)

Rate and quota outcomes record joint limit keys; later requests skip
combinations matching a stored key until its timestamp passes. Expired
entries delete on read.

A stored key is a filter over dimensions, not a set of per-entity marks: a
candidate combination is skipped when it matches **every** dimension the key
names. The provider (backend type key) and the plugin are always part of
every key and need no naming.

Examples — the plugin only names dimensions, the router resolves instances:

```lua
-- per-model quota (Gemini style): this model on this account cools down,
-- other models on the same account keep serving
err.scope = { "account", "model" }
-- per-IP limit (anonymous free tiers): every combination through this proxy
err.scope = { "proxy" }
-- per-account quota (Groq style): every combination of this account
err.scope = { "account" }
```

Behavior:

- Checks run where selection happens: credentials drop out of the pool
  before the token filter; proxies drop out of the pick list after ranking.
  When every credential is limited the full pool is kept as a last resort:
  a stale but unexpired mark never denies a request that could succeed.
  Manual proxy mode with nothing usable left fails loudly.
- `geo` marks nothing: the exit stays usable for other providers; persistent
  proxy state no longer exists.
- Keys never cross plugins or provider types: a limit for one provider
  never affects the others.

## Proxy pool

Provider HTTP (`http_client`) routes through the pooled proxy picked for
the calling provider. Pick = fastest proxy whose location is in the plugin
whitelist. Rotation walks untried picks on transport failure and never falls
back to direct: an empty list means direct was requested, an exhausted list
surfaces the last error.

Manifest tags:

```lua
--- @proxy_location US   -- repeatable whitelist, empty allows any location
--- @proxy_location GB
--- @proxy_default_option auto  -- disabled (default) | auto | manual
```

Pool rules (`RouterConfiguration`: `min_download_speed_kbps = 15000`,
`max_proxies_per_location = 10`, `update_interval_minutes = 15`):

* Two-stage probe, both legs through the proxy: CONNECT tunnel
  (`Ping`, handshake ms), then a 1MB download (`Speed`, kbit/s). Tunnel
  failure deletes at once; download failure deletes; a timed-out download
  still records the achieved speed.
* Dead proxies are deleted. Slow proxies (under the speed floor) are kept
  as fallback until their location fills, then displaced one-for-one by
  faster newcomers; rotation trims each location to the fastest N.
  Manual proxies are sacred: probed once on add, never rotated, deleted
  only by hand.
* Exit locations are verified by probing through the proxy on add; list
  metadata is only a fallback.
* Demand-driven fetch: request whitelists accumulate in `active_regions`
  (defaults `US, DE, NL, GB, FR, CA` always apply, observed entries expire
  after 48h). Scheduled fetch pauses while every demanded region holds N
  fast proxies and resumes on shortage; manual refresh short-circuits on a
  full pool. Rotation ticks on schedule regardless; source refresh fetches,
  adds, then rotates the whole pool. One rotation at a time, 64 probe
  workers.
* Source fetch covers a rotating window of 1500 candidates per fetch,
  probed in chunks of 150 while shortfall persists; a full pool costs zero
  probes.

`fetch_proxies` returns proxy candidates
`{ protocol, host, port, country }`; only probed-alive entries pool.

Source identity: declare a bare name in `register_proxy_source(name, ...)`
(the `^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$` pattern). The runtime qualifies it
per plugin as `<recordID>/<name>`: two different plugins may claim one name
and each serves its own list. The dashboard shows the bare name with the
qualified key beneath it.

## Manifest reference

Required: `@plugin`, `@author`, `@version`, `@router_version`, one or more
`@allow_host`. `@allow_host "*"` marks the plugin unsafe and must stand
alone. Removed directives (`@proxy_force_on_mismatch`) are tolerated on
install but carry no meaning. Unknown tags reject the install.

## Planned endpoints (not implemented)

None pending. Registration ignores handler names the router version does not
know, so declaring them early has no effect. Wait for the router version
that announces them.
