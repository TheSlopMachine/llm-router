package luaplugin

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/models"
)

const videoPluginSource = `--- @plugin Video Plugin
--- @author tester
--- @version 1.0.0
--- @plugin_api 1.0
--- @description Video test plugin
--- @allow_host example.com

llm_router.register("vid-type", {
  complete = function(ctx, request)
    return {
      id = "chatcmpl-vid",
      object = "chat.completion",
      created = 1700000000,
      model = request.model,
      choices = {
        { index = 0, message = { role = "assistant", content = "hi" }, finish_reason = "stop" },
      },
      usage = { prompt_tokens = 1, completion_tokens = 1, total_tokens = 2 },
    }
  end,

  generate_video = function(ctx, request)
    return {
      id = "up-123",
      polling_url = "https://upstream.example.com/v/up-123",
      status = "pending",
      generation_id = "up-123",
    }
  end,

  poll_video = function(ctx, request)
    return {
      id = request.job_id,
      polling_url = "/v1/videos/" .. request.job_id,
      status = "completed",
      unsigned_urls = { "https://cdn.example.com/up-123.mp4" },
      usage = { cost = 0.5, is_byok = false },
    }
  end,

  video_content = function(ctx, request)
    return {
      video_b64 = "RkFLRUZUWVA=",
      content_type = "video/mp4",
    }
  end,
})
`

func videoMeta() HandlerMeta {
	return testMetadata("vid-type",
		&models.Credential{ID: "c1", Data: map[string]any{"api_key": "k"}}, "vid-type/veo", nil)
}

func TestVideoHandlers_Success(t *testing.T) {
	service := setupService(t)
	if _, err := service.InstallSQLite(context.Background(), []byte(videoPluginSource), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install: %v", err)
	}
	for _, h := range []string{"generate_video", "poll_video", "video_content"} {
		if !service.HasHandler("vid-type", h) {
			t.Fatalf("%s handler must be registered", h)
		}
	}
	sub, err := service.SubmitVideo(context.Background(), videoMeta(), &models.VideoGenerationRequest{
		Model: "vid-type/veo", Prompt: "a cat", Duration: 8, Resolution: "720p",
		FrameImages: []models.VideoFrameImage{{
			Type:      "image_url",
			ImageURL:  &models.VideoReferenceURL{URL: "https://example.com/a.png"},
			FrameType: "first_frame",
		}},
	})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if sub.ID != "up-123" || sub.Status != models.VideoStatusPending {
		t.Fatalf("submit: %+v", sub)
	}
	polled, err := service.PollVideo(context.Background(), videoMeta(), "vid-type/veo", "up-123")
	if err != nil {
		t.Fatalf("poll: %v", err)
	}
	if polled.Status != models.VideoStatusCompleted || len(polled.UnsignedURLs) != 1 {
		t.Fatalf("poll: %+v", polled)
	}
	if polled.Usage == nil || polled.Usage.Cost == nil || *polled.Usage.Cost != 0.5 {
		t.Fatalf("usage: %+v", polled.Usage)
	}
	content, err := service.VideoContent(context.Background(), videoMeta(), "vid-type/veo", "up-123", 0)
	if err != nil {
		t.Fatalf("content: %v", err)
	}
	if string(content.Video) != "FAKEFTYP" || content.ContentType != "video/mp4" {
		t.Fatalf("content: %q %q", content.Video, content.ContentType)
	}
}

func TestVideoHandlers_NotFound(t *testing.T) {
	service := setupService(t)
	if _, err := service.InstallSQLite(context.Background(), []byte(testPluginSource), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install: %v", err)
	}
	handlerMetadata := testMetadata("test-type", &models.Credential{ID: "c1"}, "test-type/model-a", nil)
	if _, err := service.SubmitVideo(context.Background(), handlerMetadata, &models.VideoGenerationRequest{Model: "test-type/model-a"}); !errors.Is(err, ErrHandlerNotFound) {
		t.Fatalf("submit: expected ErrHandlerNotFound, got %v", err)
	}
	if _, err := service.PollVideo(context.Background(), handlerMetadata, "test-type/model-a", "up-1"); !errors.Is(err, ErrHandlerNotFound) {
		t.Fatalf("poll: expected ErrHandlerNotFound, got %v", err)
	}
	if _, err := service.VideoContent(context.Background(), handlerMetadata, "test-type/model-a", "up-1", 0); !errors.Is(err, ErrHandlerNotFound) {
		t.Fatalf("content: expected ErrHandlerNotFound, got %v", err)
	}
}

func TestVideoHandlers_UnknownStatus(t *testing.T) {
	service := setupService(t)
	src := strings.Replace(videoPluginSource, `status = "pending",`, `status = "queued",`, 1)
	src = strings.Replace(src, "Video Plugin", "Video Bad Status", 1)
	if _, err := service.InstallSQLite(context.Background(), []byte(src), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install: %v", err)
	}
	_, err := service.SubmitVideo(context.Background(), videoMeta(), &models.VideoGenerationRequest{Model: "vid-type/veo"})
	var perr *models.PluginInternalError
	if !errors.As(err, &perr) || !strings.Contains(perr.Cause, "unknown job status") {
		t.Fatalf("expected PluginInternalError about unknown status, got %v", err)
	}
}

func TestVideoHandlers_BadBase64(t *testing.T) {
	service := setupService(t)
	src := strings.Replace(videoPluginSource, `video_b64 = "RkFLRUZUWVA="`, `video_b64 = "!!!not-base64!!!"`, 1)
	src = strings.Replace(src, "Video Plugin", "Video Bad B64", 1)
	if _, err := service.InstallSQLite(context.Background(), []byte(src), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install: %v", err)
	}
	_, err := service.VideoContent(context.Background(), videoMeta(), "vid-type/veo", "up-123", 0)
	var perr *models.PluginInternalError
	if !errors.As(err, &perr) || !strings.Contains(perr.Cause, "base64") {
		t.Fatalf("expected PluginInternalError about base64, got %v", err)
	}
}
