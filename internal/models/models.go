// Package models defines all shared data structures for llm-router.
package models

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"
)

// Plugin API history lives with luaplugin.PluginAPIVersion (1.1).
// Past router contract notes:
//
// 0.0.5 adds the transcribe handler and llm_router.multipart.
// 0.0.6 adds the speech and generate_image handlers.
// 0.0.7 replaces the proxy pool: multi @proxy_location whitelist,
// @proxy_default_option, per-pair rate limits and blocks.
// 0.1.1 replaces per-pair proxy limits and credential quota marks with the
// unified exhausted store: joint limit keys with a scope field on the error
// contract. Old plugins are rejected by the manifest gate.
// 0.1.2 adds the payment_required error type for upstream paywalls.
// 0.2.0 adds DynamicForm Step gaps (flow/grid gap and spacer size accept
// Step int 0..8 alongside the legacy sm|md|lg enum), input_type=secret,
// around/evenly justify values, and strict link URL schemes.
// 0.3.0 reworks the provider error contract: timeout merges into upstream,
// content_policy/model_unavailable/structural_fault added, rate_limit and
// quota_exceeded require plugin-supplied retry_after, geo becomes an
// indefinite per-provider proxy ban with same-key retry on another region,
// auth/payment_required disable the credential, structural_fault disables
// the provider (DisabledBy/Reason/At on both).
// 0.3.1 restricts proxy-source candidates to unauthenticated HTTP URLs;
// the proxypool library owns proxy health and cache lifecycle.
// 0.3.4 permits proxy scope for quota_exceeded and retries proxy-scoped
// rate/quota failures on another proxy with the same credential.
// 0.3.5 adds the transport error type for connectivity failures and retries
// it with the same credential on another proxy.
// 0.3.6 adds the plugin model_specs table: pinned per-model rows merged
// over discovered catalog rows in GetModelInfos.
// 0.3.7 adds the overloaded error type for congested backends: distinct
// code, same-credential proxy retry, no marks or cooldown.
// 0.3.8 adds manual proxy exclusion: structural proxy faults mark the
// proxy dead on an escalating schedule without touching probe scoring.
// 0.3.9 adds the request cache_key: a stable cross-turn prefix-cache
// partition (model, first message, sorted tool names). Old routers serve
// no cache_key and plugins fall back to their own hash.
// 0.4.0 adds the video generation endpoint (POST /v1/videos, GET
// /v1/videos/{jobId}, GET /v1/videos/{jobId}/content, GET /v1/videos/models)
// with generate_video/poll_video/video_content plugin handlers.
// 0.5.0 adds the moderate handler serving POST /v1/moderations and the
// image_b64/image_name/mask_b64 generate_image request fields serving
// POST /v1/images/edits and POST /v1/images/variations.
// 0.5.1 removes traffic-driven disables: auth/payment_required fail over
// without disabling the credential, structural_fault fails the pool without
// disabling the provider; only admin actions and doctor fix disable.
// 0.5.2 adds the check_health handler plus per-type healthcheck_cooldown:
// failure-triggered detached credential verification, disable on explicit
// unhealthy only.
// 0.5.3 unifies same-credential proxy retries under provider proxy_retry
// policy (legacy geo sections migrate at startup) and refreshes known
// model rows from fresh discovery.
// 0.7.0 decentralizes orchestration to plugins: credential selection,
// proxy selection, retry loops and rate-limit handling move into Lua.
// Provider proxy mode/retry sections, the exhausted joint-key store and
// geo bans are removed. Plugins query proxies read-only, assign proxy_url
// per request, and return OpenAI-shaped errors only on terminal failure.
// 0.8.0 adds proxies.require: demand-driven proxy acquisition with country
// filters, exclusion lists, a request-wide wait deadline and an explicit
// direct/fail fallback policy chosen by the plugin.

// RouterVersion is obsolete: router identity ships via main.RouterVersion,
// plugin compatibility via luaplugin.PluginAPIVersion.

// ─────────────────────────────────────────────
// ModelId
// ─────────────────────────────────────────────

// ModelId uniquely identifies a model in the format: provider/model-name[:version]
type ModelId string

func (m ModelId) String() string { return string(m) }

// Parse splits a ModelId into providerID and model name. Both parts must be
// non-empty: "/model" and "provider/" are rejected instead of routing with
// an empty half.
func (m ModelId) Parse() (providerID, model string, err error) {
	s := string(m)
	idx := strings.Index(s, "/")
	if idx == -1 {
		return "", "", fmt.Errorf("invalid ModelId %q: missing '/' separator (expected provider/model-name)", s)
	}
	providerID, model = s[:idx], s[idx+1:]
	if providerID == "" || model == "" {
		return "", "", fmt.Errorf("invalid ModelId %q: provider and model must be non-empty", s)
	}
	return providerID, model, nil
}

// ParseFull splits a ModelId into adapter type, qualifier, and model name.
func (m ModelId) ParseFull() (adapterType, qualifier, model string, err error) {
	providerID, model, err := m.Parse()
	if err != nil {
		return "", "", "", err
	}
	if idx := strings.Index(providerID, ":"); idx != -1 {
		return providerID[:idx], providerID[idx+1:], model, nil
	}
	return providerID, "", model, nil
}

// ParsedModelId groups ModelId parse outputs. New code prefers this over
// the multi-return Parse/ParseFull pair; existing callers stay untouched.
type ParsedModelId struct {
	ProviderID  string
	AdapterType string
	Qualifier   string
	Model       string
}

// ParseStructured parses a ModelId into its structured form.
func (m ModelId) ParseStructured() (ParsedModelId, error) {
	adapterType, qualifier, model, err := m.ParseFull()
	if err != nil {
		return ParsedModelId{}, err
	}
	providerID, _, _ := m.Parse()
	return ParsedModelId{
		ProviderID:  providerID,
		AdapterType: adapterType,
		Qualifier:   qualifier,
		Model:       model,
	}, nil
}

// Name returns the model name without the provider prefix: everything after
// the first '/'. Plugins sending the bare name upstream use this instead of
// hand-rolled stripping. Invalid ids yield the full id unchanged.
func (m ModelId) Name() string {
	if _, model, err := m.Parse(); err == nil {
		return model
	}
	return string(m)
}

// ─────────────────────────────────────────────
// OpenAI-compatible wire types
// ─────────────────────────────────────────────

type ChatMessageContentPart struct {
	Type        string                   `json:"type,omitempty"`
	Text        string                   `json:"text,omitempty"`
	ToolUseID   string                   `json:"tool_use_id,omitempty"`
	ID          string                   `json:"id,omitempty"`
	Name        string                   `json:"name,omitempty"`
	Input       json.RawMessage          `json:"input,omitempty"`
	ImageURL    *ChatMessageImageURL     `json:"image_url,omitempty"`
	InputAudio  *ChatMessageInputAudio   `json:"input_audio,omitempty"`
	Content     []ChatMessageContentPart `json:"-"`
	ContentText string                   `json:"-"`
}

// ChatMessageImageURL is an OpenAI image_url content part. URL is either a
// remote http(s) URL or a data: URL carrying base64 bytes inline.
type ChatMessageImageURL struct {
	URL    string `json:"url,omitempty"`
	Detail string `json:"detail,omitempty"`
}

// ChatMessageInputAudio is an OpenAI input_audio content part. Data is
// base64 audio, Format is wav or mp3.
type ChatMessageInputAudio struct {
	Data   string `json:"data,omitempty"`
	Format string `json:"format,omitempty"`
}

func (p *ChatMessageContentPart) UnmarshalJSON(data []byte) error {
	type rawPart struct {
		Type       string          `json:"type,omitempty"`
		Text       string          `json:"text,omitempty"`
		ToolUseID  string          `json:"tool_use_id,omitempty"`
		ID         string          `json:"id,omitempty"`
		Name       string          `json:"name,omitempty"`
		Input      json.RawMessage `json:"input,omitempty"`
		ImageURL   json.RawMessage `json:"image_url,omitempty"`
		InputAudio json.RawMessage `json:"input_audio,omitempty"`
		Content    json.RawMessage `json:"content,omitempty"`
	}
	var raw rawPart
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	p.Type = raw.Type
	p.Text = raw.Text
	p.ToolUseID = raw.ToolUseID
	p.ID = raw.ID
	p.Name = raw.Name
	p.Input = raw.Input
	if trimmed := strings.TrimSpace(string(raw.ImageURL)); trimmed != "" && trimmed != "null" {
		var iu ChatMessageImageURL
		if err := json.Unmarshal(raw.ImageURL, &iu); err != nil {
			return err
		}
		p.ImageURL = &iu
	}
	if trimmed := strings.TrimSpace(string(raw.InputAudio)); trimmed != "" && trimmed != "null" {
		var ia ChatMessageInputAudio
		if err := json.Unmarshal(raw.InputAudio, &ia); err != nil {
			return err
		}
		p.InputAudio = &ia
	}
	rawContent := strings.TrimSpace(string(raw.Content))
	switch {
	case rawContent == "", rawContent == "null":
	case strings.HasPrefix(rawContent, "\""):
		if err := json.Unmarshal(raw.Content, &p.ContentText); err != nil {
			return err
		}
	case strings.HasPrefix(rawContent, "["):
		if err := json.Unmarshal(raw.Content, &p.Content); err != nil {
			return err
		}
	}
	return nil
}

func (p ChatMessageContentPart) MarshalJSON() ([]byte, error) {
	type rawPart struct {
		Type       string                 `json:"type,omitempty"`
		Text       string                 `json:"text,omitempty"`
		ToolUseID  string                 `json:"tool_use_id,omitempty"`
		ID         string                 `json:"id,omitempty"`
		Name       string                 `json:"name,omitempty"`
		Input      json.RawMessage        `json:"input,omitempty"`
		ImageURL   *ChatMessageImageURL   `json:"image_url,omitempty"`
		InputAudio *ChatMessageInputAudio `json:"input_audio,omitempty"`
		Content    any                    `json:"content,omitempty"`
	}
	out := rawPart{
		Type: p.Type, Text: p.Text, ToolUseID: p.ToolUseID,
		ID: p.ID, Name: p.Name, Input: p.Input,
		ImageURL: p.ImageURL, InputAudio: p.InputAudio,
	}
	if len(p.Content) > 0 {
		out.Content = p.Content
	} else if p.ContentText != "" {
		out.Content = p.ContentText
	}
	return json.Marshal(out)
}

func (p ChatMessageContentPart) TextContent() string {
	if text := strings.TrimSpace(p.Text); text != "" {
		return text
	}
	if text := strings.TrimSpace(p.ContentText); text != "" {
		return text
	}
	parts := make([]string, 0, len(p.Content))
	for _, child := range p.Content {
		if text := strings.TrimSpace(child.TextContent()); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n")
}

type ChatToolFunction struct {
	Name        string         `json:"name,omitempty"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
	Arguments   string         `json:"arguments,omitempty"`
	Strict      *bool          `json:"strict,omitempty"`
}

type ChatToolCall struct {
	ID           string                 `json:"id,omitempty"`
	Type         string                 `json:"type,omitempty"`
	Function     ChatToolFunction       `json:"function"`
	ExtraContent map[string]interface{} `json:"extra_content,omitempty"`
}

type ChatTool struct {
	Type        string            `json:"type,omitempty"`
	Name        string            `json:"name,omitempty"`
	Description string            `json:"description,omitempty"`
	Parameters  map[string]any    `json:"parameters,omitempty"`
	InputSchema map[string]any    `json:"input_schema,omitempty"`
	Function    *ChatToolFunction `json:"function,omitempty"`
	Strict      *bool             `json:"strict,omitempty"`
}

// ChatMessage is a single turn in a conversation.
//
// Content carries plain-text turns. ContentParts carries vision/audio turns:
// on the wire both serialize as the OpenAI `content` field (string or part
// array); the split property exists so the generated API schema documents
// both shapes. Custom MarshalJSON/UnmarshalJSON own the wire mapping, so
// these tags serve the spec generator only.
type ChatMessage struct {
	Role         string                   `json:"role" example:"user" enums:"system,user,assistant,tool,developer"`
	Content      string                   `json:"content,omitempty" example:"Hello, how are you?"`
	ContentParts []ChatMessageContentPart `json:"content_parts,omitempty"`
	ToolCalls    []ChatToolCall           `json:"tool_calls,omitempty"`
	ToolCallID   string                   `json:"tool_call_id,omitempty"`
	Name         string                   `json:"name,omitempty"`
	Refusal      *string                  `json:"refusal,omitempty"`
	// ReasoningContent carries chain-of-thought text exposed by reasoning
	// models (DeepSeek `reasoning_content`, Groq `reasoning`).
	ReasoningContent string `json:"reasoning_content,omitempty"`
}

func (m *ChatMessage) UnmarshalJSON(data []byte) error {
	type rawMessage struct {
		Role             string          `json:"role"`
		Content          json.RawMessage `json:"content"`
		ToolCalls        []ChatToolCall  `json:"tool_calls,omitempty"`
		ToolCallID       string          `json:"tool_call_id,omitempty"`
		Name             string          `json:"name,omitempty"`
		Refusal          *string         `json:"refusal,omitempty"`
		ReasoningContent string          `json:"reasoning_content,omitempty"`
		// Groq-style alias.
		Reasoning string `json:"reasoning,omitempty"`
	}
	var raw rawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	m.Role = raw.Role
	m.ToolCalls = raw.ToolCalls
	m.ToolCallID = raw.ToolCallID
	m.Name = raw.Name
	m.Refusal = raw.Refusal
	m.ReasoningContent = raw.ReasoningContent
	if m.ReasoningContent == "" {
		m.ReasoningContent = raw.Reasoning
	}
	m.Content = ""
	m.ContentParts = nil
	rawContent := strings.TrimSpace(string(raw.Content))
	switch {
	case rawContent == "", rawContent == "null":
	case strings.HasPrefix(rawContent, "\""):
		if err := json.Unmarshal(raw.Content, &m.Content); err != nil {
			return err
		}
	case strings.HasPrefix(rawContent, "["):
		if err := json.Unmarshal(raw.Content, &m.ContentParts); err != nil {
			return err
		}
		m.Content = flattenContentParts(m.ContentParts)
	default:
		return fmt.Errorf("unsupported message content shape")
	}
	return nil
}

func (m ChatMessage) MarshalJSON() ([]byte, error) {
	type rawMessage struct {
		// omitempty: stream deltas without a role must not emit "role":"" —
		// strict clients (older Zed) reject an empty string as an invalid role.
		Role             string         `json:"role,omitempty"`
		Content          any            `json:"content"`
		ToolCalls        []ChatToolCall `json:"tool_calls,omitempty"`
		ToolCallID       string         `json:"tool_call_id,omitempty"`
		Name             string         `json:"name,omitempty"`
		Refusal          *string        `json:"refusal,omitempty"`
		ReasoningContent string         `json:"reasoning_content,omitempty"`
	}
	content := any(m.Content)
	if len(m.ContentParts) > 0 {
		content = m.ContentParts
	}
	return json.Marshal(rawMessage{
		Role: m.Role, Content: content, ToolCalls: m.ToolCalls,
		ToolCallID: m.ToolCallID, Name: m.Name, Refusal: m.Refusal,
		ReasoningContent: m.ReasoningContent,
	})
}

func (m ChatMessage) TextContent() string {
	parts := make([]string, 0, len(m.ToolCalls)+1)
	if text := strings.TrimSpace(m.Content); text != "" {
		parts = append(parts, text)
	} else if len(m.ContentParts) > 0 {
		if text := strings.TrimSpace(flattenContentParts(m.ContentParts)); text != "" {
			parts = append(parts, text)
		}
	}
	for _, toolCall := range m.ToolCalls {
		if name := strings.TrimSpace(toolCall.Function.Name); name != "" {
			parts = append(parts, name)
		}
		if args := strings.TrimSpace(toolCall.Function.Arguments); args != "" {
			parts = append(parts, args)
		}
	}
	return strings.Join(parts, "\n")
}

func flattenContentParts(parts []ChatMessageContentPart) string {
	texts := make([]string, 0, len(parts))
	for _, part := range parts {
		if text := strings.TrimSpace(part.TextContent()); text != "" {
			texts = append(texts, text)
		}
	}
	return strings.Join(texts, "\n")
}

// StreamOptions for streaming
type StreamOptions struct {
	IncludeUsage bool `json:"include_usage,omitempty"`
}

// ChatCompletionRequest is the incoming /v1/chat/completions body.
//
// Alias fields translate derivative clients without new paths: RandomSeed
// (Mistral `random_seed`), Options/Format/Think/KeepAlive (Ollama `options`,
// `format`, `think`, `keep_alive`), Reasoning (OpenRouter
// `reasoning{effort,max_tokens}`), PromptCacheKey/Guardrails (accepted and
// ignored). NormalizeAliases folds every mapped alias into its canonical
// field; unknown fields are ignored by the JSON decoder.
type ChatCompletionRequest struct {
	Model               ModelId        `json:"model" example:"openai/gpt-4o"`
	Messages            []ChatMessage  `json:"messages"`
	Tools               []ChatTool     `json:"tools,omitempty"`
	ToolChoice          any            `json:"tool_choice,omitempty"`
	ParallelToolCalls   *bool          `json:"parallel_tool_calls,omitempty"`
	Stream              bool           `json:"stream,omitempty" example:"false"`
	StreamOptions       *StreamOptions `json:"stream_options,omitempty"`
	MaxTokens           int            `json:"max_tokens,omitempty" example:"1000"`
	MaxCompletionTokens *int           `json:"max_completion_tokens,omitempty"`
	Temperature         float64        `json:"temperature,omitempty" example:"0.7"`
	TopP                float64        `json:"top_p,omitempty" example:"1.0"`
	N                   *int           `json:"n,omitempty"`
	// Stop is a string, a list of strings, or nil (polymorphic by design).
	Stop             any      `json:"stop,omitempty"`
	Seed             *int64   `json:"seed,omitempty"`
	FrequencyPenalty *float64 `json:"frequency_penalty,omitempty"`
	PresencePenalty  *float64 `json:"presence_penalty,omitempty"`
	Logprobs         *bool    `json:"logprobs,omitempty"`
	TopLogprobs      *int     `json:"top_logprobs,omitempty"`
	// ResponseFormat is a free-form object (json_object, json_schema, ...).
	ResponseFormat  any     `json:"response_format,omitempty"`
	User            *string `json:"user,omitempty"`
	ServiceTier     *string `json:"service_tier,omitempty"`
	ReasoningEffort *string `json:"reasoning_effort,omitempty"`
	Verbosity       *string `json:"verbosity,omitempty"`
	// RandomSeed is the Mistral `random_seed` alias for Seed.
	RandomSeed *int64 `json:"random_seed,omitempty"`
	// Options carries Ollama `options{temperature,top_p,seed,stop,num_predict,num_ctx,top_k}`.
	Options map[string]any `json:"options,omitempty"`
	// Format carries the Ollama `format` alias for ResponseFormat.
	Format any `json:"format,omitempty"`
	// Think carries the Ollama `think` flag (accepted, reasoning stays model-driven).
	Think any `json:"think,omitempty"`
	// KeepAlive carries the Ollama `keep_alive` hint (accepted, ignored).
	KeepAlive any `json:"keep_alive,omitempty"`
	// Reasoning carries the OpenRouter `reasoning{effort,max_tokens}` envelope.
	Reasoning *ReasoningConfig `json:"reasoning,omitempty"`
	// PromptCacheKey is accepted and ignored (provider-side caching hint).
	PromptCacheKey any `json:"prompt_cache_key,omitempty"`
	// Guardrails is accepted and ignored (Mistral-side safety config).
	Guardrails any `json:"guardrails,omitempty"`
}

// ReasoningConfig is the OpenRouter `reasoning` envelope: effort maps to
// ReasoningEffort, MaxTokens caps reasoning output.
type ReasoningConfig struct {
	Effort    string `json:"effort,omitempty"`
	MaxTokens *int   `json:"max_tokens,omitempty"`
	Exclude   *bool  `json:"exclude,omitempty"`
	Enabled   *bool  `json:"enabled,omitempty"`
}

// NormalizeAliases folds derivative-client alias fields into their canonical
// counterparts. Canonical fields win on conflict; unmapped aliases stay
// accepted and ignored.
func (r *ChatCompletionRequest) NormalizeAliases() {
	if r.Seed == nil && r.RandomSeed != nil {
		r.Seed = r.RandomSeed
	}
	if r.Reasoning != nil {
		if r.ReasoningEffort == nil && r.Reasoning.Effort != "" {
			e := r.Reasoning.Effort
			r.ReasoningEffort = &e
		}
		if r.MaxCompletionTokens == nil && r.Reasoning.MaxTokens != nil {
			m := *r.Reasoning.MaxTokens
			r.MaxCompletionTokens = &m
		}
	}
	if r.ResponseFormat == nil && r.Format != nil {
		r.ResponseFormat = r.Format
	}
	if len(r.Options) == 0 {
		return
	}
	num := func(key string) (float64, bool) {
		v, ok := r.Options[key]
		if !ok {
			return 0, false
		}
		switch n := v.(type) {
		case float64:
			return n, true
		case int:
			return float64(n), true
		case int64:
			return float64(n), true
		}
		return 0, false
	}
	if r.Temperature == 0 {
		if v, ok := num("temperature"); ok {
			r.Temperature = v
		}
	}
	if r.TopP == 0 {
		if v, ok := num("top_p"); ok {
			r.TopP = v
		}
	}
	if r.Seed == nil {
		if v, ok := num("seed"); ok {
			s := int64(v)
			r.Seed = &s
		}
	}
	if r.Stop == nil {
		if v, ok := r.Options["stop"]; ok {
			r.Stop = v
		}
	}
	if r.MaxTokens == 0 {
		if v, ok := num("num_predict"); ok && v > 0 {
			r.MaxTokens = int(v)
		}
	}
}

// ChatCompletionResponse mirrors the OpenAI response schema.
type ChatCompletionResponse struct {
	ID                string                 `json:"id"`
	Object            string                 `json:"object"`
	Created           int64                  `json:"created"`
	Model             string                 `json:"model"`
	Choices           []ChatCompletionChoice `json:"choices"`
	Usage             ChatCompletionUsage    `json:"usage"`
	SystemFingerprint *string                `json:"system_fingerprint,omitempty"`
	ServiceTier       *string                `json:"service_tier,omitempty"`
}

type ChatCompletionChoice struct {
	Index        int         `json:"index"`
	Message      ChatMessage `json:"message"`
	FinishReason string      `json:"finish_reason"`
	Logprobs     any         `json:"logprobs,omitempty"`
}

type ChatCompletionUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`

	PromptTokensDetails     *PromptTokensDetails     `json:"prompt_tokens_details,omitempty"`
	CompletionTokensDetails *CompletionTokensDetails `json:"completion_tokens_details,omitempty"`
}

// PromptTokensDetails is the OpenAI breakdown of prompt-side token usage.
type PromptTokensDetails struct {
	CachedTokens int `json:"cached_tokens,omitempty"`
	AudioTokens  int `json:"audio_tokens,omitempty"`
}

// CompletionTokensDetails is the OpenAI breakdown of completion-side token usage.
type CompletionTokensDetails struct {
	ReasoningTokens int `json:"reasoning_tokens,omitempty"`
	AudioTokens     int `json:"audio_tokens,omitempty"`
}

// StreamChunk is a single SSE data payload for streaming responses.
type StreamChunk struct {
	ID      string               `json:"id"`
	Object  string               `json:"object"`
	Created int64                `json:"created"`
	Model   string               `json:"model"`
	Choices []StreamChunkChoice  `json:"choices"`
	Usage   *ChatCompletionUsage `json:"usage,omitempty"`
}

type StreamChunkChoice struct {
	Index        int         `json:"index"`
	Delta        ChatMessage `json:"delta"`
	FinishReason *string     `json:"finish_reason"`
	Logprobs     any         `json:"logprobs,omitempty"`
}

// ModelReasoning describes reasoning support of a model (OpenRouter-style).
type ModelReasoning struct {
	// SupportedEfforts in descending effort order (highest first).
	SupportedEfforts []string `json:"supported_efforts,omitempty"`
	DefaultEffort    string   `json:"default_effort,omitempty"`
	DefaultEnabled   bool     `json:"default_enabled,omitempty"`
	Mandatory        bool     `json:"mandatory,omitempty"`
}

// ModelInfo contains metadata about a specific model.
// Field style follows the OpenRouter model schema where applicable.
type ModelInfo struct {
	Name          string `json:"name"`
	DisplayName   string `json:"display_name"`
	Description   string `json:"description,omitempty"`
	RPM           int64  `json:"rpm"`
	TPM           int64  `json:"tpm"`
	RPD           int64  `json:"rpd"`
	ContextWindow int64  `json:"context_window,omitempty"`
	MaxTokens     int64  `json:"max_tokens,omitempty"`

	// Capabilities are UI-facing feature chips (tools, json_mode, ...).
	// Derived from the fields below when the plugin does not set them.
	// Modalities stay out: they render in their own column.
	Capabilities []string `json:"capabilities,omitempty"`

	InputModalities     []string        `json:"input_modalities,omitempty"`
	OutputModalities    []string        `json:"output_modalities,omitempty"`
	SupportedParameters []string        `json:"supported_parameters,omitempty"`
	Reasoning           *ModelReasoning `json:"reasoning,omitempty"`

	// Endpoints lists the served endpoint identifiers (Endpoint* constants).
	// Empty means chat/completions only.
	Endpoints []string `json:"endpoints,omitempty"`
}

// ModelArchitecture carries OpenRouter-style modality details on a model card.
type ModelArchitecture struct {
	InputModalities  []string `json:"input_modalities,omitempty"`
	OutputModalities []string `json:"output_modalities,omitempty"`
	Modality         string   `json:"modality,omitempty"`
}

// ModelEntry is one /v1/models card: OpenAI canonical fields plus the
// OpenRouter-style extension set.
type ModelEntry struct {
	ID                  string             `json:"id"`
	Object              string             `json:"object"`
	Created             int64              `json:"created"`
	OwnedBy             string             `json:"owned_by"`
	Name                string             `json:"name,omitempty"`
	Description         string             `json:"description,omitempty"`
	ContextLength       int64              `json:"context_length,omitempty"`
	MaxCompletionTokens int64              `json:"max_completion_tokens,omitempty"`
	Architecture        *ModelArchitecture `json:"architecture,omitempty"`
	Reasoning           *ModelReasoning    `json:"reasoning,omitempty"`
	SupportedParameters []string           `json:"supported_parameters,omitempty"`
	Capabilities        []string           `json:"capabilities,omitempty"`
}

// ModelListResponse is the GET /v1/models wire response.
type ModelListResponse struct {
	Object string       `json:"object"`
	Data   []ModelEntry `json:"data"`
}

// AnthropicModelEntry is one model card in the Anthropic models shape.
type AnthropicModelEntry struct {
	Type        string `json:"type"`
	ID          string `json:"id"`
	DisplayName string `json:"display_name,omitempty"`
	CreatedAt   string `json:"created_at,omitempty"`
}

// AnthropicModelListResponse is the Anthropic GET /v1/models wire response.
type AnthropicModelListResponse struct {
	Data    []AnthropicModelEntry `json:"data"`
	HasMore bool                  `json:"has_more"`
	FirstID *string               `json:"first_id"`
	LastID  *string               `json:"last_id"`
}

// JoinModalities renders OpenRouter-style modality strings:
// ["text","image"] -> "text+image".
func JoinModalities(mods []string) string {
	return strings.Join(mods, "+")
}

// Endpoint identifiers listed in ModelInfo.Endpoints. The router rejects a
// request when the resolved model declares Endpoints and the target endpoint
// is absent; an empty Endpoints list means chat/completions only (legacy
// plugins predate the field).
const (
	EndpointChatCompletions    = "chat/completions"
	EndpointAudioTranscription = "audio/transcriptions"
	EndpointAudioSpeech        = "audio/speech"
	EndpointImagesGenerations  = "images/generations"
	EndpointEmbeddings         = "embeddings"
	EndpointVideos             = "videos"
	// EndpointModerations names the moderation capability in model cards.
	// Request gates key on EndpointChatCompletions (every moderation
	// request names a chat-serving model); the backend capability
	// pre-check (moderate handler) decides support.
	EndpointModerations = "moderations"
	// EndpointResponses names the responses capability in model cards.
	// Responses, conversations, assistants, threads and runs execute on
	// the chat pipeline, so request gates key on
	// EndpointChatCompletions, exactly like POST /v1/messages.
	EndpointResponses = "responses"
)

// SupportsEndpoint reports whether the model serves endpoint. Empty
// Endpoints implies chat/completions only.
func (m *ModelInfo) SupportsEndpoint(endpoint string) bool {
	if len(m.Endpoints) == 0 {
		return endpoint == EndpointChatCompletions
	}
	for _, e := range m.Endpoints {
		if e == endpoint {
			return true
		}
	}
	return false
}

// ─────────────────────────────────────────────
// Audio transcription wire types (POST /v1/audio/transcriptions)
// ─────────────────────────────────────────────

// TranscriptionRequest is the parsed multipart body of
// POST /v1/audio/transcriptions. File holds the raw audio bytes.
type TranscriptionRequest struct {
	Model                  ModelId
	File                   []byte
	FileName               string
	ContentType            string
	Language               string
	Prompt                 string
	ResponseFormat         string // json (default), text, srt, verbose_json, vtt
	Temperature            *float64
	TimestampGranularities []string // word, segment
}

// TranscriptionResponse is the normalized plugin return: the OpenAI
// verbose_json shape. The router renders the client-facing response_format
// from these fields. Task echoes the requested task (transcribe/translate)
// when the edge set one.
type TranscriptionResponse struct {
	Text     string                 `json:"text"`
	Language string                 `json:"language,omitempty"`
	Duration float64                `json:"duration,omitempty"`
	Segments []TranscriptionSegment `json:"segments,omitempty"`
	Words    []TranscriptionWord    `json:"words,omitempty"`
	Task     *string                `json:"task,omitempty"`
}

type TranscriptionSegment struct {
	ID               int     `json:"id"`
	Seek             int     `json:"seek,omitempty"`
	Start            float64 `json:"start"`
	End              float64 `json:"end"`
	Text             string  `json:"text"`
	Tokens           []int   `json:"tokens,omitempty"`
	Temperature      float64 `json:"temperature,omitempty"`
	AvgLogprob       float64 `json:"avg_logprob,omitempty"`
	CompressionRatio float64 `json:"compression_ratio,omitempty"`
	NoSpeechProb     float64 `json:"no_speech_prob,omitempty"`
}

type TranscriptionWord struct {
	Word  string  `json:"word"`
	Start float64 `json:"start"`
	End   float64 `json:"end"`
}

func srtTimestamp(sec float64, sep byte) string {
	if sec < 0 {
		sec = 0
	}
	ms := int64(sec*1000 + 0.5)
	h := ms / 3600000
	m := (ms % 3600000) / 60000
	s := (ms % 60000) / 1000
	rem := ms % 1000
	return fmt.Sprintf("%02d:%02d:%02d%c%03d", h, m, s, sep, rem)
}

// SRT renders segments as a SubRip subtitle document.
func (r *TranscriptionResponse) SRT() string {
	var b strings.Builder
	for i, seg := range r.Segments {
		fmt.Fprintf(&b, "%d\n%s --> %s\n%s\n\n",
			i+1, srtTimestamp(seg.Start, ','), srtTimestamp(seg.End, ','), strings.TrimSpace(seg.Text))
	}
	return b.String()
}

// VTT renders segments as a WebVTT subtitle document.
func (r *TranscriptionResponse) VTT() string {
	var b strings.Builder
	b.WriteString("WEBVTT\n\n")
	for _, seg := range r.Segments {
		fmt.Fprintf(&b, "%s --> %s\n%s\n\n",
			srtTimestamp(seg.Start, '.'), srtTimestamp(seg.End, '.'), strings.TrimSpace(seg.Text))
	}
	return b.String()
}

// ─────────────────────────────────────────────
// Text-to-speech wire types (POST /v1/audio/speech)
// ─────────────────────────────────────────────

// SpeechRequest is the parsed body of POST /v1/audio/speech.
type SpeechRequest struct {
	Model          ModelId  `json:"model"`
	Input          string   `json:"input"`
	Voice          string   `json:"voice"`
	ResponseFormat string   `json:"response_format,omitempty"` // mp3 (default), opus, aac, flac, wav, pcm
	Speed          *float64 `json:"speed,omitempty"`
	Instructions   string   `json:"instructions,omitempty"`
}

// SpeechResponse is the normalized plugin return: raw audio bytes plus the
// format they are encoded in. The router serves the bytes with the matching
// Content-Type; no transcoding happens inside the router.
type SpeechResponse struct {
	Audio  []byte
	Format string // mp3, opus, aac, flac, wav, pcm
}

// SpeechContentType maps a speech format to its wire Content-Type.
func SpeechContentType(format string) string {
	switch format {
	case "mp3":
		return "audio/mpeg"
	case "opus":
		return "audio/opus"
	case "aac":
		return "audio/aac"
	case "flac":
		return "audio/flac"
	case "wav":
		return "audio/wav"
	case "pcm":
		return "audio/L16; rate=24000"
	default:
		return "application/octet-stream"
	}
}

// ─────────────────────────────────────────────
// Image generation wire types (POST /v1/images/generations)
// ─────────────────────────────────────────────

// ImageGenerationRequest is the parsed body of POST /v1/images/generations
// and, with edit fields set, of POST /v1/images/edits and POST
// /v1/images/variations. ImageB64/MaskB64 carry the multipart uploads as
// base64 (never serialized to clients); plugins that predate them ignore
// the extra table fields.
type ImageGenerationRequest struct {
	Model          ModelId `json:"model"`
	Prompt         string  `json:"prompt"`
	N              int     `json:"n,omitempty"`
	Size           string  `json:"size,omitempty"`
	Quality        string  `json:"quality,omitempty"`
	Style          string  `json:"style,omitempty"`
	ResponseFormat string  `json:"response_format,omitempty"` // url or b64_json
	// ImageB64 holds the base64 source image for edits/variations.
	ImageB64 string `json:"image_b64,omitempty"`
	// ImageName carries the uploaded file name for logging only.
	ImageName string `json:"image_name,omitempty"`
	// MaskB64 holds the base64 edit mask (edits only).
	MaskB64 string `json:"mask_b64,omitempty"`
}

// ImageGenerationResponse is the normalized plugin return and the client
// wire response: the OpenAI images shape.
type ImageGenerationResponse struct {
	Created int64       `json:"created"`
	Data    []ImageData `json:"data"`
}

type ImageData struct {
	URL           string `json:"url,omitempty"`
	B64JSON       string `json:"b64_json,omitempty"`
	RevisedPrompt string `json:"revised_prompt,omitempty"`
}

// ─────────────────────────────────────────────
// Embeddings wire types (POST /v1/embeddings)
// ─────────────────────────────────────────────

// EmbeddingsRequest is the parsed body of POST /v1/embeddings. Input is
// normalized to a list of strings at the edge: OpenAI accepts a single
// string, a list of strings, or token arrays; token arrays are decoded by
// clients upstream of this router and are refused here.
//
// Alias fields translate derivative clients: OutputDimension (Mistral
// `output_dimension`), OutputDtype (accepted, plugins always return float
// vectors), Truncate (accepted, inputs are used whole). NormalizeAliases
// folds OutputDimension into Dimensions.
type EmbeddingsRequest struct {
	Model          ModelId  `json:"model"`
	Input          []string `json:"input,omitempty"`
	EncodingFormat string   `json:"encoding_format,omitempty"` // float (default) or base64
	Dimensions     int      `json:"dimensions,omitempty"`
	// OutputDimension is the Mistral alias for Dimensions.
	OutputDimension int `json:"output_dimension,omitempty"`
	// OutputDtype is accepted and documented; vectors stay float32.
	OutputDtype string `json:"output_dtype,omitempty"`
	// Truncate is accepted; over-long inputs are embedded whole.
	Truncate any `json:"truncate,omitempty"`
}

// NormalizeAliases folds the output_dimension alias into Dimensions.
func (r *EmbeddingsRequest) NormalizeAliases() {
	if r.Dimensions == 0 && r.OutputDimension != 0 {
		r.Dimensions = r.OutputDimension
	}
}

// embeddingsRequestJSON decodes the polymorphic OpenAI input field.
func (r *EmbeddingsRequest) UnmarshalJSON(raw []byte) error {
	var probe struct {
		Model           ModelId         `json:"model"`
		Input           json.RawMessage `json:"input"`
		EncodingFormat  string          `json:"encoding_format,omitempty"`
		Dimensions      int             `json:"dimensions,omitempty"`
		OutputDimension int             `json:"output_dimension,omitempty"`
		OutputDtype     string          `json:"output_dtype,omitempty"`
		Truncate        any             `json:"truncate,omitempty"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return err
	}
	r.Model = probe.Model
	r.EncodingFormat = probe.EncodingFormat
	r.Dimensions = probe.Dimensions
	r.OutputDimension = probe.OutputDimension
	r.OutputDtype = probe.OutputDtype
	r.Truncate = probe.Truncate
	r.NormalizeAliases()
	if len(probe.Input) == 0 {
		return fmt.Errorf("missing required field 'input'")
	}
	var single string
	if err := json.Unmarshal(probe.Input, &single); err == nil {
		r.Input = []string{single}
		return nil
	}
	var list []string
	if err := json.Unmarshal(probe.Input, &list); err == nil {
		r.Input = list
		return nil
	}
	return fmt.Errorf("input must be a string or an array of strings")
}

// EmbeddingsResponse is the normalized plugin return and the client wire
// response. Plugins always return float vectors; the edge renders
// encoding_format=base64 from them.
type EmbeddingsResponse struct {
	Data  []Embedding      `json:"data"`
	Model string           `json:"model"`
	Usage *EmbeddingsUsage `json:"usage,omitempty"`
}

type Embedding struct {
	Index int `json:"index"`
	// Object is always "embedding" on the wire; Values holds the float
	// vector, B64Values the base64 float32-LE rendering (exactly one is
	// set when marshaled). Custom MarshalJSON/UnmarshalJSON own the wire
	// mapping; these tags serve the spec generator only.
	Object    string    `json:"object,omitempty" example:"embedding"`
	Values    []float64 `json:"embedding,omitempty"`
	B64Values string    `json:"-"`
}

// UnmarshalJSON accepts the plugin-side embedding entry: the OpenAI
// {index, embedding:[...]} shape with float values.
func (e *Embedding) UnmarshalJSON(raw []byte) error {
	var wire struct {
		Index     int       `json:"index"`
		Embedding []float64 `json:"embedding"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return err
	}
	e.Index = wire.Index
	e.Values = wire.Embedding
	return nil
}

// MarshalJSON renders one embedding entry in the OpenAI shape: float array
// by default, base64 float32-LE when the client asked for base64.
func (e Embedding) MarshalJSON() ([]byte, error) {
	type wire struct {
		Object    string `json:"object"`
		Index     int    `json:"index"`
		Embedding any    `json:"embedding"`
	}
	out := wire{Object: "embedding", Index: e.Index}
	if e.B64Values != "" {
		out.Embedding = e.B64Values
	} else {
		out.Embedding = e.Values
	}
	return json.Marshal(out)
}

type EmbeddingsUsage struct {
	PromptTokens int `json:"prompt_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

// EmbeddingBase64 encodes a float vector as base64 float32 little-endian,
// the OpenAI encoding_format=base64 wire form.
func EmbeddingBase64(values []float64) string {
	buf := make([]byte, 4*len(values))
	for i, v := range values {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(float32(v)))
	}
	return base64.StdEncoding.EncodeToString(buf)
}

// ─────────────────────────────────────────────
// Video generation wire types (OpenRouter-compatible /v1/videos)
// ─────────────────────────────────────────────

// Video statuses carried by VideoGenerationResponse.Status.
const (
	VideoStatusPending    = "pending"
	VideoStatusInProgress = "in_progress"
	VideoStatusCompleted  = "completed"
	VideoStatusFailed     = "failed"
	VideoStatusCancelled  = "cancelled"
	VideoStatusExpired    = "expired"
)

// ValidVideoStatus reports whether status is a known video job status.
func ValidVideoStatus(status string) bool {
	switch status {
	case VideoStatusPending, VideoStatusInProgress, VideoStatusCompleted,
		VideoStatusFailed, VideoStatusCancelled, VideoStatusExpired:
		return true
	}
	return false
}

// VideoReferenceURL is the `{url}` object of one video reference asset.
type VideoReferenceURL struct {
	URL string `json:"url,omitempty"`
}

// VideoFrameImage pins one image as the first or last frame of the
// generated video. FrameType is `first_frame` or `last_frame`.
type VideoFrameImage struct {
	Type      string             `json:"type,omitempty"`
	ImageURL  *VideoReferenceURL `json:"image_url,omitempty"`
	FrameType string             `json:"frame_type,omitempty"`
}

// VideoInputReference is one reference asset guiding video generation.
// Exactly one of ImageURL, AudioURL, VideoURL is set; Type names it
// (`image_url`, `audio_url`, `video_url`).
type VideoInputReference struct {
	Type     string             `json:"type,omitempty"`
	ImageURL *VideoReferenceURL `json:"image_url,omitempty"`
	AudioURL *VideoReferenceURL `json:"audio_url,omitempty"`
	VideoURL *VideoReferenceURL `json:"video_url,omitempty"`
}

// VideoGenerationRequest is the parsed body of POST /v1/videos.
type VideoGenerationRequest struct {
	Model           ModelId               `json:"model"`
	Prompt          string                `json:"prompt,omitempty"`
	Duration        int                   `json:"duration,omitempty"`
	Resolution      string                `json:"resolution,omitempty"`
	AspectRatio     string                `json:"aspect_ratio,omitempty"`
	Size            string                `json:"size,omitempty"`
	Seed            *int64                `json:"seed,omitempty"`
	GenerateAudio   *bool                 `json:"generate_audio,omitempty"`
	FrameImages     []VideoFrameImage     `json:"frame_images,omitempty"`
	InputReferences []VideoInputReference `json:"input_references,omitempty"`
	PreviousJobID   string                `json:"previous_job_id,omitempty"`
	CallbackURL     string                `json:"callback_url,omitempty"`
	ProviderOptions map[string]any        `json:"provider,omitempty"`
	User            string                `json:"user,omitempty"`
	SessionID       string                `json:"session_id,omitempty"`
}

// VideoGenerationUsage carries cost metadata of a completed video job.
type VideoGenerationUsage struct {
	Cost   *float64 `json:"cost,omitempty"`
	IsBYOK bool     `json:"is_byok,omitempty"`
}

// VideoGenerationResponse is the submit/poll wire shape: the OpenRouter
// video generation response. On submit the router returns 202 with the
// router-local ID and polling URL; on poll the same shape carries the
// terminal state, unsigned download URLs and usage.
type VideoGenerationResponse struct {
	ID           string                `json:"id"`
	PollingURL   string                `json:"polling_url"`
	Status       string                `json:"status"`
	GenerationID string                `json:"generation_id,omitempty"`
	UnsignedURLs []string              `json:"unsigned_urls,omitempty"`
	Usage        *VideoGenerationUsage `json:"usage,omitempty"`
	Error        string                `json:"error,omitempty"`
}

// VideoContentResponse is the normalized plugin return for one video
// content fetch: raw video bytes plus their media type. The router serves
// the bytes with the matching Content-Type; no transcoding happens inside
// the router.
type VideoContentResponse struct {
	Video       []byte
	ContentType string
}

// VideoContentType defaults empty content types to video/mp4.
func VideoContentType(contentType string) string {
	if strings.TrimSpace(contentType) == "" {
		return "video/mp4"
	}
	return contentType
}

// VideoUpscaleRange bounds the supported upscale factor of a video
// upscaling model.
type VideoUpscaleRange struct {
	Min *float64 `json:"min,omitempty"`
	Max *float64 `json:"max,omitempty"`
}

// VideoModel describes one entry of GET /v1/videos/models: the OpenRouter
// video model card. Capability fields stay nil when the backend does not
// report them.
type VideoModel struct {
	ID                           string             `json:"id"`
	CanonicalSlug                string             `json:"canonical_slug"`
	Name                         string             `json:"name"`
	Created                      int64              `json:"created"`
	Description                  string             `json:"description,omitempty"`
	SupportedResolutions         []string           `json:"supported_resolutions"`
	SupportedAspectRatios        []string           `json:"supported_aspect_ratios"`
	SupportedSizes               []string           `json:"supported_sizes"`
	SupportedDurations           []int              `json:"supported_durations"`
	SupportedFrameImages         []string           `json:"supported_frame_images"`
	UpscaleFactor                *VideoUpscaleRange `json:"upscale_factor"`
	Creativity                   []int              `json:"creativity"`
	GenerateAudio                *bool              `json:"generate_audio"`
	Seed                         *bool              `json:"seed"`
	AllowedPassthroughParameters []string           `json:"allowed_passthrough_parameters"`
}

// VideoModelsListResponse is the wire shape of GET /v1/videos/models.
type VideoModelsListResponse struct {
	Data []VideoModel `json:"data"`
}

// VideoJob persists one router-side video generation job: the local ID
// handed to the client plus the upstream job it polls. Model is the
// original request model (token authorization on every poll); BackendModel
// is the model polled upstream (differs for virtual fan-out winners).
type VideoJob struct {
	ID            string    `json:"id"`
	Model         ModelId   `json:"model"`
	BackendModel  ModelId   `json:"backend_model"`
	ProviderID    string    `json:"provider_id"`
	TypeKey       string    `json:"type_key"`
	UpstreamJobID string    `json:"upstream_job_id"`
	Status        string    `json:"status"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// NewVideoJobID mints a router-local video job ID in the OpenRouter
// `gen-vid-<timestamp>-<20 alphanumerics>` format.
func NewVideoJobID(now time.Time) string {
	const alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	suffix := make([]byte, 20)
	if _, err := rand.Read(suffix); err != nil {
		for i := range suffix {
			suffix[i] = alphabet[int(now.UnixNano()+int64(i))%len(alphabet)]
		}
		return fmt.Sprintf("gen-vid-%d-%s", now.Unix(), string(suffix))
	}
	for i := range suffix {
		suffix[i] = alphabet[int(suffix[i])%len(alphabet)]
	}
	return fmt.Sprintf("gen-vid-%d-%s", now.Unix(), string(suffix))
}

// DeriveCapabilities fills Capabilities from the OpenRouter-style fields.
// Explicit plugin-provided capabilities win.
func (m *ModelInfo) DeriveCapabilities() {
	if len(m.Capabilities) > 0 {
		return
	}
	has := func(list []string, v string) bool {
		for _, x := range list {
			if x == v {
				return true
			}
		}
		return false
	}
	var caps []string
	if has(m.SupportedParameters, "tools") {
		caps = append(caps, "tools")
	}
	if has(m.SupportedParameters, "response_format") {
		caps = append(caps, "json_mode")
	}
	if has(m.SupportedParameters, "structured_outputs") {
		caps = append(caps, "structured_outputs")
	}
	if m.Reasoning != nil {
		caps = append(caps, "reasoning")
	}
	m.Capabilities = caps
}

// ModelOverride is the per-provider admin override for a single model:
// disable from routing and /v1/models, register a custom model the upstream
// does not list, or adjust display metadata.
type ModelOverride struct {
	ProviderID   string   `json:"provider_id"`
	Name         string   `json:"name"`
	Disabled     bool     `json:"disabled,omitempty"`
	Custom       bool     `json:"custom,omitempty"`
	DisplayName  string   `json:"display_name,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`
	// InputModalities and OutputModalities hold probe-verified modalities,
	// keyed by full model id. Applied over upstream data when non-empty.
	InputModalities  []string `json:"input_modalities,omitempty"`
	OutputModalities []string `json:"output_modalities,omitempty"`
}

// ─────────────────────────────────────────────
// Provider errors (0.7.0)
// ─────────────────────────────────────────────

// ProviderError is the terminal error a plugin returns when its own
// retries are exhausted. The shape mirrors the OpenAI error object:
// a human message plus a wire code. Routing never inspects anything
// else, except the optional response Headers map, which the HTTP layer
// renders through its allow-list (Retry-After today). Headers never
// reach JSON envelopes or stored records.
type ProviderError struct {
	StatusCode int
	Message    string
	Code       string
	Param      string
	Headers    map[string]string `json:"-"`
}

func (e *ProviderError) Error() string {
	return fmt.Sprintf("provider error (%d): %s", e.StatusCode, e.Message)
}

// PluginInternalError is a caught Lua failure at the Go/Lua boundary.
type PluginInternalError struct {
	PluginID string
	TypeKey  string
	Cause    string
}

func (e *PluginInternalError) Error() string {
	if e.TypeKey != "" {
		return fmt.Sprintf("plugin %q (type %q) internal error: %s", e.PluginID, e.TypeKey, e.Cause)
	}
	return fmt.Sprintf("plugin %q internal error: %s", e.PluginID, e.Cause)
}

// ─────────────────────────────────────────────
// Admin
// ─────────────────────────────────────────────

// AdminUser is the dashboard operator account.
type AdminUser struct {
	Username     string    `json:"username"`
	PasswordHash string    `json:"password_hash"`
	CreatedAt    time.Time `json:"created_at"`
}

// ─────────────────────────────────────────────
// Router Tokens
// ─────────────────────────────────────────────

// RouterToken is an opaque bearer token issued by llm-router itself.
type RouterToken struct {
	ID        string     `json:"id" example:"token123"`
	Name      string     `json:"name" example:"Production API Token"`
	Token     string     `json:"token,omitempty" example:"llmr_abc123def456"`
	TokenHash string     `json:"token_hash,omitempty"`
	Rules     TokenRules `json:"rules"`
	CreatedAt time.Time  `json:"created_at" example:"2026-05-04T15:00:00Z"`
}

// TokenRules constrains what an API token is allowed to do.
type TokenRules struct {
	AllowedProviders    []string  `json:"allowed_providers"`
	AllowAllProviders   bool      `json:"allow_all_providers"`
	AllowedModels       []ModelId `json:"allowed_models"`
	AllowAllModels      bool      `json:"allow_all_models"`
	AllowedCredentials  []string  `json:"allowed_credentials"`
	AllowAllCredentials bool      `json:"allow_all_credentials"`
	// CredentialScopes grants credential access per provider: All covers
	// every current and future key of the provider, CredentialIDs pins
	// specific keys. Scopes union with the legacy flat credential fields.
	CredentialScopes []CredentialScope `json:"credential_scopes,omitempty"`
}

// CredentialScope grants a token access to credentials of one provider.
type CredentialScope struct {
	ProviderID    string   `json:"provider_id"`
	All           bool     `json:"all"`
	CredentialIDs []string `json:"credential_ids,omitempty"`
}

// AllowsProvider reports whether the token may use the given provider.
func (r TokenRules) AllowsProvider(providerID string) bool {
	if r.AllowAllProviders {
		return true
	}
	if len(r.AllowedProviders) == 0 {
		return false
	}
	for _, id := range r.AllowedProviders {
		if id == providerID {
			return true
		}
	}
	return false
}

// AllowsCredential reports whether the token may use the given credential
// of the given provider. Scopes union with the legacy flat fields: either
// source granting access allows the credential.
func (r TokenRules) AllowsCredential(providerID, credentialID string) bool {
	if r.AllowAllCredentials {
		return true
	}
	for _, scope := range r.CredentialScopes {
		if scope.ProviderID != providerID {
			continue
		}
		if scope.All {
			return true
		}
		for _, id := range scope.CredentialIDs {
			if id == credentialID {
				return true
			}
		}
	}
	if len(r.AllowedCredentials) == 0 {
		return false
	}
	for _, id := range r.AllowedCredentials {
		if id == credentialID {
			return true
		}
	}
	return false
}

// Allows reports whether the token may request the given model.
func (r TokenRules) Allows(model ModelId) bool {
	providerID, _, err := model.Parse()
	if err != nil {
		return false
	}
	if !r.AllowsProvider(providerID) {
		return false
	}
	if r.AllowAllModels {
		return true
	}
	if len(r.AllowedModels) == 0 {
		return false
	}
	for _, m := range r.AllowedModels {
		if m == model {
			return true
		}
	}
	return false
}

// ─────────────────────────────────────────────
// Providers — unified ProviderInstance
// ─────────────────────────────────────────────

// Who disabled a provider or credential. Single source of truth for
// DisabledBy comparisons across credential, provider, and healthcheck.
const (
	DisabledByAdmin       = "admin"
	DisabledBySystem      = "system"
	DisabledByPlugin      = "plugin"
	DisabledByHealthcheck = "healthcheck"
)

// ProviderInstance is the single persisted provider record for every type:
// lua-plugin type keys, built-in "custom" and "virtual".
type ProviderInstance struct {
	ID           string         `json:"id" example:"opencode-zen"`
	Name         string         `json:"name" example:"OpenCode Zen"`
	TypeKey      string         `json:"type_key" example:"opencode-zen"`
	Qualifier    string         `json:"qualifier" example:""`
	Config       map[string]any `json:"config"`
	IconURL      string         `json:"icon_url" example:"https://cdn.example.com/openai.svg"`
	IsUIReadonly bool           `json:"is_ui_readonly"`
	IsUIHidden   bool           `json:"is_ui_hidden"`
	// Disabled takes the provider out of routing and model listings.
	// Settings and discovery keep working.
	Disabled bool `json:"disabled,omitempty"`
	// DisabledBy names who disabled the provider: DisabledByAdmin
	// (dashboard PUT) or DisabledBySystem (doctor fix for backend-less
	// types). Empty when enabled.
	DisabledBy string `json:"disabled_by,omitempty"`
	// DisabledReason carries the human reason (system cause or admin note).
	DisabledReason string `json:"disabled_reason,omitempty"`
	// DisabledAt marks when the provider was disabled. Nil when enabled.
	DisabledAt *time.Time `json:"disabled_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

// Provider is kept as an alias so existing call sites keep compiling
// while the field migration (Type -> TypeKey, BaseURL -> Config) proceeds.
type Provider = ProviderInstance

// BaseURL returns Config["base_url"] for OpenAI-compatible providers.
func (p *ProviderInstance) BaseURL() string {
	if p == nil || p.Config == nil {
		return ""
	}
	s, _ := p.Config["base_url"].(string)
	return s
}

// NormalizeBaseURL trims raw and requires an http(s) URL without a trailing
// slash. Single source of truth for base_url validation across provider
// CRUD and the custom backend.
func NormalizeBaseURL(raw string) (string, error) {
	baseURL := strings.TrimSpace(raw)
	if baseURL == "" {
		return "", fmt.Errorf("base_url is required for custom providers")
	}
	if !strings.HasPrefix(baseURL, "https://") && !strings.HasPrefix(baseURL, "http://") {
		return "", fmt.Errorf("base_url must be a valid HTTP/HTTPS URL")
	}
	return strings.TrimSuffix(baseURL, "/"), nil
}

// ProviderStats holds aggregated statistics for a provider.
type ProviderStats struct {
	ModelCount      int `json:"model_count" example:"5"`
	CredentialCount int `json:"credential_count" example:"2"`
}

// ProviderInstanceCreateRequest is the create body for custom providers.
// TypeKey must be "custom"; Qualifier is ignored by the dashboard handler
// and kept only for backward compatibility with older clients.
type ProviderInstanceCreateRequest struct {
	Name      string         `json:"name"`
	TypeKey   string         `json:"type_key"`
	Qualifier string         `json:"qualifier"`
	Config    map[string]any `json:"config"`
	IconURL   string         `json:"icon_url"`
}

// ProviderInstanceUpdateRequest is the generic update body.
type ProviderInstanceUpdateRequest struct {
	Name     string         `json:"name"`
	Config   map[string]any `json:"config"`
	IconURL  string         `json:"icon_url"`
	Disabled *bool          `json:"disabled"`
}

// ─────────────────────────────────────────────
// Credentials
// ─────────────────────────────────────────────

// Credential holds provider-specific authentication data.
// Data is a flexible map to support any auth scheme, including
// non-string values such as expiry timestamps.
type Credential struct {
	ID         string         `json:"id"`
	ProviderID string         `json:"provider_id"`
	Label      string         `json:"label"`
	Data       map[string]any `json:"data"`
	ExpiresAt  *time.Time     `json:"expires_at,omitempty"`
	CreatedAt  time.Time      `json:"created_at"`
	UpdatedAt  time.Time      `json:"updated_at"`

	LastUsedAt   *time.Time `json:"last_used_at,omitempty"`
	RequestCount int64      `json:"request_count"`
	SuccessCount int64      `json:"success_count"`
	FailureCount int64      `json:"failure_count"`

	// Disabled excludes the credential from routing and fallthrough.
	Disabled bool `json:"disabled,omitempty"`
	// DisabledBy names who disabled the credential: DisabledByAdmin.
	// Empty when enabled.
	DisabledBy string `json:"disabled_by,omitempty"`
	// DisabledReason carries the human reason (admin note).
	DisabledReason string `json:"disabled_reason,omitempty"`
	// DisabledAt marks when the credential was disabled. Nil when enabled.
	DisabledAt *time.Time `json:"disabled_at,omitempty"`
	// Order is the admin-defined pool position (1-based). 0 means unordered:
	// unordered credentials sort after ordered ones by computed priority.
	Order int `json:"order,omitempty"`
}

// IsExpired reports whether the credential has passed its expiry time.
func (c *Credential) IsExpired() bool {
	if c.ExpiresAt == nil {
		return false
	}
	return time.Now().After(*c.ExpiresAt)
}

// ParkEntry is one unified cooldown park: the credential stays enabled and
// dashboard-visible, but plugin rotation skips it until Until passes.
// Transient states only (rate limits, quota windows); dead keys disable.
type ParkEntry struct {
	CredentialID string    `json:"credential_id"`
	Reason       string    `json:"reason,omitempty"`
	Until        time.Time `json:"until"`
	ParkedAt     time.Time `json:"parked_at"`
}

// Expired reports whether the park no longer holds.
func (e *ParkEntry) Expired(now time.Time) bool {
	return !now.Before(e.Until)
}

// ExpiresIn returns the duration until expiry (negative if already expired).
func (c *Credential) ExpiresIn() time.Duration {
	if c.ExpiresAt == nil {
		return 0
	}
	return time.Until(*c.ExpiresAt)
}

// Priority returns the selection priority for this credential: unused
// first, then used, expired last. Limit state lives in the exhausted store,
// never on the credential.
func (c *Credential) Priority() int {
	if c.IsExpired() {
		return 2
	}
	if c.LastUsedAt == nil {
		return 0
	}
	return 1
}

// IncrementUsage updates usage statistics after a request.
func (c *Credential) IncrementUsage(success bool) {
	now := time.Now()
	c.LastUsedAt = &now
	c.RequestCount++
	if success {
		c.SuccessCount++
	} else {
		c.FailureCount++
	}
}

// DataString returns a string view of a data value for Go adapters
// that only understand string credentials.
func (c *Credential) DataString(key string) string {
	if c == nil || c.Data == nil {
		return ""
	}
	switch v := c.Data[key].(type) {
	case string:
		return strings.TrimSpace(v)
	case fmt.Stringer:
		return strings.TrimSpace(v.String())
	case float64:
		return strings.TrimSpace(fmt.Sprintf("%v", v))
	case bool:
		if v {
			return "true"
		}
		return "false"
	case nil:
		return ""
	default:
		return ""
	}
}

// ─────────────────────────────────────────────
// Lua-driven UI tree
// ─────────────────────────────────────────────

// UINode is a single node of the lua-driven UI tree shared by
// config_schema, credential_schema and auth wizards.
type UINode struct {
	Type         string            `json:"type"`
	Text         string            `json:"text,omitempty"`
	Name         string            `json:"name,omitempty"`
	Label        string            `json:"label,omitempty"`
	InputType    string            `json:"input_type,omitempty"`
	Required     bool              `json:"required,omitempty"`
	Options      []string          `json:"options,omitempty"`
	OptionLabels map[string]string `json:"option_labels,omitempty"`
	URL          string            `json:"url,omitempty"`
	Variant      string            `json:"variant,omitempty"`
	FormAction   string            `json:"form_action,omitempty"`
	Content      []*UINode         `json:"content,omitempty"`
	Placeholder  string            `json:"placeholder,omitempty"`
	Value        any               `json:"value,omitempty"`
	Direction    string            `json:"direction,omitempty"`
	Align        string            `json:"align,omitempty"`
	Justify      string            `json:"justify,omitempty"`
	Gap          int               `json:"gap,omitempty"`
	Columns      int               `json:"columns,omitempty"`
	Title        string            `json:"title,omitempty"`
	Subtitle     string            `json:"subtitle,omitempty"`
	Wrap         bool              `json:"wrap,omitempty"`
	Grow         bool              `json:"grow,omitempty"`
	Size         int               `json:"size,omitempty"`
	Extra        map[string]any    `json:"-"`
}

// AuthStepInput is the submit payload for auth_step.
type AuthStepInput struct {
	Action string         `json:"action"`
	Values map[string]any `json:"values"`
	FlowID string         `json:"flow_id,omitempty"`
}

// AuthFlowResult is one of the three auth_step/auth_initiate outcomes,
// discriminated by which field is set.
type AuthFlowResult struct {
	Render      []*UINode      `json:"render,omitempty"`
	RedirectURL string         `json:"redirect_url,omitempty"`
	Credentials map[string]any `json:"credentials,omitempty"`
}

// ─────────────────────────────────────────────
// OpenAI-compatible wire errors
// ─────────────────────────────────────────────

// OpenAIError wraps error responses in the OpenAI error format.
type OpenAIError struct {
	Error OpenAIErrorBody `json:"error"`
}

type OpenAIErrorBody struct {
	Message string  `json:"message" example:"Invalid request: missing required field 'model'"`
	Type    string  `json:"type" example:"invalid_request_error"`
	Param   *string `json:"param" example:"model"`
	Code    string  `json:"code,omitempty" example:"invalid_request"`
}

// ─────────────────────────────────────────────
// Metrics
// ─────────────────────────────────────────────

// MetricEvent represents a single API request event.
type MetricEvent struct {
	Timestamp    time.Time
	ProviderID   string
	ProviderType string
	Model        ModelId
	TokenID      string
	Duration     time.Duration
	StatusCode   int
	TokensInput  int64
	TokensOutput int64
	ErrorType    string
}

// MetricsFilters for querying metrics.
type MetricsFilters struct {
	ProviderID string    `json:"provider_id"`
	Model      ModelId   `json:"model"`
	TimeRange  TimeRange `json:"time_range"`
}

// TimeRange represents a time window for metrics queries.
type TimeRange string

const (
	TimeRangeHour      TimeRange = "hour"
	TimeRange1Day      TimeRange = "1d"
	TimeRange7Days     TimeRange = "7d"
	TimeRange28Days    TimeRange = "28d"
	TimeRange90Days    TimeRange = "90d"
	TimeRangeThisMonth TimeRange = "month"
)

// Bounds returns the start and end times for this time range.
func (tr TimeRange) Bounds() (start, end time.Time) {
	now := time.Now()
	switch tr {
	case TimeRangeHour:
		return now.Add(-1 * time.Hour), now
	case TimeRange1Day:
		return now.Add(-24 * time.Hour), now
	case TimeRange7Days:
		return now.Add(-7 * 24 * time.Hour), now
	case TimeRange28Days:
		return now.Add(-28 * 24 * time.Hour), now
	case TimeRange90Days:
		return now.Add(-90 * 24 * time.Hour), now
	case TimeRangeThisMonth:
		y, m, _ := now.Date()
		start := time.Date(y, m, 1, 0, 0, 0, 0, now.Location())
		return start, now
	default:
		return now.Add(-1 * time.Hour), now
	}
}

// MetricsOverview for dashboard display.
type MetricsOverview struct {
	TotalRequests    int64 `json:"total_requests" example:"1500"`
	TotalErrors      int64 `json:"total_errors" example:"23"`
	PeakRequests     int64 `json:"peak_requests" example:"45"`
	PeakInputTokens  int64 `json:"peak_input_tokens" example:"12000"`
	PeakOutputTokens int64 `json:"peak_output_tokens" example:"3500"`
}

// TimeSeriesPoint for chart data.
type TimeSeriesPoint struct {
	Timestamp time.Time `json:"timestamp" example:"2026-05-04T19:00:00Z"`
	Value     int64     `json:"value" example:"42"`
}

// ─────────────────────────────────────────────
// Proxies (0.7.0)
// ─────────────────────────────────────────────

// ProxyPoolRef is the provider-level proxy selection stored in
// ProviderInstance.Config["proxy"]. Empty pool means direct (no proxy):
// plugins skip proxies.query. "auto" selects the router free pool, any
// other name selects a custom pool by ID or name.
type ProxyPoolRef struct {
	Pool string `json:"pool,omitempty"`
}

// DefaultProxyPool is the pool name for the router free pool.
const DefaultProxyPool = "auto"

// CredentialAutomationOn reports the provider disable_failed_credentials
// switch: the single master for every non-manual credential disable
// (single probes, detached health checks, plugin shared writes). Absent or
// non-boolean reads off, matching the dashboard default.
func CredentialAutomationOn(providerConfig map[string]any) bool {
	enabled, _ := providerConfig["disable_failed_credentials"].(bool)
	return enabled
}

// ParseProxyPoolRef reads the provider-level proxy pool reference from a
// provider config map. Absent or non-map proxy sections mean direct; an
// explicitly unknown shape is an error, never a silent fallback.
func ParseProxyPoolRef(providerConfig map[string]any) (ProxyPoolRef, error) {
	cfg := ProxyPoolRef{}
	raw, ok := providerConfig["proxy"].(map[string]any)
	if !ok {
		return cfg, nil
	}
	if p, ok := raw["pool"].(string); ok {
		cfg.Pool = p
	}
	return cfg, nil
}

// ProxyEntry is one proxy endpoint in a custom pool.
type ProxyEntry struct {
	URL     string `json:"url"`
	Country string `json:"country,omitempty"`
}

// CustomProxyPool is a manually created and populated proxy pool.
type CustomProxyPool struct {
	ID        string       `json:"id"`
	Name      string       `json:"name"`
	Entries   []ProxyEntry `json:"entries"`
	CreatedAt time.Time    `json:"created_at"`
	UpdatedAt time.Time    `json:"updated_at"`
}

// ProxyView is one proxy endpoint exposed to plugins through proxies.query.
type ProxyView struct {
	ID      string `json:"id"`
	URL     string `json:"url"`
	Country string `json:"country,omitempty"`
	Pool    string `json:"pool"`
}

// ProxyRequire is one proxies.require call.
type ProxyRequire struct {
	// Pool is "" (direct), "auto" (free pool) or a custom pool ID/name.
	Pool string
	// Countries is an OR list of ISO codes; empty accepts any country.
	Countries []string
	// Exclude lists proxy URLs that must not be returned.
	Exclude []string
	// Limit is the maximum number of proxies returned (at least 1).
	Limit int
	// Want is the free-pool country stock to maintain; zero uses the pool default.
	Want int
	// Timeout bounds the wait on the free pool.
	Timeout time.Duration
}

// ProxyRequireResult is the outcome of proxies.require. Proxies is empty for
// a direct pool, a timeout, or a custom pool without a matching entry.
type ProxyRequireResult struct {
	Proxies  []ProxyView
	TimedOut bool
	// Direct reports that the provider is configured without a proxy pool.
	Direct bool
}

// RecheckBannedRequest selects banned proxies for manual rechecking.
// Both filters are optional.
type RecheckBannedRequest struct {
	Reason string `json:"reason"`
	Source string `json:"source"`
}

// RecheckBannedResponse reports the number of queued manual rechecks.
type RecheckBannedResponse struct {
	Queued int `json:"queued"`
}

// ─────────────────────────────────────────────
// Agents
// ─────────────────────────────────────────────

// VirtualModel routes requests to its models in list order (fall-through)
// and reports an error only when every model failed.
type VirtualModel struct {
	ID          string              `json:"id"`
	Name        string              `json:"name"`
	Description string              `json:"description"`
	Models      []VirtualModelEntry `json:"models"`
	// Instruction is prepended to requests as the first user message.
	Instruction string `json:"instruction"`
	// ManagedBy marks provider-maintained virtual models
	// ("provider:<provider-id>:<endpoint-slug>"). Managed models resolve
	// their member list live from the provider list on every call; stored
	// members are ignored. Empty means manually maintained.
	ManagedBy string `json:"managed_by,omitempty"`
	// Disabled takes the virtual model out of routing. Probes still run.
	Disabled  bool      `json:"disabled,omitempty"`
	Version   int       `json:"version"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// VirtualModelEntry is one fall-through step. List order is the priority.
type VirtualModelEntry struct {
	ModelID ModelId `json:"model_id"`
}

// ─────────────────────────────────────────────
// API Error Responses
// ─────────────────────────────────────────────

// ErrorResponse is the standard error response format for all API endpoints.
type ErrorResponse struct {
	Error string `json:"error" example:"invalid request"`
}

// ─────────────────────────────────────────────
// RouterConfiguration
// ─────────────────────────────────────────────

// RouterConfiguration holds deployment-wide instance settings.
type RouterConfiguration struct {
	IsClusterNode    bool `json:"is_cluster_node"`
	DisableTelemetry bool `json:"disable_telemetry"`
	// ModelsFilter remembers the dashboard model visibility filter
	// ("all", "enabled", "disabled"). Empty means "all".
	ModelsFilter string `json:"models_filter,omitempty"`
}

// Validate checks the configuration ranges.
func (c RouterConfiguration) Validate() error {
	switch c.ModelsFilter {
	case "", "all", "enabled", "disabled":
	default:
		return fmt.Errorf("models_filter must be one of all, enabled, disabled")
	}
	return nil
}
