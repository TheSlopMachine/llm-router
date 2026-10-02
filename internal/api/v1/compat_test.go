package v1

import (
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/models"
)

func TestExtractToken(t *testing.T) {
	bearer := httptest.NewRequest("GET", "/v1/models", nil)
	bearer.Header.Set("Authorization", "Bearer llmr_a")
	if got := extractToken(bearer); got != "llmr_a" {
		t.Fatalf("bearer: %q", got)
	}
	alias := httptest.NewRequest("GET", "/v1/models", nil)
	alias.Header.Set("x-api-key", "llmr_b")
	if got := extractToken(alias); got != "llmr_b" {
		t.Fatalf("x-api-key: %q", got)
	}
	both := httptest.NewRequest("GET", "/v1/models", nil)
	both.Header.Set("Authorization", "Bearer llmr_a")
	both.Header.Set("x-api-key", "llmr_b")
	if got := extractToken(both); got != "llmr_a" {
		t.Fatalf("precedence: %q", got)
	}
	none := httptest.NewRequest("GET", "/v1/models", nil)
	if got := extractToken(none); got != "" {
		t.Fatalf("empty: %q", got)
	}
	if isAnthropicStyle(none) {
		t.Fatal("plain request must not be anthropic style")
	}
	versioned := httptest.NewRequest("GET", "/v1/models", nil)
	versioned.Header.Set("anthropic-version", "2023-06-01")
	if !isAnthropicStyle(versioned) {
		t.Fatal("versioned request must be anthropic style")
	}
}

func TestWriteCompatError_Envelopes(t *testing.T) {
	h := &Handler{}
	open := httptest.NewRequest("GET", "/v1/models", nil)
	rec := httptest.NewRecorder()
	h.writeCompatError(rec, open, 404, "not_found", "missing", nil)
	var openBody models.OpenAIError
	if err := json.Unmarshal(rec.Body.Bytes(), &openBody); err != nil {
		t.Fatalf("openai envelope: %v", err)
	}
	anth := httptest.NewRequest("GET", "/v1/models", nil)
	anth.Header.Set("anthropic-version", "2023-06-01")
	anthRec := httptest.NewRecorder()
	h.writeCompatError(anthRec, anth, 404, "not_found", "missing", nil)
	var anthBody struct {
		Type  string `json:"type"`
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(anthRec.Body.Bytes(), &anthBody); err != nil {
		t.Fatalf("anthropic envelope: %v", err)
	}
	if anthBody.Type != "error" || anthBody.Error.Type != "invalid_request_error" || anthBody.Error.Message != "missing" {
		t.Fatalf("envelope: %+v", anthBody)
	}
	rate := httptest.NewRecorder()
	h.writeCompatError(rate, anth, 502, "rate_limit", "slow", nil)
	if rate.Code != 429 {
		t.Fatalf("rate status: %d", rate.Code)
	}
}

func TestFilterModelsByToken(t *testing.T) {
	token := &models.RouterToken{Rules: models.TokenRules{
		AllowAllProviders: true,
		AllowedModels:     []models.ModelId{"a/m1"},
	}}
	entries := []models.ModelEntry{{ID: "a/m1"}, {ID: "a/m2"}}
	kept := filterModelsByToken(token, entries)
	if len(kept) != 1 || kept[0].ID != "a/m1" {
		t.Fatalf("kept: %+v", kept)
	}
	if got := filterModelsByToken(token, nil); len(got) != 0 {
		t.Fatalf("nil must stay empty: %+v", got)
	}
}

func TestAnthropicModelList(t *testing.T) {
	out := anthropicModelList([]models.ModelEntry{{ID: "a/m", Object: "model", Created: 1700000000, Name: "M"}})
	if len(out.Data) != 1 || out.Data[0].Type != "model" || out.Data[0].CreatedAt == "" {
		t.Fatalf("list: %+v", out)
	}
	if out.FirstID == nil || *out.FirstID != "a/m" {
		t.Fatalf("first: %+v", out.FirstID)
	}
	empty := anthropicModelList(nil)
	if len(empty.Data) != 0 || empty.FirstID != nil {
		t.Fatalf("empty: %+v", empty)
	}
}

func TestCompletionFromOpenAI_Echo(t *testing.T) {
	echo := true
	req := &models.CompletionRequest{Prompt: []string{"say "}, Suffix: "hi", Echo: &echo}
	resp := &models.ChatCompletionResponse{
		ID: "c1", Object: "chat.completion", Created: 1, Model: "mock/m",
		Choices: []models.ChatCompletionChoice{{Message: models.ChatMessage{Role: "assistant", Content: "yo"}, FinishReason: "stop"}},
	}
	out := completionFromOpenAI(resp, req)
	if out.Object != "text_completion" || len(out.Choices) != 1 {
		t.Fatalf("response: %+v", out)
	}
	if out.Choices[0].Text != "say hi"+"yo" {
		t.Fatalf("echo text: %q", out.Choices[0].Text)
	}
}

func TestCompleteFromOpenAI(t *testing.T) {
	resp := &models.ChatCompletionResponse{
		ID: "r1",
		Choices: []models.ChatCompletionChoice{{
			Message:      models.ChatMessage{Role: "assistant", Content: "done"},
			FinishReason: "length",
		}},
	}
	out := completeFromOpenAI(resp, "mock/m")
	if out.Type != "completion" || out.Completion != "done" || out.StopReason != "max_tokens" {
		t.Fatalf("completion: %+v", out)
	}
}

func TestCompletionStreamWriter_Translates(t *testing.T) {
	var sb strings.Builder
	tw := &completionStreamWriter{w: &sb}
	chunk := `{"id":"c","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"role":"assistant","content":"hi"},"finish_reason":null}]}`
	if _, err := tw.Write([]byte("data: " + chunk + "\n\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte("data: [DONE]\n\n")); err != nil {
		t.Fatal(err)
	}
	got := sb.String()
	if !strings.Contains(got, `"object":"text_completion"`) || !strings.Contains(got, `"text":"hi"`) {
		t.Fatalf("translated: %q", got)
	}
	if !strings.Contains(got, "data: [DONE]") {
		t.Fatalf("done passthrough: %q", got)
	}
}

func TestParseImageEditRequest(t *testing.T) {
	ct, body := imageMultipart(t, map[string]string{"model": "mock/m", "prompt": "edit it", "n": "2"})
	req := httptest.NewRequest("POST", "/v1/images/edits", body)
	req.Header.Set("Content-Type", ct)
	parsed, err := parseImageEditRequest(req, true)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if parsed.Model != "mock/m" || parsed.ImageB64 == "" || parsed.N != 2 {
		t.Fatalf("parsed: %+v", parsed.Model)
	}
	ct2, body2 := imageMultipart(t, map[string]string{"model": "mock/m"})
	noprompt := httptest.NewRequest("POST", "/v1/images/edits", body2)
	noprompt.Header.Set("Content-Type", ct2)
	if _, err := parseImageEditRequest(noprompt, true); err == nil {
		t.Fatal("missing prompt must fail for edits")
	}
	ct3, body3 := imageMultipart(t, map[string]string{"model": "mock/m"})
	variation := httptest.NewRequest("POST", "/v1/images/variations", body3)
	variation.Header.Set("Content-Type", ct3)
	vparsed, err := parseImageEditRequest(variation, false)
	if err != nil {
		t.Fatalf("variations allow empty prompt: %v", err)
	}
	if vparsed.ImageB64 == "" {
		t.Fatal("variation image missing")
	}
}

func imageMultipart(t *testing.T, fields map[string]string) (string, *strings.Reader) {
	t.Helper()
	boundary := "testboundary"
	var sb strings.Builder
	for k, v := range fields {
		sb.WriteString("--" + boundary + "\r\n")
		sb.WriteString(`Content-Disposition: form-data; name="` + k + "\"\r\n\r\n")
		sb.WriteString(v + "\r\n")
	}
	sb.WriteString("--" + boundary + "\r\n")
	sb.WriteString("Content-Disposition: form-data; name=\"image\"; filename=\"in.png\"\r\n")
	sb.WriteString("Content-Type: image/png\r\n\r\n")
	sb.WriteString("FAKEPNG\r\n")
	sb.WriteString("--" + boundary + "--\r\n")
	return "multipart/form-data; boundary=" + boundary, strings.NewReader(sb.String())
}

func TestParseJSONTranscriptionRequest(t *testing.T) {
	audio := base64.StdEncoding.EncodeToString([]byte("FAKEAUDIO"))
	body := `{"model":"mock/m","language":"en","input_audio":{"data":"` + audio + `","format":"wav"}}`
	req := httptest.NewRequest("POST", "/v1/audio/transcriptions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	parsed, err := parseJSONTranscriptionRequest(req)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if string(parsed.File) != "FAKEAUDIO" || parsed.FileName != "clip.wav" {
		t.Fatalf("file: %+v", parsed)
	}
	bad := httptest.NewRequest("POST", "/v1/audio/transcriptions", strings.NewReader(`{"model":"mock/m"}`))
	bad.Header.Set("Content-Type", "application/json")
	if _, err := parseJSONTranscriptionRequest(bad); err == nil {
		t.Fatal("missing input_audio must fail")
	}
}

func TestCountAnthropicTexts(t *testing.T) {
	chat := &models.ChatCompletionRequest{Messages: []models.ChatMessage{
		{Role: "user", Content: "hi"},
		{Role: "user", ContentParts: []models.ChatMessageContentPart{{Type: "text", Text: "there"}}},
	}}
	texts := countAnthropicTexts(chat)
	if len(texts) != 2 {
		t.Fatalf("texts: %+v", texts)
	}
}
