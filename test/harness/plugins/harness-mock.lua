--- @plugin Harness Mock
--- @author llm-router
--- @version 1.0.1
--- @plugin_api 1.6
--- @allow_host example.com
--- @description Deterministic three-phase shell scenario for CI harness validation. No network use.
--
-- Every harness selects one model of this plugin. The harness sends one prompt per
-- phase, and the plugin answers each phase with one shell tool call, checks the tool
-- result the harness sends back, and then replies with the phase token:
--
--   PHASE 1  shell call writes phase1.txt, reply "PHASE 1 COMPLETE"
--   PHASE 2  shell call appends phase2.txt, reply "PHASE 2 COMPLETE"
--   PHASE 3  shell call reads both files, reply "HARNESS-MOCK-OK"
--
-- The phase is the newest user turn that carries a "PHASE <n>" marker. The turn is
-- decided from the message history alone, so the plugin keeps no conversation state.
-- Tool call IDs encode the phase and the message count, so a result that belongs to
-- another phase fails the phase check instead of passing silently.
--
-- The router runs this file on Lua 5.1 (gopher-lua). Keep to 5.1 syntax and library.

local PROTOCOL_PREFIX = "harness_p"
local STORAGE_SCOPE = "harness-mock"
local CALL_COUNTER_TTL_SECONDS = 3600
local MAX_CALLS_PER_PHASE = 2
local CREATED_AT = 1700000000

-- Shell profiles. Each model name is the bare model the harness requests through the
-- router, and each profile lists the shell tool names that harness advertises, in
-- preference order. The argument builder matches that tool's published schema.
local SHELL_PROFILES = {
  ["claude-sonnet-4-5"] = {
    tool_candidates = { "Bash" },
    arguments = function(tool_name, command)
      return { command = command }
    end,
  },
  ["codex-harness"] = {
    tool_candidates = { "exec_command", "shell_command", "shell" },
    arguments = function(tool_name, command)
      if tool_name == "shell" then
        return { command = { "bash", "-lc", command } }
      end
      return { cmd = command }
    end,
  },
  ["opencode-harness"] = {
    tool_candidates = { "bash" },
    arguments = function(tool_name, command)
      return { command = command }
    end,
  },
}

-- Phases share one scenario across harnesses. expected lists substrings the phase
-- tool result must contain, and reply is the final text of the phase.
local PHASES = {
  [1] = {
    command = "printf 'phase-1 ok\\n' > phase1.txt && cat phase1.txt",
    expected = { "phase-1 ok" },
    reply = "PHASE 1 COMPLETE",
  },
  [2] = {
    command = "cat phase1.txt && printf 'phase-2 ok\\n' > phase2.txt && cat phase2.txt",
    expected = { "phase-1 ok", "phase-2 ok" },
    reply = "PHASE 2 COMPLETE",
  },
  [3] = {
    command = "cat phase1.txt phase2.txt",
    expected = { "phase-1 ok", "phase-2 ok" },
    reply = "HARNESS-MOCK-OK",
  },
}

local function usage_block()
  return { prompt_tokens = 1, completion_tokens = 1, total_tokens = 2 }
end

local function text_of(message)
  if type(message.content) == "string" then
    return message.content
  end
  return ""
end

local function tool_name_of(tool)
  if type(tool) ~= "table" then
    return nil
  end
  if type(tool["function"]) == "table" and type(tool["function"].name) == "string" then
    return tool["function"].name
  end
  if type(tool.name) == "string" then
    return tool.name
  end
  return nil
end

-- advertised_tool returns the first candidate the request offers, or nil.
local function advertised_tool(request, candidates)
  local offered = {}
  for _, tool in ipairs(request.tools or {}) do
    local name = tool_name_of(tool)
    if name ~= nil then
      offered[name] = true
    end
  end
  for _, candidate in ipairs(candidates) do
    if offered[candidate] then
      return candidate
    end
  end
  return nil
end

-- latest_phase scans user turns from the newest backwards for a PHASE marker and
-- returns the phase number and the run identifier the driver embedded in the prompt.
local function latest_phase(messages)
  for index = #messages, 1, -1 do
    local message = messages[index]
    if message.role == "user" then
      local text = text_of(message)
      local number = string.match(text, "PHASE (%d+)")
      if number ~= nil then
        return tonumber(number), string.match(text, "run=([%w%-_]+)") or "none"
      end
    end
  end
  return nil, nil
end

local function failure(run, phase_number, reason, request)
  local messages = request.messages or {}
  llm_router.log("error", "Harness phase validation fails", {
    run = run,
    phase = tostring(phase_number),
    reason = reason,
    messages = tostring(#messages),
    model = request.model,
  })
  return nil, { message = "harness-mock phase " .. tostring(phase_number) .. " failed: " .. reason, code = "server_error", status = 500 }
end

-- register_call counts tool calls per conversation and phase. A healthy phase issues
-- one call, so a repeated count means the router withheld the tool result from history.
local function register_call(request, phase_number)
  local counter_key = "calls-" .. tostring(request.cache_key) .. "-" .. tostring(phase_number)
  local count = (llm_router.storage.get(STORAGE_SCOPE, counter_key) or 0) + 1
  llm_router.storage.set(STORAGE_SCOPE, counter_key, count, { ttl = CALL_COUNTER_TTL_SECONDS })
  return count
end

-- phase_output_valid checks the tool output against the phase expectations.
local function phase_output_valid(output, phase)
  for _, expected in ipairs(phase.expected) do
    if string.find(output, expected, 1, true) == nil then
      return false
    end
  end
  return true
end

-- completed_reply returns the phase reply when history already holds a valid
-- tool result for this phase. Harnesses may follow the result with trailing
-- user text (reminders, retries); re-issuing the call then only trips the
-- loop guard, so completion is idempotent. A withheld result still issues a
-- new call through the guard below.
local function completed_reply(messages, phase_number, phase)
  local prefix = PROTOCOL_PREFIX .. tostring(phase_number) .. "_"
  for _, message in ipairs(messages) do
    if message.role == "tool" then
      local call_id = message.tool_call_id or ""
      if string.sub(call_id, 1, #prefix) == prefix and phase_output_valid(text_of(message), phase) then
        return phase.reply
      end
    end
  end
  return nil
end

-- decide returns the turn for one chat request, either a tool call or a text reply.
-- Requests without tools or without a phase marker are auxiliary (title generation,
-- summaries) and get a plain text reply. A failed check returns nil and a terminal
-- error table, which the router renders to the client.
local function decide(request)
  local messages = request.messages or {}
  if #(request.tools or {}) == 0 then
    return { kind = "text", text = "harness-mock auxiliary" }
  end
  local phase_number, run = latest_phase(messages)
  if phase_number == nil then
    return { kind = "text", text = "harness-mock auxiliary" }
  end
  local phase = PHASES[phase_number]
  if phase == nil then
    return failure(run, phase_number, "unknown_phase", request)
  end
  local profile = SHELL_PROFILES[request.model_name]
  if profile == nil then
    return failure(run, phase_number, "unknown_model", request)
  end
  local tool_name = advertised_tool(request, profile.tool_candidates)
  if tool_name == nil then
    return failure(run, phase_number, "shell_tool_missing", request)
  end
  local last = messages[#messages]
  if last == nil then
    return failure(run, phase_number, "empty_history", request)
  end

  if last.role == "user" then
    local reply = completed_reply(messages, phase_number, phase)
    if reply ~= nil then
      llm_router.log("info", "Harness phase completes", {
        run = run,
        phase = tostring(phase_number),
        step = "complete",
        messages = tostring(#messages),
        tools = tostring(#request.tools),
        model = request.model,
      })
      return { kind = "text", text = reply }
    end
    local count = register_call(request, phase_number)
    if count > MAX_CALLS_PER_PHASE then
      return failure(run, phase_number, "tool_call_repeated", request)
    end
    llm_router.log("info", "Harness phase issues tool call", {
      run = run,
      phase = tostring(phase_number),
      step = "call",
      messages = tostring(#messages),
      tools = tostring(#request.tools),
      model = request.model,
    })
    return {
      kind = "tool",
      id = PROTOCOL_PREFIX .. tostring(phase_number) .. "_m" .. tostring(#messages),
      name = tool_name,
      arguments = json.encode(profile.arguments(tool_name, phase.command)),
    }
  end

  if last.role == "tool" then
    local prefix = PROTOCOL_PREFIX .. tostring(phase_number) .. "_"
    local call_id = last.tool_call_id or ""
    if string.sub(call_id, 1, #prefix) ~= prefix then
      return failure(run, phase_number, "tool_result_phase_mismatch", request)
    end
    local output = text_of(last)
    if not phase_output_valid(output, phase) then
      return failure(run, phase_number, "expected_output_missing", request)
    end
    llm_router.log("info", "Harness phase completes", {
      run = run,
      phase = tostring(phase_number),
      step = "complete",
      messages = tostring(#messages),
      tools = tostring(#request.tools),
      model = request.model,
    })
    return { kind = "text", text = phase.reply }
  end

  return failure(run, phase_number, "unexpected_last_role", request)
end

local function tool_call_of(turn)
  return {
    id = turn.id,
    type = "function",
    ["function"] = { name = turn.name, arguments = turn.arguments },
  }
end

local function completion_of(request, turn)
  local base = {
    id = "chatcmpl-harness-mock",
    object = "chat.completion",
    created = CREATED_AT,
    model = request.model,
    usage = usage_block(),
  }
  if turn.kind == "tool" then
    base.choices = {
      {
        index = 0,
        message = { role = "assistant", tool_calls = { tool_call_of(turn) } },
        finish_reason = "tool_calls",
      },
    }
    return base
  end
  base.choices = {
    { index = 0, message = { role = "assistant", content = turn.text }, finish_reason = "stop" },
  }
  return base
end

local function chunk_of(request, delta, finish_reason)
  local body = {
    id = "chatcmpl-harness-mock",
    object = "chat.completion.chunk",
    created = CREATED_AT,
    model = request.model,
    choices = { { index = 0, delta = delta, finish_reason = finish_reason } },
  }
  if finish_reason ~= nil then
    body.usage = usage_block()
  end
  return body
end

llm_router.register("harness-mock", {
  complete = function(ctx, request)
    local turn, terminal = decide(request)
    if terminal ~= nil then
      return nil, terminal
    end
    return completion_of(request, turn)
  end,

  -- Streams the same turn as complete: the tool call opens with its name and ID, its
  -- arguments follow in one chunk, and the finish chunk carries the usage.
  -- Every tool chunk carries the call ID: strict clients reject ID-less deltas.
  complete_stream = function(ctx, request, emit)
    local turn, terminal = decide(request)
    if terminal ~= nil then
      return nil, terminal
    end
    if turn.kind == "tool" then
      emit(chunk_of(request, {
        role = "assistant",
        tool_calls = { { id = turn.id, type = "function", ["function"] = { name = turn.name, arguments = "" } } },
      }, nil))
      emit(chunk_of(request, {
        tool_calls = { { id = turn.id, type = "function", ["function"] = { arguments = turn.arguments } } },
      }, nil))
      emit(chunk_of(request, {}, "tool_calls"))
      return
    end
    emit(chunk_of(request, { role = "assistant", content = turn.text }, nil))
    emit(chunk_of(request, {}, "stop"))
  end,

  get_model_infos = function(ctx)
    return {
      { name = "claude-sonnet-4-5", display_name = "Harness Mock Claude", endpoints = { "chat/completions" }, capabilities = { "tools" } },
      { name = "codex-harness", display_name = "Harness Mock Codex", endpoints = { "chat/completions" }, capabilities = { "tools" } },
      { name = "opencode-harness", display_name = "Harness Mock OpenCode", endpoints = { "chat/completions" }, capabilities = { "tools" } },
    }
  end,
})
