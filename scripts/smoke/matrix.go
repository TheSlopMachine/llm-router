package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

type modelView struct {
	Name      string   `json:"name"`
	Disabled  bool     `json:"disabled"`
	Endpoints []string `json:"endpoints"`
}

func (m modelView) serves(endpoint string) bool {
	if len(m.Endpoints) == 0 {
		return endpoint == "chat/completions"
	}
	for _, e := range m.Endpoints {
		if e == endpoint {
			return true
		}
	}
	return false
}

// skipCodes are wire codes that skip a check with reason instead of failing.
var skipCodes = map[string]bool{
	"payment_required":       true,
	"quota_exceeded":         true,
	"model_not_found":        true,
	"not_found":              true,
	"endpoint_not_supported": true,
}

// classifyRetryEmpty classifies a check, retrying once on degenerate
// empty responses before accepting the outcome.
func classifyRetryEmpty(check func() error) (string, error) {
	if reason, err := classify(check); err == nil || !errors.Is(err, errEmpty) {
		return reason, err
	} else {
		return classify(check)
	}
}

// classify maps a check error to a skip reason ("" = real failure).
// rate_limit retries once, then skips as transient.
func classify(run func() error) (skipReason string, err error) {
	err = run()
	if err == nil {
		return "", nil
	}
	if werr, ok := err.(*wireError); ok {
		if werr.status == 402 {
			return "payment_required", nil
		}
		if werr.code == "rate_limit" {
			if err := run(); err == nil {
				return "", nil
			} else if werr, ok := err.(*wireError); ok && werr.code == "rate_limit" {
				return "rate_limit", nil
			} else {
				return "", err
			}
		}
		if skipCodes[werr.code] {
			return werr.code, nil
		}
	}
	return "", err
}

func chatBody(model string, stream bool) map[string]any {
	body := map[string]any{
		"model":      model,
		"messages":   []any{map[string]any{"role": "user", "content": "Reply with: ok"}},
		"max_tokens": 64,
	}
	if stream {
		body["stream"] = true
	}
	return body
}

// errEmpty marks degenerate upstream responses (thinking models
// nondeterministically return no content on tight budgets). Smoke asserts
// integration, not upstream determinism, so one retry separates flakes
// from breakage: two consecutive empties fail.
var errEmpty = fmt.Errorf("empty response")

func emptyErr(err error, out string) error {
	if err != nil {
		return err
	}
	if out == "" {
		return errEmpty
	}
	return nil
}

func checkCompletions(cfg config, model string) error {
	status, raw, err := doJSON("POST", cfg.api+"/v1/chat/completions", chatBody(model, false))
	if err != nil {
		return err
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := requireOK(status, raw, &out); err != nil {
		return err
	}
	if len(out.Choices) == 0 || out.Choices[0].Message.Content == "" {
		return errEmpty
	}
	return nil
}

func checkCompletionsStream(cfg config, model string) error {
	status, raw, err := doJSON("POST", cfg.api+"/v1/chat/completions", chatBody(model, true))
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return &wireError{status: status, code: wireCode(raw), msg: strings.TrimSpace(string(raw))}
	}
	if !strings.Contains(string(raw), "[DONE]") {
		return fmt.Errorf("stream without [DONE]")
	}
	return nil
}

func checkMessages(cfg config, model string, stream bool) error {
	// Thinking models starve on tiny budgets ("hi" + 16 tokens yields
	// empty candidates), so the probe mirrors the completions prompt
	// and budget instead of asserting less.
	body := map[string]any{
		"model":      model,
		"max_tokens": 64,
		"messages":   []any{map[string]any{"role": "user", "content": "Reply with: ok"}},
	}
	if stream {
		body["stream"] = true
	}
	status, raw, err := doJSON("POST", cfg.api+"/v1/messages", body)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return &wireError{status: status, code: wireCode(raw), msg: strings.TrimSpace(string(raw))}
	}
	text := string(raw)
	if stream {
		if !strings.Contains(text, "message_stop") {
			return fmt.Errorf("anthropic stream without message_stop")
		}
		return nil
	}
	var out struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return fmt.Errorf("decode messages response: %w", err)
	}
	if len(out.Content) == 0 {
		return errEmpty
	}
	return nil
}

func checkDashboardChat(cfg config, model string) error {
	status, raw, err := doJSON("POST", cfg.web+"/api/llm-router/dashboard/chat/completions", chatBody(model, false))
	if err != nil {
		return err
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := requireOK(status, raw, &out); err != nil {
		// Dashboard errors carry no wire code: classify via a cheap
		// completions probe on the same model.
		if status == 402 {
			return &wireError{status: status, code: "payment_required"}
		}
		if probeErr := checkCompletions(cfg, model); probeErr != nil {
			return probeErr
		}
		return err
	}
	if len(out.Choices) == 0 {
		return errEmpty
	}
	return nil
}

// synthWAV builds a minimal silent WAV for transcription uploads.
func synthWAV() []byte {
	u16 := func(n int) []byte { return []byte{byte(n), byte(n >> 8)} }
	u32 := func(n int) []byte { return []byte{byte(n), byte(n >> 8), byte(n >> 16), byte(n >> 24)} }
	out := []byte("RIFF")
	out = append(out, u32(36)...)
	out = append(out, []byte("WAVEfmt ")...)
	out = append(out, u32(16)...)
	out = append(out, u16(1)...)
	out = append(out, u16(1)...)
	out = append(out, u32(8000)...)
	out = append(out, u32(16000)...)
	out = append(out, u16(2)...)
	out = append(out, u16(16)...)
	out = append(out, []byte("data")...)
	out = append(out, u32(0)...)
	return out
}

func checkTranscribe(cfg config, model string) error {
	status, raw, err := postMultipart(cfg.api+"/v1/audio/transcriptions", model, "smoke.wav", synthWAV())
	if err != nil {
		return err
	}
	var out struct {
		Text string `json:"text"`
	}
	if err := requireOK(status, raw, &out); err != nil {
		return err
	}
	if out.Text == "" {
		return fmt.Errorf("empty transcript")
	}
	return nil
}

func checkSpeech(cfg config, model string) error {
	status, raw, err := doJSON("POST", cfg.api+"/v1/audio/speech", map[string]any{
		"model": model, "input": "hi",
	})
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return &wireError{status: status, code: wireCode(raw), msg: strings.TrimSpace(string(raw))}
	}
	if len(raw) == 0 {
		return fmt.Errorf("empty audio")
	}
	return nil
}

func checkImage(cfg config, model string) error {
	status, raw, err := doJSON("POST", cfg.api+"/v1/images/generations", map[string]any{
		"model": model, "prompt": "a cat", "n": 1,
	})
	if err != nil {
		return err
	}
	var out struct {
		Data []struct {
			URL     string `json:"url"`
			B64JSON string `json:"b64_json"`
		} `json:"data"`
	}
	if err := requireOK(status, raw, &out); err != nil {
		return err
	}
	if len(out.Data) == 0 || (out.Data[0].URL == "" && out.Data[0].B64JSON == "") {
		return fmt.Errorf("empty image data")
	}
	return nil
}

func checkEmbeddings(cfg config, model string) error {
	status, raw, err := doJSON("POST", cfg.api+"/v1/embeddings", map[string]any{
		"model": model, "input": []string{"hi"},
	})
	if err != nil {
		return err
	}
	var out struct {
		Data []struct {
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
	}
	if err := requireOK(status, raw, &out); err != nil {
		return err
	}
	if len(out.Data) == 0 || len(out.Data[0].Embedding) == 0 {
		return fmt.Errorf("empty embedding")
	}
	return nil
}

// runMatrix tests one model per requested capability: the first enabled
// model serving each endpoint. Quota on one model never cancels the rest.
func runMatrix(cfg config, rep *report, pluginType, providerID string, models []modelView) {
	byEndpoint := map[string]string{}
	for _, m := range models {
		if m.Disabled {
			continue
		}
		for _, e := range []string{"chat/completions", "audio/transcriptions", "audio/speech", "images/generations", "embeddings"} {
			if _, ok := byEndpoint[e]; !ok && m.serves(e) {
				byEndpoint[e] = providerID + "/" + m.Name
			}
		}
	}
	run := func(target, model string, check func() error) {
		if model == "" {
			rep.add(pluginType, "-", target, skip, "no model serves capability", 0)
			return
		}
		start := time.Now()
		if reason, err := classifyRetryEmpty(check); err != nil {
			rep.add(pluginType, model, target, fail, err.Error(), time.Since(start))
		} else if reason != "" {
			rep.add(pluginType, model, target, skip, reason, time.Since(start))
		} else {
			rep.add(pluginType, model, target, pass, "", time.Since(start))
		}
	}
	if cfg.targets["completions"] {
		run("completions", byEndpoint["chat/completions"], func() error {
			return checkCompletions(cfg, byEndpoint["chat/completions"])
		})
		run("completions-stream", byEndpoint["chat/completions"], func() error {
			return checkCompletionsStream(cfg, byEndpoint["chat/completions"])
		})
		run("dashboard-chat", byEndpoint["chat/completions"], func() error {
			return checkDashboardChat(cfg, byEndpoint["chat/completions"])
		})
	}
	if cfg.targets["messages"] {
		run("messages", byEndpoint["chat/completions"], func() error {
			return checkMessages(cfg, byEndpoint["chat/completions"], false)
		})
		run("messages-stream", byEndpoint["chat/completions"], func() error {
			return checkMessages(cfg, byEndpoint["chat/completions"], true)
		})
	}
	if cfg.targets["transcribe"] {
		run("transcribe", byEndpoint["audio/transcriptions"], func() error {
			return checkTranscribe(cfg, byEndpoint["audio/transcriptions"])
		})
	}
	if cfg.targets["speech"] {
		run("speech", byEndpoint["audio/speech"], func() error {
			return checkSpeech(cfg, byEndpoint["audio/speech"])
		})
	}
	if cfg.targets["image"] {
		run("image", byEndpoint["images/generations"], func() error {
			return checkImage(cfg, byEndpoint["images/generations"])
		})
	}
	if cfg.targets["embeddings"] {
		run("embeddings", byEndpoint["embeddings"], func() error {
			return checkEmbeddings(cfg, byEndpoint["embeddings"])
		})
	}
	// Mock self-check: mock-limited must skip with quota, proving the
	// harness skip pipeline end to end.
	if pluginType == "mock" && cfg.targets["completions"] {
		limited := providerID + "/mock-limited"
		start := time.Now()
		if reason, err := classify(func() error { return checkCompletions(cfg, limited) }); err != nil {
			rep.add(pluginType, limited, "skip-path", fail, err.Error(), time.Since(start))
		} else if reason == "" {
			rep.add(pluginType, limited, "skip-path", fail, "limited model served", time.Since(start))
		} else {
			rep.add(pluginType, limited, "skip-path", pass, "skip: "+reason, time.Since(start))
		}
	}
}

func listModels(cfg config, providerID string) ([]modelView, error) {
	status, raw, err := doJSON("GET", cfg.web+"/api/llm-router/dashboard/providers/"+providerID+"/models", nil)
	if err != nil {
		return nil, fmt.Errorf("list models: %w", err)
	}
	var views []modelView
	if err := requireOK(status, raw, &views); err != nil {
		return nil, err
	}
	return views, nil
}
