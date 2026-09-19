# Plugin Contract

The binding contract between the router core and Lua provider plugins. Every
handler, argument table, return shape and error form listed here is enforced
by the core: schema violations become `PluginInternalError` and are recorded
as plugin crashes.

Router version: **0.0.6** (`models.CurrentVersion`). A plugin using a feature
must declare the `@router_version` that introduced it; older routers refuse
to install it.

## Version history

| Router | Adds |
|---|---|
| 0.0.4 | complete, complete_stream, validate_credentials, get_model_infos, needs_refresh, refresh_credential, config_schema, credential_schema, auth_initiate, auth_step; proxy sources; storage; uuid_v5; random_hex |
| 0.0.5 | `transcribe` handler, `llm_router.multipart`, `ModelInfo.endpoints` |
| 0.0.6 | `speech` handler, `generate_image` handler, `embed` handler, `llm_router.base64_encode/decode` |

## Registration

```lua
llm_router.register(type_key, {
  complete = function(ctx, credential, request) ... end,  -- required
  transcribe = function(ctx, credential, request) ... end, -- optional
  ...
})
```

`complete` is required today. Optional handlers a plugin does not declare are
reported to the caller as "endpoint not supported" — loud, never a silent
fallback.

### Common arguments

- `ctx` — `{ provider_config = {...} }` when the provider has config rows.
- `credential` — `{ id = "...", data = {...} }`; `data` holds the credential
  fields the user saved through `credential_schema`.
- Handlers return two values: `(result, nil)` on success or `(nil, err)` on
  failure. `err` is the contract table `{ type = ..., message = ..., retry_after = ... }`:
  - `type`: `rate_limit` | `quota_exceeded` | `auth` | `upstream` | `timeout` | `invalid_request` | `geo`
  - `retry_after`: optional unix timestamp; mandatory for `quota_exceeded`
    (defaults to now+60s when absent)
- The type decides what the retry engine does. `rate_limit`,
  `quota_exceeded`, `upstream` (transient 5xx/overload) and `timeout` move to
  the next candidate — another credential, and inside a virtual model the next
  fall-through model. `auth`, `invalid_request` and `geo` are terminal:
  `auth`/`invalid_request` fail the request, `geo` marks the proxy bad.
- Any other error form (raised errors, wrong shapes) becomes
  `PluginInternalError` and counts as a plugin crash.

## Handlers

### complete (required)

`complete(ctx, credential, request)` → ChatCompletionResponse table.

`request` mirrors the OpenAI chat completion body. `stream` is absent by
contract: streaming is served by `complete_stream`, never by a flag. The
response must contain a non-empty `choices` array.

### complete_stream (optional)

`complete_stream(ctx, credential, request, emit)` → `nil, err`.

Calls `emit(chunk)` per SSE chunk; chunks follow the OpenAI stream shape.
When absent, the core emulates streaming from `complete` output.

### transcribe (optional, 0.0.5)

Serves `POST /v1/audio/transcriptions`.

`transcribe(ctx, credential, request)` → normalized transcription table.

Request table:

| Field | Type | Notes |
|---|---|---|
| `model` | string | full ModelId, strip the provider prefix as usual |
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
| `model` | string | full ModelId, strip the provider prefix as usual |
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
| `model` | string | full ModelId, strip the provider prefix as usual |
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
| `model` | string | full ModelId, strip the provider prefix as usual |
| `input` | array of strings | one embedding per entry, order preserved |
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
{ name = "whisper-large-v3", display_name = "Whisper Large v3",
  endpoints = { "audio/transcriptions" }, ... }
{ name = "llama-3.3-70b", endpoints = { "chat/completions" }, ... }
```

Endpoint identifiers: `chat/completions`, `audio/transcriptions`,
`audio/speech`, `images/generations`, `embeddings`. A model
with no `endpoints` field serves chat/completions only. The router rejects
requests against a declared-but-absent endpoint with `endpoint_not_supported`.

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
| `llm_router.create_http_client({timeout_ms})` | SSRF-guarded client: `request({method,url,headers,body})` → `(resp, err)`, `stream({...on_line/on_chunk})` → `err?` |
| `llm_router.multipart(parts)` (0.0.5) | build a multipart/form-data body |
| `llm_router.base64_encode(s)` / `llm_router.base64_decode(s)` (0.0.6) | binary-safe base64 codec |
| `llm_router.storage` | per-plugin key-value storage |
| `llm_router.uuid_v5(namespace, name)` | RFC 4122 UUIDv5 |
| `llm_router.random_hex(nbytes)` | random hex string |
| `json.encode` / `json.decode` | JSON codec |

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

## Planned endpoints (not implemented)

None pending. Registration ignores handler names the router version does not
know, so declaring them early has no effect. Wait for the router version
that announces them.
