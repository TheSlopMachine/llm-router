# Plugin Contract

The binding contract between the router core and Lua provider plugins. Every
handler, argument table, return shape and error form listed here is enforced
by the core: schema violations become `PluginInternalError` and are recorded
as plugin crashes.

Router version: **0.1.2** (`models.CurrentVersion`). A plugin using a feature
declares the `@router_version` that introduced it; older routers refuse to
install it. Routers serve no contract older than **0.1.1**: plugins declaring
`0.0.x` fail install and need reissue.

`@version` and `@router_version` accept `MAJOR.MINOR[.PATCH]` (missing patch
means `.0`, one leading `v` allowed); anything else fails install.

## Version history

| Router | Adds |
|---|---|
| 0.0.4 | complete, complete_stream, validate_credentials, get_model_infos, needs_refresh, refresh_credential, config_schema, credential_schema, auth_initiate, auth_step; proxy sources; storage; uuid_v5; random_hex |
| 0.0.5 | `transcribe` handler, `llm_router.multipart`, `ModelInfo.endpoints` |
| 0.0.6 | `speech` handler, `generate_image` handler, `embed` handler, `llm_router.base64_encode/decode` |
| 0.0.7 | proxy pool rework: repeatable `@proxy_location` whitelist, `@proxy_default_option`, per-pair rate limits and blocks |
| 0.1.1 | unified exhausted store (`scope` on the error contract); `classify_error` handler slot + `llm_router.classify_error` helper; `request.model_name`; `embed.encoding_format`; `client:stream` returns `(resp, err)` with `on_response` hook; `llm_router.http_client` (renamed); `invalid_request` stops pool failover; manifest floor 0.1.1 |
| 0.1.2 | `payment_required` error type for upstream paywalls (status 402) |

## Responsibility split

The router owns orchestration; the plugin owns wire translation.

- Router: credential pool order and single-pass iteration, token filtering,
  proxy pick order and rotation, exhausted skip filtering, stream first-byte
  gate, model cache, metrics, sandboxing, version gating, crash accounting.
- Plugin: request payload building, response normalization, error
  classification (via the helper below), model catalog mapping, and the
  limit semantics of its upstream expressed as error `scope`.

Plugins keep no limit state of their own: no quota tables in
`llm_router.storage`, no retry parsing duplicated per method. All of that
lives in `classify_error` plus `scope`.

## Manifest reference

The header is the contiguous run of `---` lines at byte 0 of the file.
Unknown `@tags` fail install.

| Tag | Cardinality | Constraint |
|---|---|---|
| `@plugin` | required, once | non-empty display name |
| `@author` | required, once | non-empty |
| `@version` | required, once | valid semver |
| `@router_version` | required, once | valid semver, `>= 0.1.1` |
| `@allow_host` | required, repeatable | bare hostname; `*` marks the plugin unsafe and stands alone |
| `@description` | optional, once | free text |
| `@license` | optional, once | free text |
| `@proxy_location` | optional, repeatable | ISO country code, normalized and deduped |
| `@proxy_default_option` | optional, once | `disabled` \| `auto` \| `manual` |
| `@proxy_source` | optional, once | `true` (or bare) marks a proxy-list source |
| `@proxy_force_on_mismatch` | removed | tolerated on install, means nothing |

## Registration

```lua
llm_router.register(type_key, {
  complete = function(ctx, credential, request) ... end,  -- required
  classify_error = function(raw, default_err) ... end,    -- optional
  transcribe = function(ctx, credential, request) ... end, -- optional
  ...
})

llm_router.register_proxy_source(name, {
  fetch_proxies = function() ... end,  -- required
})
```

- `type_key` and source names: start alnum, `[A-Za-z0-9_.-]`, 1–64 chars.
- `complete` is required. Undeclared optional handlers report
  `endpoint_not_supported` — loud, never a silent fallback. Unknown handler
  names are ignored.
- `classify_error` absent means the core default decides alone.
- One bare source name may be claimed by several plugins; the runtime
  qualifies each as `<recordID>/<name>`.

## Handler slots

| Slot | Required | Args | Returns |
|---|---|---|---|
| `complete` | yes | `(ctx, credential, request)` | `(result, err)` |
| `complete_stream` | no | `(ctx, credential, request, emit)` | `(nil, err)` |
| `transcribe` | no | `(ctx, credential, request)` | `(result, err)` |
| `speech` | no | `(ctx, credential, request)` | `(result, err)` |
| `generate_image` | no | `(ctx, credential, request)` | `(result, err)` |
| `embed` | no | `(ctx, credential, request)` | `(result, err)` |
| `classify_error` | no | `(raw, default_err)` | `err table` or `nil` |
| `validate_credentials` | no | `(data)` | `(boolean, err?)` |
| `get_model_infos` | no | `(ctx, credential, provider_config)` | `(array, err)` |
| `needs_refresh` | no | `(credential)` | `boolean` |
| `refresh_credential` | no | `(ctx, credential)` | `(data table, err)` |
| `config_schema` | no | `()` | `array?` |
| `credential_schema` | no | `()` | `array?` |
| `auth_initiate` | no | `(ctx)` | `auth result` |
| `auth_step` | no | `(ctx, {action, values})` | `auth result` |
| `fetch_proxies` | no | `()` | `array?` |

### Common arguments

- `ctx` — `{ provider_config = {...} }` when the provider has config rows.
  Only auth handlers also carry `flow_id`.
- `credential` — `{ id = "...", data = {...} }`; `data` holds the fields
  saved through `credential_schema`. Unpinned calls pass `{ id = "", data = {} }`.
- Every `request` table carries `model` (full `provider/model` id) and
  `model_name` (bare name for upstream payloads). `stream` is absent by
  contract: streaming is served by `complete_stream`, never by a flag.
  Never forward a request table verbatim upstream: `model_name` is
  router-only and upstreams reject unknown properties.
- Request handlers return `(result, nil)` or `(nil, err)`; `nil` result is
  always a schema violation.

### Error contract

`err` is `{ type = ..., message = ..., retry_after = ..., scope = ... }`:

- `type`: `rate_limit` | `quota_exceeded` | `auth` | `upstream` | `timeout` | `invalid_request` | `geo` | `not_found` | `payment_required`
- `message`: human string, required.
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
- `payment_required`: the upstream paywall (subscription, credits, 402).
  Skip, never mark: the exhausted store ignores it like `auth`.

Pool semantics: the core tries the sorted credential pool in order, at most
once per key, and returns the first success or the last error.
`invalid_request` stops the pool after the first key. Streaming stops
failover after the first byte reaches the client. Any other error form
(raised errors, wrong shapes) becomes `PluginInternalError` and counts as
a plugin crash.

### Request handlers

`complete(ctx, credential, request)` → ChatCompletionResponse table.
`request` mirrors the OpenAI chat completion body plus `model_name`. The
response needs a non-empty `choices` array.

`complete_stream(ctx, credential, request, emit)` → `nil, err`. Calls
`emit(chunk)` per chunk; each chunk carries `choices` or `usage`
(usage-only final chunks pass, anything else raises a plugin crash). When
absent, the core emulates streaming from `complete` output. Native
streaming is required for non-SSE upstreams (binary protocols),
gate-compliant anonymous paths, and faithful thought/tool deltas — the
emulator only splits plain text.

`transcribe(ctx, credential, request)` → normalized transcription table:

| Field | Type | Notes |
|---|---|---|
| `model` / `model_name` | string | full id / bare name |
| `file` | string | raw audio bytes (Lua strings are byte arrays) |
| `file_name` | string | original upload filename |
| `content_type` | string | upload MIME type, `application/octet-stream` fallback |
| `language` | string? | ISO-639-1, when set |
| `prompt` | string? | style/vocabulary hint, when set |
| `response_format` | string? | client's: `json` (default), `text`, `srt`, `verbose_json`, `vtt` |
| `temperature` | number? | when set |
| `timestamp_granularities` | array? | `word`, `segment`, when non-empty |
| `needs_segments` | boolean | always present; true when the client asked for `srt`/`vtt` |

Returns the OpenAI `verbose_json` shape (`text` required, `language` /
`duration` optional, `segments` required when `needs_segments`, `words`
optional). Always fetch the most detailed upstream format; the router
renders the client's `response_format` from it. When `needs_segments` is
true and the model cannot return segments, fail with `invalid_request`.
Upload size is capped at 32 MB at the router edge.

`speech(ctx, credential, request)` → `{ audio_b64, format }`:

| Field | Type | Notes |
|---|---|---|
| `model` / `model_name` | string | full id / bare name |
| `input` | string | text to speak, non-empty |
| `voice` | string? | client's voice name, when set |
| `response_format` | string? | `mp3` (default), `opus`, `aac`, `flac`, `wav`, `pcm` |
| `speed` | number? | 0.25..4.0, when set |
| `instructions` | string? | style hint, when set |

Audio crosses the boundary base64-encoded and non-empty (both violations
are plugin crashes). The router does not transcode: return the native
format in `format`; the response Content-Type follows it, not the request.

`generate_image(ctx, credential, request)` → OpenAI images table:

| Field | Type | Notes |
|---|---|---|
| `model` / `model_name` | string | full id / bare name |
| `prompt` | string | required, non-empty |
| `n` | number? | 1..10, when positive |
| `size` / `quality` / `style` | string? | client's values, when set |
| `response_format` | string? | `url` or `b64_json` |

Each `data` entry carries `b64_json` or `url` (or both); empty `data` is
a plugin crash. The client's `response_format` is a preference: base64-only
upstreams may return `b64_json` for a `url` request. `created` defaults to
now when absent.

`embed(ctx, credential, request)` → embeddings table:

| Field | Type | Notes |
|---|---|---|
| `model` / `model_name` | string | full id / bare name |
| `input` | array of strings | one embedding per entry, order preserved |
| `encoding_format` | string? | `float` (default) or `base64` |
| `dimensions` | number? | requested dimensionality, when positive |

Return float vectors, one per input string in order; short results and
empty vectors are plugin crashes, indexes are assigned by the router.
`encoding_format=base64` renders at the edge (base64 float32-LE).
`dimensions` is a request, not a guarantee.

### classify_error (optional)

`classify_error(raw, default_err)` → final error table or `nil`.

- `raw` — the untouched upstream failure: `{ status, headers, body }`
  (body is the raw string as received).
- `default_err` — the core verdict, always present: `{ type, message,
  retry_after?, scope? }`.
- Return the final contract table to override, or `nil` to accept the
  default. Returning anything else is a schema violation (plugin crash).
- The handler must be pure: no network, no storage. Calling
  `llm_router.classify_error` from inside the extension is rejected
  (no recursion).
- Put limit semantics here, once, instead of repeating them per method:
  adjust `type` and set `scope` where the upstream needs it. All request
  handlers then collapse to one line:
  `return nil, llm_router.classify_error({ status, headers, body })`.

### Credential handlers

- `validate_credentials(data)` → `(boolean, err?)`. `false` without error
  means rejected (`invalid_request`); `true` with an error surfaces the
  error; non-boolean returns crash. Absent means accept everything.
- `needs_refresh(credential)` → `boolean` (`nil` counts as false; anything
  else crashes). Absent means never.
- `refresh_credential(ctx, credential)` → `(data table, err)`. Absent means
  not refreshable.
- `config_schema()` / `credential_schema()` → UI node array or `nil`.
  Absent means raw JSON editing in the dashboard.

### Auth handlers

- `auth_initiate(ctx)` → auth result. `auth_step(ctx, {action, values})`
  → auth result. `ctx` carries `flow_id`; storage scopes per flow under
  `"auth_flow:" .. flow_id` by convention.
- Auth result is exactly one of: `{ render = <UI tree> }`,
  `{ redirect_url = "<non-empty>" }`, `{ credentials = {<string fields>} }`.
  Anything else crashes. Absent handlers mean single-step manual token entry.

### get_model_infos (optional)

`get_model_infos(ctx, credential, provider_config)` — note `provider_config`
arrives as the third argument here, not inside `ctx`. Returns an array of
model cards (empty array, never nil):

| Field | Type | Notes |
|---|---|---|
| `name` | string | upstream model id, required |
| `display_name` | string | falls back to `name` |
| `description` | string? | |
| `rpm` / `tpm` / `rpd` | number | rate estimates, 0 when unknown |
| `context_window` / `max_tokens` | number? | |
| `capabilities` | array? | explicit wins; else derived (`tools` from `supported_parameters`, `json_mode` from `response_format`, `reasoning` flag) |
| `input_modalities` / `output_modalities` | array? | e.g. `text`, `image`, `audio` |
| `supported_parameters` | array? | OpenAI parameter names the model accepts |
| `reasoning` | table? | `{ supported_efforts, default_effort, default_enabled }` |
| `endpoints` | array? | `chat/completions`, `audio/transcriptions`, `audio/speech`, `images/generations`, `embeddings`; empty means chat-only |

The router rejects requests against a declared-but-absent endpoint with
`endpoint_not_supported`. Listing policy is the plugin's choice: live fetch
failing closed, live fetch with a hardcoded fallback list, or a static
list. Document the choice in `@description`.

### fetch_proxies (proxy sources)

`fetch_proxies()` — no arguments. Returns an array of `{ protocol, host,
port, country }` (`protocol` is `http`/`https`/`socks4`/`socks5`, `port` a
number) or `nil` (means none). Only probed-alive entries pool.

## llm_router API

| Function | Purpose |
|---|---|
| `llm_router.register(type_key, handlers)` | claim a provider type key |
| `llm_router.register_proxy_source(key, {fetch_proxies})` | claim a proxy list source |
| `llm_router.http_client({timeout_ms})` | SSRF-guarded client: `request({...})` → `(resp, err)`, `stream({...})` → `(resp, err)` |
| `llm_router.classify_error({status, headers, body})` | default classification, then the `classify_error` extension when declared; returns the final contract table |
| `llm_router.multipart(parts)` | build a multipart/form-data body |
| `llm_router.base64_encode(s)` / `llm_router.base64_decode(s)` | binary-safe base64 codec |
| `llm_router.storage` | per-plugin key-value storage, no quotas |
| `llm_router.uuid_v5(namespace, name)` | RFC 4122 UUIDv5 |
| `llm_router.random_hex(nbytes)` | 1..1024 bytes as hex string |
| `json.encode` / `json.decode` | JSON codec |

### llm_router.http_client({timeout_ms}) → client

Timeout in milliseconds, clamped to 1000..300000 (default 60000). One
timeout budget covers one attempt, including whole streams; every key
attempt gets a fresh budget.

`client:request({method, url, headers, body})` → `(resp, err)`:

- `resp` — `{ status, headers, body }` with lowercased header names. Bodies
  truncate silently at 16MiB.
- `err` — transport failure only, `{ type = "upstream", message = ... }`.
  HTTP statuses arrive as data: the plugin classifies them itself.

`client:stream({method, url, headers, body, on_response?, on_line?, on_chunk?})`
→ `(resp, err)`:

- Exactly one of `on_line` / `on_chunk` is required; with both,
  `on_chunk` wins. Lines over 1MiB fail the stream; chunks stream in
  32KiB slices.
- `resp` — `{ status, headers }` on success.
- `on_response(resp)` (optional) runs on the response head before the body
  streams: return an error table to abort with it, `nil` to continue. On
  error statuses the bounded body (64KiB cap) is buffered first, so the
  hook sees full context — `{status, headers, body}` — and classifies once,
  with no string re-parsing downstream. On success the hook sees `{status,
  headers}`. The canonical use is early classification:
  `on_response = function(r) if r.status ~= 200 then return
  llm_router.classify_error({ status = r.status, headers = r.headers,
  body = r.body or "" }) end end`.
- Without a hook decision, a non-2xx surfaces as `upstream`, never as a
  silent stream. Callback failures surface as `upstream` with the cause in
  the message. `on_response` must return a contract table or `nil`;
  anything else is a plugin crash.
- Header tables accept string values; non-string entries drop silently.
  Custom `Host` headers validate against the allow-list like URLs do.

SSRF guard: only `http`/`https` URLs; private and link-local addresses
never dial (even with wildcard `*`); direct dials pin the resolved IP
(closing DNS rebinding), proxied requests check hostnames with DNS at the
proxy; at most 10 redirects, each revalidated.

### llm_router.classify_error({status, headers, body}) → err

Default classification, then the plugin `classify_error` extension when the
plugin declares one. `status` is required (positive number); `headers` is
an optional lowercase-keyed map; `body` is the raw string (default `""`).

The default maps status plus the structured envelope code/type
(`{"error":{"code","type","message"}}`); message text only feeds quota
wording on bare 429s (`per day`, `perday`, `daily`, `quota`, `free_tier`,
`free tier`, `billing`) and the human message. `retry_after` resolves from
the `retry-after` header (delta seconds) or a `retry in N` hint in the body
(seconds, or milliseconds with `ms`); quota errors without any hint default
to now+60s. Unknown shapes degrade to `upstream` — the default never
asserts `auth`, `geo`, `quota_exceeded` or `payment_required` on weak
signals.

### llm_router.multipart(parts) → body, content_type

```lua
local body, ctype = llm_router.multipart({
  { name = "model", value = "whisper-large-v3" },                -- plain field
  { name = "file", filename = "a.wav",                            -- file field
    content_type = "audio/wav", data = request.file },
})
-- ctype = "multipart/form-data; boundary=..."; send body as the request body
```

1..64 parts (empty arrays rejected). Each part needs `name` plus `value`
or `data` (both strings); `filename` defaults to `""`, `content_type` to
`application/octet-stream`. Header metacharacters in names/filenames are
stripped.

### llm_router.storage → set/get/delete

`storage.set(scope, key, value)` → `true`. `storage.get(scope, key)` →
value or `nil` on miss. `storage.delete(scope, key)` → `true` (missing
keys still succeed). Scopes and keys are namespaced per plugin; blank ones
are rejected. Values are JSON (no functions, no sparse or mixed tables).
No quotas enforced: no size caps, no TTL, no eviction — prune what is stored.

### Small utilities

- `llm_router.uuid_v5(namespace, name)` — RFC 4122 UUIDv5; the namespace
  is a UUID string.
- `llm_router.random_hex(nbytes)` — `crypto/rand` hex, `nbytes` 1..1024.
- `json.encode` / `json.decode` — JSON codec (Lua strings carry raw bytes).

## UI trees

`config_schema`, `credential_schema`, `auth_initiate` and `auth_step` return
UI node arrays rendered by `DynamicForm`. Node kinds (15):

- Leafs: `text`, `input`, `select`, `checkbox`, `button`, `link`,
  `banner`, `secret`, `code`.
- Containers: `group`, `flow`, `grid`, `section`, `spacer`, `divider`.

Key validations: `input`/`select`/`checkbox`/`secret` need `name`;
`input.input_type` is `text`/`password`/`number`; `select.option_labels`
must subset `options`; `link` needs `url`; `button` defaults
`form_action="submit"`, `variant` is `primary`/`secondary`/`danger`;
`banner.variant` is `info`/`error`/`success`; `section` needs `title`;
`code` needs `text`; containers need non-empty `content` (`flow.direction`
`horizontal`/`vertical`, `grid.columns` 1..6). Trees cap at depth 8 and 200
nodes. Buttons render in host-owned footers, never inline. No raw HTML from
plugins, ever — new widgets ship as first-class node kinds, not markup.

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

Provider proxy mode lives in the provider config (`proxy: { mode, ids? }`,
`disabled` default, `manual` takes explicit proxy IDs) and reaches handlers
as `ctx.provider_config`. Manifest tags:

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
