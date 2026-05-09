// Package testutil provides shared test utilities for llm-router tests.
package testutil

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/models"
)

// ProviderInfo is a minimal struct to avoid import cycle with provider package
type ProviderInfo struct {
	Name      string
	Qualifier string
	BaseURL   string
	IconURL   string
}

// SetupTestDB creates a temporary bbolt database for testing.
// The database is automatically cleaned up when the test completes.
func SetupTestDB(t *testing.T) *db.DB {
	t.Helper()

	f, err := os.CreateTemp("", "llm-router-test-*.db")
	if err != nil {
		t.Fatalf("create temp db file: %v", err)
	}
	path := f.Name()
	f.Close()

	database, err := db.Open(path)
	if err != nil {
		os.Remove(path)
		t.Fatalf("open test db: %v", err)
	}

	t.Cleanup(func() {
		database.Close()
		os.Remove(path)
	})

	return database
}

// MockAdapter is a simple, configurable test adapter.
type MockAdapter struct {
	typeKey            string
	authType           models.AuthType
	completeFunc       func(context.Context, *models.Credential, *models.ChatCompletionRequest) (*models.ChatCompletionResponse, error)
	completeStreamFunc func(context.Context, *models.Credential, *models.ChatCompletionRequest, io.Writer) error
	validateFunc       func(map[string]string) error
	needsRefreshFunc   func(*models.Credential) bool
	refreshFunc        func(context.Context, *models.Credential) (*models.Credential, error)
	modelInfosFunc     func(context.Context, *models.Credential, string) ([]models.ModelInfo, error)
}

// NewMockAdapter creates a mock adapter with sensible defaults.
func NewMockAdapter(typeKey string) *MockAdapter {
	return &MockAdapter{
		typeKey:  typeKey,
		authType: models.AuthTypeAPIKey,
		completeFunc: func(ctx context.Context, cred *models.Credential, req *models.ChatCompletionRequest) (*models.ChatCompletionResponse, error) {
			return &models.ChatCompletionResponse{
				ID:      "mock-" + fmt.Sprintf("%d", time.Now().Unix()),
				Object:  "chat.completion",
				Created: time.Now().Unix(),
				Model:   string(req.Model),
				Choices: []models.ChatCompletionChoice{
					{
						Index: 0,
						Message: models.ChatMessage{
							Role:    "assistant",
							Content: "mock response",
						},
						FinishReason: "stop",
					},
				},
				Usage: models.ChatCompletionUsage{
					PromptTokens:     10,
					CompletionTokens: 5,
					TotalTokens:      15,
				},
			}, nil
		},
		completeStreamFunc: func(ctx context.Context, cred *models.Credential, req *models.ChatCompletionRequest, w io.Writer) error {
			chunk := models.StreamChunk{
				ID:      "mock-stream",
				Object:  "chat.completion.chunk",
				Created: time.Now().Unix(),
				Model:   string(req.Model),
				Choices: []models.StreamChunkChoice{
					{
						Index: 0,
						Delta: models.ChatMessage{
							Role:    "assistant",
							Content: "mock",
						},
					},
				},
			}
			data, _ := json.Marshal(chunk)
			fmt.Fprintf(w, "data: %s\n\n", data)
			return nil
		},
		validateFunc: func(data map[string]string) error {
			if data["api_key"] == "" {
				return fmt.Errorf("api_key required")
			}
			return nil
		},
		needsRefreshFunc: func(cred *models.Credential) bool {
			return false
		},
		refreshFunc: func(ctx context.Context, cred *models.Credential) (*models.Credential, error) {
			return nil, fmt.Errorf("no refresh needed")
		},
		modelInfosFunc: func(ctx context.Context, cred *models.Credential, qualifier string) ([]models.ModelInfo, error) {
			return []models.ModelInfo{
				{
					Name:          "mock-model",
					DisplayName:   "Mock Model",
					ContextWindow: 4096,
					MaxTokens:     2048,
				},
			}, nil
		},
	}
}

func (m *MockAdapter) TypeKey() string                     { return m.typeKey }
func (m *MockAdapter) AuthType() models.AuthType           { return m.authType }
func (m *MockAdapter) ValidateCredentials(data map[string]string) error { return m.validateFunc(data) }
func (m *MockAdapter) Complete(ctx context.Context, cred *models.Credential, req *models.ChatCompletionRequest) (*models.ChatCompletionResponse, error) {
	return m.completeFunc(ctx, cred, req)
}
func (m *MockAdapter) CompleteStream(ctx context.Context, cred *models.Credential, req *models.ChatCompletionRequest, w io.Writer) error {
	return m.completeStreamFunc(ctx, cred, req, w)
}
func (m *MockAdapter) NeedsRefresh(cred *models.Credential) bool { return m.needsRefreshFunc(cred) }
func (m *MockAdapter) RefreshCredential(ctx context.Context, cred *models.Credential) (*models.Credential, error) {
	return m.refreshFunc(ctx, cred)
}
func (m *MockAdapter) GetModelInfos(ctx context.Context, cred *models.Credential, qualifier string) ([]models.ModelInfo, error) {
	return m.modelInfosFunc(ctx, cred, qualifier)
}
func (m *MockAdapter) GetAuthFlow() interface{} { return nil }
func (m *MockAdapter) GetDefaultProviders() []ProviderInfo {
	return []ProviderInfo{
		{
			Name:      "Mock Provider",
			Qualifier: "",
			BaseURL:   "",
			IconURL:   "",
		},
	}
}

// WithCompleteFunc configures the Complete behavior.
func (m *MockAdapter) WithCompleteFunc(f func(context.Context, *models.Credential, *models.ChatCompletionRequest) (*models.ChatCompletionResponse, error)) *MockAdapter {
	m.completeFunc = f
	return m
}

// WithCompleteStreamFunc configures the CompleteStream behavior.
func (m *MockAdapter) WithCompleteStreamFunc(f func(context.Context, *models.Credential, *models.ChatCompletionRequest, io.Writer) error) *MockAdapter {
	m.completeStreamFunc = f
	return m
}

// WithValidateFunc configures the ValidateCredentials behavior.
func (m *MockAdapter) WithValidateFunc(f func(map[string]string) error) *MockAdapter {
	m.validateFunc = f
	return m
}

// WithModelInfos configures the GetModelInfos behavior.
func (m *MockAdapter) WithModelInfos(infos []models.ModelInfo) *MockAdapter {
	m.modelInfosFunc = func(ctx context.Context, cred *models.Credential, qualifier string) ([]models.ModelInfo, error) {
		return infos, nil
	}
	return m
}

// BuildCredential creates a test credential with defaults.
func BuildCredential(providerID string, opts ...func(*models.Credential)) *models.Credential {
	cred := &models.Credential{
		ID:         fmt.Sprintf("cred-%d", time.Now().UnixNano()),
		ProviderID: providerID,
		Data: map[string]string{
			"api_key": "test-key",
		},
		LastUsedAt: nil,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
	for _, opt := range opts {
		opt(cred)
	}
	return cred
}

// WithCredentialData sets credential data.
func WithCredentialData(data map[string]string) func(*models.Credential) {
	return func(c *models.Credential) {
		c.Data = data
	}
}

// WithCredentialExpiry sets credential expiry time.
func WithCredentialExpiry(t time.Time) func(*models.Credential) {
	return func(c *models.Credential) {
		c.ExpiresAt = &t
	}
}

// WithCredentialQuota sets credential quota exceeded time.
func WithCredentialQuota(resetAt time.Time) func(*models.Credential) {
	return func(c *models.Credential) {
		c.QuotaResetAt = &resetAt
	}
}

// WithCredentialLastUsed sets last used time.
func WithCredentialLastUsed(t time.Time) func(*models.Credential) {
	return func(c *models.Credential) {
		c.LastUsedAt = &t
	}
}

// BuildToken creates a test token with defaults.
func BuildToken(opts ...func(*models.RouterToken)) *models.RouterToken {
	tok := &models.RouterToken{
		ID:        fmt.Sprintf("tok-%d", time.Now().UnixNano()),
		Name:      "test-token",
		TokenHash: "",
		Rules:     models.TokenRules{AllowedModels: []models.ModelId{}},
		CreatedAt: time.Now(),
	}
	for _, opt := range opts {
		opt(tok)
	}
	return tok
}

// WithTokenRules sets token rules.
func WithTokenRules(rules models.TokenRules) func(*models.RouterToken) {
	return func(t *models.RouterToken) {
		t.Rules = rules
	}
}

// WithTokenName sets token name.
func WithTokenName(name string) func(*models.RouterToken) {
	return func(t *models.RouterToken) {
		t.Name = name
	}
}

// WithTokenHash sets token hash.
func WithTokenHash(hash string) func(*models.RouterToken) {
	return func(t *models.RouterToken) {
		t.TokenHash = hash
	}
}

// BuildRequest creates a test chat completion request.
func BuildRequest(model models.ModelId, opts ...func(*models.ChatCompletionRequest)) *models.ChatCompletionRequest {
	req := &models.ChatCompletionRequest{
		Model: model,
		Messages: []models.ChatMessage{
			{Role: "user", Content: "test message"},
		},
	}
	for _, opt := range opts {
		opt(req)
	}
	return req
}

// WithMessages sets request messages.
func WithMessages(msgs []models.ChatMessage) func(*models.ChatCompletionRequest) {
	return func(r *models.ChatCompletionRequest) {
		r.Messages = msgs
	}
}

// WithStreaming sets streaming flag.
func WithStreaming(stream bool) func(*models.ChatCompletionRequest) {
	return func(r *models.ChatCompletionRequest) {
		r.Stream = stream
	}
}

// BuildProvider creates a test provider.
func BuildProvider(typeKey string, opts ...func(*models.Provider)) *models.Provider {
	p := &models.Provider{
		ID:        fmt.Sprintf("%s", typeKey),
		Name:      typeKey,
		Type:      typeKey,
		Qualifier: "",
		BaseURL:   "",
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// WithProviderName sets provider name.
func WithProviderName(name string) func(*models.Provider) {
	return func(p *models.Provider) {
		p.Name = name
	}
}

// WithProviderQualifier sets provider qualifier.
func WithProviderQualifier(q string) func(*models.Provider) {
	return func(p *models.Provider) {
		p.Qualifier = q
		if q != "" {
			p.ID = p.Type + ":" + q
		}
	}
}

// MakeAuthRequest creates an HTTP request with bearer token.
func MakeAuthRequest(method, url, token string, body io.Reader) *http.Request {
	req := httptest.NewRequest(method, url, body)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return req
}

// ParseSSEStream parses Server-Sent Events from response body.
func ParseSSEStream(r io.Reader) ([]models.StreamChunk, error) {
	var chunks []models.StreamChunk
	decoder := json.NewDecoder(r)

	for {
		var chunk models.StreamChunk
		if err := decoder.Decode(&chunk); err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		chunks = append(chunks, chunk)
	}

	return chunks, nil
}

