package luaplugin

import (
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/models"
	lua "github.com/yuin/gopher-lua"
)

// testMeta builds a HandlerMeta for handler tests: fixed test provider,
// given type key, credential, model and config.
func testMeta(typeKey string, cred *models.Credential, model models.ModelId, cfg map[string]any) HandlerMeta {
	return HandlerMeta{ProviderID: "test-provider", TypeKey: typeKey, Credential: cred, Model: model, ProviderConfig: cfg}
}

func luaTestTableString(t *testing.T, tbl *lua.LTable, key string) string {
	t.Helper()
	v := tbl.RawGetString(key)
	s, ok := v.(lua.LString)
	if !ok {
		t.Fatalf("key %q is not a string: %v", key, v)
	}
	return string(s)
}

func TestRequestTables_CarryModelAndModelName(t *testing.T) {
	L := newLuaState(t)
	defer L.Close()

	chat := requestTable(L, &models.ChatCompletionRequest{Model: "groq/openai/gpt-oss-20b"})
	if got := luaTestTableString(t, chat, "model"); got != "groq/openai/gpt-oss-20b" {
		t.Fatalf("chat model: %q", got)
	}
	if got := luaTestTableString(t, chat, "model_name"); got != "openai/gpt-oss-20b" {
		t.Fatalf("chat model_name: %q", got)
	}

	stt := transcriptionRequestTable(L, &models.TranscriptionRequest{Model: "groq/whisper-large-v3"})
	if got := luaTestTableString(t, stt, "model_name"); got != "whisper-large-v3" {
		t.Fatalf("transcribe model_name: %q", got)
	}

	sp := speechRequestTable(L, &models.SpeechRequest{Model: "google/gemini-tts", Input: "hi"})
	if got := luaTestTableString(t, sp, "model_name"); got != "gemini-tts" {
		t.Fatalf("speech model_name: %q", got)
	}

	img := imageRequestTable(L, &models.ImageGenerationRequest{Model: "google/imagen-3", Prompt: "x"})
	if got := luaTestTableString(t, img, "model_name"); got != "imagen-3" {
		t.Fatalf("image model_name: %q", got)
	}

	emb := embeddingsRequestTable(L, &models.EmbeddingsRequest{Model: "google/emb", Input: []string{"a"}, EncodingFormat: "base64"})
	if got := luaTestTableString(t, emb, "model_name"); got != "emb" {
		t.Fatalf("embed model_name: %q", got)
	}
	if got := luaTestTableString(t, emb, "encoding_format"); got != "base64" {
		t.Fatalf("embed encoding_format: %q", got)
	}
}
