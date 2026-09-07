--- @plugin OpenCode Zen
--- @author TheSlopMachine
--- @version 1.0.0
--- @router_version 0.0.4
--- @description OpenAI/Anthropic/Google compatible free provider OpenCode Zen
--- @allow_host opencode.ai

local BASE_URL = "https://opencode.ai/zen/v1"

local function endpoint_for_model(model)
  local m = model:lower()
  if m:match("^gpt%-") or m:match("muse%-spark") or m:match("^grok%-") then
    return "/responses"
  end
  return "/chat/completions"
end

local function opencode_headers(extra)
  local h = {
    ["User-Agent"] = "opencode/1.18.29",
    ["x-opencode-client"] = "opencode",
    ["x-opencode-project"] = "proj_llm-router",
  }
  for k, v in pairs(extra or {}) do h[k] = v end
  return h
end

local function classify_error(status, body)
  local message = body
  local ok, parsed = pcall(json.decode, body)
  if ok and parsed and parsed.error and parsed.error.message then
    message = parsed.error.message
  elseif ok and parsed and type(parsed.message) == "string" and parsed.message ~= "" then
    message = parsed.message
  elseif ok and parsed and type(parsed.error) == "string" and parsed.error ~= "" then
    message = parsed.error
  end
  if status == 401 or status == 403 then
    return nil, { type = "auth", message = message }
  elseif status == 429 then
    local t = message:lower():find("quota")
    return nil, { type = t and "quota_exceeded" or "rate_limit", message = message, retry_after = os.time() + 60 }
  elseif status == 408 or status == 504 then
    return nil, { type = "timeout", message = message }
  elseif status >= 500 then
    return nil, { type = "upstream", message = message }
  end
  return nil, { type = "invalid_request", message = message }
end

local function api_key_of(credential)
  if credential == nil or credential.data == nil then return "" end
  return credential.data.api_key or credential.data.access_token or ""
end

local FALLBACK_MODELS = {
  { name = "nemotron-3-ultra-free", display_name = "Nemotron 3 Ultra Free" },
  { name = "nemotron-3.5-lightning-free", display_name = "Nemotron 3.5 Lightning Free" },
  { name = "mimo-v2.5-free", display_name = "MiMo V2.5 Free" },
  { name = "big-pickle", display_name = "Big Pickle" },
  { name = "ling-3.0-flash-fin-free", display_name = "Ling 3.0 Flash Fin Free" },
  { name = "muse-spark-1.2-contributor-free", display_name = "Muse Spark 1.2 Contributor Free" },
  { name = "muse-spark-1.3-contributor-free", display_name = "Muse Spark 1.3 Contributor Free" },
  { name = "gpt-5", display_name = "GPT-5" },
  { name = "claude-sonnet-4.5", display_name = "Claude Sonnet 4.5" },
  { name = "gemini-3-flash", display_name = "Gemini 3 Flash" },
}

local function with_limits(infos)
  for _, m in ipairs(infos) do
    m.context_window = 200000
    m.max_tokens = 32000
    m.rpm = 60
    m.tpm = 100000
    m.rpd = 500
  end
  return infos
end

-- Build the OpenAI Responses input from chat messages.
local function build_responses_input(messages)
  local input = {}
  for _, m in ipairs(messages or {}) do
    local role = m.role
    if role == "system" then
      local text = (m.content or "")
      if text ~= "" then
        table.insert(input, { role = "system", content = text })
      end
    elseif role == "user" then
      local text = (m.content or "")
      if text ~= "" then
        table.insert(input, { role = "user", content = { { type = "input_text", text = text } } })
      end
    elseif role == "assistant" then
      local text = (m.content or "")
      if text ~= "" then
        table.insert(input, { role = "assistant", content = { { type = "output_text", text = text } } })
      end
      for _, tc in ipairs(m.tool_calls or {}) do
        local name = tc["function"] and tc["function"].name or ""
        if name ~= "" then
          local args = tc["function"].arguments or "{}"
          if args == "" then args = "{}" end
          table.insert(input, { type = "function_call", call_id = tc.id or "", name = name, arguments = args })
        end
      end
    elseif role == "tool" then
      table.insert(input, { type = "function_call_output", call_id = m.tool_call_id or "call_unknown", output = m.content or "" })
    end
  end
  if #input == 0 then
    table.insert(input, { role = "user", content = { { type = "input_text", text = "ping" } } })
  end
  return input
end

local function build_responses_tools(tools)
  local out = {}
  for _, t in ipairs(tools or {}) do
    local name = t.name or ""
    local desc = t.description or ""
    local params = t.parameters
    if t["function"] then
      if t["function"].name and t["function"].name ~= "" then name = t["function"].name end
      if t["function"].description and t["function"].description ~= "" then desc = t["function"].description end
      if t["function"].parameters then params = t["function"].parameters end
    end
    if name ~= "" then
      if params == nil then params = { type = "object", properties = {} } end
      table.insert(out, { type = "function", name = name, description = desc, parameters = params })
    end
  end
  return out
end

local function extract_responses_text(raw)
  local output = raw.output
  if type(output) == "table" then
    for _, item in ipairs(output) do
      if type(item) == "table" and item.type == "message" and type(item.content) == "table" then
        for _, c in ipairs(item.content) do
          if type(c) == "table" then
            if type(c.text) == "string" and c.text ~= "" then return c.text end
            if type(c.output_text) == "string" and c.output_text ~= "" then return c.output_text end
          end
        end
      end
    end
  end
  if type(raw.output_text) == "string" then return raw.output_text end
  return ""
end

local function extract_responses_tool_calls(raw)
  local out = {}
  local output = raw.output
  if type(output) ~= "table" then return out end
  for _, item in ipairs(output) do
    if type(item) == "table" and item.type == "function_call" and type(item.name) == "string" and item.name ~= "" then
      local args = item.arguments or "{}"
      if type(args) == "table" then args = json.encode(args) end
      if args == "" then args = "{}" end
      local id = item.call_id or item.id or ""
      table.insert(out, { id = id, type = "function", ["function"] = { name = item.name, arguments = args } })
    end
  end
  return out
end

llm_router.register("opencode-zen", {

  credential_schema = function()
    return {
      { type = "text", text = "Free models work without a key. A key is only needed for paid models." },
      { type = "input", name = "api_key", input_type = "password", label = "API Key (optional)" },
      { type = "button", text = "Save", form_action = "submit" },
    }
  end,

  validate_credentials = function(data)
    local key = data.api_key
    if key ~= nil and key ~= "" and #key < 20 then
      return false, { type = "invalid_request", message = "api_key: minimum 20 characters" }
    end
    return true
  end,

  get_model_infos = function(ctx, credential, provider_config)
    local client = llm_router.create_http_client({})
    local headers = opencode_headers({})
    local key = api_key_of(credential)
    if key ~= "" then headers["Authorization"] = "Bearer " .. key end
    local resp, err = client:request({
      method = "GET", url = BASE_URL .. "/models",
      headers = headers,
    })
    if err then
      -- Upstream listing failed: fall back to the known model set.
      return with_limits(FALLBACK_MODELS)
    end
    if resp.status ~= 200 then
      return with_limits(FALLBACK_MODELS)
    end
    local ok, parsed = pcall(json.decode, resp.body)
    if not ok or not parsed or type(parsed.data) ~= "table" then
      return with_limits(FALLBACK_MODELS)
    end
    local infos = {}
    for _, m in ipairs(parsed.data) do
      if type(m.id) == "string" and m.id ~= "" then
        table.insert(infos, { name = m.id, display_name = m.id })
      end
    end
    if #infos == 0 then
      return with_limits(FALLBACK_MODELS)
    end
    return with_limits(infos)
  end,

  complete = function(ctx, credential, request)
    local model = request.model:match("([^/]+)$")
    local endpoint = endpoint_for_model(model)
    local client = llm_router.create_http_client({})

    local api_key = api_key_of(credential)

    if endpoint == "/responses" then
      local payload = { model = model, input = build_responses_input(request.messages), stream = false }
      if request.max_tokens and request.max_tokens > 0 then payload.max_output_tokens = request.max_tokens end
      if request.temperature and request.temperature > 0 then payload.temperature = request.temperature end
      if request.top_p and request.top_p > 0 then payload.top_p = request.top_p end
      local tools = build_responses_tools(request.tools)
      if #tools > 0 then payload.tools = tools end

      local headers = opencode_headers({ ["Content-Type"] = "application/json", ["Accept"] = "application/json" })
      if api_key ~= "" then headers["Authorization"] = "Bearer " .. api_key end
      local resp, err = client:request({
        method = "POST", url = BASE_URL .. "/responses",
        headers = headers, body = json.encode(payload),
      })
      if err then return nil, err end
      if resp.status ~= 200 then return classify_error(resp.status, resp.body) end
      local raw = json.decode(resp.body)
      local text = extract_responses_text(raw)
      local tool_calls = extract_responses_tool_calls(raw)
      local finish = "stop"
      if #tool_calls > 0 then finish = "tool_calls" end
      local usage = { prompt_tokens = 0, completion_tokens = 0, total_tokens = 0 }
      if type(raw.usage) == "table" then
        usage.prompt_tokens = raw.usage.input_tokens or 0
        usage.completion_tokens = raw.usage.output_tokens or 0
        usage.total_tokens = raw.usage.total_tokens or (usage.prompt_tokens + usage.completion_tokens)
      end
      return {
        id = "zen-" .. tostring(os.time()), object = "chat.completion", created = os.time(),
        model = request.model,
        choices = {
          { index = 0, message = { role = "assistant", content = text, tool_calls = tool_calls }, finish_reason = finish },
        },
        usage = usage,
      }
    end

    local payload = { model = model, messages = request.messages, stream = false }
    if request.max_tokens and request.max_tokens > 0 then payload.max_tokens = request.max_tokens end
    if request.temperature and request.temperature > 0 then payload.temperature = request.temperature end
    if request.top_p and request.top_p > 0 then payload.top_p = request.top_p end
    if request.tools then payload.tools = request.tools end
    if request.tool_choice then payload.tool_choice = request.tool_choice end

    local headers = opencode_headers({ ["Content-Type"] = "application/json", ["Accept"] = "application/json" })
    if api_key ~= "" then headers["Authorization"] = "Bearer " .. api_key end
    local resp, err = client:request({
      method = "POST", url = BASE_URL .. endpoint,
      headers = headers,
      body = json.encode(payload),
    })
    if err then return nil, err end
    if resp.status ~= 200 then return classify_error(resp.status, resp.body) end
    local out = json.decode(resp.body)
    if out.model == nil or out.model == "" then out.model = request.model end
    return out
  end,

  -- complete_stream is intentionally not declared: the router emulates
  -- streaming over complete() with a single chunk, which is what the
  -- original client did for free models with unreliable upstream SSE.
})
