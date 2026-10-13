// Package mockplugin owns deterministic Lua plugin fixtures for Go tests.
//
// Each builder returns one small source with a distinct type key, one model
// card per capability, and no network use. Tests install a fixture with
// Install, create a provider instance for its type key, and route by
// provider ID plus Fixture.Models entry.
package mockplugin

// Fixture is one deterministic Lua plugin source: its type key, the bare
// model names its get_model_infos declares, the handlers it implements,
// and the source itself.
type Fixture struct {
	PluginName string
	TypeKey    string
	Models     []string
	Handlers   []string
	Source     string
}

// ModelID builds the provider/model id for the fixture's first model.
func (fixture Fixture) ModelID(providerID string) string {
	return providerID + "/" + fixture.Models[0]
}

const manifestTail = `--- @author llm-router
--- @version 1.0.0
--- @plugin_api 1.2
--- @allow_host example.com
`

const commonChecks = `
  validate_credentials = function(data)
    return true
  end,

  check_health = function(ctx, credential)
    return { status = "healthy" }
  end,
`

const chatCompletion = `
  complete = function(ctx, request)
    return {
      id = "mockplugin-chat", object = "chat.completion", created = 1700000000, model = request.model,
      choices = {
        { index = 0, message = { role = "assistant", content = "mockplugin ok" }, finish_reason = "stop" },
      },
      usage = { prompt_tokens = 2, completion_tokens = 2, total_tokens = 4 },
    }
  end,
`

func modelInfos(entries string) string {
	return `
  get_model_infos = function(ctx)
    return { ` + entries + ` }
  end,
`
}

func assemble(pluginName, typeKey string, models, handlers []string, modelEntries, handlerBodies string) Fixture {
	return Fixture{
		PluginName: pluginName,
		TypeKey:    typeKey,
		Models:     models,
		Handlers:   handlers,
		Source: "--- @plugin " + pluginName + "\n" + manifestTail +
			"--- @description Deterministic " + pluginName + " fixture for Go tests. No network use.\n" +
			"\nllm_router.register(\"" + typeKey + "\", {" +
			commonChecks + modelInfos(modelEntries) + handlerBodies + "})\n",
	}
}

// Chat serves one chat model with a fixed completion.
func Chat() Fixture {
	return assemble("MockPlugin Chat", "mockplugin-chat",
		[]string{"chat"}, []string{"complete"},
		`{ name = "chat", display_name = "MockPlugin Chat", endpoints = { "chat/completions" } }`,
		chatCompletion)
}

// Tools serves one chat model echoing the first requested tool call.
func Tools() Fixture {
	return assemble("MockPlugin Tools", "mockplugin-tools",
		[]string{"tools"}, []string{"complete"},
		`{ name = "tools", display_name = "MockPlugin Tools", endpoints = { "chat/completions" } }`,
		`
  complete = function(ctx, request)
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
      id = "mockplugin-tools", object = "chat.completion", created = 1700000000, model = request.model,
      choices = {
        { index = 0,
          message = { role = "assistant", tool_calls = {
            { id = "call_mockplugin", type = "function",
              ["function"] = { name = name, arguments = "{}" } },
          } },
          finish_reason = "tool_calls" },
      },
      usage = { prompt_tokens = 2, completion_tokens = 2, total_tokens = 4 },
    }
  end,
`)
}

// Limited serves one chat model reporting quota exhaustion on every call.
func Limited() Fixture {
	return assemble("MockPlugin Limited", "mockplugin-limited",
		[]string{"limited"}, []string{"complete"},
		`{ name = "limited", display_name = "MockPlugin Limited", endpoints = { "chat/completions" } }`,
		`
  complete = function(ctx, request)
    return nil, { message = "mockplugin quota", code = "insufficient_quota", status = 429 }
  end,
`)
}

// Denied serves one chat model reporting an authentication failure.
func Denied() Fixture {
	return assemble("MockPlugin Denied", "mockplugin-denied",
		[]string{"denied"}, []string{"complete"},
		`{ name = "denied", display_name = "MockPlugin Denied", endpoints = { "chat/completions" } }`,
		`
  complete = function(ctx, request)
    return nil, { message = "mockplugin denied", code = "authentication_error", status = 401 }
  end,
`)
}

// Stream serves one chat model emitting two chunks per stream.
func Stream() Fixture {
	return assemble("MockPlugin Stream", "mockplugin-stream",
		[]string{"stream"}, []string{"complete", "complete_stream"},
		`{ name = "stream", display_name = "MockPlugin Stream", endpoints = { "chat/completions" } }`,
		chatCompletion+`
  complete_stream = function(ctx, request, emit)
    emit({ id = "mockplugin-stream", object = "chat.completion.chunk", created = 1700000000, model = request.model,
      choices = { { index = 0, delta = { role = "assistant", content = "mockplugin " } } } })
    emit({ id = "mockplugin-stream", object = "chat.completion.chunk", created = 1700000000, model = request.model,
      choices = { { index = 0, delta = { content = "ok" }, finish_reason = "stop" } } })
  end,
`)
}

// Transcribe serves one transcription model with text plus segments.
func Transcribe() Fixture {
	return assemble("MockPlugin Transcribe", "mockplugin-transcribe",
		[]string{"transcribe"}, []string{"complete", "transcribe"},
		`{ name = "transcribe", display_name = "MockPlugin Transcribe", endpoints = { "audio/transcriptions" } }`,
		chatCompletion+`
  transcribe = function(ctx, request)
    return { text = "mockplugin transcript",
      segments = { { id = 0, start = 0.0, ["end"] = 1.0, text = "mockplugin transcript" } } }
  end,
`)
}

// Speech serves one speech model returning fixed WAV bytes.
func Speech() Fixture {
	return assemble("MockPlugin Speech", "mockplugin-speech",
		[]string{"speech"}, []string{"complete", "speech"},
		`{ name = "speech", display_name = "MockPlugin Speech", endpoints = { "audio/speech" } }`,
		chatCompletion+`
  speech = function(ctx, request)
    return { audio_b64 = "RkFLRVdBVg==", format = "wav" }
  end,
`)
}

// Image serves one image model honoring url and b64_json formats.
func Image() Fixture {
	return assemble("MockPlugin Image", "mockplugin-image",
		[]string{"image"}, []string{"complete", "generate_image"},
		`{ name = "image", display_name = "MockPlugin Image", endpoints = { "images/generations" } }`,
		chatCompletion+`
  generate_image = function(ctx, request)
    if request.response_format == "url" then
      return { created = 1700000000, data = { { url = "http://example.com/mockplugin.png" } } }
    end
    return { created = 1700000000, data = { { b64_json = "aGk=" } } }
  end,
`)
}

// Embeddings serves one embeddings model returning one vector per input.
func Embeddings() Fixture {
	return assemble("MockPlugin Embeddings", "mockplugin-embeddings",
		[]string{"embeddings"}, []string{"complete", "embed"},
		`{ name = "embeddings", display_name = "MockPlugin Embeddings", endpoints = { "embeddings" } }`,
		chatCompletion+`
  embed = function(ctx, request)
    local data = {}
    for i in ipairs(request.input) do
      table.insert(data, { embedding = { 0.1, 0.2, 0.3 } })
    end
    return { data = data }
  end,
`)
}

const videoBodies = `
  poll_video = function(ctx, request)
    return {
      id = request.job_id,
      polling_url = "/v1/videos/" .. request.job_id,
      status = "completed",
      generation_id = request.job_id,
      unsigned_urls = { "http://example.com/mockplugin.mp4" },
    }
  end,

  video_content = function(ctx, request)
    local function be32(n)
      return string.char(
        math.floor(n / 16777216) % 256,
        math.floor(n / 65536) % 256,
        math.floor(n / 256) % 256,
        n % 256)
    end
    local mp4 = be32(24) .. "ftyp" .. "isom" .. be32(0) .. "isom" .. "iso2"
      .. be32(16) .. "mdat" .. "MOCKPLUGINMP4"
    return { video_b64 = llm_router.base64_encode(mp4), content_type = "video/mp4" }
  end,
`

// Video serves one video model completing the job at submit time.
func Video() Fixture {
	return assemble("MockPlugin Video", "mockplugin-video",
		[]string{"video"}, []string{"complete", "generate_video", "poll_video", "video_content"},
		`{ name = "video", display_name = "MockPlugin Video", endpoints = { "videos" } }`,
		chatCompletion+`
  generate_video = function(ctx, request)
    local id = "up-mockplugin-completed"
    return {
      id = id,
      polling_url = "/v1/videos/" .. id,
      status = "completed",
      generation_id = id,
      unsigned_urls = { "http://example.com/mockplugin.mp4" },
    }
  end,
`+videoBodies)
}

// VideoPending serves one video model pending at submit, completed on poll.
func VideoPending() Fixture {
	return assemble("MockPlugin Video Pending", "mockplugin-video-pending",
		[]string{"video"}, []string{"complete", "generate_video", "poll_video", "video_content"},
		`{ name = "video", display_name = "MockPlugin Video Pending", endpoints = { "videos" } }`,
		chatCompletion+`
  generate_video = function(ctx, request)
    local id = "up-mockplugin-pending"
    return {
      id = id,
      polling_url = "/v1/videos/" .. id,
      status = "pending",
      generation_id = id,
    }
  end,
`+videoBodies)
}

// ChatOnly serves one chat-only model with no other handlers.
func ChatOnly() Fixture {
	return assemble("MockPlugin Chat Only", "mockplugin-chat-only",
		[]string{"chat"}, []string{"complete"},
		`{ name = "chat", display_name = "MockPlugin Chat Only", endpoints = { "chat/completions" } }`,
		chatCompletion)
}

// All returns every builder for install coverage.
func All() []Fixture {
	return []Fixture{
		Chat(), Tools(), Limited(), Denied(), Stream(),
		Transcribe(), Speech(), Image(), Embeddings(),
		Video(), VideoPending(), ChatOnly(),
	}
}
