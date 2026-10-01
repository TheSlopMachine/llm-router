package luaplugin

import (
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/models"
	lua "github.com/yuin/gopher-lua"
)

// testMeta builds a HandlerMeta for handler tests: the given type key,
// credential, model and config. ProviderID defaults to typeKey, mirroring
// the real unqualified-instance case where instance ID equals the type key;
// tests exercising multiple instances of one type build HandlerMeta
// directly with distinct ProviderID values instead of using this helper.
func testMeta(typeKey string, cred *models.Credential, model models.ModelId, cfg map[string]any) HandlerMeta {
	return HandlerMeta{ProviderID: typeKey, TypeKey: typeKey, Credential: cred, Model: model, ProviderConfig: cfg}
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

func TestPrefixCacheKey_SharedAcrossTurnBoundary(t *testing.T) {
	turn1 := &models.ChatCompletionRequest{
		Model:    "opencode-free/muse-spark",
		Messages: []models.ChatMessage{{Role: "user", Content: "hi"}},
	}
	turn2 := &models.ChatCompletionRequest{
		Model: "opencode-free/muse-spark",
		Messages: []models.ChatMessage{
			{Role: "user", Content: "hi"},
			{Role: "assistant", Content: "hello"},
			{Role: "user", Content: "more"},
		},
	}
	k1 := prefixCacheKey(turn1)
	k2 := prefixCacheKey(turn2)
	if k1 == "" || k2 == "" {
		t.Fatal("cache keys must be non-empty")
	}
	if k1 == "prefix-boot" {
		t.Fatal("non-empty history must hash a real key")
	}
	// Appends keep the conversation root: every turn shares one key.
	if k1 != k2 {
		t.Fatalf("appended turn must reuse the root key: %q vs %q", k1, k2)
	}
	// Different roots partition apart.
	other := &models.ChatCompletionRequest{
		Model:    "opencode-free/muse-spark",
		Messages: []models.ChatMessage{{Role: "user", Content: "other"}},
	}
	if prefixCacheKey(other) == k1 {
		t.Fatal("distinct roots must hash distinct keys")
	}
	// Empty history falls back to the boot constant.
	if got := prefixCacheKey(&models.ChatCompletionRequest{Model: "x/y"}); got != "prefix-boot" {
		t.Fatalf("empty history key: %q", got)
	}
	// requestTable carries the key.
	L := newLuaState(t)
	defer L.Close()
	tbl := requestTable(L, turn2)
	if got := luaTestTableString(t, tbl, "cache_key"); got != k2 {
		t.Fatalf("table cache_key: %q vs %q", got, k2)
	}
}
