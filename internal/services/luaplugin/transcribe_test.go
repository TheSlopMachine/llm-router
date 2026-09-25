package luaplugin

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/models"
)

const transcribePluginSource = `--- @plugin Transcribe Plugin
--- @author tester
--- @version 1.0.0
--- @router_version 0.1.1
--- @description Transcribe test plugin
--- @allow_host example.com

llm_router.register("stt-type", {
  complete = function(ctx, credential, request)
    return {
      id = "chatcmpl-stt",
      object = "chat.completion",
      created = 1700000000,
      model = request.model,
      choices = {
        { index = 0, message = { role = "assistant", content = "hi" }, finish_reason = "stop" },
      },
      usage = { prompt_tokens = 1, completion_tokens = 1, total_tokens = 2 },
    }
  end,

  transcribe = function(ctx, credential, request)
    -- Echo request fields back through the response for assertions.
    local echoed = request.file .. "|" .. request.file_name .. "|" .. request.content_type
      .. "|" .. tostring(request.needs_segments) .. "|" .. (request.language or "")
    return {
      text = echoed,
      language = "en",
      duration = 1.5,
      segments = {
        { id = 0, start = 0.0, ["end"] = 1.5, text = "hello world" },
      },
    }
  end,
})
`

func TestTranscribe_Success(t *testing.T) {
	svc := setupService(t)
	if _, err := svc.Install([]byte(transcribePluginSource), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install: %v", err)
	}
	if !svc.HasHandler("stt-type", "transcribe") {
		t.Fatal("transcribe handler must be registered")
	}
	resp, err := svc.Transcribe(context.Background(), testMeta("stt-type",
		&models.Credential{ID: "c1", Data: map[string]any{"api_key": "k"}}, "stt-type/whisper-large-v3", nil),
		&models.TranscriptionRequest{
			Model:          "stt-type/whisper-large-v3",
			File:           []byte("FAKEAUDIO\x00\x01\x02"),
			FileName:       "clip.wav",
			ContentType:    "audio/wav",
			Language:       "en",
			ResponseFormat: "srt",
		})
	if err != nil {
		t.Fatalf("transcribe: %v", err)
	}
	want := "FAKEAUDIO\x00\x01\x02|clip.wav|audio/wav|true|en"
	if resp.Text != want {
		t.Fatalf("echo: got %q want %q", resp.Text, want)
	}
	if resp.Language != "en" || resp.Duration != 1.5 {
		t.Fatalf("metadata: %+v", resp)
	}
	if len(resp.Segments) != 1 || resp.Segments[0].Text != "hello world" || resp.Segments[0].End != 1.5 {
		t.Fatalf("segments: %+v", resp.Segments)
	}
}

func TestTranscribe_HandlerNotFound(t *testing.T) {
	svc := setupService(t)
	if _, err := svc.Install([]byte(testPluginSource), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install: %v", err)
	}
	_, err := svc.Transcribe(context.Background(), testMeta("test-type",
		&models.Credential{ID: "c1"}, "test-type/model-a", nil),
		&models.TranscriptionRequest{Model: "test-type/model-a", File: []byte("x"), FileName: "a.wav"})
	if !errors.Is(err, ErrHandlerNotFound) {
		t.Fatalf("expected ErrHandlerNotFound, got %v", err)
	}
}

func TestTranscribe_ContractError(t *testing.T) {
	svc := setupService(t)
	src := strings.Replace(transcribePluginSource, `local echoed = request.file`,
		`do return nil, { type = "rate_limit", message = "slow down" } end
    local echoed = request.file`, 1)
	src = strings.Replace(src, "Transcribe Plugin", "Transcribe Err", 1)
	if _, err := svc.Install([]byte(src), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install: %v", err)
	}
	_, err := svc.Transcribe(context.Background(), testMeta("stt-type",
		&models.Credential{ID: "c1"}, "stt-type/m", nil),
		&models.TranscriptionRequest{Model: "stt-type/m", File: []byte("x"), FileName: "a.wav"})
	perr, ok := err.(*models.ProviderError)
	if !ok || perr.Type != models.ErrorTypeRateLimit {
		t.Fatalf("expected rate_limit ProviderError, got %T (%v)", err, err)
	}
}

func TestMultipartHelper(t *testing.T) {
	svc := setupService(t)
	src := `--- @plugin Multipart Plugin
--- @author tester
--- @version 1.0.0
--- @router_version 0.1.1
--- @description Multipart test plugin
--- @allow_host example.com

llm_router.register("mp-type", {
  complete = function(ctx, credential, request)
    return {
      id = "x", object = "chat.completion", created = 1, model = request.model,
      choices = { { index = 0, message = { role = "assistant", content = "hi" }, finish_reason = "stop" } },
      usage = { prompt_tokens = 1, completion_tokens = 1, total_tokens = 2 },
    }
  end,

  transcribe = function(ctx, credential, request)
    local body, ctype = llm_router.multipart({
      { name = "model", value = "whisper-large-v3" },
      { name = "file", filename = request.file_name, content_type = request.content_type, data = request.file },
      { name = "language", value = "en" },
    })
    return { text = body, language = ctype }
  end,
})
`
	if _, err := svc.Install([]byte(src), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install: %v", err)
	}
	resp, err := svc.Transcribe(context.Background(), testMeta("mp-type",
		&models.Credential{ID: "c1"}, "mp-type/m", nil),
		&models.TranscriptionRequest{
			Model: "mp-type/m", File: []byte("BINARY\x00\x01\x02"), FileName: "a.wav", ContentType: "audio/wav",
		})
	if err != nil {
		t.Fatalf("transcribe: %v", err)
	}
	ctype := resp.Language
	if !strings.HasPrefix(ctype, "multipart/form-data; boundary=") {
		t.Fatalf("content type: %q", ctype)
	}
	boundary := strings.TrimPrefix(ctype, "multipart/form-data; boundary=")
	body := resp.Text
	for _, want := range []string{
		"\r\n--" + boundary + "\r\n",
		`Content-Disposition: form-data; name="model"` + "\r\n\r\nwhisper-large-v3",
		`Content-Disposition: form-data; name="file"; filename="a.wav"` + "\r\n" + `Content-Type: audio/wav` + "\r\n\r\nBINARY\x00\x01\x02",
		`name="language"` + "\r\n\r\nen",
		"\r\n--" + boundary + "--\r\n",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %q\nbody: %q", want, body)
		}
	}
}

func TestMultipartHelperValidation(t *testing.T) {
	svc := setupService(t)
	src := `--- @plugin Multipart Bad Plugin
--- @author tester
--- @version 1.0.0
--- @router_version 0.1.1
--- @description Multipart validation test
--- @allow_host example.com

llm_router.register("mpbad-type", {
  complete = function(ctx, credential, request)
    return {
      id = "x", object = "chat.completion", created = 1, model = request.model,
      choices = { { index = 0, message = { role = "assistant", content = "hi" }, finish_reason = "stop" } },
      usage = { prompt_tokens = 1, completion_tokens = 1, total_tokens = 2 },
    }
  end,

  transcribe = function(ctx, credential, request)
    local body, ctype = llm_router.multipart({ { value = "no name" } })
    return { text = body }
  end,
})
`
	if _, err := svc.Install([]byte(src), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install: %v", err)
	}
	_, err := svc.Transcribe(context.Background(), testMeta("mpbad-type",
		&models.Credential{ID: "c1"}, "mpbad-type/m", nil),
		&models.TranscriptionRequest{Model: "mpbad-type/m", File: []byte("x"), FileName: "a.wav"})
	var ierr *models.PluginInternalError
	if !errors.As(err, &ierr) {
		t.Fatalf("expected PluginInternalError, got %T (%v)", err, err)
	}
	if !strings.Contains(ierr.Cause, "name is required") {
		t.Fatalf("cause: %q", ierr.Cause)
	}
}
