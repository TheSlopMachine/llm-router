package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

type modelView struct {
	Name      string   `json:"name"`
	Disabled  bool     `json:"disabled"`
	Endpoints []string `json:"endpoints"`
}

func (view modelView) serves(endpoint string) bool {
	// Mirrors models.ModelInfo.SupportsEndpoint: empty means
	// chat/completions only. The scripts module has no dependency on the
	// main module, so the literal stays instead of importing models.
	if len(view.Endpoints) == 0 {
		return endpoint == "chat/completions"
	}
	for _, candidate := range view.Endpoints {
		if candidate == endpoint {
			return true
		}
	}
	return false
}

// skipCodes are wire codes that skip a check with reason instead of failing.
// content_policy, model_unavailable and geo_blocked say nothing about the
// router: the prompt, the model state and the exit are at fault.
// structural_fault stays a failure: broken provider config must be loud.
var skipCodes = map[string]bool{
	"payment_required":       true,
	"quota_exceeded":         true,
	"insufficient_quota":     true,
	"model_not_found":        true,
	"not_found":              true,
	"endpoint_not_supported": true,
	"content_policy":         true,
	"model_unavailable":      true,
	"geo_blocked":            true,
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
	if wireFailure, ok := err.(*wireError); ok {
		if wireFailure.status == 402 {
			return "payment_required", nil
		}
		if wireFailure.code == "rate_limit" {
			if err := run(); err == nil {
				return "", nil
			} else if wireFailure, ok := err.(*wireError); ok && wireFailure.code == "rate_limit" {
				return "rate_limit", nil
			} else {
				return "", err
			}
		}
		if skipCodes[wireFailure.code] {
			return wireFailure.code, nil
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

func emptyErr(err error, output string) error {
	if err != nil {
		return err
	}
	if output == "" {
		return errEmpty
	}
	return nil
}

func checkCompletions(testConfiguration configuration, model string) error {
	statusCode, raw, err := doJSON("POST", testConfiguration.api+"/v1/chat/completions", chatBody(model, false))
	if err != nil {
		return err
	}
	var output struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := requireOK(statusCode, raw, &output); err != nil {
		return err
	}
	if len(output.Choices) == 0 || output.Choices[0].Message.Content == "" {
		return errEmpty
	}
	return nil
}

func checkCompletionsStream(testConfiguration configuration, model string) error {
	statusCode, raw, err := doJSON("POST", testConfiguration.api+"/v1/chat/completions", chatBody(model, true))
	if err != nil {
		return err
	}
	if statusCode < 200 || statusCode >= 300 {
		return &wireError{status: statusCode, code: wireCode(raw), message: strings.TrimSpace(string(raw))}
	}
	if !strings.Contains(string(raw), "[DONE]") {
		return fmt.Errorf("stream without [DONE]")
	}
	return nil
}

func checkMessagesTools(testConfiguration configuration, model string) error {
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
	statusCode, raw, err := doJSON("POST", testConfiguration.api+"/v1/messages", body)
	if err != nil {
		return err
	}
	if statusCode < 200 || statusCode >= 300 {
		return &wireError{status: statusCode, code: wireCode(raw), message: strings.TrimSpace(string(raw))}
	}
	if !strings.Contains(string(raw), "tool_use") {
		return fmt.Errorf("no tool_use block")
	}
	return nil
}

// checkEndpointGate asserts the negative path: a capability the model does
// not serve must fail closed with endpoint_not_supported, never silently.
func checkEndpointGate(testConfiguration configuration, chatModel string) error {
	statusCode, raw, err := postMultipart(testConfiguration.api+"/v1/audio/transcriptions", chatModel, "smoke.wav", synthesizeWAV(), "json")
	if err != nil {
		return err
	}
	if statusCode < 200 || statusCode >= 300 {
		if code := wireCode(raw); code == "endpoint_not_supported" {
			return nil
		}
		return &wireError{status: statusCode, code: wireCode(raw), message: strings.TrimSpace(string(raw))}
	}
	return fmt.Errorf("transcribe on chat-only model must not succeed")
}

func checkVirtualChat(testConfiguration configuration, member string) error {
	virtualModel, err := createVirtual(testConfiguration, member)
	if err != nil {
		return err
	}
	defer deleteVirtual(testConfiguration, virtualModel)
	return checkCompletions(testConfiguration, "virtual/"+virtualModel)
}

func createVirtual(testConfiguration configuration, member string) (string, error) {
	statusCode, raw, err := doJSON("POST", testConfiguration.web+"/api/llm-router/dashboard/virtual-models", map[string]any{
		"name":   testConfiguration.virtualModelName,
		"models": []any{map[string]any{"model_id": member}},
	})
	if err != nil {
		return "", fmt.Errorf("create virtual model: %w", err)
	}
	var virtualModel struct {
		ID string `json:"id"`
	}
	if err := requireOK(statusCode, raw, &virtualModel); err != nil {
		return "", err
	}
	if virtualModel.ID == "" {
		return "", fmt.Errorf("create virtual model: empty id")
	}
	return virtualModel.ID, nil
}

func deleteVirtual(testConfiguration configuration, virtualModelID string) {
	statusCode, raw, err := doJSON("DELETE", testConfiguration.web+"/api/llm-router/dashboard/virtual-models/"+virtualModelID, nil)
	if err == nil {
		_ = requireOK(statusCode, raw, nil)
	}
}

// cleanupStaleVirtualModels removes any virtual model left over from an
// earlier, interrupted run before this run creates its own. createVirtual
// only ever registers cleanup for a create it made itself succeed, so a row
// orphaned by a crash, a Ctrl+C, or an older harness build never self-heals
// on its own — it just permanently blocks every future run's virtual-chat
// check, since virtual model names must be globally unique and the harness
// always asks for the same name. Best-effort: a failure here must not stop
// the run, since the harness's own unique-per-run name still avoids
// colliding with whatever this sweep could not remove.
func cleanupStaleVirtualModels(testConfiguration configuration) {
	statusCode, raw, err := doJSON("GET", testConfiguration.web+"/api/llm-router/dashboard/virtual-models", nil)
	if err != nil {
		return
	}
	var virtualModels []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if requireOK(statusCode, raw, &virtualModels) != nil {
		return
	}
	for _, virtualModel := range virtualModels {
		if virtualModel.ID == "" || !strings.HasPrefix(strings.ToLower(virtualModel.Name), "smoke-vm") {
			continue
		}
		deleteVirtual(testConfiguration, virtualModel.ID)
	}
}

// continuationCodes are probe codes that move to the next model instead of
// failing: limits, gone models and malformed-model answers say nothing
// about the credential or the router. structural_fault is absent on
// purpose: it fails, broken config must be loud. unsupported (no
// check_health handler) skips the same way: nothing to verify.
var continuationCodes = map[string]bool{
	"quota_exceeded": true, "insufficient_quota": true, "payment_required": true, "rate_limit": true,
	"model_not_found": true, "not_found": true,
	"endpoint_not_supported": true, "invalid_request_error": true,
	"content_policy": true, "model_unavailable": true, "geo_blocked": true,
	"unsupported": true,
}

type probeResult struct {
	OK    bool   `json:"ok"`
	Code  string `json:"code"`
	Error string `json:"error"`
}

func postCredentialTest(testConfiguration configuration, credentialID, model string) (probeResult, error) {
	var result probeResult
	statusCode, raw, err := doJSON("POST", testConfiguration.web+"/api/llm-router/dashboard/credentials/"+credentialID+"/test", map[string]any{
		"model": model,
	})
	if err != nil {
		return result, err
	}
	if err := requireOK(statusCode, raw, &result); err != nil {
		return result, err
	}
	return result, nil
}

// checkCredentialTest tries chat-serving models per credential until one
// passes. First success closes the check; auth-dead ends fail only when
// nothing else passed, exhausted dimensions skip with reason.
func checkCredentialTest(testConfiguration configuration, report *report, pluginType, providerID string, credentials []credentialCandidate, models []modelView) {
	var chat []string
	for _, view := range models {
		if !view.Disabled && view.serves("chat/completions") {
			chat = append(chat, view.Name)
		}
	}
	if len(chat) == 0 {
		report.add(pluginType, "-", "credential-test", skip, "no chat model to probe", 0)
		return
	}
	if len(credentials) == 0 {
		report.add(pluginType, "-", "credential-test", skip, "no credential to probe", 0)
		return
	}
	var sawAuthentication bool
	reason := "no models to try"
	for _, credential := range credentials {
		for _, name := range chat {
			model := providerID + "/" + name
			start := time.Now()
			result, err := postCredentialTest(testConfiguration, credential.ID, name)
			if err != nil {
				report.add(pluginType, model, "credential-test", fail, err.Error(), time.Since(start))
				return
			}
			if result.OK {
				report.add(pluginType, model, "credential-test", pass, "key "+credential.Label, time.Since(start))
				return
			}
			if result.Code == "auth_error" {
				sawAuthentication = true
				reason = "auth_error on key " + credential.Label
				break
			}
			if continuationCodes[result.Code] {
				reason = result.Code + " on " + model
				continue
			}
			report.add(pluginType, model, "credential-test", fail, result.Error, time.Since(start))
			return
		}
	}
	if sawAuthentication {
		report.add(pluginType, "-", "credential-test", fail, reason, 0)
		return
	}
	report.add(pluginType, "-", "credential-test", skip, reason, 0)
}

func checkMessages(testConfiguration configuration, model string, stream bool) error {
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
	statusCode, raw, err := doJSON("POST", testConfiguration.api+"/v1/messages", body)
	if err != nil {
		return err
	}
	if statusCode < 200 || statusCode >= 300 {
		return &wireError{status: statusCode, code: wireCode(raw), message: strings.TrimSpace(string(raw))}
	}
	text := string(raw)
	if stream {
		if !strings.Contains(text, "message_stop") {
			return fmt.Errorf("anthropic stream without message_stop")
		}
		return nil
	}
	var output struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &output); err != nil {
		return fmt.Errorf("decode messages response: %w", err)
	}
	if len(output.Content) == 0 {
		return errEmpty
	}
	return nil
}

func checkDashboardChat(testConfiguration configuration, model string) error {
	statusCode, raw, err := doJSON("POST", testConfiguration.web+"/api/llm-router/dashboard/chat/completions", chatBody(model, false))
	if err != nil {
		return err
	}
	var output struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := requireOK(statusCode, raw, &output); err != nil {
		// Dashboard errors carry no wire code: classify via a cheap
		// completions probe on the same model.
		if statusCode == 402 {
			return &wireError{status: statusCode, code: "payment_required"}
		}
		if probeErr := checkCompletions(testConfiguration, model); probeErr != nil {
			return probeErr
		}
		return err
	}
	if len(output.Choices) == 0 {
		return errEmpty
	}
	return nil
}

// synthesizeWAV builds a minimal silent WAV for transcription uploads.
func synthesizeWAV() []byte {
	encodeUint16 := func(number int) []byte { return []byte{byte(number), byte(number >> 8)} }
	encodeUint32 := func(number int) []byte {
		return []byte{byte(number), byte(number >> 8), byte(number >> 16), byte(number >> 24)}
	}
	output := []byte("RIFF")
	output = append(output, encodeUint32(36)...)
	output = append(output, []byte("WAVEfmt ")...)
	output = append(output, encodeUint32(16)...)
	output = append(output, encodeUint16(1)...)
	output = append(output, encodeUint16(1)...)
	output = append(output, encodeUint32(8000)...)
	output = append(output, encodeUint32(16000)...)
	output = append(output, encodeUint16(2)...)
	output = append(output, encodeUint16(16)...)
	output = append(output, []byte("data")...)
	output = append(output, encodeUint32(0)...)
	return output
}

func checkTranscribe(testConfiguration configuration, model string) error {
	statusCode, raw, err := postMultipart(testConfiguration.api+"/v1/audio/transcriptions", model, "smoke.wav", synthesizeWAV(), "srt")
	if err != nil {
		return err
	}
	if statusCode < 200 || statusCode >= 300 {
		return &wireError{status: statusCode, code: wireCode(raw), message: strings.TrimSpace(string(raw))}
	}
	// srt exercises the segments contract plus server-side rendering.
	if !strings.Contains(string(raw), "-->") {
		return fmt.Errorf("srt without cue timing")
	}
	return nil
}

func checkSpeech(testConfiguration configuration, model string) error {
	statusCode, raw, err := doJSON("POST", testConfiguration.api+"/v1/audio/speech", map[string]any{
		"model": model, "input": "hi",
	})
	if err != nil {
		return err
	}
	if statusCode < 200 || statusCode >= 300 {
		return &wireError{status: statusCode, code: wireCode(raw), message: strings.TrimSpace(string(raw))}
	}
	if len(raw) == 0 {
		return fmt.Errorf("empty audio")
	}
	return nil
}

func checkImage(testConfiguration configuration, model string) error {
	statusCode, raw, err := doJSON("POST", testConfiguration.api+"/v1/images/generations", map[string]any{
		"model": model, "prompt": "a cat", "n": 1, "response_format": "url",
	})
	if err != nil {
		return err
	}
	var output struct {
		Data []struct {
			URL     string `json:"url"`
			B64JSON string `json:"b64_json"`
		} `json:"data"`
	}
	if err := requireOK(statusCode, raw, &output); err != nil {
		return err
	}
	if len(output.Data) == 0 {
		return fmt.Errorf("empty image data")
	}
	// Deterministic providers honor url; the contract allows b64 fallback.
	if output.Data[0].URL == "" && output.Data[0].B64JSON == "" {
		return fmt.Errorf("empty image data")
	}
	return nil
}

func checkEmbeddings(testConfiguration configuration, model string) error {
	statusCode, raw, err := doJSON("POST", testConfiguration.api+"/v1/embeddings", map[string]any{
		"model": model, "input": []string{"hi"}, "encoding_format": "base64",
	})
	if err != nil {
		return err
	}
	var output struct {
		Data []struct {
			Embedding any `json:"embedding"`
		} `json:"data"`
	}
	if err := requireOK(statusCode, raw, &output); err != nil {
		return err
	}
	if len(output.Data) == 0 {
		return fmt.Errorf("empty embeddings")
	}
	// base64 wire form renders the vector as a string, not an array.
	encodedEmbedding, ok := output.Data[0].Embedding.(string)
	if !ok || encodedEmbedding == "" {
		return fmt.Errorf("base64 embedding must be a non-empty string")
	}
	return nil
}

// getBytes fetches a binary asset with its content type. The 32MiB cap
// covers generated clips; the mock serves bytes.
func getBytes(url string) (int, string, []byte, error) {
	response, err := apiClient.Get(url)
	if err != nil {
		return 0, "", nil, err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 32<<20))
	if err != nil {
		return 0, "", nil, err
	}
	return response.StatusCode, response.Header.Get("Content-Type"), raw, nil
}

// checkVideo exercises the full video pipeline: submit (202) → poll
// (completed) → content (mp4 bytes with ftyp magic).
func checkVideo(testConfiguration configuration, model string) error {
	statusCode, raw, err := doJSON("POST", testConfiguration.api+"/v1/videos", map[string]any{
		"model": model, "prompt": "a cat", "duration": 1,
	})
	if err != nil {
		return err
	}
	var submission struct {
		ID         string `json:"id"`
		PollingURL string `json:"polling_url"`
		Status     string `json:"status"`
	}
	if err := requireOK(statusCode, raw, &submission); err != nil {
		return err
	}
	if submission.ID == "" || submission.PollingURL == "" {
		return fmt.Errorf("video submit without job id")
	}
	pollingURL := submission.PollingURL
	if !strings.HasPrefix(pollingURL, "http") {
		pollingURL = testConfiguration.api + pollingURL
	}
	statusCode, raw, err = doJSON("GET", pollingURL, nil)
	if err != nil {
		return err
	}
	var job struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := requireOK(statusCode, raw, &job); err != nil {
		return err
	}
	if job.Status != "completed" {
		return fmt.Errorf("video job status %q, want completed", job.Status)
	}
	statusCode, contentType, body, err := getBytes(testConfiguration.api + "/v1/videos/" + submission.ID + "/content?index=0")
	if err != nil {
		return err
	}
	if statusCode < 200 || statusCode >= 300 {
		return &wireError{status: statusCode, code: wireCode(body), message: strings.TrimSpace(string(body))}
	}
	if !strings.Contains(contentType, "video/") {
		return fmt.Errorf("video content type %q, want video/*", contentType)
	}
	if len(body) < 8 || string(body[4:8]) != "ftyp" {
		return fmt.Errorf("video bytes without ftyp magic")
	}
	return nil
}

// checkVideoGate asserts the negative path: submitting video on a
// chat-only model must fail closed with endpoint_not_supported.
func checkVideoGate(testConfiguration configuration, chatModel string) error {
	statusCode, raw, err := doJSON("POST", testConfiguration.api+"/v1/videos", map[string]any{
		"model": chatModel, "prompt": "a cat",
	})
	if err != nil {
		return err
	}
	if statusCode < 200 || statusCode >= 300 {
		if code := wireCode(raw); code == "endpoint_not_supported" {
			return nil
		}
		return &wireError{status: statusCode, code: wireCode(raw), message: strings.TrimSpace(string(raw))}
	}
	return fmt.Errorf("video submit on chat-only model must not succeed")
}

// runMatrix tests requested capabilities, trying serving models in list
// order until one passes. First success closes the capability; quota and
// sibling skips move to the next model, real failures stop. Quota on one
// model never cancels the rest.
func runMatrix(testConfiguration configuration, report *report, pluginType, providerID string, models []modelView) {
	byEndpoint := map[string][]string{}
	for _, view := range models {
		if view.Disabled {
			continue
		}
		for _, endpoint := range []string{"chat/completions", "audio/transcriptions", "audio/speech", "images/generations", "embeddings", "videos"} {
			if view.serves(endpoint) {
				byEndpoint[endpoint] = append(byEndpoint[endpoint], providerID+"/"+view.Name)
			}
		}
	}
	served := map[string]string{}
	// runCandidates tries every serving model until one passes or fails
	// loudly. Skips fall through with the last reason. Single place owning
	// classifyRetryEmpty and report.add.
	runCandidates := func(target string, candidates []string, check func(string) error) string {
		if len(candidates) == 0 {
			report.add(pluginType, "-", target, skip, "no model serves capability", 0)
			return ""
		}
		reason := "no models to try"
		for _, model := range candidates {
			start := time.Now()
			skipReason, err := classifyRetryEmpty(func() error { return check(model) })
			if err != nil {
				report.add(pluginType, model, target, fail, err.Error(), time.Since(start))
				return ""
			}
			if skipReason != "" {
				reason = skipReason
				continue
			}
			report.add(pluginType, model, target, pass, "", time.Since(start))
			served[target] = model
			return model
		}
		report.add(pluginType, "-", target, skip, reason, 0)
		return ""
	}
	// Streams inherit the unary outcome: a stream failing after headers
	// yields 200 plus a truncated body with no classifiable error, so a
	// skipped unary means the stream would hit the same wall.
	runStream := func(target, base string, check func(string) error) {
		if model, ok := served[base]; ok {
			runCandidates(target, []string{model}, check)
			return
		}
		if reason, ok := report.skipReason(base); ok {
			report.add(pluginType, "-", target, skip, "unary skipped: "+reason, 0)
			return
		}
		if candidates := byEndpoint["chat/completions"]; len(candidates) > 0 {
			runCandidates(target, candidates[:1], check)
		}
	}
	chat := byEndpoint["chat/completions"]
	if testConfiguration.targets["completions"] {
		runCandidates("completions", chat, func(modelName string) error {
			return checkCompletions(testConfiguration, modelName)
		})
		runStream("completions-stream", "completions", func(modelName string) error {
			return checkCompletionsStream(testConfiguration, modelName)
		})
		runCandidates("dashboard-chat", chat, func(modelName string) error {
			return checkDashboardChat(testConfiguration, modelName)
		})
	}
	if testConfiguration.targets["messages"] {
		runCandidates("messages", chat, func(modelName string) error {
			return checkMessages(testConfiguration, modelName, false)
		})
		runStream("messages-stream", "messages", func(modelName string) error {
			return checkMessages(testConfiguration, modelName, true)
		})
		// Tool blocks only where deterministic: the mock tools model
		// echoes one tool_call per request. Real providers decide
		// themselves, so no assertion there.
		if pluginType == "mock" {
			runCandidates("messages-tools", []string{providerID + "/mock-tools"}, func(modelName string) error {
				return checkMessagesTools(testConfiguration, modelName)
			})
		}
	}
	if testConfiguration.targets["transcribe"] {
		runCandidates("transcribe", byEndpoint["audio/transcriptions"], func(modelName string) error {
			return checkTranscribe(testConfiguration, modelName)
		})
	}
	if testConfiguration.targets["speech"] {
		runCandidates("speech", byEndpoint["audio/speech"], func(modelName string) error {
			return checkSpeech(testConfiguration, modelName)
		})
	}
	if testConfiguration.targets["image"] {
		runCandidates("image", byEndpoint["images/generations"], func(modelName string) error {
			return checkImage(testConfiguration, modelName)
		})
	}
	if testConfiguration.targets["embeddings"] {
		runCandidates("embeddings", byEndpoint["embeddings"], func(modelName string) error {
			return checkEmbeddings(testConfiguration, modelName)
		})
	}
	if testConfiguration.targets["video"] {
		runCandidates("video", byEndpoint["videos"], func(modelName string) error {
			return checkVideo(testConfiguration, modelName)
		})
	}
	// Mock self-check: mock-limited must skip with quota, proving the
	// harness skip pipeline end to end.
	if pluginType == "mock" && testConfiguration.targets["completions"] {
		limited := providerID + "/mock-limited"
		start := time.Now()
		if reason, err := classify(func() error { return checkCompletions(testConfiguration, limited) }); err != nil {
			report.add(pluginType, limited, "skip-path", fail, err.Error(), time.Since(start))
		} else if reason == "" {
			report.add(pluginType, limited, "skip-path", fail, "limited model served", time.Since(start))
		} else {
			report.add(pluginType, limited, "skip-path", pass, "skip: "+reason, time.Since(start))
		}
	}
	// Negative path: a capability the model does not serve must fail
	// closed, never silently. Free: gated before any backend is touched.
	if len(chat) > 0 {
		runCandidates("endpoint-gate", chat[:1], func(modelName string) error {
			return checkEndpointGate(testConfiguration, modelName)
		})
		runCandidates("endpoint-gate-video", chat[:1], func(modelName string) error {
			return checkVideoGate(testConfiguration, modelName)
		})
	}
	// Virtual fan-out through one ad-hoc virtual model per provider.
	if len(chat) > 0 && testConfiguration.targets["completions"] {
		runCandidates("virtual-chat", chat[:1], func(modelName string) error {
			return checkVirtualChat(testConfiguration, modelName)
		})
	}
}

func listModels(testConfiguration configuration, providerID string) ([]modelView, error) {
	statusCode, raw, err := doJSON("GET", testConfiguration.web+"/api/llm-router/dashboard/providers/"+providerID+"/models", nil)
	if err != nil {
		return nil, fmt.Errorf("list models: %w", err)
	}
	var views []modelView
	if err := requireOK(statusCode, raw, &views); err != nil {
		return nil, err
	}
	return views, nil
}
