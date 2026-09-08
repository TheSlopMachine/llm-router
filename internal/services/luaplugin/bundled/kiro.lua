--- @plugin Kiro AI
--- @author TheSlopMachine
--- @version 1.0.2
--- @router_version 0.0.4
--- @description AWS Kiro models via device login (OAuth2 with proactive refresh)
--- @allow_host codewhisperer.us-east-1.amazonaws.com
--- @allow_host oidc.us-east-1.amazonaws.com

local DEFAULT_REGION = "us-east-1"
local BUILDER_START_URL = "https://view.awsapps.com/start"
local ISSUER_URL = "https://identitycenter.amazonaws.com/ssoins-722374e8c3c8e6c6"
local GENERATE_URL = "https://codewhisperer.us-east-1.amazonaws.com/generateAssistantResponse"
local CONVERSATION_NS = "34f7193f-561d-4050-bc84-9547d953d6bf"

local function oidc_url(region, path)
  return "https://oidc." .. region .. ".amazonaws.com/" .. path
end

local function region_of(credential, provider_config)
  if credential and credential.data and credential.data.region and credential.data.region ~= "" then
    return credential.data.region
  end
  if provider_config and provider_config.region and provider_config.region ~= "" then
    return provider_config.region
  end
  return DEFAULT_REGION
end

local function classify_error(status, body)
  local message = body
  local ok, parsed = pcall(json.decode, body)
  if ok and type(parsed) == "table" then
    if type(parsed.message) == "string" and parsed.message ~= "" then
      message = parsed.message
    elseif type(parsed.error) == "table" and type(parsed.error.message) == "string" and parsed.error.message ~= "" then
      message = parsed.error.message
    end
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

-- ── Binary AWS event-stream framing ──

local function u32be(s, pos)
  local a, b, c, d = string.byte(s, pos, pos + 3)
  return ((a * 256 + b) * 256 + c) * 256 + d
end

-- Split a buffer into complete frames, returning frames + remainder.
local function split_frames(buf)
  local frames = {}
  local pos = 1
  while #buf - pos + 1 >= 16 do
    local total = u32be(buf, pos)
    if total < 16 or pos + total - 1 > #buf then break end
    table.insert(frames, buf:sub(pos, pos + total - 1))
    pos = pos + total
  end
  return frames, buf:sub(pos)
end

local function parse_frame(frame)
  local headers_len = u32be(frame, 5)
  local headers = {}
  local off = 13
  local header_end = 12 + headers_len
  while off < header_end do
    local name_len = string.byte(frame, off)
    off = off + 1
    local name = frame:sub(off, off + name_len - 1)
    off = off + name_len
    local vtype = string.byte(frame, off)
    off = off + 1
    if vtype ~= 7 then return nil end
    local vlen = string.byte(frame, off) * 256 + string.byte(frame, off + 1)
    off = off + 2
    headers[name] = frame:sub(off, off + vlen - 1)
    off = off + vlen
  end
  local payload = {}
  local payload_end = #frame - 4
  if payload_end > header_end then
    local raw = frame:sub(header_end + 1, payload_end):match("^%s*(.-)%s*$")
    if raw ~= "" then
      local ok, parsed = pcall(json.decode, raw)
      if ok and type(parsed) == "table" then
        payload = parsed
      else
        payload = { raw = raw }
      end
    end
  end
  return { headers = headers, payload = payload }
end

local function new_aggregate()
  return { content = {}, prompt_tokens = 0, completion_tokens = 0, total_tokens = 0,
           finish_reason = "", tool_calls = {}, builders = {}, order = {} }
end

local function flush_builder(state, id)
  local b = state.builders[id]
  if b == nil or b.name == nil or b.name == "" then return end
  local args = b.args:match("^%s*(.-)%s*$")
  if args == "" then args = "{}" end
  table.insert(state.tool_calls, { id = id, type = "function",
    ["function"] = { name = b.name, arguments = args } })
  state.finish_reason = "tool_calls"
  state.builders[id] = nil
end

local function consume_event(state, frame)
  local etype = frame.headers[":event-type"]
  local p = frame.payload
  if etype == "assistantResponseEvent" or etype == "codeEvent" then
    local content = p.content
    if type(content) == "string" and content ~= "" then
      table.insert(state.content, content)
    end
  elseif etype == "metricsEvent" then
    local m = p.metricsEvent or p
    state.prompt_tokens = m.inputTokens or 0
    state.completion_tokens = m.outputTokens or 0
    state.total_tokens = state.prompt_tokens + state.completion_tokens
  elseif etype == "toolUseEvent" then
    local id = p.toolUseId or ""
    if id == "" then id = "call_" .. tostring(os.time()) end
    local b = state.builders[id]
    if b == nil then
      b = { name = "", args = "" }
      state.builders[id] = b
      table.insert(state.order, id)
    end
    if type(p.name) == "string" and p.name ~= "" then b.name = p.name end
    local is_string_input = false
    if p.input ~= nil then
      if type(p.input) == "string" then
        b.args = b.args .. p.input
        is_string_input = true
      else
        b.args = json.encode(p.input)
      end
    end
    local should_emit = false
    if p.stop == true then should_emit = true
    elseif not is_string_input and p.input ~= nil and b.name ~= "" then should_emit = true end
    if should_emit then flush_builder(state, id) end
  elseif etype == "messageStopEvent" then
    if state.finish_reason == "" then state.finish_reason = "stop" end
  end
end

local function aggregate_response(body)
  local state = new_aggregate()
  local frames = split_frames(body)
  for _, f in ipairs(frames) do
    local frame = parse_frame(f)
    if frame then consume_event(state, frame) end
  end
  for _, id in ipairs(state.order) do
    if state.builders[id] and state.builders[id].name ~= "" then
      flush_builder(state, id)
    end
  end
  return state
end

-- ── Request transform ──

-- Plain-text view of a chat message. Content arrives either as a string
-- or as an array of content parts (multimodal/tool messages).
local function message_text(m)
  local content = m.content
  if type(content) == "string" then return content end
  if type(content) ~= "table" then return "" end
  local parts = {}
  for _, p in ipairs(content) do
    if type(p) == "string" then
      if p ~= "" then table.insert(parts, p) end
    elseif type(p) == "table" and type(p.text) == "string" and p.text ~= "" then
      table.insert(parts, p.text)
    end
  end
  return table.concat(parts, "\n")
end

local function build_tool_specs(tools)
  local specs = {}
  for _, t in ipairs(tools or {}) do
    local name = t.name or ""
    local desc = t.description or ""
    local schema = t.parameters
    if t["function"] then
      if t["function"].name and t["function"].name ~= "" then name = t["function"].name end
      if t["function"].description and t["function"].description ~= "" then desc = t["function"].description end
      if t["function"].parameters then schema = t["function"].parameters end
    end
    if name ~= "" then
      if desc == "" then desc = "Tool: " .. name end
      if schema == nil then schema = { type = "object", properties = {} } end
      table.insert(specs, { toolSpecification = {
        name = name, description = desc, inputSchema = { json = schema }, strict = true } })
    end
  end
  return specs
end

local function convert_messages(messages, tools, model)
  local history = {}
  local current_role = nil
  local user_parts = {}
  local assistant_parts = {}
  local tool_results = {}

  local function flush_user()
    local content = table.concat(user_parts, "\n\n"):match("^%s*(.-)%s*$")
    if content == "" and #tool_results == 0 then user_parts = {}; return end
    local msg = { content = content, modelId = model }
    if msg.content == "" then msg.content = "continue" end
    if #tool_results > 0 then
      msg.userInputMessageContext = { toolResults = tool_results }
    end
    if #tools > 0 and #history == 0 then
      msg.userInputMessageContext = msg.userInputMessageContext or {}
      msg.userInputMessageContext.tools = build_tool_specs(tools)
    end
    table.insert(history, { userInputMessage = msg })
    user_parts = {}
    tool_results = {}
  end

  local function flush_assistant()
    local content = table.concat(assistant_parts, "\n\n"):match("^%s*(.-)%s*$")
    if content == "" then content = "..." end
    table.insert(history, { assistantResponseMessage = { content = content } })
    assistant_parts = {}
  end

  for _, m in ipairs(messages or {}) do
    local role = "user"
    if m.role == "assistant" then role = "assistant" end
    if current_role and current_role ~= role then
      if current_role == "user" then flush_user() else flush_assistant() end
    end
    current_role = role
    if role == "assistant" then
      local text = message_text(m):match("^%s*(.-)%s*$")
      if text ~= "" then table.insert(assistant_parts, text) end
      local uses = {}
      for _, tc in ipairs(m.tool_calls or {}) do
        local name = tc["function"] and tc["function"].name or ""
        if name ~= "" then
          local args = tc["function"].arguments or "{}"
          local ok, parsed = pcall(json.decode, args)
          if not ok or type(parsed) ~= "table" then parsed = {} end
          table.insert(uses, { toolUseId = tc.id or "", name = name, input = parsed })
        end
      end
      if #uses > 0 then
        flush_assistant()
        history[#history].assistantResponseMessage.toolUses = uses
        current_role = nil
      end
    else
      if m.role == "tool" then
        table.insert(tool_results, { toolUseId = m.tool_call_id or "",
          status = "success", content = { { text = message_text(m) } } })
      else
        local text = message_text(m):match("^%s*(.-)%s*$")
        if text ~= "" then table.insert(user_parts, text) end
      end
    end
  end
  if current_role == "user" then flush_user()
  elseif current_role == "assistant" then flush_assistant() end

  local current = nil
  if #history > 0 and history[#history].userInputMessage then
    current = history[#history].userInputMessage
    table.remove(history)
  end
  if current == nil or current.content == "" then
    current = { content = "continue", modelId = model }
  end
  if current.modelId == nil or current.modelId == "" then current.modelId = model end
  if #tools > 0 then
    current.userInputMessageContext = current.userInputMessageContext or {}
    if current.userInputMessageContext.tools == nil then
      current.userInputMessageContext.tools = build_tool_specs(tools)
    end
  end
  for _, entry in ipairs(history) do
    local e = entry.userInputMessage
    if e and e.userInputMessageContext then
      e.userInputMessageContext.tools = nil
      if e.userInputMessageContext.toolResults == nil then
        e.userInputMessageContext = nil
      end
    end
    if e and (e.modelId == nil or e.modelId == "") then e.modelId = model end
  end
  return history, current
end

local function conversation_id(history, current_content)
  local seed = current_content or ""
  if #history > 0 and history[1].userInputMessage
      and type(history[1].userInputMessage.content) == "string"
      and history[1].userInputMessage.content ~= "" then
    seed = history[1].userInputMessage.content
  end
  if #seed > 4000 then seed = seed:sub(1, 4000) end
  return llm_router.uuid_v5(CONVERSATION_NS, seed)
end

local function build_payload(request, model)
  local history, current = convert_messages(request.messages, request.tools or {}, model)
  if current.content == "" then current.content = "continue" end
  current.content = "[Context: Current time is " .. os.date("!%Y-%m-%dT%H:%M:%SZ") .. "]\n\n" .. current.content
  current.origin = "AI_EDITOR"
  local state = {
    chatTriggerType = "MANUAL",
    conversationId = conversation_id(history, current.content),
    currentMessage = { userInputMessage = current },
  }
  if #history > 0 then state.history = history end
  local payload = { conversationState = state }
  if (request.max_tokens and request.max_tokens > 0)
      or (request.temperature and request.temperature > 0)
      or (request.top_p and request.top_p > 0) then
    payload.inferenceConfig = {}
    if request.max_tokens and request.max_tokens > 0 then payload.inferenceConfig.maxTokens = request.max_tokens end
    if request.temperature and request.temperature > 0 then payload.inferenceConfig.temperature = request.temperature end
    if request.top_p and request.top_p > 0 then payload.inferenceConfig.topP = request.top_p end
  end
  return payload
end

local function generate_headers(credential)
  local token = ""
  if credential and credential.data then token = credential.data.access_token or "" end
  return {
    ["Authorization"] = "Bearer " .. token,
    ["Content-Type"] = "application/json",
    ["Accept"] = "application/vnd.amazon.eventstream",
    ["X-Amz-Target"] = "AmazonCodeWhispererStreamingService.GenerateAssistantResponse",
    ["User-Agent"] = "AWS-SDK-JS/3.0.0 kiro-ide/1.0.0",
    ["X-Amz-User-Agent"] = "aws-sdk-js/3.0.0 kiro-ide/1.0.0",
    ["Amz-Sdk-Request"] = "attempt=1; max=3",
    ["Amz-Sdk-Invocation-Id"] = llm_router.random_hex(16),
    ["x-amzn-bedrock-cache-control"] = "enable",
    ["anthropic-beta"] = "prompt-caching-2024-07-31",
  }
end

-- ── OAuth device flow (auth wizard) ──

local function start_page(error_text, region, start_url, method)
  local nodes = {}
  if error_text and error_text ~= "" then
    table.insert(nodes, { type = "banner", variant = "error", text = error_text })
  end
  table.insert(nodes, { type = "text", text = "Sign in with the same Kiro account you use in the IDE." })
  table.insert(nodes, { type = "select", name = "device_method", label = "Method",
    options = { "builder-id", "idc" }, value = method or "builder-id" })
  table.insert(nodes, { type = "input", name = "region", label = "Region", value = region or DEFAULT_REGION })
  table.insert(nodes, { type = "input", name = "start_url", label = "Start URL", value = start_url or BUILDER_START_URL })
  table.insert(nodes, { type = "button", text = "Start Device Login", form_action = "start_device" })
  return { render = nodes }
end

local function device_page(message_text, state)
  local nodes = {}
  if message_text and message_text ~= "" then
    table.insert(nodes, { type = "banner", variant = "info", text = message_text })
  end
  table.insert(nodes, { type = "text", text = "Open the verification page and enter code " .. (state.user_code or "") })
  table.insert(nodes, { type = "link", text = "Open verification page",
    url = state.verification_uri_complete or state.verification_uri or "" })
  table.insert(nodes, { type = "button", text = "Check Authorization", form_action = "poll_device" })
  table.insert(nodes, { type = "button", text = "Start Over", form_action = "restart" })
  return { render = nodes }
end

local function flow_scope(flow_id)
  return "auth_flow:" .. flow_id
end

llm_router.register("kiro", {
  icon = "https://kiro.dev/favicon.ico",

  config_schema = function()
    return {
      { type = "select", name = "region", label = "Region", options = { "us-east-1" } },
    }
  end,

  credential_schema = function()
    return {
      { type = "text", text = "Paste tokens from a previous login, or use device login instead." },
      { type = "input", name = "access_token", input_type = "password", label = "Access Token" },
      { type = "input", name = "refresh_token", input_type = "password", label = "Refresh Token" },
      { type = "button", text = "Save", form_action = "submit" },
    }
  end,

  validate_credentials = function(data)
    local access = data.access_token or ""
    local refresh = data.refresh_token or ""
    if access == "" and refresh == "" then
      return false, { type = "invalid_request", message = "either access_token or refresh_token is required" }
    end
    if access ~= "" and #access < 20 then
      return false, { type = "invalid_request", message = "access_token appears invalid (too short)" }
    end
    if refresh ~= "" and #refresh < 20 then
      return false, { type = "invalid_request", message = "refresh_token appears invalid (too short)" }
    end
    return true
  end,

  get_model_infos = function(ctx, credential, provider_config)
    return {
      { name = "claude-opus-4.7", display_name = "Claude Opus 4.7", context_window = 200000, max_tokens = 32000 },
      { name = "claude-opus-4.6", display_name = "Claude Opus 4.6", context_window = 200000, max_tokens = 32000 },
      { name = "claude-sonnet-4.6", display_name = "Claude Sonnet 4.6", context_window = 200000, max_tokens = 32000 },
      { name = "claude-sonnet-4.5", display_name = "Claude Sonnet 4.5", context_window = 200000, max_tokens = 32000 },
      { name = "claude-haiku-4.5", display_name = "Claude Haiku 4.5", context_window = 200000, max_tokens = 32000 },
    }
  end,

  needs_refresh = function(credential)
    local data = credential.data or {}
    if not data.refresh_token or data.refresh_token == "" then return false end
    if not data.access_token or data.access_token == "" then return true end
    if not data.expires_at or data.expires_at == "" then return false end
    local now = os.time()
    local exp = nil
    local y, mo, d, h, mi, s = data.expires_at:match("^(%d+)-(%d+)-(%d+)T(%d+):(%d+):(%d+)")
    if y then
      exp = os.time({ year = tonumber(y), month = tonumber(mo), day = tonumber(d),
        hour = tonumber(h), min = tonumber(mi), sec = tonumber(s) })
    else
      exp = tonumber(data.expires_at)
    end
    if exp == nil then return false end
    return (exp - now) <= 300
  end,

  refresh_credential = function(ctx, credential)
    local data = credential.data or {}
    local region = data.region
    if not region or region == "" then region = DEFAULT_REGION end
    local client = llm_router.create_http_client({})
    local resp, err = client:request({
      method = "POST", url = oidc_url(region, "token"),
      headers = { ["Content-Type"] = "application/json", ["Accept"] = "application/json" },
      body = json.encode({
        clientId = data.client_id or "", clientSecret = data.client_secret or "",
        refreshToken = data.refresh_token or "", grantType = "refresh_token",
      }),
    })
    if err then return nil, err end
    if resp.status ~= 200 then return classify_error(resp.status, resp.body) end
    local out = json.decode(resp.body)
    if not out.accessToken or out.accessToken == "" then
      return nil, { type = "auth", message = "refresh response did not include an access token" }
    end
    local merged = {}
    for k, v in pairs(data) do merged[k] = v end
    merged.access_token = out.accessToken
    if out.refreshToken and out.refreshToken ~= "" then merged.refresh_token = out.refreshToken end
    if out.expiresIn and out.expiresIn > 0 then
      merged.expires_at = os.date("!%Y-%m-%dT%H:%M:%SZ", os.time() + out.expiresIn)
    end
    return merged
  end,

  auth_initiate = function(ctx)
    return start_page("", DEFAULT_REGION, BUILDER_START_URL, "builder-id")
  end,

  auth_step = function(ctx, input)
    local action = input.action or ""
    local values = input.values or {}
    local scope = flow_scope(ctx.flow_id or "")

    if action == "restart" or action == "cancel" then
      llm_router.storage.delete(scope, "device")
      return start_page("", DEFAULT_REGION, BUILDER_START_URL, "builder-id")
    end

    if action == "start_device" then
      local method = values.device_method or "builder-id"
      if method == "" then method = "builder-id" end
      local region = values.region or DEFAULT_REGION
      if region == "" then region = DEFAULT_REGION end
      local start_url = values.start_url or BUILDER_START_URL
      if start_url == "" then start_url = BUILDER_START_URL end
      if method ~= "builder-id" and method ~= "idc" then
        return start_page("Unsupported device login method.", region, start_url, method)
      end
      if method == "builder-id" then start_url = BUILDER_START_URL end

      local client = llm_router.create_http_client({})
      local reg_resp, reg_err = client:request({        method = "POST", url = oidc_url(region, "client/register"),
        headers = { ["Content-Type"] = "application/json" },
        body = json.encode({
          clientName = "kiro-oauth-client", clientType = "public",
          scopes = { "codewhisperer:completions", "codewhisperer:analysis", "codewhisperer:conversations" },
          grantTypes = { "urn:ietf:params:oauth:grant-type:device_code", "refresh_token" },
          issuerUrl = ISSUER_URL,
        }),
      })
      if reg_err then return start_page("Client registration failed.", region, start_url, method) end
      if reg_resp.status ~= 200 then return start_page("Client registration failed.", region, start_url, method) end
      local reg = json.decode(reg_resp.body)
      if not reg.clientId or not reg.clientSecret then
        return start_page("Client registration response was incomplete.", region, start_url, method)
      end

      local dev_resp, dev_err = client:request({
        method = "POST", url = oidc_url(region, "device_authorization"),
        headers = { ["Content-Type"] = "application/json" },
        body = json.encode({ clientId = reg.clientId, clientSecret = reg.clientSecret, startUrl = start_url }),
      })
      if dev_err then return start_page("Device authorization failed.", region, start_url, method) end
      if dev_resp.status ~= 200 then return start_page("Device authorization failed.", region, start_url, method) end
      local dev = json.decode(dev_resp.body)
      if not dev.deviceCode or not dev.userCode then
        return start_page("Device authorization response was incomplete.", region, start_url, method)
      end

      local state = {
        method = method, region = region, start_url = start_url,
        client_id = reg.clientId, client_secret = reg.clientSecret,
        device_code = dev.deviceCode, user_code = dev.userCode,
        verification_uri = dev.verificationUri or "",
        verification_uri_complete = dev.verificationUriComplete or "",
        expires_at = os.time() + (dev.expiresIn or 600),
      }
      llm_router.storage.set(scope, "device", state)
      return device_page("", state)
    end

    if action == "poll_device" or action == "submit" then
      local state = llm_router.storage.get(scope, "device")
      if state == nil then
        return start_page("Device login session expired. Start again.", DEFAULT_REGION, BUILDER_START_URL, "builder-id")
      end
      if state.expires_at and os.time() > state.expires_at then
        llm_router.storage.delete(scope, "device")
        return start_page("Device login expired. Start again.", state.region, state.start_url, state.method)
      end
      local client = llm_router.create_http_client({})
      local resp, err = client:request({
        method = "POST", url = oidc_url(state.region, "token"),
        headers = { ["Content-Type"] = "application/json", ["Accept"] = "application/json" },
        body = json.encode({
          clientId = state.client_id, clientSecret = state.client_secret,
          deviceCode = state.device_code,
          grantType = "urn:ietf:params:oauth:grant-type:device_code",
        }),
      })
      if err then return device_page("Token request failed.", state) end
      local tok = json.decode(resp.body)
      if resp.status ~= 200 or (tok.error and tok.error ~= "") then
        if tok.error == "authorization_pending" or tok.error == "slow_down" then
          local msg = tok.error_description or tok.error
          if msg == "" then msg = "Authorization is still pending. Finish the browser step, then check again." end
          return device_page(msg, state)
        end
        return device_page(tok.error_description or "Device login failed.", state)
      end
      llm_router.storage.delete(scope, "device")
      local creds = { access_token = tok.accessToken, auth_method = state.method }
      if tok.refreshToken and tok.refreshToken ~= "" then creds.refresh_token = tok.refreshToken end
      if state.region and state.region ~= "" then creds.region = state.region end
      if state.client_id then creds.client_id = state.client_id end
      if state.client_secret then creds.client_secret = state.client_secret end
      if tok.expiresIn and tok.expiresIn > 0 then
        creds.expires_at = os.date("!%Y-%m-%dT%H:%M:%SZ", os.time() + tok.expiresIn)
      end
      return { credentials = creds }
    end

    return start_page("Choose a device login method to continue.", DEFAULT_REGION, BUILDER_START_URL, "builder-id")
  end,

  complete = function(ctx, credential, request)
    local model = request.model:match("([^/]+)$")
    local client = llm_router.create_http_client({})
    local resp, err = client:request({
      method = "POST", url = GENERATE_URL,
      headers = generate_headers(credential),
      body = json.encode(build_payload(request, model)),
    })
    if err then return nil, err end
    if resp.status ~= 200 then return classify_error(resp.status, resp.body) end
    local state = aggregate_response(resp.body)
    local text = table.concat(state.content, "")
    local finish = state.finish_reason
    if finish == "" then finish = "stop" end
    if #state.tool_calls > 0 then finish = "tool_calls" end
    local total = state.total_tokens
    if total == 0 then
      local completion = math.max(1, math.floor(#resp.body / 4))
      total = state.prompt_tokens + completion
      state.completion_tokens = completion
    end
    return {
      id = "kiro-" .. tostring(os.time()), object = "chat.completion", created = os.time(),
      model = request.model,
      choices = {
        { index = 0, message = { role = "assistant", content = text, tool_calls = state.tool_calls },
          finish_reason = finish },
      },
      usage = { prompt_tokens = state.prompt_tokens,
        completion_tokens = state.completion_tokens, total_tokens = total },
    }
  end,

  complete_stream = function(ctx, credential, request, emit)
    local model = request.model
    local short_model = request.model:match("([^/]+)$")
    local response_id = "kiro-" .. tostring(os.time())
    local created = os.time()
    local buffer = ""
    local builders = {}
    local saw_tool = false
    local first = true
    local payload = build_payload(request, short_model)
    local stream_err = client_stream_raw(generate_headers(credential), payload, function(bytes)
      buffer = buffer .. bytes
      while true do
        if #buffer < 16 then break end
        local total = u32be(buffer, 1)
        if total < 16 or total > #buffer then break end
        local frame = parse_frame(buffer:sub(1, total))
        buffer = buffer:sub(total + 1)
        if frame then
          local etype = frame.headers[":event-type"]
          local p = frame.payload
          if etype == "assistantResponseEvent" or etype == "codeEvent" then
            if type(p.content) == "string" and p.content ~= "" then
              local delta = { content = p.content }
              if first then delta.role = "assistant" end
              first = false
              emit({ id = response_id, object = "chat.completion.chunk", created = created,
                model = model, choices = { { index = 0, delta = delta } } })
            end
          elseif etype == "toolUseEvent" then
            local id = p.toolUseId or ""
            if id == "" then id = "call_" .. tostring(os.time()) end
            local b = builders[id]
            if b == nil then b = { name = "", args = "" }; builders[id] = b end
            if type(p.name) == "string" and p.name ~= "" then b.name = p.name end
            local is_string = false
            if p.input ~= nil then
              if type(p.input) == "string" then b.args = b.args .. p.input; is_string = true
              else b.args = json.encode(p.input) end
            end
            local should = false
            if p.stop == true then should = true
            elseif not is_string and p.input ~= nil and b.name ~= "" then should = true end
            if should and b.name ~= "" then
              local args = b.args:match("^%s*(.-)%s*$")
              if args == "" then args = "{}" end
              saw_tool = true
              local delta = { tool_calls = { { index = 0, id = id, type = "function",
                ["function"] = { name = b.name, arguments = args } } } }
              if first then delta.role = "assistant" end
              first = false
              emit({ id = response_id, object = "chat.completion.chunk", created = created,
                model = model, choices = { { index = 0, delta = delta } } })
              builders[id] = nil
            end
          end
        end
      end
    end)
    if stream_err then return nil, stream_err end
    local finish = "stop"
    if saw_tool then finish = "tool_calls" end
    emit({ id = response_id, object = "chat.completion.chunk", created = created,
      model = model, choices = { { index = 0, delta = {}, finish_reason = finish } } })
  end,
})

-- Raw event-stream POST with chunked delivery. Declared after register so
-- the closure above resolves it at call time, not at load time.
function client_stream_raw(headers, payload, on_bytes)
  local client = llm_router.create_http_client({})
  return client:stream({
    method = "POST", url = GENERATE_URL,
    headers = headers, body = json.encode(payload),
    on_chunk = function(bytes) on_bytes(bytes) end,
  })
end
