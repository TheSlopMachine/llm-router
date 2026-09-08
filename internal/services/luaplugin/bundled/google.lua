--- @plugin Google AI Studio
--- @author TheSlopMachine
--- @version 1.0.1
--- @router_version 0.0.4
--- @description Google Gemini models via AI Studio API
--- @allow_host generativelanguage.googleapis.com

local BASE_URL = "https://generativelanguage.googleapis.com/v1beta"

local function classify_error(status, body)
  local message = body
  local ok, parsed = pcall(json.decode, body)
  if ok and parsed and parsed.error and type(parsed.error.message) == "string" and parsed.error.message ~= "" then
    message = parsed.error.message
  end
  if status == 401 or status == 403 then
    return nil, { type = "auth", message = message }
  elseif status == 429 then
    return nil, { type = "rate_limit", message = message, retry_after = os.time() + 60 }
  elseif status == 408 or status == 504 then
    return nil, { type = "timeout", message = message }
  elseif status >= 500 then
    return nil, { type = "upstream", message = message }
  end
  return nil, { type = "invalid_request", message = message }
end

local function api_key_of(credential)
  if credential == nil or credential.data == nil then return "" end
  return credential.data.api_key or ""
end

local function map_finish_reason(r)
  if r == "STOP" then return "stop"
  elseif r == "MAX_TOKENS" then return "length"
  elseif r == "SAFETY" or r == "RECITATION" then return "content_filter"
  else return "stop" end
end

-- Merge consecutive same-role messages into Google contents.
local function build_contents(messages)
  local contents = {}
  local current = nil
  local current_role = nil
  local function flush()
    if current ~= nil then
      table.insert(contents, current)
      current = nil
    end
  end
  for _, m in ipairs(messages or {}) do
    local role = m.role
    if role == "system" then role = "user"
    elseif role == "assistant" then role = "model"
    else role = "user" end
    if current == nil or current_role ~= role then
      flush()
      current = { role = role, parts = {} }
      current_role = role
    end
    local text = m.content or ""
    table.insert(current.parts, { text = text })
  end
  flush()
  return contents
end

local function build_generation_config(request)
  local cfg = nil
  if request.max_tokens and request.max_tokens > 0 then
    cfg = cfg or {}
    cfg.maxOutputTokens = request.max_tokens
  end
  if request.temperature and request.temperature > 0 then
    cfg = cfg or {}
    cfg.temperature = request.temperature
  end
  if request.top_p and request.top_p > 0 then
    cfg = cfg or {}
    cfg.topP = request.top_p
  end
  return cfg
end

local function estimate_rpm(name)
  local m = name:lower()
  if m:find("flash") then
    if m:find("lite") then return 2000 end
    return 1000
  end
  if m:find("pro") then return 500 end
  return 1000
end

local function estimate_tpm(name)
  local m = name:lower()
  if m:find("flash") then
    if m:find("lite") then return 2000000 end
    return 1000000
  end
  if m:find("pro") then return 500000 end
  return 1000000
end

local function estimate_rpd(name)
  local m = name:lower()
  if m:find("flash") then
    if m:find("lite") then return 3000 end
    return 1500
  end
  if m:find("pro") then return 1000 end
  return 1500
end

local function model_short_name(entry)
  if type(entry.name) == "string" and entry.name ~= "" then
    return entry.name:gsub("^models/", "")
  end
  if type(entry.baseModelId) == "string" and entry.baseModelId ~= "" then
    return entry.baseModelId
  end
  return "unknown-model"
end

local function supports_generate_content(entry)
  for _, m in ipairs(entry.supportedGenerationMethods or {}) do
    if m == "generateContent" then return true end
  end
  return false
end

llm_router.register("google", {
  icon = "https://www.gstatic.com/lamda/images/favicon_v1_150160cddff7f294ce30.svg",

  credential_schema = function()
    return {
      { type = "text", text = "Get your API key from Google AI Studio." },
      { type = "link", text = "Open AI Studio", url = "https://aistudio.google.com/app/apikey" },
      { type = "input", name = "api_key", input_type = "password", label = "API Key", required = true },
      { type = "button", text = "Save", form_action = "submit" },
    }
  end,

  validate_credentials = function(data)
    local key = data.api_key
    if key == nil or key == "" then
      return false, { type = "invalid_request", message = "api_key is required" }
    end
    if #key < 20 then
      return false, { type = "invalid_request", message = "api_key: minimum 20 characters" }
    end
    return true
  end,

  get_model_infos = function(ctx, credential, provider_config)
    local client = llm_router.create_http_client({})
    local key = api_key_of(credential)
    local url = BASE_URL .. "/models"
    local infos = {}
    while url do
      local resp, err = client:request({
        method = "GET", url = url,
        headers = { ["x-goog-api-key"] = key },
      })
      if err then return nil, err end
      if resp.status ~= 200 then return classify_error(resp.status, resp.body) end
      local page = json.decode(resp.body)
      for _, entry in ipairs(page.models or {}) do
        local name = model_short_name(entry)
        local skip = false
        if not supports_generate_content(entry) then skip = true end
        local lower = name:lower()
        if lower:find("embedding") or lower:find("aqa") then skip = true end
        if not skip then
          local display = entry.displayName
          if display == nil or display == "" then display = name end
          table.insert(infos, {
            name = name, display_name = display,
            rpm = estimate_rpm(name), tpm = estimate_tpm(name), rpd = estimate_rpd(name),
            context_window = entry.inputTokenLimit or 0,
            max_tokens = entry.outputTokenLimit or 0,
          })
        end
      end
      if page.nextPageToken and page.nextPageToken ~= "" then
        url = BASE_URL .. "/models?pageToken=" .. page.nextPageToken
      else
        url = nil
      end
    end
    return infos
  end,

  complete = function(ctx, credential, request)
    local model = request.model:match("([^/]+)$")
    local client = llm_router.create_http_client({})
    local payload = { contents = build_contents(request.messages) }
    local cfg = build_generation_config(request)
    if cfg then payload.generationConfig = cfg end
    local resp, err = client:request({
      method = "POST", url = BASE_URL .. "/models/" .. model .. ":generateContent",
      headers = { ["x-goog-api-key"] = api_key_of(credential), ["Content-Type"] = "application/json" },
      body = json.encode(payload),
    })
    if err then return nil, err end
    if resp.status ~= 200 then return classify_error(resp.status, resp.body) end
    local g = json.decode(resp.body)
    local choices = {}
    if g.candidates and #g.candidates > 0 then
      local cand = g.candidates[1]
      local text = ""
      if cand.content and cand.content.parts and #cand.content.parts > 0 then
        text = cand.content.parts[1].text or ""
      end
      table.insert(choices, {
        index = cand.index or 0,
        message = { role = "assistant", content = text },
        finish_reason = map_finish_reason(cand.finishReason or ""),
      })
    end
    local usage = { prompt_tokens = 0, completion_tokens = 0, total_tokens = 0 }
    if g.usageMetadata then
      usage.prompt_tokens = g.usageMetadata.promptTokenCount or 0
      usage.completion_tokens = g.usageMetadata.candidatesTokenCount or 0
      usage.total_tokens = g.usageMetadata.totalTokenCount or 0
    end
    return {
      id = "google-" .. tostring(os.time()), object = "chat.completion", created = os.time(),
      model = request.model, choices = choices, usage = usage,
    }
  end,

  complete_stream = function(ctx, credential, request, emit)
    local model = request.model:match("([^/]+)$")
    local client = llm_router.create_http_client({})
    local payload = { contents = build_contents(request.messages) }
    local cfg = build_generation_config(request)
    if cfg then payload.generationConfig = cfg end
    local full_model = request.model
    local stream_err = client:stream({
      method = "POST", url = BASE_URL .. "/models/" .. model .. ":streamGenerateContent",
      headers = { ["x-goog-api-key"] = api_key_of(credential), ["Content-Type"] = "application/json" },
      body = json.encode(payload),
      on_line = function(line)
        if line:sub(1, 6) ~= "data: " then return end
        local data = line:sub(7)
        if data == "[DONE]" then return end
        local ok, g = pcall(json.decode, data)
        if not ok or not g.candidates or #g.candidates == 0 then return end
        local cand = g.candidates[1]
        local text = ""
        if cand.content and cand.content.parts and #cand.content.parts > 0 then
          text = cand.content.parts[1].text or ""
        end
        local chunk = {
          id = "google-" .. tostring(os.time()), object = "chat.completion.chunk", created = os.time(),
          model = full_model,
          choices = {
            { index = cand.index or 0, delta = { role = "assistant", content = text } },
          },
        }
        if cand.finishReason and cand.finishReason ~= "" then
          chunk.choices[1].finish_reason = map_finish_reason(cand.finishReason)
        end
        emit(chunk)
      end,
    })
    if stream_err then
      if stream_err.status then
        -- client:stream only returns transport errors; status errors surface per-line.
        return nil, stream_err
      end
      return nil, stream_err
    end
  end,
})
