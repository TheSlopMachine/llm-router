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
--- @plugin_api 1.0
--- @description Speech and image test plugin
--- @allow_host example.com

llm_router.register("tts-type", {
  complete = function(ctx, request)
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

  speech = function(ctx, request)
    return {
      audio_b64 = "RkFLRU9HRA==",
      format = "ogg",
    }
  end,

  generate_image = function(ctx, request)
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
	service := setupService(t)
	if _, err := service.InstallSQLite(context.Background(), []byte(speechPluginSource), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install: %v", err)
	}
	if !service.HasHandler("tts-type", "speech") {
		t.Fatal("speech handler must be registered")
	}
	speed := 1.25
	response, err := service.Speech(context.Background(), testMetadata("tts-type",
		&models.Credential{ID: "c1", Data: map[string]any{"api_key": "k"}}, "tts-type/gemini-tts", nil),
		&models.SpeechRequest{
			Model:          "tts-type/gemini-tts",
			Input:          "hello",
			Voice:          "Kore",
			ResponseFormat: "ogg",
			Speed:          &speed,
		})
	if err != nil {
		t.Fatalf("speech: %v", err)
	}
	if string(response.Audio) != "FAKEOGD" {
		t.Fatalf("audio: got %q", response.Audio)
	}
	if response.Format != "ogg" {
		t.Fatalf("format: got %q", response.Format)
	}
}

func TestSpeech_HandlerNotFound(t *testing.T) {
	service := setupService(t)
	if _, err := service.InstallSQLite(context.Background(), []byte(testPluginSource), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install: %v", err)
	}
	_, err := service.Speech(context.Background(), testMetadata("test-type",
		&models.Credential{ID: "c1"}, "test-type/model-a", nil),
		&models.SpeechRequest{Model: "test-type/model-a", Input: "hi"})
	if !errors.Is(err, ErrHandlerNotFound) {
		t.Fatalf("expected ErrHandlerNotFound, got %v", err)
	}
}

func TestSpeech_ContractError(t *testing.T) {
	service := setupService(t)
	src := strings.Replace(speechPluginSource, `return {
      audio_b64 = "RkFLRU9HRA==",
      format = "ogg",
    }`,
		`do return nil, { message = "out of quota", code = "insufficient_quota", status = 429 } end`, 1)
	src = strings.Replace(src, "Speech Plugin", "Speech Err", 1)
	if _, err := service.InstallSQLite(context.Background(), []byte(src), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install: %v", err)
	}
	_, err := service.Speech(context.Background(), testMetadata("tts-type",
		&models.Credential{ID: "c1"}, "tts-type/m", nil),
		&models.SpeechRequest{Model: "tts-type/m", Input: "hi"})
	perr, ok := err.(*models.ProviderError)
	if !ok || perr.Code != "insufficient_quota" || perr.StatusCode != 429 {
		t.Fatalf("expected terminal quota ProviderError, got %T (%v)", err, err)
	}
}

func TestSpeech_BadBase64(t *testing.T) {
	service := setupService(t)
	src := strings.Replace(speechPluginSource, `audio_b64 = "RkFLRU9HRA=="`, `audio_b64 = "!!!not-base64!!!"`, 1)
	src = strings.Replace(src, "Speech Plugin", "Speech Bad B64", 1)
	if _, err := service.InstallSQLite(context.Background(), []byte(src), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install: %v", err)
	}
	_, err := service.Speech(context.Background(), testMetadata("tts-type",
		&models.Credential{ID: "c1"}, "tts-type/m", nil),
		&models.SpeechRequest{Model: "tts-type/m", Input: "hi"})
	var perr *models.PluginInternalError
	if !errors.As(err, &perr) || !strings.Contains(perr.Cause, "base64") {
		t.Fatalf("expected PluginInternalError about base64, got %v", err)
	}
}

func TestSpeech_EmptyAudio(t *testing.T) {
	service := setupService(t)
	src := strings.Replace(speechPluginSource, `audio_b64 = "RkFLRU9HRA=="`, `audio_b64 = ""`, 1)
	src = strings.Replace(src, "Speech Plugin", "Speech Empty", 1)
	if _, err := service.InstallSQLite(context.Background(), []byte(src), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install: %v", err)
	}
	_, err := service.Speech(context.Background(), testMetadata("tts-type",
		&models.Credential{ID: "c1"}, "tts-type/m", nil),
		&models.SpeechRequest{Model: "tts-type/m", Input: "hi"})
	var perr *models.PluginInternalError
	if !errors.As(err, &perr) || !strings.Contains(perr.Cause, "empty audio") {
		t.Fatalf("expected PluginInternalError about empty audio, got %v", err)
	}
}

func TestGenerateImage_Success(t *testing.T) {
	service := setupService(t)
	if _, err := service.InstallSQLite(context.Background(), []byte(speechPluginSource), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install: %v", err)
	}
	if !service.HasHandler("tts-type", "generate_image") {
		t.Fatal("generate_image handler must be registered")
	}
	response, err := service.GenerateImage(context.Background(), testMetadata("tts-type",
		&models.Credential{ID: "c1"}, "tts-type/imagen-3", nil),
		&models.ImageGenerationRequest{
			Model:  "tts-type/imagen-3",
			Prompt: "a cat",
			N:      2,
			Size:   "1024x1024",
		})
	if err != nil {
		t.Fatalf("generate_image: %v", err)
	}
	if response.Created != 1700000001 {
		t.Fatalf("created: %d", response.Created)
	}
	if len(response.Data) != 2 {
		t.Fatalf("data len: %d", len(response.Data))
	}
	decoded, derr := base64.StdEncoding.DecodeString(response.Data[0].B64JSON)
	if derr != nil || string(decoded) != "image-bytes" {
		t.Fatalf("b64: %q err=%v", decoded, derr)
	}
	if response.Data[0].RevisedPrompt != "a cat (revised)" {
		t.Fatalf("revised prompt: %q", response.Data[0].RevisedPrompt)
	}
	if response.Data[1].URL != "https://example.com/img.png" {
		t.Fatalf("url: %q", response.Data[1].URL)
	}
}

func TestGenerateImage_HandlerNotFound(t *testing.T) {
	service := setupService(t)
	if _, err := service.InstallSQLite(context.Background(), []byte(testPluginSource), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install: %v", err)
	}
	_, err := service.GenerateImage(context.Background(), testMetadata("test-type",
		&models.Credential{ID: "c1"}, "test-type/model-a", nil),
		&models.ImageGenerationRequest{Model: "test-type/model-a", Prompt: "x"})
	if !errors.Is(err, ErrHandlerNotFound) {
		t.Fatalf("expected ErrHandlerNotFound, got %v", err)
	}
}

func TestGenerateImage_EmptyData(t *testing.T) {
	service := setupService(t)
	src := strings.Replace(speechPluginSource, `data = {
        { b64_json = "aW1hZ2UtYnl0ZXM=", revised_prompt = request.prompt .. " (revised)" },
        { url = "https://example.com/img.png" },
      },`, `data = {},`, 1)
	src = strings.Replace(src, "Speech Plugin", "Image Empty", 1)
	if _, err := service.InstallSQLite(context.Background(), []byte(src), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install: %v", err)
	}
	_, err := service.GenerateImage(context.Background(), testMetadata("tts-type",
		&models.Credential{ID: "c1"}, "tts-type/m", nil),
		&models.ImageGenerationRequest{Model: "tts-type/m", Prompt: "x"})
	var perr *models.PluginInternalError
	if !errors.As(err, &perr) || !strings.Contains(perr.Cause, "empty data") {
		t.Fatalf("expected PluginInternalError about empty data, got %v", err)
	}
}
