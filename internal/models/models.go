// Package models defines all shared data structures for llm-router.
package models

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// CurrentVersion is the router version plugins declare compatibility with
// via the @router_version manifest tag.
const CurrentVersion = "0.0.4"

// ─────────────────────────────────────────────
// ModelId
// ─────────────────────────────────────────────

// ModelId uniquely identifies a model in the format: provider/model-name[:version]
type ModelId string

func (m ModelId) String() string { return string(m) }

// Parse splits a ModelId into providerID and model name.
func (m ModelId) Parse() (providerID, model string, err error) {
	s := string(m)
	idx := strings.Index(s, "/")
	if idx == -1 {
		return "", "", fmt.Errorf("invalid ModelId %q: missing '/' separator (expected provider/model-name)", s)
	}
	return s[:idx], s[idx+1:], nil
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
	Content     []ChatMessageContentPart `json:"-"`
	ContentText string                   `json:"-"`
}

func (p *ChatMessageContentPart) UnmarshalJSON(data []byte) error {
	type rawPart struct {
		Type      string          `json:"type,omitempty"`
		Text      string          `json:"text,omitempty"`
		ToolUseID string          `json:"tool_use_id,omitempty"`
		ID        string          `json:"id,omitempty"`
		Name      string          `json:"name,omitempty"`
		Input     json.RawMessage `json:"input,omitempty"`
		Content   json.RawMessage `json:"content,omitempty"`
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
		Type      string          `json:"type,omitempty"`
		Text      string          `json:"text,omitempty"`
		ToolUseID string          `json:"tool_use_id,omitempty"`
		ID        string          `json:"id,omitempty"`
		Name      string          `json:"name,omitempty"`
		Input     json.RawMessage `json:"input,omitempty"`
		Content   any             `json:"content,omitempty"`
	}
	out := rawPart{
		Type: p.Type, Text: p.Text, ToolUseID: p.ToolUseID,
		ID: p.ID, Name: p.Name, Input: p.Input,
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
	ID       string           `json:"id,omitempty"`
	Type     string           `json:"type,omitempty"`
	Function ChatToolFunction `json:"function"`
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
type ChatMessage struct {
	Role         string                   `json:"role" example:"user" enums:"system,user,assistant,tool,developer"`
	Content      string                   `json:"-" example:"Hello, how are you?"`
	ContentParts []ChatMessageContentPart `json:"-"`
	ToolCalls    []ChatToolCall           `json:"tool_calls,omitempty"`
	ToolCallID   string                   `json:"tool_call_id,omitempty"`
	Name         string                   `json:"name,omitempty"`
	Refusal      *string                  `json:"refusal,omitempty"`
}

func (m *ChatMessage) UnmarshalJSON(data []byte) error {
	type rawMessage struct {
		Role       string          `json:"role"`
		Content    json.RawMessage `json:"content"`
		ToolCalls  []ChatToolCall  `json:"tool_calls,omitempty"`
		ToolCallID string          `json:"tool_call_id,omitempty"`
		Name       string          `json:"name,omitempty"`
		Refusal    *string         `json:"refusal,omitempty"`
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
		Role       string         `json:"role"`
		Content    any            `json:"content"`
		ToolCalls  []ChatToolCall `json:"tool_calls,omitempty"`
		ToolCallID string         `json:"tool_call_id,omitempty"`
		Name       string         `json:"name,omitempty"`
		Refusal    *string        `json:"refusal,omitempty"`
	}
	content := any(m.Content)
	if len(m.ContentParts) > 0 {
		content = m.ContentParts
	}
	return json.Marshal(rawMessage{
		Role: m.Role, Content: content, ToolCalls: m.ToolCalls,
		ToolCallID: m.ToolCallID, Name: m.Name, Refusal: m.Refusal,
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
	Stop                any            `json:"stop,omitempty"`
	Seed                *int64         `json:"seed,omitempty"`
	FrequencyPenalty    *float64       `json:"frequency_penalty,omitempty"`
	PresencePenalty     *float64       `json:"presence_penalty,omitempty"`
	Logprobs            *bool          `json:"logprobs,omitempty"`
	TopLogprobs         *int           `json:"top_logprobs,omitempty"`
	ResponseFormat      any            `json:"response_format,omitempty"`
	User                *string        `json:"user,omitempty"`
	ServiceTier         *string        `json:"service_tier,omitempty"`
	ReasoningEffort     *string        `json:"reasoning_effort,omitempty"`
	Verbosity           *string        `json:"verbosity,omitempty"`
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

// ModelInfo contains metadata about a specific model.
type ModelInfo struct {
	Name          string `json:"name"`
	DisplayName   string `json:"display_name"`
	RPM           int64  `json:"rpm"`
	TPM           int64  `json:"tpm"`
	RPD           int64  `json:"rpd"`
	ContextWindow int64  `json:"context_window,omitempty"`
	MaxTokens     int64  `json:"max_tokens,omitempty"`
}

// ─────────────────────────────────────────────
// Provider errors
// ─────────────────────────────────────────────

// ErrorType classifies provider errors for retry logic.
type ErrorType int

const (
	ErrorTypeUnknown        ErrorType = iota
	ErrorTypeRateLimit                // Temporary rate limit, rotate credential
	ErrorTypeQuotaExceeded            // Credential quota exhausted, deprioritize (MUST have RetryAfter)
	ErrorTypeAuth                     // Auth failure, credential may be invalid
	ErrorTypeUpstream                 // Upstream error, don't retry
	ErrorTypeTimeout                  // Timeout, may retry
	ErrorTypeInvalidRequest           // Invalid request, don't retry
)

// ProviderError represents errors returned by provider backends.
type ProviderError struct {
	StatusCode int
	Message    string
	Type       ErrorType
	RetryAfter *time.Time
}

func (e *ProviderError) Error() string {
	return fmt.Sprintf("provider error (%d): %s", e.StatusCode, e.Message)
}

// IsRetryable reports whether this error triggers credential rotation.
func (e *ProviderError) IsRetryable() bool {
	return e.Type == ErrorTypeRateLimit || e.Type == ErrorTypeQuotaExceeded
}

// Retryable reports the same as IsRetryable to satisfy retry.Classifiable.
func (e *ProviderError) Retryable() bool { return e.IsRetryable() }

// PluginInternalError is a caught Lua failure at the Go/Lua boundary.
// Always retryable: the retry engine moves to the next candidate.
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

// Retryable always returns true for plugin crashes.
func (e *PluginInternalError) Retryable() bool { return true }

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

// AllowsCredential reports whether the token may use the given credential.
func (r TokenRules) AllowsCredential(credentialID string) bool {
	if r.AllowAllCredentials {
		return true
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

// ProviderInstance is the single persisted provider record for every type:
// lua-plugin type keys, built-in "custom" and "agents".
type ProviderInstance struct {
	ID           string         `json:"id" example:"opencode-zen"`
	Name         string         `json:"name" example:"OpenCode Zen"`
	TypeKey      string         `json:"type_key" example:"opencode-zen"`
	Qualifier    string         `json:"qualifier" example:""`
	Config       map[string]any `json:"config"`
	IconURL      string         `json:"icon_url" example:"https://cdn.example.com/openai.svg"`
	IsUIReadonly bool           `json:"is_ui_readonly"`
	IsUIHidden   bool           `json:"is_ui_hidden"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
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

// ProviderStats holds aggregated statistics for a provider.
type ProviderStats struct {
	ModelCount      int   `json:"model_count" example:"5"`
	CredentialCount int   `json:"credential_count" example:"2"`
	RequestsToday   int64 `json:"requests_today" example:"1234"`
}

// ProviderInstanceCreateRequest is the generic create body for any provider type.
type ProviderInstanceCreateRequest struct {
	Name      string         `json:"name"`
	TypeKey   string         `json:"type_key"`
	Qualifier string         `json:"qualifier"`
	Config    map[string]any `json:"config"`
	IconURL   string         `json:"icon_url"`
}

// ProviderInstanceUpdateRequest is the generic update body.
type ProviderInstanceUpdateRequest struct {
	Name    string         `json:"name"`
	Config  map[string]any `json:"config"`
	IconURL string         `json:"icon_url"`
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

	QuotaResetAt *time.Time `json:"quota_reset_at,omitempty"`
}

// IsExpired reports whether the credential has passed its expiry time.
func (c *Credential) IsExpired() bool {
	if c.ExpiresAt == nil {
		return false
	}
	return time.Now().After(*c.ExpiresAt)
}

// ExpiresIn returns the duration until expiry (negative if already expired).
func (c *Credential) ExpiresIn() time.Duration {
	if c.ExpiresAt == nil {
		return 0
	}
	return time.Until(*c.ExpiresAt)
}

// IsQuotaExceeded reports whether the credential's quota is currently exceeded.
func (c *Credential) IsQuotaExceeded() bool {
	if c.QuotaResetAt == nil {
		return false
	}
	return time.Now().Before(*c.QuotaResetAt)
}

// Priority returns the selection priority for this credential.
func (c *Credential) Priority() int {
	if c.IsExpired() {
		return 3
	}
	if c.IsQuotaExceeded() {
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

// MarkQuotaExceeded marks this credential as quota-exceeded until resetAt.
func (c *Credential) MarkQuotaExceeded(resetAt time.Time) {
	c.QuotaResetAt = &resetAt
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
	Gap          string            `json:"gap,omitempty"`
	Columns      int               `json:"columns,omitempty"`
	Title        string            `json:"title,omitempty"`
	Subtitle     string            `json:"subtitle,omitempty"`
	Wrap         bool              `json:"wrap,omitempty"`
	Grow         bool              `json:"grow,omitempty"`
	Size         string            `json:"size,omitempty"`
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
// Agents
// ─────────────────────────────────────────────

// Agent is a virtual provider that orchestrates requests across multiple real providers.
type Agent struct {
	ID            string               `json:"id"`
	Name          string               `json:"name"`
	Description   string               `json:"description"`
	Models        []AgentModel         `json:"models"`
	Instructions  AgentInstructions    `json:"instructions"`
	DecisionModel *DecisionModelConfig `json:"decision_model,omitempty"`
	MaxTokens     int                  `json:"max_tokens"`
	Version       int                  `json:"version"`
	IsDraft       bool                 `json:"is_draft"`
	CreatedAt     time.Time            `json:"created_at"`
	UpdatedAt     time.Time            `json:"updated_at"`
}

// AgentModel represents a model that an agent can use.
type AgentModel struct {
	ModelID      ModelId `json:"model_id"`
	Priority     int     `json:"priority"`
	Description  string  `json:"description"`
	Instructions string  `json:"instructions"`
}

// AgentInstructions defines how instructions are injected into requests.
type AgentInstructions struct {
	Content   string            `json:"content"`
	Injection InjectionStrategy `json:"injection"`
}

// InjectionStrategy defines where instructions are injected in the message list.
type InjectionStrategy string

const (
	InjectionBeginning InjectionStrategy = "beginning"
	InjectionEnd       InjectionStrategy = "end"
)

// DecisionModelConfig configures the optional decision model for intelligent routing.
type DecisionModelConfig struct {
	ModelID      ModelId `json:"model_id"`
	SystemPrompt string  `json:"system_prompt"`
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
	MaxRetries       int  `json:"max_retries"`
}

// Validate checks MaxRetries is in range 0-20.
func (c RouterConfiguration) Validate() error {
	if c.MaxRetries < 0 || c.MaxRetries > 20 {
		return fmt.Errorf("max_retries must be between 0 and 20")
	}
	return nil
}
