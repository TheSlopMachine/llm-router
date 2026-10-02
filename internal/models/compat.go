// Package models compatibility additions: OpenAI prior-generation and
// Anthropic-compatible wire types plus the router-side async stores backing
// responses, conversations, assistants, threads, runs and message batches.
//
// Every record minted here carries its ID prefix (resp_, asst_, thread_,
// msg_, run_, batch_, conv_) so logs and clients distinguish kinds without
// a second lookup.
package models

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ─────────────────────────────────────────────
// Shared ID minting
// ─────────────────────────────────────────────

const compatIDAlphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

func mintCompatID(prefix string, randLen int) string {
	suffix := make([]byte, randLen)
	if _, err := rand.Read(suffix); err != nil {
		for i := range suffix {
			suffix[i] = compatIDAlphabet[int(time.Now().UnixNano()+int64(i))%len(compatIDAlphabet)]
		}
		return fmt.Sprintf("%s%s", prefix, string(suffix))
	}
	for i := range suffix {
		suffix[i] = compatIDAlphabet[int(suffix[i])%len(compatIDAlphabet)]
	}
	return fmt.Sprintf("%s%s", prefix, string(suffix))
}

// NewResponseID mints a resp_* identifier for response and conversation items.
func NewResponseID() string { return mintCompatID("resp_", 24) }

// NewAssistantID mints an asst_* identifier.
func NewAssistantID() string { return mintCompatID("asst_", 24) }

// NewThreadID mints a thread_* identifier.
func NewThreadID() string { return mintCompatID("thread_", 24) }

// NewThreadMessageID mints a msg_* identifier.
func NewThreadMessageID() string { return mintCompatID("msg_", 24) }

// NewRunID mints a run_* identifier.
func NewRunID() string { return mintCompatID("run_", 24) }

// NewBatchID mints a batch_* identifier for Anthropic message batches.
func NewBatchID() string { return mintCompatID("batch_", 24) }

// NewConversationID mints a conv_* identifier.
func NewConversationID() string { return mintCompatID("conv_", 24) }

// ─────────────────────────────────────────────
// Legacy completions (POST /v1/completions)
// ─────────────────────────────────────────────

// CompletionRequest is the incoming POST /v1/completions body. Prompt is
// polymorphic (string, list of strings, token arrays are refused as in
// embeddings). Alias fields translate derivative clients: System (Ollama
// `system`), Suffix doubling as Mistral FIM suffix, Images/Raw/Think/Options
// (Ollama generate fields), RandomSeed (Mistral `random_seed`), Format
// (Ollama `format` alias for logit_bias-free response shaping, accepted).
// ToChat maps the request onto the chat pipeline.
type CompletionRequest struct {
	Model          ModelId         `json:"model"`
	Prompt         []string        `json:"-"`
	RawPrompt      json.RawMessage `json:"-"`
	Suffix         string          `json:"suffix,omitempty"`
	MaxTokens      int             `json:"max_tokens,omitempty"`
	Temperature    float64         `json:"temperature,omitempty"`
	TopP           float64         `json:"top_p,omitempty"`
	N              *int            `json:"n,omitempty"`
	BestOf         *int            `json:"best_of,omitempty"`
	Echo           *bool           `json:"echo,omitempty"`
	Logprobs       *int            `json:"logprobs,omitempty"`
	Stop           any             `json:"stop,omitempty"`
	Stream         bool            `json:"stream,omitempty"`
	Seed           *int64          `json:"seed,omitempty"`
	RandomSeed     *int64          `json:"random_seed,omitempty"`
	User           *string         `json:"user,omitempty"`
	System         string          `json:"system,omitempty"`
	Images         []string        `json:"images,omitempty"`
	Raw            *bool           `json:"raw,omitempty"`
	Think          any             `json:"think,omitempty"`
	Options        map[string]any  `json:"options,omitempty"`
	Format         any             `json:"format,omitempty"`
	PromptCacheKey any             `json:"prompt_cache_key,omitempty"`
}

// UnmarshalJSON accepts prompt as a string or a list of strings. Token-ID
// arrays are refused: the router has no tokenizer to decode them.
func (r *CompletionRequest) UnmarshalJSON(raw []byte) error {
	type plain CompletionRequest
	var p plain
	if err := json.Unmarshal(raw, &p); err != nil {
		return err
	}
	*r = CompletionRequest(p)
	var probe struct {
		Prompt json.RawMessage `json:"prompt"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return err
	}
	if len(probe.Prompt) == 0 || strings.TrimSpace(string(probe.Prompt)) == "null" {
		return fmt.Errorf("missing required field 'prompt'")
	}
	var single string
	if err := json.Unmarshal(probe.Prompt, &single); err == nil {
		r.Prompt = []string{single}
		r.RawPrompt = probe.Prompt
		r.NormalizeAliases()
		return nil
	}
	var list []string
	if err := json.Unmarshal(probe.Prompt, &list); err == nil {
		r.Prompt = list
		r.RawPrompt = probe.Prompt
		r.NormalizeAliases()
		return nil
	}
	return fmt.Errorf("prompt must be a string or an array of strings")
}

// NormalizeAliases folds derivative-client aliases into canonical fields.
func (r *CompletionRequest) NormalizeAliases() {
	if r.Seed == nil && r.RandomSeed != nil {
		r.Seed = r.RandomSeed
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

// ToChat maps the completion onto a single-turn chat request: optional
// system message, user turn of prompt plus suffix. Images attach as image
// parts when present.
func (r *CompletionRequest) ToChat() *ChatCompletionRequest {
	out := &ChatCompletionRequest{
		Model:       r.Model,
		Stream:      r.Stream,
		Temperature: r.Temperature,
		TopP:        r.TopP,
		Seed:        r.Seed,
		Stop:        r.Stop,
		User:        r.User,
	}
	if r.MaxTokens > 0 {
		out.MaxTokens = r.MaxTokens
	}
	if r.System != "" {
		out.Messages = append(out.Messages, ChatMessage{Role: "system", Content: r.System})
	}
	text := strings.Join(r.Prompt, "\n")
	if r.Suffix != "" {
		text += r.Suffix
	}
	msg := ChatMessage{Role: "user"}
	if len(r.Images) > 0 {
		if text != "" {
			msg.ContentParts = append(msg.ContentParts, ChatMessageContentPart{Type: "text", Text: text})
		}
		for _, img := range r.Images {
			msg.ContentParts = append(msg.ContentParts, ChatMessageContentPart{
				Type:     "image_url",
				ImageURL: &ChatMessageImageURL{URL: img},
			})
		}
		msg.Content = text
	} else {
		msg.Content = text
	}
	out.Messages = append(out.Messages, msg)
	return out
}

// CompletionResponse is the text_completion wire response.
type CompletionResponse struct {
	ID      string              `json:"id"`
	Object  string              `json:"object"`
	Created int64               `json:"created"`
	Model   string              `json:"model"`
	Choices []CompletionChoice  `json:"choices"`
	Usage   ChatCompletionUsage `json:"usage"`
}

// CompletionChoice is one text completion choice.
type CompletionChoice struct {
	Index        int    `json:"index"`
	Text         string `json:"text"`
	FinishReason string `json:"finish_reason"`
	Logprobs     any    `json:"logprobs,omitempty"`
}

// ─────────────────────────────────────────────
// Moderations (POST /v1/moderations)
// ─────────────────────────────────────────────

// ModerationRequest is the incoming POST /v1/moderations body. Input is
// normalized at the edge: a string, a list of strings, or a list of
// message-like parts (Mistral chat/moderations shape) joined to text.
type ModerationRequest struct {
	Model ModelId  `json:"model"`
	Input []string `json:"-"`
}

// UnmarshalJSON accepts input as string, string array, or parts array with
// text fields.
func (r *ModerationRequest) UnmarshalJSON(raw []byte) error {
	var probe struct {
		Model ModelId         `json:"model"`
		Input json.RawMessage `json:"input"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return err
	}
	r.Model = probe.Model
	if len(probe.Input) == 0 || strings.TrimSpace(string(probe.Input)) == "null" {
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
	var parts []struct {
		Text    string `json:"text"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(probe.Input, &parts); err == nil {
		for _, p := range parts {
			t := p.Text
			if t == "" {
				t = p.Content
			}
			r.Input = append(r.Input, t)
		}
		return nil
	}
	return fmt.Errorf("input must be a string, an array of strings, or an array of text parts")
}

// ModerationResult is one per-input verdict.
type ModerationResult struct {
	Flagged        bool               `json:"flagged"`
	Categories     map[string]bool    `json:"categories"`
	CategoryScores map[string]float64 `json:"category_scores"`
}

// ModerationResponse is the moderation wire response.
type ModerationResponse struct {
	ID      string             `json:"id"`
	Model   string             `json:"model"`
	Results []ModerationResult `json:"results"`
}

// NewModerationID mints a modr_* identifier.
func NewModerationID() string { return mintCompatID("modr_", 24) }

// ─────────────────────────────────────────────
// Responses + conversations (POST /v1/responses, /v1/conversations)
// ─────────────────────────────────────────────

// ResponseInputItem is one free-form input item (message, function output,
// file reference). Content stays opaque JSON: the router stores it and
// renders message texts into the chat turn.
type ResponseInputItem struct {
	Type    string          `json:"type,omitempty"`
	Role    string          `json:"role,omitempty"`
	Content json.RawMessage `json:"content,omitempty"`
	Name    string          `json:"name,omitempty"`
	CallID  string          `json:"call_id,omitempty"`
}

// ResponseOutputItem is one produced output block.
type ResponseOutputItem struct {
	Type   string          `json:"type"`
	Text   string          `json:"text,omitempty"`
	Name   string          `json:"name,omitempty"`
	ID     string          `json:"id,omitempty"`
	Status string          `json:"status,omitempty"`
	Extra  json.RawMessage `json:"extra,omitempty"`
}

// ResponseUsage mirrors chat usage for response objects.
type ResponseUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

// ResponseRequest is the incoming POST /v1/responses body. Input accepts a
// plain string or response input items; tools accept web_search, file_search,
// function and mcp definitions (stored, function tools execute through the
// chat pipeline).
type ResponseRequest struct {
	Model              ModelId           `json:"model"`
	Input              json.RawMessage   `json:"input,omitempty"`
	Instructions       string            `json:"instructions,omitempty"`
	Tools              []json.RawMessage `json:"tools,omitempty"`
	ToolChoice         any               `json:"tool_choice,omitempty"`
	Reasoning          *ReasoningConfig  `json:"reasoning,omitempty"`
	ReasoningEffort    *string           `json:"reasoning_effort,omitempty"`
	Text               json.RawMessage   `json:"text,omitempty"`
	Stream             bool              `json:"stream,omitempty"`
	MaxOutputTokens    *int              `json:"max_output_tokens,omitempty"`
	Store              *bool             `json:"store,omitempty"`
	Background         *bool             `json:"background,omitempty"`
	PreviousResponseID string            `json:"previous_response_id,omitempty"`
	ConversationID     string            `json:"conversation,omitempty"`
	Metadata           map[string]string `json:"metadata,omitempty"`
	User               *string           `json:"user,omitempty"`
}

// InputTexts renders the request input as chat text turns.
func (r *ResponseRequest) InputTexts() []string {
	trimmed := strings.TrimSpace(string(r.Input))
	if trimmed == "" || trimmed == "null" {
		return nil
	}
	var s string
	if err := json.Unmarshal(r.Input, &s); err == nil {
		if strings.TrimSpace(s) == "" {
			return nil
		}
		return []string{s}
	}
	var items []ResponseInputItem
	if err := json.Unmarshal(r.Input, &items); err != nil {
		return nil
	}
	var out []string
	for _, it := range items {
		t := strings.TrimSpace(string(it.Content))
		if t == "" || t == "null" {
			continue
		}
		var cs string
		if err := json.Unmarshal(it.Content, &cs); err == nil {
			if strings.TrimSpace(cs) != "" {
				out = append(out, cs)
			}
			continue
		}
		var blocks []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if err := json.Unmarshal(it.Content, &blocks); err == nil {
			for _, b := range blocks {
				if strings.TrimSpace(b.Text) != "" {
					out = append(out, b.Text)
				}
			}
		}
	}
	return out
}

// ToChat maps the response request onto the chat pipeline: instructions
// become the system turn, input texts become the user turn, function tools
// become chat tools.
func (r *ResponseRequest) ToChat() *ChatCompletionRequest {
	out := &ChatCompletionRequest{
		Model:  r.Model,
		Stream: r.Stream,
		User:   r.User,
	}
	if r.ReasoningEffort != nil {
		out.ReasoningEffort = r.ReasoningEffort
	} else if r.Reasoning != nil && r.Reasoning.Effort != "" {
		e := r.Reasoning.Effort
		out.ReasoningEffort = &e
	}
	if r.MaxOutputTokens != nil {
		out.MaxTokens = *r.MaxOutputTokens
	}
	if strings.TrimSpace(r.Instructions) != "" {
		out.Messages = append(out.Messages, ChatMessage{Role: "system", Content: r.Instructions})
	}
	for _, t := range r.InputTexts() {
		out.Messages = append(out.Messages, ChatMessage{Role: "user", Content: t})
	}
	for _, raw := range r.Tools {
		var fn struct {
			Type        string         `json:"type"`
			Name        string         `json:"name"`
			Description string         `json:"description,omitempty"`
			Parameters  map[string]any `json:"parameters,omitempty"`
		}
		if err := json.Unmarshal(raw, &fn); err != nil {
			continue
		}
		if fn.Type != "" && fn.Type != "function" {
			continue
		}
		if fn.Name == "" {
			continue
		}
		out.Tools = append(out.Tools, ChatTool{
			Type: "function",
			Function: &ChatToolFunction{
				Name:        fn.Name,
				Description: fn.Description,
				Parameters:  fn.Parameters,
			},
		})
	}
	if r.ToolChoice != nil {
		out.ToolChoice = r.ToolChoice
	}
	return out
}

// ResponseObject is the stored and served response.
type ResponseObject struct {
	ID                 string               `json:"id"`
	Object             string               `json:"object"`
	CreatedAt          int64                `json:"created_at"`
	Model              string               `json:"model"`
	Status             string               `json:"status"`
	Output             []ResponseOutputItem `json:"output"`
	Usage              *ResponseUsage       `json:"usage,omitempty"`
	Instructions       string               `json:"instructions,omitempty"`
	PreviousResponseID string               `json:"previous_response_id,omitempty"`
	ConversationID     string               `json:"conversation,omitempty"`
	Metadata           map[string]string    `json:"metadata,omitempty"`
	Error              *ResponseError       `json:"error,omitempty"`
}

// ResponseError carries a failed response cause.
type ResponseError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ConversationObject is the stored and served conversation.
type ConversationObject struct {
	ID        string            `json:"id"`
	Object    string            `json:"object"`
	CreatedAt int64             `json:"created_at"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

// ─────────────────────────────────────────────
// Assistants, threads, runs (prior-generation shims)
// ─────────────────────────────────────────────

// Assistant is a stored response preset plus chat pipeline defaults.
type Assistant struct {
	ID             string            `json:"id"`
	Object         string            `json:"object"`
	CreatedAt      int64             `json:"created_at"`
	Model          string            `json:"model"`
	Name           string            `json:"name,omitempty"`
	Description    string            `json:"description,omitempty"`
	Instructions   string            `json:"instructions,omitempty"`
	Tools          []json.RawMessage `json:"tools,omitempty"`
	ResponseFormat json.RawMessage   `json:"response_format,omitempty"`
	Metadata       map[string]string `json:"metadata,omitempty"`
}

// AssistantRequest is the create/update body for assistants.
type AssistantRequest struct {
	Model          ModelId           `json:"model,omitempty"`
	Name           string            `json:"name,omitempty"`
	Description    string            `json:"description,omitempty"`
	Instructions   string            `json:"instructions,omitempty"`
	Tools          []json.RawMessage `json:"tools,omitempty"`
	ResponseFormat json.RawMessage   `json:"response_format,omitempty"`
	Metadata       map[string]string `json:"metadata,omitempty"`
}

// Thread is a conversation handle in the prior-generation API.
type Thread struct {
	ID        string            `json:"id"`
	Object    string            `json:"object"`
	CreatedAt int64             `json:"created_at"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

// ThreadMessage is one message inside a thread.
type ThreadMessage struct {
	ID        string            `json:"id"`
	Object    string            `json:"object"`
	CreatedAt int64             `json:"created_at"`
	ThreadID  string            `json:"thread_id"`
	Role      string            `json:"role"`
	Content   []ThreadContent   `json:"content"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

// ThreadContent is one text block of a thread message.
type ThreadContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// ThreadMessageRequest is the create body for thread messages.
type ThreadMessageRequest struct {
	Role     string            `json:"role"`
	Content  json.RawMessage   `json:"content"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// ThreadMessageTexts renders the request content as plain texts.
func (r *ThreadMessageRequest) ThreadMessageTexts() []string {
	trimmed := strings.TrimSpace(string(r.Content))
	if trimmed == "" || trimmed == "null" {
		return nil
	}
	var s string
	if err := json.Unmarshal(r.Content, &s); err == nil {
		if strings.TrimSpace(s) == "" {
			return nil
		}
		return []string{s}
	}
	var blocks []ThreadContent
	if err := json.Unmarshal(r.Content, &blocks); err == nil {
		var out []string
		for _, b := range blocks {
			if strings.TrimSpace(b.Text) != "" {
				out = append(out, b.Text)
			}
		}
		return out
	}
	return nil
}

// Run is one assistant execution inside a thread, mapped onto a resp_*
// response execution.
type Run struct {
	ID             string             `json:"id"`
	Object         string             `json:"object"`
	CreatedAt      int64              `json:"created_at"`
	ThreadID       string             `json:"thread_id"`
	AssistantID    string             `json:"assistant_id"`
	Model          string             `json:"model"`
	Status         string             `json:"status"`
	Instructions   string             `json:"instructions,omitempty"`
	RequiredAction *RunRequiredAction `json:"required_action,omitempty"`
	Metadata       map[string]string  `json:"metadata,omitempty"`
}

// RunRequiredAction carries tool outputs the client must submit.
type RunRequiredAction struct {
	Type              string             `json:"type"`
	SubmitToolOutputs *SubmitToolOutputs `json:"submit_tool_outputs,omitempty"`
}

// SubmitToolOutputs lists tool calls awaiting outputs.
type SubmitToolOutputs struct {
	ToolCalls []RunToolCall `json:"tool_calls"`
}

// RunToolCall is one function call awaiting a client output.
type RunToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// RunRequest is the create body for runs.
type RunRequest struct {
	AssistantID  string            `json:"assistant_id"`
	Model        ModelId           `json:"model,omitempty"`
	Instructions string            `json:"instructions,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
	Stream       bool              `json:"stream,omitempty"`
}

// ToolOutput is one submitted tool result.
type ToolOutput struct {
	ToolCallID string `json:"tool_call_id"`
	Output     string `json:"output"`
}

// ─────────────────────────────────────────────
// Response store envelope (responses bucket)
// ─────────────────────────────────────────────

// ResponseRecordKind discriminates rows in the responses bucket.
type ResponseRecordKind string

const (
	ResponseRecordResponse      ResponseRecordKind = "response"
	ResponseRecordConversation  ResponseRecordKind = "conversation"
	ResponseRecordAssistant     ResponseRecordKind = "assistant"
	ResponseRecordThread        ResponseRecordKind = "thread"
	ResponseRecordThreadMessage ResponseRecordKind = "thread_message"
	ResponseRecordRun           ResponseRecordKind = "run"
)

// ResponseRecord is one row in the responses bucket: every async compat
// object (response, conversation, assistant, thread, message, run) shares
// this envelope. Payload holds the wire object JSON; ParentID links thread
// children and run bindings (thread id); RefID links runs to assistants and
// responses to previous responses. Input holds the opaque input that
// produced the object (response input items, conversation items, pending
// run chat turns) for input_items listings and tool-output continuations.
type ResponseRecord struct {
	ID        string             `json:"id"`
	Kind      ResponseRecordKind `json:"kind"`
	Model     ModelId            `json:"model"`
	Status    string             `json:"status,omitempty"`
	ParentID  string             `json:"parent_id,omitempty"`
	RefID     string             `json:"ref_id,omitempty"`
	Payload   json.RawMessage    `json:"payload,omitempty"`
	Input     json.RawMessage    `json:"input,omitempty"`
	CreatedAt time.Time          `json:"created_at"`
	UpdatedAt time.Time          `json:"updated_at"`
}

// ─────────────────────────────────────────────
// Anthropic-compatible wire types (POST /v1/messages family)
// ─────────────────────────────────────────────

// AnthropicCacheControl marks a block for prompt caching.
type AnthropicCacheControl struct {
	Type string `json:"type,omitempty"`
	TTL  string `json:"ttl,omitempty"`
}

// AnthropicContentSource is an image or document source.
type AnthropicContentSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type,omitempty"`
	Data      string `json:"data,omitempty"`
	URL       string `json:"url,omitempty"`
}

// AnthropicContentBlock is one typed content block in a message, tool
// definition, or tool result. CacheControl is accepted on every block and
// forwarded as a caching hint; document blocks are refused at the edge
// (the chat pipeline has no document part).
type AnthropicContentBlock struct {
	Type         string                  `json:"type"`
	Text         string                  `json:"text,omitempty"`
	Thinking     string                  `json:"thinking,omitempty"`
	Source       *AnthropicContentSource `json:"source,omitempty"`
	ID           string                  `json:"id,omitempty"`
	Name         string                  `json:"name,omitempty"`
	Input        json.RawMessage         `json:"input,omitempty"`
	ToolUseID    string                  `json:"tool_use_id,omitempty"`
	Content      json.RawMessage         `json:"content,omitempty"`
	IsError      bool                    `json:"is_error,omitempty"`
	CacheControl *AnthropicCacheControl  `json:"cache_control,omitempty"`
}

// AnthropicTool is one tool definition.
type AnthropicTool struct {
	Name         string                 `json:"name"`
	Description  string                 `json:"description,omitempty"`
	InputSchema  map[string]any         `json:"input_schema"`
	CacheControl *AnthropicCacheControl `json:"cache_control,omitempty"`
}

// AnthropicToolChoice is the tool_choice selector.
type AnthropicToolChoice struct {
	Type                   string `json:"type"`
	Name                   string `json:"name,omitempty"`
	DisableParallelToolUse bool   `json:"disable_parallel_tool_use,omitempty"`
}

// AnthropicThinkingConfig is the top-level thinking configuration. Enabled
// thinking with a budget maps assistant thinking blocks through
// ReasoningContent; the budget itself is accepted and documented.
type AnthropicThinkingConfig struct {
	Type         string `json:"type"`
	BudgetTokens *int   `json:"budget_tokens,omitempty"`
}

// AnthropicMetadata carries request metadata; UserID maps to chat User.
type AnthropicMetadata struct {
	UserID string `json:"user_id,omitempty"`
}

// AnthropicMessageRequest is the POST /v1/messages body. System accepts a
// string or text blocks; messages accept user/assistant roles with string
// or block content; TopK is accepted (no chat equivalent, documented).
type AnthropicMessageRequest struct {
	Model         string                   `json:"model"`
	MaxTokens     int                      `json:"max_tokens"`
	System        json.RawMessage          `json:"system,omitempty"`
	Messages      []json.RawMessage        `json:"messages"`
	Tools         []AnthropicTool          `json:"tools,omitempty"`
	ToolChoice    *AnthropicToolChoice     `json:"tool_choice,omitempty"`
	Thinking      *AnthropicThinkingConfig `json:"thinking,omitempty"`
	Temperature   *float64                 `json:"temperature,omitempty"`
	TopP          *float64                 `json:"top_p,omitempty"`
	TopK          *int                     `json:"top_k,omitempty"`
	StopSequences []string                 `json:"stop_sequences,omitempty"`
	Stream        bool                     `json:"stream,omitempty"`
	Metadata      *AnthropicMetadata       `json:"metadata,omitempty"`
}

// AnthropicUsage carries token usage for messages.
type AnthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// AnthropicMessage is the non-stream /v1/messages answer.
type AnthropicMessage struct {
	ID           string           `json:"id"`
	Type         string           `json:"type"`
	Role         string           `json:"role"`
	Model        string           `json:"model"`
	Content      []map[string]any `json:"content"`
	StopReason   string           `json:"stop_reason"`
	StopSequence *string          `json:"stop_sequence"`
	Usage        AnthropicUsage   `json:"usage"`
}

// AnthropicTokenCount is the /v1/messages/count_tokens answer.
type AnthropicTokenCount struct {
	InputTokens int `json:"input_tokens"`
}

// AnthropicCompleteRequest is the POST /v1/complete legacy body.
type AnthropicCompleteRequest struct {
	Model             string   `json:"model"`
	Prompt            string   `json:"prompt"`
	MaxTokensToSample int      `json:"max_tokens_to_sample"`
	StopSequences     []string `json:"stop_sequences,omitempty"`
	Stream            bool     `json:"stream,omitempty"`
	Temperature       *float64 `json:"temperature,omitempty"`
	TopP              *float64 `json:"top_p,omitempty"`
	TopK              *int     `json:"top_k,omitempty"`
}

// ToMessageRequest wraps the completion as a single-turn messages request.
func (r *AnthropicCompleteRequest) ToMessageRequest() *AnthropicMessageRequest {
	user, _ := json.Marshal(r.Prompt)
	return &AnthropicMessageRequest{
		Model:     r.Model,
		MaxTokens: r.MaxTokensToSample,
		Messages: []json.RawMessage{
			json.RawMessage(`{"role":"user","content":` + string(user) + `}`),
		},
		StopSequences: r.StopSequences,
		Stream:        r.Stream,
		Temperature:   r.Temperature,
		TopP:          r.TopP,
		TopK:          r.TopK,
	}
}

// AnthropicCompleteResponse is the /v1/complete answer.
type AnthropicCompleteResponse struct {
	ID         string `json:"id"`
	Type       string `json:"type"`
	Completion string `json:"completion"`
	StopReason string `json:"stop_reason"`
	Model      string `json:"model"`
}

// ─────────────────────────────────────────────
// Anthropic message batches
// ─────────────────────────────────────────────

// BatchEntry is one request inside a batch create.
type BatchEntry struct {
	CustomID string          `json:"custom_id"`
	Params   json.RawMessage `json:"params"`
}

// BatchCreateRequest is the POST /v1/messages/batches body.
type BatchCreateRequest struct {
	Requests []BatchEntry `json:"requests"`
}

// BatchRequestCounts tracks batch request states.
type BatchRequestCounts struct {
	Processing int `json:"processing"`
	Succeeded  int `json:"succeeded"`
	Errored    int `json:"errored"`
	Canceled   int `json:"canceled"`
	Expired    int `json:"expired"`
}

// BatchInfo is the served batch object.
type BatchInfo struct {
	ID               string             `json:"id"`
	Type             string             `json:"type"`
	ProcessingStatus string             `json:"processing_status"`
	RequestCounts    BatchRequestCounts `json:"request_counts"`
	CreatedAt        string             `json:"created_at"`
	ExpiresAt        string             `json:"expires_at,omitempty"`
	EndedAt          *string            `json:"ended_at,omitempty"`
	ResultsURL       string             `json:"results_url,omitempty"`
}

// BatchResultLine is one JSONL result line.
type BatchResultLine struct {
	CustomID string          `json:"custom_id"`
	Result   json.RawMessage `json:"result"`
}

// BatchRecord persists one batch: the entries plus per-entry results.
type BatchRecord struct {
	ID        string            `json:"id"`
	Model     ModelId           `json:"model"`
	Status    string            `json:"status"`
	Entries   []BatchEntry      `json:"entries,omitempty"`
	Results   []BatchResultLine `json:"results,omitempty"`
	CreatedAt time.Time         `json:"created_at"`
	UpdatedAt time.Time         `json:"updated_at"`
	EndedAt   *time.Time        `json:"ended_at,omitempty"`
}

// BatchResults renders stored results as JSONL lines.
func (b *BatchRecord) BatchResults() string {
	var sb strings.Builder
	for _, r := range b.Results {
		raw, err := json.Marshal(r)
		if err != nil {
			continue
		}
		sb.Write(raw)
		sb.WriteByte('\n')
	}
	return sb.String()
}

// ToBatchInfo renders the wire batch object. ResultsURL points at the
// router-local results endpoint when results exist.
func (b *BatchRecord) ToBatchInfo(resultsURL string) *BatchInfo {
	counts := BatchRequestCounts{}
	for _, r := range b.Results {
		var probe struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(r.Result, &probe); err != nil {
			counts.Errored++
			continue
		}
		switch probe.Type {
		case "succeeded":
			counts.Succeeded++
		case "errored":
			counts.Errored++
		case "canceled":
			counts.Canceled++
		case "expired":
			counts.Expired++
		default:
			counts.Errored++
		}
	}
	total := len(b.Entries)
	done := counts.Succeeded + counts.Errored + counts.Canceled + counts.Expired
	counts.Processing = total - done
	if counts.Processing < 0 {
		counts.Processing = 0
	}
	out := &BatchInfo{
		ID:               b.ID,
		Type:             "message_batch",
		ProcessingStatus: b.Status,
		RequestCounts:    counts,
		CreatedAt:        b.CreatedAt.UTC().Format(time.RFC3339),
		ExpiresAt:        b.CreatedAt.Add(24 * time.Hour).UTC().Format(time.RFC3339),
	}
	if b.EndedAt != nil {
		s := b.EndedAt.UTC().Format(time.RFC3339)
		out.EndedAt = &s
	}
	if len(b.Results) > 0 {
		out.ResultsURL = resultsURL
	}
	return out
}

// ─────────────────────────────────────────────
// Moderation store types are wire-only (no persistence).
// ─────────────────────────────────────────────
