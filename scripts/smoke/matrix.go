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

func checkMessagesTools(cfg config, model string) error {
	body := map[string]any{
		"model":      model,
		"max_tokens": 64,
		"messages":   []any{map[string]any{"role": "user", "content": "hi"}},
		"tools": []any{map[string]any{
			"name": "get_weather", "description": "Get weather",
			"input_schema": map[string]any{"type": "object"},
		}},
		"tool_choice": map[string]any{"type": "any"},
	}
	status, raw, err := doJSON("POST", cfg.api+"/v1/messages", body)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return &wireError{status: status, code: wireCode(raw), msg: strings.TrimSpace(string(raw))}
	}
	if !strings.Contains(string(raw), "tool_use") {
		return fmt.Errorf("no tool_use block")
	}
	return nil
}

// checkEndpointGate asserts the negative path: a capability the model does
// not serve must fail closed with endpoint_not_supported, never silently.
func checkEndpointGate(cfg config, chatModel string) error {
	status, raw, err := postMultipart(cfg.api+"/v1/audio/transcriptions", chatModel, "smoke.wav", synthWAV(), "json")
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		if code := wireCode(raw); code == "endpoint_not_supported" {
			return nil
		}
		return &wireError{status: status, code: wireCode(raw), msg: strings.TrimSpace(string(raw))}
	}
	return fmt.Errorf("transcribe on chat-only model must not succeed")
}

func checkVirtualChat(cfg config, member string) error {
	vm, err := createVirtual(cfg, member)
	if err != nil {
		return err
	}
	defer deleteVirtual(cfg, vm)
	return checkCompletions(cfg, "virtual/"+vm)
}

func createVirtual(cfg config, member string) (string, error) {
	status, raw, err := doJSON("POST", cfg.web+"/api/llm-router/dashboard/virtual-models", map[string]any{
		"name":   "smoke-vm",
		"models": []any{map[string]any{"model_id": member}},
	})
	if err != nil {
		return "", fmt.Errorf("create virtual model: %w", err)
	}
	var vm struct {
		ID string `json:"id"`
	}
	if err := requireOK(status, raw, &vm); err != nil {
		return "", err
	}
	if vm.ID == "" {
		return "", fmt.Errorf("create virtual model: empty id")
	}
	return vm.ID, nil
}

func deleteVirtual(cfg config, vmID string) {
	status, raw, err := doJSON("DELETE", cfg.web+"/api/llm-router/dashboard/virtual-models/"+vmID, nil)
	if err == nil {
		_ = requireOK(status, raw, nil)
	}
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
	status, raw, err := postMultipart(cfg.api+"/v1/audio/transcriptions", model, "smoke.wav", synthWAV(), "srt")
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return &wireError{status: status, code: wireCode(raw), msg: strings.TrimSpace(string(raw))}
	}
	// srt exercises the segments contract plus server-side rendering.
	if !strings.Contains(string(raw), "-->") {
		return fmt.Errorf("srt without cue timing")
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
		"model": model, "prompt": "a cat", "n": 1, "response_format": "url",
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
	if len(out.Data) == 0 {
		return fmt.Errorf("empty image data")
	}
	// Deterministic providers honor url; the contract allows b64 fallback.
	if out.Data[0].URL == "" && out.Data[0].B64JSON == "" {
		return fmt.Errorf("empty image data")
	}
	return nil
}

func checkEmbeddings(cfg config, model string) error {
	status, raw, err := doJSON("POST", cfg.api+"/v1/embeddings", map[string]any{
		"model": model, "input": []string{"hi"}, "encoding_format": "base64",
	})
	if err != nil {
		return err
	}
	var out struct {
		Data []struct {
			Embedding any `json:"embedding"`
		} `json:"data"`
	}
	if err := requireOK(status, raw, &out); err != nil {
		return err
	}
	if len(out.Data) == 0 {
		return fmt.Errorf("empty embeddings")
	}
	// base64 wire form renders the vector as a string, not an array.
	s, ok := out.Data[0].Embedding.(string)
	if !ok || s == "" {
		return fmt.Errorf("base64 embedding must be a non-empty string")
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
		// Tool blocks only where deterministic: the mock tools model
		// echoes one tool_call per request. Real providers decide
		// themselves, so no assertion there.
		if pluginType == "mock" {
			run("messages-tools", providerID+"/mock-tools", func() error {
				return checkMessagesTools(cfg, providerID+"/mock-tools")
			})
		}
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
	// Negative path: a capability the model does not serve must fail
	// closed, never silently. Free: gated before any backend is touched.
	if chat := byEndpoint["chat/completions"]; chat != "" {
		run("endpoint-gate", chat, func() error {
			return checkEndpointGate(cfg, chat)
		})
	}
	// Virtual fan-out through one ad-hoc virtual model per provider.
	if chat := byEndpoint["chat/completions"]; chat != "" && cfg.targets["completions"] {
		run("virtual-chat", chat, func() error {
			return checkVirtualChat(cfg, chat)
		})
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
