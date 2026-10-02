package v1

import (
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/models"
)

func TestValidateVideoRequest(t *testing.T) {
	valid := &models.VideoGenerationRequest{Model: "mock/veo", Prompt: "a cat", Duration: 8}
	if err := validateVideoRequest(valid); err != nil {
		t.Fatalf("valid request rejected: %v", err)
	}
	imageOnly := &models.VideoGenerationRequest{
		Model:       "mock/veo",
		FrameImages: []models.VideoFrameImage{{Type: "image_url", ImageURL: &models.VideoReferenceURL{URL: "https://example.com/a.png"}, FrameType: "first_frame"}},
	}
	if err := validateVideoRequest(imageOnly); err != nil {
		t.Fatalf("image-only request rejected: %v", err)
	}
	cases := []struct {
		name string
		req  *models.VideoGenerationRequest
	}{
		{"missing model", &models.VideoGenerationRequest{Prompt: "a cat"}},
		{"missing prompt", &models.VideoGenerationRequest{Model: "mock/veo"}},
		{"negative duration", &models.VideoGenerationRequest{Model: "mock/veo", Prompt: "a cat", Duration: -1}},
		{"bad size", &models.VideoGenerationRequest{Model: "mock/veo", Prompt: "a cat", Size: "720p"}},
		{"bad frame type", &models.VideoGenerationRequest{Model: "mock/veo", FrameImages: []models.VideoFrameImage{{FrameType: "middle"}}}},
		{"frame without url", &models.VideoGenerationRequest{Model: "mock/veo", FrameImages: []models.VideoFrameImage{{FrameType: "first_frame"}}}},
		{"bad reference type", &models.VideoGenerationRequest{Model: "mock/veo", Prompt: "a cat", InputReferences: []models.VideoInputReference{{Type: "file"}}}},
		{"http callback", &models.VideoGenerationRequest{Model: "mock/veo", Prompt: "a cat", CallbackURL: "http://example.com/hook"}},
	}
	for _, tc := range cases {
		if err := validateVideoRequest(tc.req); err == nil {
			t.Fatalf("%s: expected rejection", tc.name)
		}
	}
}
