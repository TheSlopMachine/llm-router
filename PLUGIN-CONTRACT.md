# Plugin Contract

The binding contract between the router core and Lua provider plugins. Every
handler, argument table, return shape and error form listed here is enforced
by the core: schema violations become `PluginInternalError` and are recorded
as plugin crashes.

Router version: **0.0.5** (`models.CurrentVersion`). A plugin using a feature
must declare the `@router_version` that introduced it; older routers refuse
to install it.

## Version history

| Router | Adds |
|---|---|
| 0.0.4 | complete, complete_stream, validate_credentials, get_model_infos, needs_refresh, refresh_credential, config_schema, credential_schema, auth_initiate, auth_step; proxy sources; storage; uuid_v5; random_hex |
| 0.0.5 | `transcribe` handler, `llm_router.multipart`, `ModelInfo.endpoints` |

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

### get_model_infos (optional)

Returns an array of model cards. Endpoint routing uses the `endpoints` field:

```lua
{ name = "whisper-large-v3", display_name = "Whisper Large v3",
  endpoints = { "audio/transcriptions" }, ... }
{ name = "llama-3.3-70b", endpoints = { "chat/completions" }, ... }
```

Endpoint identifiers: `chat/completions`, `audio/transcriptions`. A model
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

`speech` (TTS), `embed` (embeddings), `generate_image`. Registration ignores
handler names the router version does not know, so declaring them early has
no effect. Wait for the router version that announces them.
