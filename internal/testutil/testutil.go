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
	"path/filepath"
	"testing"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/models"
)

// SetupTestDB creates a temporary bbolt database for testing.
// The database is automatically cleaned up when the test completes.
func SetupTestDB(t *testing.T) *db.DB {
	t.Helper()

	file, err := os.CreateTemp("", "llm-router-test-*.db")
	if err != nil {
		t.Fatalf("create temp db file: %v", err)
	}
	path := file.Name()
	file.Close()

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

// SetupTestSQLite creates a temporary SQLite database for testing with the
// full migrated schema. Cleanup closes the pool and removes the file.
func SetupTestSQLite(t *testing.T) *db.Database {
	t.Helper()

	path := filepath.Join(t.TempDir(), "llm-router-test.sqlite3")
	database, err := db.OpenSQLite(context.Background(), path)
	if err != nil {
		t.Fatalf("open test sqlite: %v", err)
	}

	t.Cleanup(func() {
		database.Close()
	})

	return database
}

// MockAdapter is a simple, configurable test adapter using models types.
type MockAdapter struct {
	typeKey            string
	completeFunc       func(context.Context, []*models.Credential, *models.ChatCompletionRequest) (*models.ChatCompletionResponse, error)
	completeStreamFunc func(context.Context, []*models.Credential, *models.ChatCompletionRequest, io.Writer) error
	validateFunc       func(map[string]any) error
	needsRefreshFunc   func(*models.Credential) bool
	refreshFunc        func(context.Context, *models.Credential) (map[string]any, error)
	modelInfosFunc     func(context.Context, *models.Credential, string) ([]models.ModelInfo, error)
	// Calls counts Complete invocations; SeenPools records each call's pool.
	Calls     int
	SeenPools [][]*models.Credential
}

// NewMockAdapter creates a mock adapter with sensible defaults.
func NewMockAdapter(typeKey string) *MockAdapter {
	return &MockAdapter{
		typeKey: typeKey,
		completeFunc: func(ctx context.Context, credentials []*models.Credential, request *models.ChatCompletionRequest) (*models.ChatCompletionResponse, error) {
			return &models.ChatCompletionResponse{
				ID:      "mock-" + fmt.Sprintf("%d", time.Now().Unix()),
				Object:  "chat.completion",
				Created: time.Now().Unix(),
				Model:   string(request.Model),
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
		completeStreamFunc: func(ctx context.Context, credentials []*models.Credential, request *models.ChatCompletionRequest, writer io.Writer) error {
			chunk := models.StreamChunk{
				ID:      "mock-stream",
				Object:  "chat.completion.chunk",
				Created: time.Now().Unix(),
				Model:   string(request.Model),
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
			fmt.Fprintf(writer, "data: %s\n\n", data)
			return nil
		},
		validateFunc: func(data map[string]any) error {
			if s, _ := data["api_key"].(string); s == "" {
				return fmt.Errorf("api_key required")
			}
			return nil
		},
		needsRefreshFunc: func(credential *models.Credential) bool {
			return false
		},
		refreshFunc: func(ctx context.Context, credential *models.Credential) (map[string]any, error) {
			return nil, fmt.Errorf("no refresh needed")
		},
		modelInfosFunc: func(ctx context.Context, credential *models.Credential, qualifier string) ([]models.ModelInfo, error) {
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

func (m *MockAdapter) TypeKey() string { return m.typeKey }
func (m *MockAdapter) ValidateCredentials(data map[string]any) error {
	return m.validateFunc(data)
}
func (m *MockAdapter) Complete(ctx context.Context, credentials []*models.Credential, request *models.ChatCompletionRequest, _ map[string]any) (*models.ChatCompletionResponse, error) {
	m.Calls++
	m.SeenPools = append(m.SeenPools, credentials)
	return m.completeFunc(ctx, credentials, request)
}
func (m *MockAdapter) CompleteStream(ctx context.Context, credentials []*models.Credential, request *models.ChatCompletionRequest, writer io.Writer, _ map[string]any) error {
	m.Calls++
	m.SeenPools = append(m.SeenPools, credentials)
	return m.completeStreamFunc(ctx, credentials, request, writer)
}
func (m *MockAdapter) NeedsRefresh(credential *models.Credential) bool {
	return m.needsRefreshFunc(credential)
}
func (m *MockAdapter) RefreshCredential(ctx context.Context, credential *models.Credential) (map[string]any, error) {
	return m.refreshFunc(ctx, credential)
}
func (m *MockAdapter) GetModelInfos(ctx context.Context, credential *models.Credential, _ map[string]any) ([]models.ModelInfo, error) {
	return m.modelInfosFunc(ctx, credential, "")
}

// WithCompleteFunc configures the Complete behavior.
func (m *MockAdapter) WithCompleteFunc(f func(context.Context, []*models.Credential, *models.ChatCompletionRequest) (*models.ChatCompletionResponse, error)) *MockAdapter {
	m.completeFunc = f
	return m
}

// WithCompleteStreamFunc configures the CompleteStream behavior.
func (m *MockAdapter) WithCompleteStreamFunc(f func(context.Context, []*models.Credential, *models.ChatCompletionRequest, io.Writer) error) *MockAdapter {
	m.completeStreamFunc = f
	return m
}

// WithValidateFunc configures the ValidateCredentials behavior.
func (m *MockAdapter) WithValidateFunc(f func(map[string]any) error) *MockAdapter {
	m.validateFunc = f
	return m
}

// WithModelInfos configures the GetModelInfos behavior.
func (m *MockAdapter) WithModelInfos(modelInfos []models.ModelInfo) *MockAdapter {
	m.modelInfosFunc = func(ctx context.Context, credential *models.Credential, qualifier string) ([]models.ModelInfo, error) {
		return modelInfos, nil
	}
	return m
}

// WithNeedsRefreshFunc configures the NeedsRefresh behavior.
func (m *MockAdapter) WithNeedsRefreshFunc(f func(*models.Credential) bool) *MockAdapter {
	m.needsRefreshFunc = f
	return m
}

// WithRefreshFunc configures the RefreshCredential behavior.
func (m *MockAdapter) WithRefreshFunc(f func(context.Context, *models.Credential) (map[string]any, error)) *MockAdapter {
	m.refreshFunc = f
	return m
}

// BuildCredential creates a test credential with defaults.
func BuildCredential(providerID string, options ...func(*models.Credential)) *models.Credential {
	credential := &models.Credential{
		ID:         fmt.Sprintf("cred-%d", time.Now().UnixNano()),
		ProviderID: providerID,
		Data: map[string]any{
			"api_key": "test-key",
		},
		LastUsedAt: nil,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
	for _, option := range options {
		option(credential)
	}
	return credential
}

// WithCredentialData sets credential data.
func WithCredentialData(data map[string]any) func(*models.Credential) {
	return func(credential *models.Credential) {
		credential.Data = data
	}
}

// WithCredentialExpiry sets credential expiry time.
func WithCredentialExpiry(t time.Time) func(*models.Credential) {
	return func(credential *models.Credential) {
		credential.ExpiresAt = &t
	}
}

// WithCredentialLastUsed sets last used time.
func WithCredentialLastUsed(t time.Time) func(*models.Credential) {
	return func(credential *models.Credential) {
		credential.LastUsedAt = &t
	}
}

// BuildToken creates a test token with defaults.
func BuildToken(options ...func(*models.RouterToken)) *models.RouterToken {
	token := &models.RouterToken{
		ID:        fmt.Sprintf("tok-%d", time.Now().UnixNano()),
		Name:      "test-token",
		TokenHash: "",
		Rules:     models.TokenRules{AllowedModels: []models.ModelId{}},
		CreatedAt: time.Now(),
	}
	for _, option := range options {
		option(token)
	}
	return token
}

// WithTokenRules sets token rules.
func WithTokenRules(rules models.TokenRules) func(*models.RouterToken) {
	return func(token *models.RouterToken) {
		token.Rules = rules
	}
}

// WithTokenName sets token name.
func WithTokenName(name string) func(*models.RouterToken) {
	return func(token *models.RouterToken) {
		token.Name = name
	}
}

// WithTokenHash sets token hash.
func WithTokenHash(hash string) func(*models.RouterToken) {
	return func(token *models.RouterToken) {
		token.TokenHash = hash
	}
}

// BuildRequest creates a test chat completion request.
func BuildRequest(model models.ModelId, options ...func(*models.ChatCompletionRequest)) *models.ChatCompletionRequest {
	request := &models.ChatCompletionRequest{
		Model: model,
		Messages: []models.ChatMessage{
			{Role: "user", Content: "test message"},
		},
	}
	for _, option := range options {
		option(request)
	}
	return request
}

// WithMessages sets request messages.
func WithMessages(messages []models.ChatMessage) func(*models.ChatCompletionRequest) {
	return func(request *models.ChatCompletionRequest) {
		request.Messages = messages
	}
}

// WithStreaming sets streaming flag.
func WithStreaming(stream bool) func(*models.ChatCompletionRequest) {
	return func(request *models.ChatCompletionRequest) {
		request.Stream = stream
	}
}

// BuildProvider creates a test provider instance.
func BuildProvider(typeKey string, options ...func(*models.ProviderInstance)) *models.ProviderInstance {
	providerInstance := &models.ProviderInstance{
		ID:      typeKey,
		Name:    typeKey,
		TypeKey: typeKey,
	}
	for _, option := range options {
		option(providerInstance)
	}
	return providerInstance
}

// WithProviderName sets provider name.
func WithProviderName(name string) func(*models.ProviderInstance) {
	return func(providerInstance *models.ProviderInstance) {
		providerInstance.Name = name
	}
}

// WithProviderQualifier sets provider qualifier.
func WithProviderQualifier(qualifier string) func(*models.ProviderInstance) {
	return func(providerInstance *models.ProviderInstance) {
		providerInstance.Qualifier = qualifier
		if qualifier != "" {
			providerInstance.ID = providerInstance.TypeKey + ":" + qualifier
		}
	}
}

// MakeAuthRequest creates an HTTP request with bearer token.
func MakeAuthRequest(method, url, token string, body io.Reader) *http.Request {
	request := httptest.NewRequest(method, url, body)
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	return request
}

// ParseSSEStream parses Server-Sent Events from response body.
func ParseSSEStream(reader io.Reader) ([]models.StreamChunk, error) {
	var chunks []models.StreamChunk
	decoder := json.NewDecoder(reader)

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
