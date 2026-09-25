--- @plugin Smoke Mock
--- @author llm-router
--- @version 1.1.0
--- @router_version 0.1.2
--- @description Deterministic mock provider for the smoke harness. No network use.
--- @allow_host example.com
-- NOTE: bump @version on every edit of this file. The harness skips
-- reinstalling when the installed version matches the source.

-- Model routing by bare name: mock-limited always reports quota_exceeded so
-- the harness exercises its skip path deterministically.
local function limited(model_name)
  return model_name == "mock-limited"
end

local function quota_err()
  return { type = "quota_exceeded", message = "smoke quota", retry_after = os.time() + 60, scope = { "account" } }
end

llm_router.register("mock", {
  credential_schema = function()
    return {
      { type = "section", title = "Smoke Mock",
        content = {
          { type = "secret", name = "api_key", label = "API Key" },
          { type = "button", text = "Save", form_action = "submit" },
        } },
    }
  end,

  validate_credentials = function(data)
    return true
  end,

  get_model_infos = function(ctx, credential, provider_config)
    return {
      { name = "mock-chat", display_name = "Mock Chat", endpoints = { "chat/completions" } },
      { name = "mock-stt", display_name = "Mock STT", endpoints = { "audio/transcriptions" } },
      { name = "mock-tts", display_name = "Mock TTS", endpoints = { "audio/speech" } },
      { name = "mock-img", display_name = "Mock Image", endpoints = { "images/generations" } },
      { name = "mock-emb", display_name = "Mock Embeddings", endpoints = { "embeddings" } },
      { name = "mock-limited", display_name = "Mock Limited", endpoints = { "chat/completions" } },
      { name = "mock-tools", display_name = "Mock Tools", endpoints = { "chat/completions" } },
    }
  end,

  complete = function(ctx, credential, request)
    if limited(request.model_name) then return nil, quota_err() end
    if request.model_name == "mock-tools" and type(request.tools) == "table" and #request.tools > 0 then
      local name = "get_weather"
      local first = request.tools[1]
      if type(first) == "table" then
        if type(first["function"]) == "table" and type(first["function"].name) == "string" then
          name = first["function"].name
        elseif type(first.name) == "string" then
          name = first.name
        end
      end
      return {
        id = "smoke-tools", object = "chat.completion", created = os.time(), model = request.model,
        choices = {
          { index = 0,
            message = { role = "assistant", tool_calls = {
              { id = "call_smoke", type = "function",
                ["function"] = { name = name, arguments = "{}" } },
            } },
            finish_reason = "tool_calls" },
        },
        usage = { prompt_tokens = 2, completion_tokens = 2, total_tokens = 4 },
      }
    end
    return {
      id = "smoke-chat", object = "chat.completion", created = os.time(), model = request.model,
      choices = {
        { index = 0, message = { role = "assistant", content = "smoke ok" }, finish_reason = "stop" },
      },
      usage = { prompt_tokens = 2, completion_tokens = 2, total_tokens = 4 },
    }
  end,

  complete_stream = function(ctx, credential, request, emit)
    if limited(request.model_name) then return nil, quota_err() end
    emit({ id = "smoke-stream", object = "chat.completion.chunk", created = os.time(), model = request.model,
      choices = { { index = 0, delta = { role = "assistant", content = "smoke " } } } })
    emit({ id = "smoke-stream", object = "chat.completion.chunk", created = os.time(), model = request.model,
      choices = { { index = 0, delta = { content = "ok" }, finish_reason = "stop" } } })
  end,

  transcribe = function(ctx, credential, request)
    if request.needs_segments then
      return { text = "smoke transcript",
        segments = { { id = 0, start = 0.0, ["end"] = 1.0, text = "smoke transcript" } } }
    end
    return { text = "smoke transcript" }
  end,

  speech = function(ctx, credential, request)
    local function u16(n)
      return string.char(n % 256, math.floor(n / 256) % 256)
    end
    local function u32(n)
      return string.char(n % 256, math.floor(n / 256) % 256, math.floor(n / 65536) % 256, math.floor(n / 16777216) % 256)
    end
    local wav = "RIFF" .. u32(36) .. "WAVEfmt " .. u32(16) .. u16(1) .. u16(1)
      .. u32(8000) .. u32(16000) .. u16(2) .. u16(16) .. "data" .. u32(0)
    return { audio_b64 = llm_router.base64_encode(wav), format = "wav" }
  end,

  generate_image = function(ctx, credential, request)
    if request.response_format == "url" then
      return { created = os.time(), data = { { url = "http://example.com/smoke.png" } } }
    end
    return { created = os.time(), data = { { b64_json = "aGk=" } } }
  end,

  embed = function(ctx, credential, request)
    local data = {}
    for i in ipairs(request.input) do
      table.insert(data, { embedding = { 0.1, 0.2, 0.3 } })
    end
    return { data = data }
  end,
})
