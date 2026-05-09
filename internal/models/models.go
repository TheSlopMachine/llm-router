// Package models defines all shared data structures for llm-router.
package models

import (
	"time"

	sdk "github.com/TheSlopMachine/llm-router-sdk"
)

// Re-export SDK types for internal use
type AuthType = sdk.AuthType
type ModelId = sdk.ModelId
type ChatMessage = sdk.ChatMessage
type ChatCompletionRequest = sdk.ChatCompletionRequest
type ChatCompletionResponse = sdk.ChatCompletionResponse
type ChatCompletionChoice = sdk.ChatCompletionChoice
type ChatCompletionUsage = sdk.ChatCompletionUsage
type StreamChunk = sdk.StreamChunk
type StreamChunkChoice = sdk.StreamChunkChoice
type ModelInfo = sdk.ModelInfo

const (
	AuthTypeAPIKey = sdk.AuthTypeAPIKey
	AuthTypeOAuth2 = sdk.AuthTypeOAuth2
	AuthTypeBasic  = sdk.AuthTypeBasic
)

// ─────────────────────────────────────────────
// Admin
// ─────────────────────────────────────────────

// AdminUser is the dashboard operator account.
type AdminUser struct {
	Username     string    `json:"username"`
	PasswordHash string    `json:"password_hash"` // bcrypt
	CreatedAt    time.Time `json:"created_at"`
}

// ─────────────────────────────────────────────
// Router Tokens  (our own tokens for /v1 API)
// ─────────────────────────────────────────────

// RouterToken is an opaque bearer token issued by llm-router itself.
// Clients present this in Authorization: Bearer <token> when calling /v1/*.
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
	// AllowedModels is the list of ModelIds this token may request.
	// An empty slice means ALL models are permitted.
	AllowedModels []ModelId `json:"allowed_models"`
}

func (r TokenRules) Allows(model ModelId) bool {
	if len(r.AllowedModels) == 0 {
		return true
	}
	for _, m := range r.AllowedModels {
		if m == model {
			return true
		}
	}
	return false
}

// ─────────────────────────────────────────────
// Providers
// ─────────────────────────────────────────────

// Provider is a registered upstream LLM backend.
type Provider struct {
	ID        string   `json:"id" example:"openai"`                    // Composite ID: "openai" or "openai:azure"
	Name      string   `json:"name" example:"OpenAI"`                  // Display name
	Type      string   `json:"type" example:"openai"`                  // Adapter type key
	Qualifier string   `json:"qualifier" example:"azure"`              // Optional qualifier (empty for default)
	BaseURL   string   `json:"base_url" example:"https://api.openai.com"`
	IconURL   string   `json:"icon_url" example:"https://cdn.example.com/openai.svg"` // Icon URL
	AuthType  AuthType `json:"auth_type" example:"api_key"`
}

// ProviderStats holds aggregated statistics for a provider.
type ProviderStats struct {
	ModelCount      int   `json:"model_count" example:"5"`
	CredentialCount int   `json:"credential_count" example:"2"`
	RequestsToday   int64 `json:"requests_today" example:"1234"`
}

// ─────────────────────────────────────────────
// Credentials  (provider-specific auth data)
// ─────────────────────────────────────────────

// Credential holds provider-specific authentication data.
// Data is intentionally a flexible map to support any auth scheme.
type Credential struct {
	ID         string            `json:"id"`          // UUID
	ProviderID string            `json:"provider_id"` // references Provider.ID
	Label      string            `json:"label"`       // human label
	Data       map[string]string `json:"data"`        // e.g. {"api_key": "sk-…"} or {"access_token": "…", "refresh_token": "…"}
	ExpiresAt  *time.Time        `json:"expires_at,omitempty"`
	CreatedAt  time.Time         `json:"created_at"`
	UpdatedAt  time.Time         `json:"updated_at"`
	
	// Usage tracking for LRU selection
	LastUsedAt   *time.Time `json:"last_used_at,omitempty"`
	RequestCount int64      `json:"request_count"`
	SuccessCount int64      `json:"success_count"`
	FailureCount int64      `json:"failure_count"`
	
	// Quota management
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
// Lower values = higher priority.
// 0 = never used (highest priority)
// 1 = normal (used, not quota-exceeded)
// 2 = quota exceeded (lowest priority, may recover)
// 3 = expired (never use)
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

// ─────────────────────────────────────────────
// OpenAI-compatible wire types
// ─────────────────────────────────────────────

// OpenAIError wraps error responses in the OpenAI error format.
type OpenAIError struct {
	Error OpenAIErrorBody `json:"error"`
}

type OpenAIErrorBody struct {
	Message string `json:"message" example:"Invalid request: missing required field 'model'"`
	Type    string `json:"type" example:"invalid_request_error"`
	Code    string `json:"code,omitempty" example:"invalid_request"`
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
	ErrorType    string // empty if no error, otherwise: "auth_error", "timeout", "rate_limit", "upstream_error", etc.
}

// MetricsFilters for querying metrics.
type MetricsFilters struct {
	ProviderID string    `json:"provider_id"` // empty = all providers
	Model      ModelId   `json:"model"`       // empty = all models
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
	TotalRequests  int64 `json:"total_requests" example:"1500"`
	TotalErrors    int64 `json:"total_errors" example:"23"`
	PeakRPM        int64 `json:"peak_rpm" example:"45"`
	PeakTPMInput   int64 `json:"peak_tpm_input" example:"12000"`
	PeakTPMOutput  int64 `json:"peak_tpm_output" example:"3500"`
	PeakRPD        int64 `json:"peak_rpd" example:"35000"`
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


// ToSDK converts internal Credential to SDK Credential
func (c *Credential) ToSDK() *sdk.Credential {
	return &sdk.Credential{
		ID:   c.ID,
		Data: c.Data,
	}
}
