package router

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	apierrors "github.com/TheSlopMachine/llm-router/internal/errors"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
)

// TestResult reports the outcome of a credential or model probe.
// QuotaExceeded marks temporary rate-limit failures: the model is alive,
// the quota is not. Callers must not disable models over it.
// Summary is a short human string for toasts; the full error goes to logs.
type TestResult struct {
	OK            bool   `json:"ok"`
	Latency       int64  `json:"latency_ms"`
	Error         string `json:"error,omitempty"`
	Response      string `json:"response,omitempty"`
	QuotaExceeded bool   `json:"quota_exceeded,omitempty"`
	Summary       string `json:"summary,omitempty"`
}

// probeRequest builds a minimal completion request for connectivity tests.
func probeRequest(model models.ModelId) *models.ChatCompletionRequest {
	return &models.ChatCompletionRequest{
		Model:     model,
		MaxTokens: 16,
		Messages:  []models.ChatMessage{{Role: "user", Content: "Reply with: ok"}},
	}
}

// TestCredential runs a single probe request pinned to one credential,
// bypassing pool rotation. The probe model is the provider's first listed
// model, or overrideModel when given.
func (s *Service) TestCredential(ctx context.Context, providerID, credentialID string, overrideModel string) TestResult {
	resolved, err := provider.Resolve(s.providerSvc, providerID)
	if err != nil {
		return TestResult{Error: err.Error()}
	}
	cred, err := s.credSvc.Get(credentialID)
	if err != nil {
		return TestResult{Error: "credential not found"}
	}
	if cred.ProviderID != providerID {
		return TestResult{Error: "credential does not belong to this provider"}
	}
	model := overrideModel
	if model == "" {
		infos, err := s.modelInfoSvc.GetModelInfos(ctx, providerID)
		if err != nil || len(infos) == 0 {
			return TestResult{Error: "no model available for probe: model discovery failed"}
		}
		model = infos[0].Name
	}
	req := probeRequest(models.ModelId(providerID + "/" + model))
	start := time.Now()
	resp, err := s.completeOne(ctx, resolved, []*models.Credential{cred}, req)
	if err != nil {
		return probeResult(start, "", err)
	}
	text := ""
	if len(resp.Choices) > 0 {
		text = resp.Choices[0].Message.TextContent()
	}
	return probeResult(start, text, nil)
}

// TestModel runs a probe through the normal routing path (credential pool
// included), as an internal unrestricted call. Manually disabled models stay
// testable: the admin override gates routing, not probing.
func (s *Service) TestModel(ctx context.Context, modelID models.ModelId) TestResult {
	start := time.Now()
	resp, err := s.complete(ctx, probeRequest(modelID), nil, true)
	if err != nil {
		return probeResult(start, "", err)
	}
	text := ""
	if len(resp.Choices) > 0 {
		text = resp.Choices[0].Message.TextContent()
	}
	return probeResult(start, text, nil)
}

// probePixelPNG is a 1x1 PNG for vision probes: no network needed to build
// the request.
const probePixelPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="

// probeWAV synthesizes a short mono beep for transcription probes.
func probeWAV() []byte {
	const sampleRate = 8000
	const samples = sampleRate / 2
	buf := make([]byte, 44+samples*2)
	copy(buf[0:], "RIFF")
	put32(buf[4:], uint32(36+samples*2))
	copy(buf[8:], "WAVEfmt ")
	put32(buf[16:], 16)
	put16(buf[20:], 1)
	put16(buf[22:], 1)
	put32(buf[24:], sampleRate)
	put32(buf[28:], sampleRate*2)
	put16(buf[32:], 2)
	put16(buf[34:], 16)
	copy(buf[36:], "data")
	put32(buf[40:], uint32(samples*2))
	for i := range samples {
		v := int16(10000 * math.Sin(2*math.Pi*440*float64(i)/sampleRate))
		put16(buf[44+i*2:], uint16(v))
	}
	return buf
}

func put16(b []byte, v uint16) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
}

func put32(b []byte, v uint32) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
	b[2] = byte(v >> 16)
	b[3] = byte(v >> 24)
}

// probeResult builds a TestResult from a probe outcome.
func probeResult(start time.Time, respText string, err error) TestResult {
	res := TestResult{OK: err == nil, Latency: time.Since(start).Milliseconds()}
	if err != nil {
		res.Error = err.Error()
		res.Summary = probeSummary(err)
		var perr *models.ProviderError
		if errors.As(err, &perr) && (perr.Type == models.ErrorTypeRateLimit || perr.Type == models.ErrorTypeQuotaExceeded) {
			res.QuotaExceeded = true
		}
		return res
	}
	res.Response = respText
	return res
}

// probeSummary maps a probe error to a short human string for toasts.
func probeSummary(err error) string {
	var perr *models.ProviderError
	if errors.As(err, &perr) {
		switch perr.Type {
		case models.ErrorTypeRateLimit, models.ErrorTypeQuotaExceeded:
			return "quota exceeded, temporary"
		case models.ErrorTypeAuth:
			return "authentication failed"
		case models.ErrorTypeInvalidRequest:
			return "invalid request"
		case models.ErrorTypeUpstream:
			return "upstream error"
		case models.ErrorTypeTimeout:
			return "request timed out"
		case models.ErrorTypeGeo:
			return "region blocked"
		case models.ErrorTypePaymentRequired:
			return "payment required"
		}
	}
	return "probe failed"
}

// TestVision runs a minimal image-input probe, bypassing manual disable.
func (s *Service) TestVision(ctx context.Context, modelID models.ModelId) TestResult {
	start := time.Now()
	req := probeRequest(modelID)
	req.Messages = []models.ChatMessage{{
		Role:    "user",
		Content: "What is in this image? Reply with: ok",
		ContentParts: []models.ChatMessageContentPart{{
			Type:     "image_url",
			ImageURL: &models.ChatMessageImageURL{URL: "data:image/png;base64," + probePixelPNG},
		}},
	}}
	resp, err := s.complete(ctx, req, nil, true)
	if err != nil || len(resp.Choices) == 0 {
		if err == nil {
			err = fmt.Errorf("empty response")
		}
		return probeResult(start, "", err)
	}
	return probeResult(start, resp.Choices[0].Message.TextContent(), nil)
}

// TestSpeech runs a minimal speech probe, bypassing manual disable.
func (s *Service) TestSpeech(ctx context.Context, modelID models.ModelId) TestResult {
	start := time.Now()
	resp, err := s.speech(ctx, &models.SpeechRequest{Model: modelID, Input: "test"}, nil, true)
	if err != nil {
		return probeResult(start, "", err)
	}
	return probeResult(start, fmt.Sprintf("%d bytes of audio", len(resp.Audio)), nil)
}

// TestTranscribe runs a minimal transcription probe, bypassing manual disable.
func (s *Service) TestTranscribe(ctx context.Context, modelID models.ModelId) TestResult {
	start := time.Now()
	resp, err := s.transcribe(ctx, &models.TranscriptionRequest{
		Model:       modelID,
		File:        probeWAV(),
		FileName:    "probe.wav",
		ContentType: "audio/wav",
	}, nil, true)
	if err != nil {
		return probeResult(start, "", err)
	}
	return probeResult(start, resp.Text, nil)
}

// TestImageGeneration runs a minimal image probe, bypassing manual disable.
func (s *Service) TestImageGeneration(ctx context.Context, modelID models.ModelId) TestResult {
	start := time.Now()
	resp, err := s.generateImage(ctx, &models.ImageGenerationRequest{Model: modelID, Prompt: "test", N: 1}, nil, true)
	if err != nil {
		return probeResult(start, "", err)
	}
	return probeResult(start, fmt.Sprintf("%d image(s)", len(resp.Data)), nil)
}

// TestEmbeddings runs a minimal embeddings probe, bypassing manual disable.
func (s *Service) TestEmbeddings(ctx context.Context, modelID models.ModelId) TestResult {
	start := time.Now()
	resp, err := s.embed(ctx, &models.EmbeddingsRequest{Model: modelID, Input: []string{"test"}}, nil, true)
	if err != nil {
		return probeResult(start, "", err)
	}
	dims := 0
	if len(resp.Data) > 0 {
		dims = len(resp.Data[0].Values)
	}
	return probeResult(start, fmt.Sprintf("%d embedding(s), %d dims", len(resp.Data), dims), nil)
}

// ProbeCapabilities detects model features with live probe requests.
// Each probe consumes a small amount of quota. Unprobed capabilities are
// simply absent from the result.
func (s *Service) ProbeCapabilities(ctx context.Context, modelID models.ModelId) ([]string, error) {
	providerID, _, err := modelID.Parse()
	if err != nil {
		return nil, fmt.Errorf("invalid model id: %w", err)
	}
	if _, err := provider.Resolve(s.providerSvc, providerID); err != nil {
		return nil, fmt.Errorf("%w: %s", apierrors.ErrProviderNotFound, providerID)
	}

	caps := []string{}

	// Reasoning models spend tokens on thinking before any tool call or
	// JSON output; probes need real headroom.
	toolReq := probeRequest(modelID)
	toolReq.MaxTokens = 512
	toolReq.Messages[0].Content = "Call the function report_weather with city=Paris."
	toolReq.Tools = []models.ChatTool{{
		Type: "function",
		Function: &models.ChatToolFunction{
			Name:        "report_weather",
			Description: "Report weather for a city",
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{"city": map[string]any{"type": "string"}},
				"required":   []string{"city"},
			},
		},
	}}
	toolReq.ToolChoice = map[string]any{"type": "function", "function": map[string]any{"name": "report_weather"}}
	if resp, err := s.Complete(ctx, toolReq, nil); err == nil && len(resp.Choices) > 0 && len(resp.Choices[0].Message.ToolCalls) > 0 {
		caps = append(caps, "tools")
	}

	jsonReq := probeRequest(modelID)
	jsonReq.MaxTokens = 512
	jsonReq.Messages[0].Content = `Output exactly: {"ok": true}`
	jsonReq.ResponseFormat = map[string]any{"type": "json_object"}
	if _, err := s.Complete(ctx, jsonReq, nil); err == nil {
		caps = append(caps, "json_mode")
	}

	return caps, nil
}
