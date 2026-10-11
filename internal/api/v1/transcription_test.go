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
	var buffer bytes.Buffer
	writer := multipart.NewWriter(&buffer)
	for key, value := range fields {
		if err := writer.WriteField(key, value); err != nil {
			t.Fatalf("write field %s: %v", key, err)
		}
	}
	if withFile {
		formWriter, err := writer.CreateFormFile("file", "clip.wav")
		if err != nil {
			t.Fatalf("create form file: %v", err)
		}
		if _, err := formWriter.Write([]byte("FAKEAUDIO")); err != nil {
			t.Fatalf("write file: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	return writer.FormDataContentType(), &buffer
}

func TestParseTranscriptionRequest_Success(t *testing.T) {
	contentType, buffer := multipartBody(t, map[string]string{
		"model":                     "groq/whisper-large-v3",
		"language":                  "en",
		"prompt":                    "hello",
		"response_format":           "verbose_json",
		"temperature":               "0.2",
		"timestamp_granularities[]": "segment",
	}, true)
	request := httptest.NewRequest("POST", "/v1/audio/transcriptions", buffer)
	request.Header.Set("Content-Type", contentType)

	parsed, err := parseTranscriptionRequest(request)
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
			wantErr: "missing required field 'model'",
		},
		{
			name: "missing file",
			build: func(t *testing.T) (string, *bytes.Buffer) {
				return multipartBody(t, map[string]string{"model": "groq/whisper-large-v3"}, false)
			},
			wantErr: "missing required field 'file'",
		},
		{
			name: "bad response format",
			build: func(t *testing.T) (string, *bytes.Buffer) {
				return multipartBody(t, map[string]string{"model": "groq/whisper-large-v3", "response_format": "yaml"}, true)
			},
			wantErr: `invalid response_format "yaml": expected json, text, srt, verbose_json or vtt`,
		},
		{
			name: "bad granularity",
			build: func(t *testing.T) (string, *bytes.Buffer) {
				return multipartBody(t, map[string]string{"model": "groq/whisper-large-v3", "timestamp_granularities[]": "millisecond"}, true)
			},
			wantErr: `invalid timestamp granularity "millisecond": expected word or segment`,
		},
		{
			name: "bad temperature",
			build: func(t *testing.T) (string, *bytes.Buffer) {
				return multipartBody(t, map[string]string{"model": "groq/whisper-large-v3", "temperature": "hot"}, true)
			},
			wantErr: `invalid temperature "hot": must be a number`,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			contentType, buffer := testCase.build(t)
			request := httptest.NewRequest("POST", "/v1/audio/transcriptions", buffer)
			request.Header.Set("Content-Type", contentType)
			_, err := parseTranscriptionRequest(request)
			if err == nil {
				t.Fatal("expected error")
			}
			if err.Error() != testCase.wantErr {
				t.Fatalf("error %q must equal %q", err.Error(), testCase.wantErr)
			}
		})
	}
}

func TestParseTranscriptionRequest_RejectsNonMultipart(t *testing.T) {
	request := httptest.NewRequest("POST", "/v1/audio/transcriptions", strings.NewReader(`{"model":"groq/whisper"}`))
	request.Header.Set("Content-Type", "application/json")
	if _, err := parseTranscriptionRequest(request); err == nil {
		t.Fatal("expected error for non-multipart body")
	}
}

func TestTranscriptionResponse_SRTVTT(t *testing.T) {
	response := transcriptionFixture()
	srt := response.SRT()
	if !strings.Contains(srt, "1\n00:00:00,000 --> 00:00:01,500\nhello world\n") {
		t.Fatalf("srt: %q", srt)
	}
	vtt := response.VTT()
	if !strings.HasPrefix(vtt, "WEBVTT\n\n") || !strings.Contains(vtt, "00:00:00.000 --> 00:00:01.500") {
		t.Fatalf("vtt: %q", vtt)
	}
}
