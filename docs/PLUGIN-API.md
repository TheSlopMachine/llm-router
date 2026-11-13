# Plugin Contract

The binding contract between the router core and Lua provider plugins. Every
handler, argument table, return shape and error form listed here is enforced
by the core: schema violations become `PluginInternalError` and are recorded
as plugin crashes.

Plugin API: **1.7** (`luaplugin.PluginAPIVersion`). A plugin declares the
`@plugin_api` contract it speaks; the router label (`main.RouterVersion`,
stamped from the release tag) carries no compatibility meaning. Install,
rollback, and the startup registry compare majors only: plugin major above
the served one fails with `too_new`, below fails with `too_old`, a missing
tag fails with `no_api_version`. A rejected install or rollback writes
nothing; startup skips incompatible rows with a warning while keeping them
inventoried. Minor is informational for plugin maintainers.

`@version` accepts `MAJOR.MINOR[.PATCH]` (missing patch means `.0`, one
leading `v` allowed); `@plugin_api` accepts `MAJOR.MINOR[.PATCH]` in the same
spelling. Anything else fails install. Neither `@plugin` nor `@author` may
contain a `/` character: the pair forms the plugin identity `author/plugin`
(each part sanitized: runs of characters outside `A-Z a-z 0-9 - _ .` become
one `-`, leading and trailing `-` are trimmed), and installation is
interchangeable across sources. Installs from any store or a file upload with
the same `@author`/`@plugin` pair override the previous installation and
append it to the rollback history (the last 10 versions are kept). The install
core replaces the row on every install, whatever the version; `make
upload-plugins` skips a file whose `@version` equals the installed one. A
plugin file is at most 1 MiB. A type key already served by a different plugin
fails the install.

## Table of Contents

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
- [Provider proxy configuration](#provider-proxy-configuration)
- [fetch_proxies](#fetch_proxies)
- [credentials](#credentials)
- [storage](#storage)
- [log and print](#log-and-print)
- [Logging convention](#logging-convention)
- [Utilities](#utilities)
- [Schemas UI nodes locales static metadata](#schemas-ui-nodes-locales-static-metadata)
- [Sandbox limits](#sandbox-limits)
- [Worked example](#worked-example)

## Responsibility split

The plugin owns protocol translation and orchestration; the router owns routing,
authorization, hosting and rendering.

- Router: `ModelId` parse/resolve/disabled/model-override/endpoint gates,
  token allow-list gate over credential identifiers, proxy-pool hosting (free pool,
  custom pools, adaptive refresh, `MarkDead` from transport faults), model cache,
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
The block ends at the first non-`---` line. A `---` line whose text does not
start with `@` is ignored. Carriage returns normalize to line feeds. Unknown
tags fail install. Duplicate single-value tags fail
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
| `@plugin` | required, once | Display name. Non-empty string. Must not contain `/`. Forms the plugin identity with `@author`. |
| `@author` | required, once | Author name. Non-empty string. Must not contain `/`. Forms the plugin identity with `@plugin`. |
| `@version` | required, once | Plugin version. Format `MAJOR.MINOR[.PATCH]`, leading `v` allowed, missing patch means `.0`. Every content edit bumps this value: the dashboard offers an update only when the store version is greater than the installed one, and `make upload-plugins` skips an equal version. |
| `@plugin_api` | required, once | Contract declaration. Format `x.y[.z]`, leading `v` allowed. Major must equal served major `1`. |
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
and again on every handler call, because each call loads the source into a
pooled state before invoking the handler. Top-level code calls
`llm_router.register` once per type key and `llm_router.register_proxy_source`
once per proxy source, at least one call in total; a file without a call
fails install. Handler bodies do not run during dry-run. Dry-run validates
handlers, icon, model specifications, health cooldown, schemas, locales, and
jobs. The `credentials` and `proxies` tables are absent during dry-run.

The plugin catalog runs the same dry-run on repository files that are not
installed, to read their subsystems and icon. That run has no `storage` and no
log sink, and stops after 5 seconds with an error.

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
| `type_key` | Backend identifier. Must match `^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`. Must be unique within the file and across installed plugins. |
| `handlers.complete` | Required function for provider backends. |
| Optional handlers | `complete_stream`, `transcribe`, `speech`, `generate_image`, `embed`, `moderate`, `generate_video`, `poll_video`, `video_content`, `validate_credentials`, `get_model_infos`, `check_health`, `check_model`, `auth_initiate`, `auth_step`, `initialize`, `deinitialize`. Each must be a function when present. Unknown handler names are ignored. |
| Static tables | `credential_schema`, `config_schema`, `settings_schema`, `proxy_schema`, `locales`, `jobs`, `model_specs`. Each must be a table when present. |
| Optional values | `icon`, `healthcheck_cooldown`. See static metadata section. |
| `source.fetch_proxies` | Required function for proxy sources. |
| `source.icon` | Optional, same rules as the `icon` value. |

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
| `fetch_proxies` | yes, for `register_proxy_source` | `()` | `array?` |
| `initialize` | no | `(ctx)` | `nil`/`true`, or terminal error table |
| `deinitialize` | no | `(ctx)` | `nil`/`true`, or terminal error table |

Undeclared optional handlers report `endpoint_not_supported`. Unknown handler
names are ignored.

## Common arguments

### `ctx`

High-level description: first argument to every handler and to job `run`
functions, except `validate_credentials` and `fetch_proxies`, which take no
`ctx`. Only the fields listed below ever appear.

API signature:

```lua
--- @param ctx table: execution context.
complete = function(ctx, request) ... end
```

Parameters:

| Field | Description |
|---|---|
| `provider_config` | Table with provider configuration. Present when the configuration is non-empty. Absent in lifecycle hooks other than provider `disable`. |
| `flow_id` | String. Present for authentication flows only. |
| `reason` | String. Present in lifecycle hooks only: `install`, `update`, `startup`, `enable` (initialize) and `delete`, `update`, `shutdown`, `disable` (deinitialize). Absent in jobs and request handlers. |
| `provider_id` | String. Present on provider disable deinitialization only. |
| `job_name` | String. Present in job execution. |
| `run_reason` | String. Present in job execution only: `tick`, `startup`, `manual`. Jobs carry no `reason` field. |

### `credential`

High-level description: credential record passed to health checks. The same
record shape is returned by `credentials.list` and `credentials.get`.

API signature:

```lua
--- @param credential table: credential record.
check_health = function(ctx, credential) ... end
```

Parameters:

| Field | Description |
|---|---|
| `id` | Credential identifier string. |
| `label` | Dashboard name of the key. |
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
`provider/model` identifier) and `model_name` (bare name for upstream
payloads). Chat requests (`complete`, `complete_stream`) also carry
`cache_key`, an opaque cross-turn prefix-cache partition derived from model,
first message and sorted tool names (`pck_` plus 32 hex digits;
`"prefix-boot"` for empty histories). `stream` is absent by contract:
streaming is served by `complete_stream`, never by a flag. Never forward a
request table verbatim upstream: `model_name` and `cache_key` are router-only
and upstreams reject unknown properties. Optional fields are absent when the
client did not set them (chat `max_tokens`, `temperature` and `top_p` are
also absent when zero).

### Return idiom

High-level description: request handlers return `(result, nil)` or
`(nil, call_error)`; `nil` result is always a schema violation. Streams must
not fail over after the first `emit` call: the stream belongs to that attempt.

## Chat handlers

### `complete`

High-level description: serves unary chat completions. Required handler.
The router overwrites the response `model` (and the `model` of every
`complete_stream` chunk, and of `embed` responses) with the routed id
`<provider>/<model>`: clients always see the model that answered in router
form, so plugins may return the bare upstream name or leave it empty.

API signature:

```lua
--- Serve unary chat completion.
-- @param ctx table: execution context with optional provider_config.
-- @param request table: chat completion request without stream flag, with model, model_name, cache_key, messages, tools and the optional fields below.
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
| `request.messages` | Message array of `{role, content, tool_calls, tool_call_id, name, refusal, reasoning_content}`; `tool_calls` entries are `{id, type, function={name, arguments}}` with `arguments` a JSON string. `content` is a string or an OpenAI part array: `{type="text", text}`, `{type="image_url", image_url={url, detail}}`, `{type="input_audio", input_audio={data, format}}`, `{type="file", file={file_data, filename}}`. `image_url.url` is a remote `http(s)` URL or a `data:` URL, `input_audio.data` is base64 (`wav` or `mp3`), `file_data` is always a base64 `data:` URL; any number of file parts may appear. Responses `input_image`/`input_file` and Anthropic `document` blocks arrive in the same shapes. |
| `request.tools` | Tool array when present. Entries are `{type, function={name, description, parameters, strict}}` or the flat form `{type, name, description, parameters, input_schema, strict}`. |
| Sampling and limits | `tool_choice`, `parallel_tool_calls`, `max_tokens`, `max_completion_tokens`, `temperature`, `top_p`, `n`, `stop` (string or string array), `seed`, `frequency_penalty`, `presence_penalty`, `logprobs`, `top_logprobs`, `response_format`, `user`, `service_tier`, `reasoning_effort`, `verbosity`, `stream_options`. |
| Client aliases | `random_seed`, `options`, `format`, `think`, `keep_alive`, `reasoning` (`{effort, max_tokens, exclude, enabled}`), `prompt_cache_key`, `guardrails` pass through when the client sent them. The `/v1/chat/completions` edge folds `random_seed`, `reasoning.effort`, `reasoning.max_tokens` (into `max_completion_tokens`), `format` and the Ollama `options` keys `temperature`, `top_p`, `seed`, `stop`, `num_predict` into the canonical fields above before the handler runs; canonical fields win. |

Return values: `ChatCompletionResponse`: `{id, object, created, model,
choices, usage, system_fingerprint, service_tier}`. Each choice is `{index,
message, finish_reason}` with `message` as `{role, content, tool_calls,
reasoning_content}` (`reasoning` is accepted as an alias of
`reasoning_content`); `usage` is `{prompt_tokens, completion_tokens,
total_tokens, prompt_tokens_details={cached_tokens, audio_tokens},
completion_tokens_details={reasoning_tokens, audio_tokens}}`. The `choices`
array must be non-empty. Second slot is `nil` on success, terminal error
table on failure.

Image (`image_url`), audio (`input_audio`) and file parts reach a plugin
only when its model card lists `image`, `audio` or `file` respectively in
`input_modalities`, or lists no input modalities at all: a card that
declares modalities without the one a part needs makes the router answer
400 `invalid_request_error` before the handler runs. A card must therefore
list every input the model really takes. Models absent from the model cache
pass the gate. Model card fields are listed under `get_model_infos`.

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
| `emit` | Function accepting one chunk table `{id, object, created, model, choices, usage}` with `choices` entries `{index, delta, finish_reason}`. Each chunk must contain non-empty `choices` or `usage`; an invalid chunk or a failed client write raises a Lua error. Each call writes `data: <canonical-json>\n\n` and sets delivery flag. An empty `choices` table encodes as `[]`. |

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
-- @param request table: model, model_name, task (transcribe|translate), file raw string, file_name, content_type, language, prompt, response_format, temperature, timestamp_granularities array, needs_segments boolean.
-- @return table: TranscriptionResponse.
transcribe = function(ctx, request) return result, nil end
```

Parameters: `model` full identifier, `model_name` bare name, `task` (`transcribe`
or `translate`; defaults to `transcribe` for older callers), `file` raw bytes
string, `file_name` string, `content_type` string, `language` optional string,
`prompt` optional string, `response_format` optional string, `temperature`
optional number, `timestamp_granularities` optional string array,
`needs_segments` boolean (true when `response_format` is `srt` or `vtt`).
The router validates `audio/transcriptions` and `audio/translations` separately
against each model's declared `endpoints` before calling this handler.

Return values: transcription response table in the OpenAI `verbose_json`
shape: `{text, language, duration, segments, words, task}`. `segments` entries
are `{id, seek, start, end, text, tokens, temperature, avg_logprob,
compression_ratio, no_speech_prob}`, `words` entries `{word, start, end}`. The
router renders the client-facing `response_format` from these fields.

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

Return values: table with `audio_b64` base64 audio (standard alphabet,
non-empty after decoding; otherwise a schema violation) and `format` string
(`mp3`, `opus`, `aac`, `flac`, `wav`, `pcm`).

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

Return values: image response `{created, data}` with non-empty `data`; each
entry is `{url, b64_json, revised_prompt}`.

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

Return values: embeddings response `{data, model, usage}` where length of
`data` equals length of `input` and each entry is `{index, embedding}` with
`embedding` a non-empty array of numbers. The router renumbers `index` from 0.

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
`input`. Each result is `{flagged, categories, category_scores}`; missing
`categories` and `category_scores` become empty maps, and the router fills an
empty `id` with `modr_router` and an empty `model` with the routed id.

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
`input_references` optional array of `{type, image_url={url},
audio_url={url}, video_url={url}}`, `previous_job_id` optional string,
`callback_url` storage only, `provider` optional table `{options}` carrying
the provider options, `user` optional string, `session_id` optional string.
`poll_video` takes `job_id` (the upstream job identifier). `video_content`
takes `job_id` and `index` (number).

Return values: submit and poll return `{id, status}` plus optional
`generation_id`, `unsigned_urls` (string array), `usage` (`{cost, is_byok}`)
and `error` (string). `id` must be non-empty and `status` one of `pending`,
`in_progress`, `completed`, `failed`, `cancelled`, `expired`; anything else is
a schema violation. Content returns `{video_b64, content_type}` with
`video_b64` non-empty valid base64 and `content_type` defaulting to
`video/mp4`.

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
`400 invalid_request_error` with message `credentials rejected`; `false` plus
an error table renders that table. A `nil` result accepts. Any other
non-boolean result is a crash. The handler receives no `ctx`.

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
provider-scoped credential list. Static `model_specs` overlay the result. A
type that declares `credential_schema` is not called while its provider has no
credential: the list is empty (the dashboard shows no models, a list cached
earlier included) until a credential is added. Keyless types (no
`credential_schema`) are always called.

API signature:

```lua
--- List upstream models.
-- @param ctx table: execution context.
-- @return table: array of model cards {name, display_name, endpoints array, ...}.
get_model_infos = function(ctx) return result, nil end
```

Parameters: `ctx` execution context.

Return values: array of model cards; `nil` reads as an empty array. Card
fields:

| Field | Description |
|---|---|
| `name` | Bare upstream model name. |
| `display_name` | Display name. |
| `description` | Optional text. |
| `rpm`, `tpm`, `rpd` | Rate limits as whole numbers, `0` when unknown. |
| `context_window`, `max_tokens` | Optional whole numbers. |
| `capabilities` | Optional string array of feature chips (`tools`, `json_mode`, `structured_outputs`, `reasoning`). When absent the router derives them from `supported_parameters` (`tools`, `response_format`, `structured_outputs`) and `reasoning`. `vision` and `audio` chips are dropped: modalities carry them. |
| `input_modalities`, `output_modalities` | Optional string arrays (`text`, `image`, `audio`, `file`, ...). See the input gate under `complete`. |
| `supported_parameters` | Optional string array of request parameters the model takes. |
| `reasoning` | Optional table following OpenRouter: `supported_efforts` (highest first), `default_effort`, `default_enabled`, `mandatory`, `supports_max_tokens` (the model takes `reasoning.max_tokens` as a thinking budget). |
| `endpoints` | Optional string array of served endpoints: `chat/completions`, `audio/transcriptions`, `audio/translations`, `audio/speech`, `images/generations`, `embeddings`, `videos`, `moderations`, `responses`. Empty or absent means `chat/completions` only. |

The router rejects a request with `endpoint_not_supported` when the cached
card declares `endpoints` without the target endpoint. Chat-pipeline routes
(`/v1/messages`, responses, conversations, assistants, threads, runs,
moderations) gate on `chat/completions`. A model absent from the cache passes
the gate. A fresh result refreshes known cards and adds new ones; cards the
result omits stay until a request reports the model `not_found`. A field of
the wrong type is a schema violation. The result is cached for one hour.

Example:

```lua
get_model_infos = function(ctx)
  return { { name = "model-1", display_name = "Model 1", endpoints = { "chat/completions" } } }, nil
end
```

### `check_health`

High-level description: checks one credential. The router calls it from
explicit credential probes (dashboard credential test, probe endpoints). Failed
traffic requests do not trigger it: request paths carry no credential identity.
Only `unhealthy` disables the credential, and only when provider automation is
on. Cooldown comes from
`healthcheck_cooldown`.

API signature:

```lua
--- Check credential health.
-- @param ctx table: execution context.
-- @param credential table: {id, label, data}.
-- @return table: {status="healthy|unhealthy|unknown", message=optional string}.
check_health = function(ctx, credential) return result end
```

Parameters: `ctx` with the credential's `provider_config` (so the probe uses
the provider's selected proxy pool), `credential` with `id`, `label` and `data`.

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

Semantics: real availability, judged by actual generation. The endpoints
listing first rules the model out cheaply (missing model upstream, or the
architecture output modalities exclude the endpoint), then a minimal probe
generation runs on every available credential and the first success wins:
`chat`/`vision` send a one-word prompt with `max_tokens=8`, `embeddings` one
short string, `speech` one word, `transcription` one second of silence, `image`
a one-word prompt, `video` a one-word job submission (no polling). Probes never
park, bench, or disable; a rejecting key only skips its credential.

Return values: `healthy` keeps routing (some credential served a real
generation), `quota_exceeded` skips without disable (any 429, or every
credential answered 402 without an affordable `max_tokens`), `unhealthy` fails
the model (every listing said missing upstream, any probe generation 404, or
all generations hit provider-side failures such as wrapped
"Provider returned error" raws and 5xx), `unknown` leaves state unchanged for
indeterminate probe conditions (capability mismatch through
`response="endpoint not supported ..."`, transport failure, unparsable listing,
no usable credential). `response` holds a short success excerpt. Unknown
endpoints return `{status="unknown", message="endpoint not supported ..."}`.
Types without the handler report `unsupported` with no live request.

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
-- @param ctx table: {flow_id, provider_config optional}.
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
-- @param ctx table: {reason="install|update|startup|enable"}.
-- @return nil|boolean|table: nil or true on success, terminal error table on failure.
initialize = function(ctx) return true end

--- Run deinitialization.
-- @param ctx table: {reason="delete|update|shutdown|disable"}; provider_id and provider_config join on a provider disable.
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
`multipart`, `base64_*`. `credentials` is present when the type serves
credentials but exposes `list`, `get` and `parked` only; `list` is scoped to
the provider, so it returns rows only in the provider `disable` hook. No
lifecycle hook writes credentials. Each call has a 30 second bound. Calls fan
out across types with at most 4 running at once; failures log and never block
install, delete, disable, enable, startup or shutdown. Each call loads the
source again (see Sandbox limits), so top-level file code reruns on every
call and stays side-effect free. One-time state persists through
`llm_router.storage`.

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
| Name | Pattern `^[a-z0-9_]{1,32}$`. Maximum 8 jobs per type. A violation fails install. |
| `interval_seconds` | Required number, interval in seconds. Range 60 to 86400; a value outside the range fails install. |
| `run_on_startup` | Boolean. Runs once at startup when true. Defaults to false. |
| `timeout_ms` | Timeout in milliseconds. Defaults to 60000. Bounds 1000 to 900000. Values outside bounds clamp to bounds. |
| `run` | Required function `function(ctx)` where `ctx` carries `job_name`, `run_reason` (`tick`, `startup`, `manual`), and optional `provider_config`. Returns `true`, `nil`, or terminal error table. Jobs are the only context where `credentials.update` works. |

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
| `code` | Optional non-empty string, rendered verbatim. Default `server_error`. Conventional values: `invalid_request_error`, `authentication_error`, `payment_required`, `not_found`, `rate_limit`, `insufficient_quota`, `server_error`. A non-string or empty value rejects the table as a crash. |
| `param` | Optional string naming the offending field. Validated (a non-string rejects the table as a crash) but not rendered into the envelope. |
| `status` | Optional HTTP status override, a number from 400 to 599. Default 502. Any other value rejects the table as a crash. |
| `headers` | Optional string-to-string table rendered as response headers on unary `/v1` errors in both envelopes. Names pass a strict allow-list (`Retry-After` today); anything else is dropped, never an error. `Retry-After` accepts non-negative integer seconds or an HTTP date. Mid-stream server-sent event failures carry no headers because status is already 200. Non-string keys or values are dropped, never fatal. |

Return behavior: the router renders `message`/`code` verbatim into the
OpenAI `{"error":{"message","type","param","code"}}` envelope (Anthropic
`{"type":"error","error":{"type","message"}}` envelope on requests carrying an
`anthropic-version` header); `param` is always `null`. The envelope `type` derives from `code`:

| `code` | OpenAI `type` | Anthropic `type` |
|---|---|---|
| `invalid_request_error`, `not_found` | `invalid_request_error` | `invalid_request_error` |
| `auth_error` | `invalid_request_error` | `authentication_error` |
| `payment_required` | `invalid_request_error` | `permission_error` |
| `rate_limit`, `quota_exceeded` | `rate_limit_error` | `rate_limit_error` |
| `overloaded` | `server_error` | `overloaded_error` |
| `timeout` | `api_error` | `api_error` |
| `server_error`, `upstream_error`, `transport_error`, `model_unavailable`, `structural_fault`, `internal_error` | `server_error` | `api_error` |
| any other value, such as `authentication_error` or `insufficient_quota` | `invalid_request_error` | `api_error` |

The HTTP status is the table `status` (502 by default); the Anthropic
envelope answers 429 for `rate_limit` and `quota_exceeded` and 529 for
`overloaded` regardless of `status`. No `type`/`scope`/`retry_after`/`upstream_status`/
`upstream_body` fields exist: rate interpretation, parking and backoff live in
plugin code backed by `storage` time to live. `not_found` means the requested
model does not exist upstream. The router drops the model from the model cache
(best-effort) and returns the error as-is. Emit it only when the upstream
names the model as missing, never for bad endpoints or malformed requests.
Calling Lua `error()` with a terminal table on the stack maps to provider
error. Any other Lua error, and any malformed return value, records a crash
and answers `502 upstream_error` with the plugin crash text. Branch on
`response.status` directly: `401` tries next credential, `429` parks plus
next, `400` or `404` returns terminal `invalid_request_error`.

Example:

```lua
return nil, { message = "upstream rejected request", code = "invalid_request_error", status = 400 }
```

## `llm_router` APIs

### `http_client({timeout_ms?})`

High-level description: creates an HTTP client bound to `@allow_host` plus
server-side request forgery guard. The router classifies network faults on the
proxied leg and reports them to the proxy pool internally; plugins never mark
proxies. Reasons `dns_resolution`, `address_parse`, `tls_certificate_verification`
and `tls_handshake` mark the proxy dead (persisted ban). Reasons
`connection_refused`, `connection_reset`, `broken_pipe`, `early_eof` and
`unexpected_eof` only mark it suspect: the pool re-verifies it before any ban.
Direct requests never report.

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
| `timeout_ms` | Timeout in milliseconds. Default 60000. Range 1000 to 900000. Values outside range clamp to range. |

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
| `proxy_url` | Optional non-empty string; absent means direct connection, an empty or non-string value raises. Must be a URL served by `proxies.query` / `proxies.require` in the same execution, or one a router pool still holds (free pool cache or a custom pool entry); any other value raises `request blocked: proxy_url is not served by the router proxy pools`. |
| `headers` | Table string to string; entries with non-string keys or values are ignored. Custom `Host` header is revalidated against allow-list. |
| `body` | String payload. Any other type raises. |

Return values: success returns `(response, nil)` where `response` holds
`status` number, `headers` table with lowercased keys (repeated headers join
with `, `), `body` string capped at 32 MiB (a longer body is cut, not
failed). Failure returns `(nil, fault)` where `fault` follows the error
contract plus transport fields below. A call that fails validation raises a
Lua error instead of returning a fault: a missing `url`, a scheme other than
`http`/`https`, a host outside `@allow_host`, a blocked custom `Host` header,
a non-string `body` and a rejected `proxy_url`. Redirects recheck allow-list,
maximum 10 hops; a blocked hop returns a fault. Non-public destinations
(loopback, private, link-local, CGNAT, multicast, reserved, NAT64, 6to4,
`localhost`, and hostnames ending in `.localhost`, `.local`, `.internal`,
`.lan`, `.home.arpa`) are blocked even under `*`: direct requests resolve the
host and dial the validated IP; requests with `proxy_url` check the hostname
and IP literals only, because the proxy resolves the target. Direct requests
of provider types dial IPv4 only.

`timeout_ms` bounds the whole exchange, including the response body: a stream
running longer than `timeout_ms` ends with a transport fault. Streaming
handlers pass a large value (up to 900000).

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
| `method`, `url`, `proxy_url`, `headers`, `body` | Same semantics as `request`. At least one of `on_line` or `on_chunk` is required. |
| `on_line` | Function `function(line)` called per line, without the line terminator. Scanner buffer 64 KiB to 1 MiB; a longer line ends the stream with a fault. |
| `on_chunk` | Function `function(bytes)` called per byte chunk. Buffer 32 KiB. Takes precedence when `on_line` is also given. |
| `on_response` | Function `function(head)` where `head` is `{status, headers}` plus `body` (up to 64 KiB) on non-2xx. Runs before any body streams. Non-nil table return aborts with that table as the error. Non-table return raises. `nil` accepts a 2xx response. |

Return values: a non-2xx response whose hook is absent or returns `nil`
returns `(nil, {message="unexpected status N: <body up to 64 KiB>",
code="server_error"})`. A callback that raises ends the stream with
`(nil, {message="on_line failed: <cause>", code="server_error"})` (`on_chunk
failed` for chunks). Success returns `(head, nil)` with `head` as
`{status, headers}`.

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
| `pool` | Pool selector. Empty or absent means direct: the call returns an empty array, so a loop over the result never runs. Use `proxies.require` to tell direct from an empty pool. `"auto"` means free pool. Otherwise a custom pool identifier or name. |
| `country` | Singular country filter string, case-insensitive. |
| `limit` | Maximum rows. Number. Zero or negative means all rows. |

Return values: array of `{id, url, country, pool}`, free pool in rank order. The
array is empty, never an error, when the pool selector is empty or when the
router proxy switches are off (see `Provider proxy configuration`). An unknown
custom pool raises an error. `query` never waits.

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
| `countries` | Array of two-letter ISO codes, OR semantics. Empty or absent accepts any country. Plural form, unlike `query`. The free pool upper-cases, deduplicates and validates codes: an invalid code raises an error. A custom pool compares case-insensitively and does not validate. |
| `exclude` | Array of proxy URLs that must not be returned. |
| `limit` | Maximum number of distinct proxies returned, 1 to 50, default 1. Whole numbers only. |
| `want` | Free pool only: alive proxies the pool keeps in stock for these countries, 1 to 200. Default is the pool default. Has no effect without `countries`. |
| `timeout_ms` | Free pool only: wait deadline, default 60000, at most 900000. |
| `fallback` | `"fail"` (default) or `"direct"`: behavior when no proxy is available. |

Return values: table `{proxies={{id,url,country,pool},...}, timed_out,
direct}`; `country` is absent when unknown.

| Outcome | Result |
|---|---|
| Proxies found | `proxies` non-empty, `timed_out=false`, `direct=false`. |
| `pool` empty | `proxies` empty, `direct=true`. |
| Non-empty `pool` while the router proxy switches are off (see `Provider proxy configuration`), `fallback="direct"` | `proxies` empty, `timed_out=false`, `direct=true`. The plugin goes direct. |
| Non-empty `pool` while the router proxy switches are off, `fallback="fail"` | `nil, "disabled"`. |
| Free pool deadline passed, `fallback="direct"` | `proxies` empty, `timed_out=true`, `direct=false`. The plugin goes direct. |
| Custom pool without a matching entry, `fallback="direct"` | `proxies` empty, `timed_out=false`, `direct=false`. The plugin goes direct. |
| Free pool deadline passed, `fallback="fail"` | `nil, "timeout"`. |
| Custom pool without a matching entry, `fallback="fail"` | `nil, "no_match"`. |
| Request cancelled | `nil, "canceled"`, with either `fallback`. |

With `fallback="direct"` an empty `proxies` list means "connect directly"
whatever `direct` says (`direct=true` only tells that no pool applies). With
`fallback="fail"` an empty list comes only with `direct=true` (empty `pool`):
a plugin that must never connect directly passes `fallback="fail"` and a
non-empty `pool`, and gets proxies or `nil` with a code. The router makes no direct connection
itself; the plugin connects directly by omitting `proxy_url`.

Malformed options raise an error. So does an exhausted demand registry
(`too many active proxy demands`, free pool only) and an unknown custom pool;
the error counts as a plugin crash. The free pool returns matching live proxies
immediately. Without a match it registers a demand, prioritizes finding the
requested country, and waits until a match is alive or the deadline passes. The
wait deadline is shared by every `require` call of one request: the first call
fixes it. Cancelling the client request stops only the wait; the pool keeps
working on the demand so the next request finds a proxy. Returned free-pool
proxies come from the lowest-latency window of matches in random order. A
custom pool answers from its own entries in stored order and never waits.

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

## Provider proxy configuration

High-level description: how the dashboard proxy switch reaches a plugin. The
router never routes provider traffic through a proxy by itself. It stores the
operator choice in the provider config, passes the config to every handler as
`ctx.provider_config`, and hosts the pool; the plugin reads the choice and
assigns `proxy_url` per request.

Provider config keys:

| Key | Description |
|---|---|
| `proxy.pool` | Operator pool selection. Absent key or empty string means direct. `"auto"` selects the free pool, any other value a custom pool by identifier or name. The provider page switch "Route through a proxy pool" writes `{pool = "auto"}`; the pool selector replaces the value; switching off deletes the `proxy` key. |
| `disable_failed_credentials` | Boolean, default off. Master switch for `credentials.disable` / `credentials.enable` (see `credentials`). |

The provider page shows the switch when the type declares `proxy_schema`, even
an empty one. Nodes of a non-empty `proxy_schema` render below the switch and
save with the plugin settings under `config.settings`.

Router-wide switches, set in the router configuration (`proxy_enabled`,
`proxy_pool_enabled`) and independent of provider config:

| Switch | Effect on plugin calls |
|---|---|
| `proxy_enabled` off | Every `proxies.query` returns an empty array. `proxies.require` with a non-empty `pool` returns `nil, "disabled"` under `fallback="fail"` and `direct=true` under `fallback="direct"`. Discovery stops; stored proxies stay. |
| `proxy_pool_enabled` off | Same for pool `"auto"` only. Custom pools keep serving. |

Plugin flow:

1. Read `ctx.provider_config.proxy.pool`; treat absent or empty as direct.
2. Call `proxies.require` with `pool`, `countries` and `fallback`; handle
   `direct=true`, empty `proxies` and the `nil, code` results.
3. Pass `proxy_url` to `client:request` / `client:stream`; the router executes
   the request through that proxy and reports proxied-leg faults to the pool.
4. On a fault table with `retryable=true`, retry the same credential on the next
   proxy, passing used URLs in `exclude`. Keep per-exit cooldowns in
   `llm_router.storage` with a time to live; the router keeps no per-plugin
   exit state.

A plugin that must not connect directly (for example a region-locked upstream)
passes `fallback="fail"`: with the router-wide switches off it gets
`nil, "disabled"` and returns its own terminal error.

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

Return values: array of rows in either shape below, or `nil`, which reads as
an empty array: nothing new this fetch, earlier candidates stay in the pool. Rows without an address are dropped; other schemes and
non-public hosts (private ranges, loopback, local hostnames) count as
unsupported. A row built from `protocol`, `host` and `port` needs a non-empty
`host` and a `port` above 0. The router verifies every candidate through the
pool before serving it.

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
`credential_schema` exists or `auth_initiate` is declared. Elsewhere
`llm_router.credentials` is `nil`.

### `list`

High-level description: lists usable credentials.

API signature:

```lua
--- List usable credentials.
-- @return table: array of {id, label, data}.
local list = llm_router.credentials.list()
```

Parameters: none.

Return values: array `[{id, label, data}]` filtered by the router-token allow-list
(`label` is the dashboard name of the key: errors that blame one key name it);
parked rows are skipped automatically, expired, disabled and denied rows are
skipped.
An absent allow-list (admin probes, jobs, lifecycle hooks) means unrestricted.
The router rejects a request before the plugin runs when the token rules
leave no credential (`credential_not_allowed`), so a plugin never sees an
empty allow-list on a request path.

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
-- @return table|nil: {id, label, data} or nil when missing, disabled, or denied. No error is raised.
local item = llm_router.credentials.get(id)
```

Parameters: `id` credential identifier string.

Return values: `{id, label, data}` table or `nil`.

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
-- @param data table: new payload, replaces the stored one.
-- @return boolean: true on success, raises otherwise. Job contexts only.
llm_router.credentials.update(id, { key = "next" })
```

Parameters: `id` string, `data` table. The table replaces the whole stored
payload (no merge, so pass every field) and clears the credential expiry.

Return values: `true` on success; an unknown `id` raises. Request paths,
lifecycle hooks and every other non-job context calling it fail loudly.

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
(first-wins against other causes; an empty `reason` becomes `disabled by
plugin`, reasons cap at 1024 bytes). `enable` clears plugin-caused disables
only. An unknown `id` raises. Lifecycle hooks have neither function.

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

Parameters: `id` string, `ttl` number seconds (greater than 0, at most 90
days), `reason` optional string.

Return values: `park` and `unpark` return `true`; `unpark` of a live
credential succeeds. `parked` returns `nil` for a live credential or
`{reason, until}` for a parked one. Misuse (non-numeric time to live, one
outside the bounds, non-string reason, unknown `id` on `park`) raises a Lua
error. All three serve request and job contexts with no switch; lifecycle
hooks only read (`parked`).

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
| `scope` | Non-empty namespace string (not blank). |
| `key` | Non-empty key string (not blank). |
| `value` | Any JSON-compatible Lua value: tables with string keys or a gap-free array part. Functions, mixed tables and sparse arrays raise. |
| `ttl` | Seconds as number, fractions allowed. Absent or `0` means durable database. Greater than `0` means cache database with expiry. Negative fails. Greater than 90 days fails. Non-number raises. |

Return values: `set` and `delete` return `true`. `get` returns stored value
or `nil`. Reads check durable storage first, then cache with expiry check.
`set` migrates rows between files when time-to-live class changes. The store
is per plugin file: every type key of one plugin shares it, other plugins
never see it. Entries with a time to live live in the disposable cache file:
deleting it while the router is stopped drops them. Durable entries stay in
the database file. Every failure raises a Lua error.

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
-- @param message any: required message, tables encode as JSON, truncated to 2048 bytes.
-- @param keys table|nil: optional string keys to string, number, boolean, or JSON-encoded table. Maximum 32 keys. Reserved plugin and type rejected.
llm_router.log("warn", "rate limited", { code = "rate_limit", cred_id = id })
```

Parameters:

| Parameter | Description |
|---|---|
| `severity` | `debug`, `info`, `warn`, `error`, `fatal`. `warning` aliases `warn`. `critical` aliases `fatal`. |
| `message` | Required message (a missing or `nil` message raises). Tables encode as JSON. Truncated to 2048 bytes. |
| `keys` | Optional table. Maximum 32 entries. Values truncate at 2048 bytes; `nil` values are skipped. Reserved `plugin` and `type` rejected. |

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

Return values: encoded text or decoded Lua value. `encode` maps a table with
only consecutive integer keys from 1 to an array and any other table to an
object; an empty table encodes as `{}`. Functions, userdata, mixed tables,
sparse arrays and non-string object keys raise. Invalid JSON raises in
`decode`. Handler results normalize an empty `{}` to `[]` for the array fields
`choices`, `tool_calls`, `data`, `results`, `segments`, `words` and
`unsigned_urls`.

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
-- @param nodes table: array table, maximum 200 nodes per array, nesting depth maximum 8.
local tree = { { type = "secret", name = "api_key", label = "API Key" } }
```

Parameters:

| Kind | Rules |
|---|---|
| all kinds | `type` required. Shared optional fields: `text`, `name`, `label`, `placeholder`, `required` (boolean), `value`, `title`, `subtitle`. A non-string text field reads as empty. `columns` needs a number, `wrap` and `grow` booleans, `option_labels` a table with non-empty string keys and values, `options` and `content` arrays; a wrong type fails install. |
| `input`, `select`, `checkbox`, `secret` | Require `name`. `input.input_type` is `text`, `password`, `number`, or `secret`. `select.options` is a string array, `option_labels` keys must be a subset of `options`. |
| `link` | Requires `url` with `http`, `https`, `mailto`, or relative form. Rejects `javascript:` and `data:`. |
| `button` | `form_action` defaults to `submit`. Variant is `primary`, `secondary`, or `danger`. Buttons render in host footer. |
| `banner` | Variant is `info`, `error`, or `success`. |
| `code` | Requires `text`. |
| `group`, `flow`, `grid`, `section` | Require non-empty `content`. `flow.direction` is `horizontal` or `vertical`, default `vertical`. `flow` and `grid` `gap` is a whole step 0 to 8 or `sm`, `md`, `lg` (steps 2, 4, 6), default `md`. `flow.align` is `start`, `center`, `end`, or `stretch`, default `stretch`. `flow.justify` is `start`, `center`, `end`, `between`, `around`, or `evenly`, default `start`. `flow.wrap` defaults to true. `grid.columns` required, range 1 to 6. `section.title` required, maximum 120 bytes, `subtitle` maximum 240 bytes. |
| `spacer`, `divider` | No content. `spacer.size` is a step value (whole 0 to 8 or `sm`, `md`, `lg`), `grow` defaults to true. |

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
-- @param locales table: {en={key=text}, ru={key=text}}. Maximum 1000 entries per locale. Key maximum 128 bytes. Value maximum 1000 bytes.
locales = {
  en = { ["API Key"] = "API Key" },
  ru = { ["API Key"] = "API Key RU" },
}
```

Parameters: locale codes `en` and `ru` only (case-insensitive). Entry keys 1
to 128 bytes, values 1 to 1000 bytes, maximum 1000 entries per locale. Keys
and values must be non-blank strings.

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

High-level description: dashboard icon of the plugin and of its providers.
Valid in `register` and `register_proxy_source` tables.

API signature:

```lua
--- @param icon string: empty string, https:// URL, or data:image/ URI. Maximum 64 KiB.
icon = "https://example.com/icon.png"
```

Parameters: `icon` string.

Return behavior: absent or empty means no icon. Other schemes fail install.
The dashboard shows the icon of the first type key (sorted) that declares one
as the plugin icon, and the icon of a provider's type key as the provider icon
unless the provider has its own. Without an icon, or when the image fails to
load, the dashboard shows the icon of the plugin's subsystem. A `data:image/`
value needs no network access: declare it as a local above the registration,
at most 256x256 pixels.

```lua
local ICON = "data:image/png;base64,iVBORw0KGgo..."

llm_router.register("example", { icon = ICON, complete = function(ctx, request) end })
```

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

Parameters: map keys select model names. Every model card field listed under
`get_model_infos` except `name` merges over the `get_model_infos` row.

Return behavior: only non-zero, non-empty specification fields replace the
row (a specification cannot reset a field to `0` or empty); fields absent from
the specification pass through. Entries for undiscovered names are ignored
(specifications never resurrect removed models). Unknown fields, `name`
inside an entry, and mistyped values fail install.

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

High-level description: one Lua state per call, taken from a pool per plugin
and type key (`LUA_POOL_BUCKET_SIZE` idle states each, default 16) and rebound; the plugin source
loads again in it on every call. Module-level globals a plugin creates
survive into later calls of the same type; per-call state belongs in locals
or `storage`.

API surface:

```lua
-- Open libraries: base (pairs, ipairs, next, select, type, tostring, tonumber, error, pcall, xpcall, setmetatable, getmetatable, rawget, rawset, rawequal, assert), table, string, math, os (time, clock, date only).
-- Globals: print shim, json, llm_router.
```

Parameters: none.

Limits: no `io`, `debug`, `package`, `coroutine`, `require`, `load`,
`loadstring`, `dofile`, `loadfile`, `unpack`, `rawlen`, `collectgarbage`;
`print` is the shim that logs at info severity. Plugin file 1 MiB. Response
bodies cap at 32 MiB; stream error bodies at 64 KiB; redirects cap at 10 hops
validated against `@allow_host`; private and link-local dial targets are
blocked even under wildcard hosts. `on_chunk` buffer 32 KiB. `on_line`
scanner 64 KiB to 1 MiB. `multipart` 1 to 64 parts. `random_hex` 1 to 1024
bytes. Log values cap at 2048 bytes and 32 keys. UI trees cap at depth 8 and
200 nodes per array. Locales cap at 1000 entries per locale. Jobs cap at 8
per type. Storage `ttl` caps at 90 days. `http_client` `timeout_ms` clamps to
1000 to 900000.

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
    local acquired, code = llm_router.proxies.require({ pool = pool, limit = 3, fallback = "direct" })
    if acquired == nil then
      return nil, { message = "proxy acquisition failed: " .. tostring(code), code = "server_error" }
    end
    -- Direct (empty pool, or router proxy switches off) and an expired wait
    -- all yield an empty list: one attempt without proxy_url.
    local exits = acquired.proxies
    if #exits == 0 then exits = { {} } end
    for _, proxy_entry in ipairs(exits) do
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
