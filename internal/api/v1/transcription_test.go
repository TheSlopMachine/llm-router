package v1

import (
	"bytes"
	"mime/multipart"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/models"
)

func transcriptionFixture() *models.TranscriptionResponse {
	return &models.TranscriptionResponse{
		Text:     "hello world",
		Language: "en",
		Duration: 1.5,
		Segments: []models.TranscriptionSegment{
			{ID: 0, Start: 0, End: 1.5, Text: "hello world"},
		},
	}
}

func multipartBody(t *testing.T, fields map[string]string, withFile bool) (string, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for k, v := range fields {
		if err := w.WriteField(k, v); err != nil {
			t.Fatalf("write field %s: %v", k, err)
		}
	}
	if withFile {
		fw, err := w.CreateFormFile("file", "clip.wav")
		if err != nil {
			t.Fatalf("create form file: %v", err)
		}
		if _, err := fw.Write([]byte("FAKEAUDIO")); err != nil {
			t.Fatalf("write file: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	return w.FormDataContentType(), &buf
}

func TestParseTranscriptionRequest_Success(t *testing.T) {
	ct, buf := multipartBody(t, map[string]string{
		"model":                     "groq/whisper-large-v3",
		"language":                  "en",
		"prompt":                    "hello",
		"response_format":           "verbose_json",
		"temperature":               "0.2",
		"timestamp_granularities[]": "segment",
	}, true)
	req := httptest.NewRequest("POST", "/v1/audio/transcriptions", buf)
	req.Header.Set("Content-Type", ct)

	parsed, err := parseTranscriptionRequest(req)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if parsed.Model != "groq/whisper-large-v3" {
		t.Fatalf("model: %q", parsed.Model)
	}
	if string(parsed.File) != "FAKEAUDIO" || parsed.FileName != "clip.wav" {
		t.Fatalf("file: name=%q bytes=%q", parsed.FileName, parsed.File)
	}
	if parsed.ContentType != "application/octet-stream" && parsed.ContentType == "" {
		t.Fatalf("content type must default: %q", parsed.ContentType)
	}
	if parsed.Language != "en" || parsed.Prompt != "hello" || parsed.ResponseFormat != "verbose_json" {
		t.Fatalf("fields: %+v", parsed)
	}
	if parsed.Temperature == nil || *parsed.Temperature != 0.2 {
		t.Fatalf("temperature: %v", parsed.Temperature)
	}
	if len(parsed.TimestampGranularities) != 1 || parsed.TimestampGranularities[0] != "segment" {
		t.Fatalf("granularities: %v", parsed.TimestampGranularities)
	}
}

func TestParseTranscriptionRequest_Errors(t *testing.T) {
	cases := []struct {
		name    string
		build   func(t *testing.T) (string, *bytes.Buffer)
		wantErr string
	}{
		{
			name: "missing model",
			build: func(t *testing.T) (string, *bytes.Buffer) {
				return multipartBody(t, map[string]string{}, true)
			},
			wantErr: "model",
		},
		{
			name: "missing file",
			build: func(t *testing.T) (string, *bytes.Buffer) {
				return multipartBody(t, map[string]string{"model": "groq/whisper-large-v3"}, false)
			},
			wantErr: "file",
		},
		{
			name: "bad response format",
			build: func(t *testing.T) (string, *bytes.Buffer) {
				return multipartBody(t, map[string]string{"model": "groq/whisper-large-v3", "response_format": "yaml"}, true)
			},
			wantErr: "response_format",
		},
		{
			name: "bad granularity",
			build: func(t *testing.T) (string, *bytes.Buffer) {
				return multipartBody(t, map[string]string{"model": "groq/whisper-large-v3", "timestamp_granularities[]": "millisecond"}, true)
			},
			wantErr: "granularity",
		},
		{
			name: "bad temperature",
			build: func(t *testing.T) (string, *bytes.Buffer) {
				return multipartBody(t, map[string]string{"model": "groq/whisper-large-v3", "temperature": "hot"}, true)
			},
			wantErr: "temperature",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ct, buf := tc.build(t)
			req := httptest.NewRequest("POST", "/v1/audio/transcriptions", buf)
			req.Header.Set("Content-Type", ct)
			_, err := parseTranscriptionRequest(req)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error %q must mention %q", err.Error(), tc.wantErr)
			}
		})
	}
}

func TestParseTranscriptionRequest_RejectsNonMultipart(t *testing.T) {
	req := httptest.NewRequest("POST", "/v1/audio/transcriptions", strings.NewReader(`{"model":"groq/whisper"}`))
	req.Header.Set("Content-Type", "application/json")
	if _, err := parseTranscriptionRequest(req); err == nil {
		t.Fatal("expected error for non-multipart body")
	}
}

func TestTranscriptionResponse_SRTVTT(t *testing.T) {
	resp := transcriptionFixture()
	srt := resp.SRT()
	if !strings.Contains(srt, "1\n00:00:00,000 --> 00:00:01,500\nhello world\n") {
		t.Fatalf("srt: %q", srt)
	}
	vtt := resp.VTT()
	if !strings.HasPrefix(vtt, "WEBVTT\n\n") || !strings.Contains(vtt, "00:00:00.000 --> 00:00:01.500") {
		t.Fatalf("vtt: %q", vtt)
	}
}
