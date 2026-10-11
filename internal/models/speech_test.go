package models

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSpeechContentType(t *testing.T) {
	cases := map[string]string{
		"mp3":  "audio/mpeg",
		"opus": "audio/opus",
		"aac":  "audio/aac",
		"flac": "audio/flac",
		"wav":  "audio/wav",
		"pcm":  "audio/L16; rate=24000",
		"xyz":  "application/octet-stream",
		"":     "application/octet-stream",
	}
	for format, want := range cases {
		if got := SpeechContentType(format); got != want {
			t.Fatalf("format %q: got %q want %q", format, got, want)
		}
	}
}

func TestSpeechRequest_JSONRoundTrip(t *testing.T) {
	raw := `{"model":"google/gemini-2.5-flash-preview-tts","input":"hello","voice":"Kore","response_format":"wav","speed":1.5,"instructions":"speak slowly"}`
	var request SpeechRequest
	if err := json.Unmarshal([]byte(raw), &request); err != nil {
		t.Fatal(err)
	}
	if request.Model != "google/gemini-2.5-flash-preview-tts" || request.Input != "hello" || request.Voice != "Kore" {
		t.Fatalf("fields: %+v", request)
	}
	if request.Speed == nil || *request.Speed != 1.5 {
		t.Fatalf("speed: %v", request.Speed)
	}
	if request.Instructions != "speak slowly" {
		t.Fatalf("instructions: %q", request.Instructions)
	}
}

func TestImageGenerationResponse_WireShape(t *testing.T) {
	response := ImageGenerationResponse{
		Created: 1700000001,
		Data: []ImageData{
			{B64JSON: "aW1n", RevisedPrompt: "revised"},
			{URL: "https://example.com/i.png"},
		},
	}
	raw, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	wire := string(raw)
	for _, want := range []string{`"created":1700000001`, `"b64_json":"aW1n"`, `"revised_prompt":"revised"`, `"url":"https://example.com/i.png"`} {
		if !strings.Contains(wire, want) {
			t.Fatalf("wire missing %s: %s", want, wire)
		}
	}
	var back ImageGenerationResponse
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if len(back.Data) != 2 || back.Data[0].B64JSON != "aW1n" || back.Data[1].URL == "" {
		t.Fatalf("round trip: %+v", back)
	}
}

func TestModelInfo_SupportsEndpoint_NewEndpoints(t *testing.T) {
	mi := ModelInfo{Name: "m", Endpoints: []string{EndpointAudioSpeech, EndpointImagesGenerations}}
	if !mi.SupportsEndpoint(EndpointAudioSpeech) {
		t.Fatal("audio/speech must be supported")
	}
	if !mi.SupportsEndpoint(EndpointImagesGenerations) {
		t.Fatal("images/generations must be supported")
	}
	if mi.SupportsEndpoint(EndpointChatCompletions) {
		t.Fatal("chat must not be supported when absent from endpoints")
	}
	legacy := ModelInfo{Name: "legacy"}
	if !legacy.SupportsEndpoint(EndpointChatCompletions) {
		t.Fatal("empty endpoints implies chat/completions")
	}
	if legacy.SupportsEndpoint(EndpointAudioSpeech) {
		t.Fatal("empty endpoints must not imply audio/speech")
	}
}

func TestEmbeddingsRequest_UnmarshalInputForms(t *testing.T) {
	var single EmbeddingsRequest
	if err := json.Unmarshal([]byte(`{"model":"google/gemini-embedding-001","input":"hello"}`), &single); err != nil {
		t.Fatal(err)
	}
	if len(single.Input) != 1 || single.Input[0] != "hello" {
		t.Fatalf("single: %+v", single.Input)
	}

	var list EmbeddingsRequest
	if err := json.Unmarshal([]byte(`{"model":"m","input":["a","b"],"encoding_format":"base64","dimensions":256}`), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Input) != 2 || list.EncodingFormat != "base64" || list.Dimensions != 256 {
		t.Fatalf("list: %+v", list)
	}

	var tokens EmbeddingsRequest
	if err := json.Unmarshal([]byte(`{"model":"m","input":[1,2,3]}`), &tokens); err == nil {
		t.Fatal("token arrays must be rejected")
	}

	var missing EmbeddingsRequest
	if err := json.Unmarshal([]byte(`{"model":"m"}`), &missing); err == nil {
		t.Fatal("missing input must be rejected")
	}
}

func TestEmbeddingsResponse_WireShape(t *testing.T) {
	response := EmbeddingsResponse{
		Model: "google/gemini-embedding-001",
		Data: []Embedding{
			{Index: 0, Values: []float64{0.5, -0.25}},
		},
		Usage: &EmbeddingsUsage{PromptTokens: 2, TotalTokens: 2},
	}
	raw, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	wire := string(raw)
	for _, want := range []string{`"object":"embedding"`, `"embedding":[0.5,-0.25]`, `"prompt_tokens":2`} {
		if !strings.Contains(wire, want) {
			t.Fatalf("wire missing %s: %s", want, wire)
		}
	}

	response.Data[0].B64Values = EmbeddingBase64(response.Data[0].Values)
	response.Data[0].Values = nil
	raw, err = json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	wire = string(raw)
	if !strings.Contains(wire, `"embedding":"`) {
		t.Fatalf("base64 form must be a string: %s", wire)
	}
}

func TestEmbeddingBase64_Float32LE(t *testing.T) {
	// 1.0 float32 LE = 00 00 80 3f; base64 of two such floats.
	got := EmbeddingBase64([]float64{1.0, 1.0})
	if got != "AACAPwAAgD8=" {
		t.Fatalf("base64: %q", got)
	}
}
