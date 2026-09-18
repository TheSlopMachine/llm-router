package luaplugin

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/models"
)

const speechPluginSource = `--- @plugin Speech Plugin
--- @author tester
--- @version 1.0.0
--- @router_version 0.0.6
--- @description Speech and image test plugin
--- @allow_host example.com

llm_router.register("tts-type", {
  complete = function(ctx, credential, request)
    return {
      id = "chatcmpl-tts",
      object = "chat.completion",
      created = 1700000000,
      model = request.model,
      choices = {
        { index = 0, message = { role = "assistant", content = "hi" }, finish_reason = "stop" },
      },
      usage = { prompt_tokens = 1, completion_tokens = 1, total_tokens = 2 },
    }
  end,

  speech = function(ctx, credential, request)
    return {
      audio_b64 = "RkFLRU9HRA==",
      format = "ogg",
    }
  end,

  generate_image = function(ctx, credential, request)
    return {
      created = 1700000001,
      data = {
        { b64_json = "aW1hZ2UtYnl0ZXM=", revised_prompt = request.prompt .. " (revised)" },
        { url = "https://example.com/img.png" },
      },
    }
  end,
})
`

func TestSpeech_Success(t *testing.T) {
	svc := setupService(t)
	if _, err := svc.Install([]byte(speechPluginSource), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install: %v", err)
	}
	if !svc.HasHandler("tts-type", "speech") {
		t.Fatal("speech handler must be registered")
	}
	speed := 1.25
	resp, err := svc.Speech(context.Background(), "tts-type",
		&models.Credential{ID: "c1", Data: map[string]any{"api_key": "k"}},
		&models.SpeechRequest{
			Model:          "tts-type/gemini-tts",
			Input:          "hello",
			Voice:          "Kore",
			ResponseFormat: "ogg",
			Speed:          &speed,
		}, nil)
	if err != nil {
		t.Fatalf("speech: %v", err)
	}
	if string(resp.Audio) != "FAKEOGD" {
		t.Fatalf("audio: got %q", resp.Audio)
	}
	if resp.Format != "ogg" {
		t.Fatalf("format: got %q", resp.Format)
	}
}

func TestSpeech_HandlerNotFound(t *testing.T) {
	svc := setupService(t)
	if _, err := svc.Install([]byte(testPluginSource), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install: %v", err)
	}
	_, err := svc.Speech(context.Background(), "test-type",
		&models.Credential{ID: "c1"},
		&models.SpeechRequest{Model: "test-type/model-a", Input: "hi"}, nil)
	if !errors.Is(err, ErrHandlerNotFound) {
		t.Fatalf("expected ErrHandlerNotFound, got %v", err)
	}
}

func TestSpeech_ContractError(t *testing.T) {
	svc := setupService(t)
	src := strings.Replace(speechPluginSource, `return {
      audio_b64 = "RkFLRU9HRA==",
      format = "ogg",
    }`,
		`do return nil, { type = "quota_exceeded", message = "out of quota" } end`, 1)
	src = strings.Replace(src, "Speech Plugin", "Speech Err", 1)
	if _, err := svc.Install([]byte(src), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install: %v", err)
	}
	_, err := svc.Speech(context.Background(), "tts-type",
		&models.Credential{ID: "c1"},
		&models.SpeechRequest{Model: "tts-type/m", Input: "hi"}, nil)
	perr, ok := err.(*models.ProviderError)
	if !ok || perr.Type != models.ErrorTypeQuotaExceeded {
		t.Fatalf("expected quota_exceeded ProviderError, got %T (%v)", err, err)
	}
}

func TestSpeech_BadBase64(t *testing.T) {
	svc := setupService(t)
	src := strings.Replace(speechPluginSource, `audio_b64 = "RkFLRU9HRA=="`, `audio_b64 = "!!!not-base64!!!"`, 1)
	src = strings.Replace(src, "Speech Plugin", "Speech Bad B64", 1)
	if _, err := svc.Install([]byte(src), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install: %v", err)
	}
	_, err := svc.Speech(context.Background(), "tts-type",
		&models.Credential{ID: "c1"},
		&models.SpeechRequest{Model: "tts-type/m", Input: "hi"}, nil)
	var perr *models.PluginInternalError
	if !errors.As(err, &perr) || !strings.Contains(perr.Cause, "base64") {
		t.Fatalf("expected PluginInternalError about base64, got %v", err)
	}
}

func TestSpeech_EmptyAudio(t *testing.T) {
	svc := setupService(t)
	src := strings.Replace(speechPluginSource, `audio_b64 = "RkFLRU9HRA=="`, `audio_b64 = ""`, 1)
	src = strings.Replace(src, "Speech Plugin", "Speech Empty", 1)
	if _, err := svc.Install([]byte(src), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install: %v", err)
	}
	_, err := svc.Speech(context.Background(), "tts-type",
		&models.Credential{ID: "c1"},
		&models.SpeechRequest{Model: "tts-type/m", Input: "hi"}, nil)
	var perr *models.PluginInternalError
	if !errors.As(err, &perr) || !strings.Contains(perr.Cause, "empty audio") {
		t.Fatalf("expected PluginInternalError about empty audio, got %v", err)
	}
}

func TestGenerateImage_Success(t *testing.T) {
	svc := setupService(t)
	if _, err := svc.Install([]byte(speechPluginSource), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install: %v", err)
	}
	if !svc.HasHandler("tts-type", "generate_image") {
		t.Fatal("generate_image handler must be registered")
	}
	resp, err := svc.GenerateImage(context.Background(), "tts-type",
		&models.Credential{ID: "c1"},
		&models.ImageGenerationRequest{
			Model:  "tts-type/imagen-3",
			Prompt: "a cat",
			N:      2,
			Size:   "1024x1024",
		}, nil)
	if err != nil {
		t.Fatalf("generate_image: %v", err)
	}
	if resp.Created != 1700000001 {
		t.Fatalf("created: %d", resp.Created)
	}
	if len(resp.Data) != 2 {
		t.Fatalf("data len: %d", len(resp.Data))
	}
	decoded, derr := base64.StdEncoding.DecodeString(resp.Data[0].B64JSON)
	if derr != nil || string(decoded) != "image-bytes" {
		t.Fatalf("b64: %q err=%v", decoded, derr)
	}
	if resp.Data[0].RevisedPrompt != "a cat (revised)" {
		t.Fatalf("revised prompt: %q", resp.Data[0].RevisedPrompt)
	}
	if resp.Data[1].URL != "https://example.com/img.png" {
		t.Fatalf("url: %q", resp.Data[1].URL)
	}
}

func TestGenerateImage_HandlerNotFound(t *testing.T) {
	svc := setupService(t)
	if _, err := svc.Install([]byte(testPluginSource), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install: %v", err)
	}
	_, err := svc.GenerateImage(context.Background(), "test-type",
		&models.Credential{ID: "c1"},
		&models.ImageGenerationRequest{Model: "test-type/model-a", Prompt: "x"}, nil)
	if !errors.Is(err, ErrHandlerNotFound) {
		t.Fatalf("expected ErrHandlerNotFound, got %v", err)
	}
}

func TestGenerateImage_EmptyData(t *testing.T) {
	svc := setupService(t)
	src := strings.Replace(speechPluginSource, `data = {
        { b64_json = "aW1hZ2UtYnl0ZXM=", revised_prompt = request.prompt .. " (revised)" },
        { url = "https://example.com/img.png" },
      },`, `data = {},`, 1)
	src = strings.Replace(src, "Speech Plugin", "Image Empty", 1)
	if _, err := svc.Install([]byte(src), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install: %v", err)
	}
	_, err := svc.GenerateImage(context.Background(), "tts-type",
		&models.Credential{ID: "c1"},
		&models.ImageGenerationRequest{Model: "tts-type/m", Prompt: "x"}, nil)
	var perr *models.PluginInternalError
	if !errors.As(err, &perr) || !strings.Contains(perr.Cause, "empty data") {
		t.Fatalf("expected PluginInternalError about empty data, got %v", err)
	}
}
