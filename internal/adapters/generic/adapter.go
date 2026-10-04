// Package generic provides a generic OpenAI-compatible backend for custom providers.
package generic

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
)

// Adapter implements the generic OpenAI-compatible backend for custom providers.
type Adapter struct {
	logger *slog.Logger
}

// SetLogger wires the logger for backend lines.
func (a *Adapter) SetLogger(l *slog.Logger) { a.logger = l }

func (a *Adapter) TypeKey() string { return provider.TypeCustom }

func (a *Adapter) ValidateCredentials(data map[string]any) error {
	apiKey, _ := data["api_key"].(string)
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return fmt.Errorf("custom provider: api_key is required")
	}
	if len(apiKey) < 8 {
		return fmt.Errorf("custom provider: api_key appears invalid (too short)")
	}
	return nil
}

func baseURLFromConfig(config map[string]any) (string, error) {
	raw, _ := config["base_url"].(string)
	return models.NormalizeBaseURL(raw)
}

func (a *Adapter) Complete(
	ctx context.Context,
	creds []*models.Credential,
	req *models.ChatCompletionRequest,
	providerConfig map[string]any,
) (*models.ChatCompletionResponse, error) {
	adapterType, _, modelName, err := req.Model.ParseFull()
	if err != nil {
		return nil, fmt.Errorf("invalid model id: %w", err)
	}
	if adapterType != provider.TypeCustom {
		return nil, fmt.Errorf("generic adapter called for non-custom model %q", req.Model)
	}
	baseURL, err := baseURLFromConfig(providerConfig)
	if err != nil {
		return nil, err
	}
	if len(creds) == 0 {
		return nil, fmt.Errorf("no credentials available")
	}
	client := newClient(baseURL)
	var lastErr error
	for _, cred := range creds {
		var apiKey string
		if cred != nil {
			apiKey = cred.DataString("api_key")
		}
		resp, err := client.ChatCompletion(ctx, apiKey, modelName, req)
		if err == nil {
			return resp, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

func (a *Adapter) CompleteStream(
	ctx context.Context,
	creds []*models.Credential,
	req *models.ChatCompletionRequest,
	w io.Writer,
	providerConfig map[string]any,
) error {
	adapterType, _, modelName, err := req.Model.ParseFull()
	if err != nil {
		return fmt.Errorf("invalid model id: %w", err)
	}
	if adapterType != provider.TypeCustom {
		return fmt.Errorf("generic adapter called for non-custom model %q", req.Model)
	}
	baseURL, err := baseURLFromConfig(providerConfig)
	if err != nil {
		return err
	}
	if len(creds) == 0 {
		return fmt.Errorf("no credentials available")
	}
	client := newClient(baseURL)
	var lastErr error
	for _, cred := range creds {
		var apiKey string
		if cred != nil {
			apiKey = cred.DataString("api_key")
		}
		if err := client.ChatCompletionStream(ctx, apiKey, modelName, req, w); err == nil {
			return nil
		} else {
			lastErr = err
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no credentials available")
	}
	return lastErr
}

func (a *Adapter) NeedsRefresh(cred *models.Credential) bool { return false }

func (a *Adapter) RefreshCredential(ctx context.Context, cred *models.Credential) (map[string]any, error) {
	return nil, provider.ErrNotRefreshable
}

func (a *Adapter) GetModelInfos(
	ctx context.Context,
	cred *models.Credential,
	providerConfig map[string]any,
) ([]models.ModelInfo, error) {
	baseURL, err := baseURLFromConfig(providerConfig)
	if err != nil {
		return nil, err
	}
	apiKey := ""
	if cred != nil {
		apiKey = cred.DataString("api_key")
	}
	client := newClient(baseURL)
	return client.ListModels(ctx, apiKey)
}

// SubmitVideo routes one video generation submit over the credential pool.
func (a *Adapter) SubmitVideo(
	ctx context.Context,
	creds []*models.Credential,
	req *models.VideoGenerationRequest,
	providerConfig map[string]any,
) (*models.VideoGenerationResponse, error) {
	adapterType, _, modelName, err := req.Model.ParseFull()
	if err != nil {
		return nil, fmt.Errorf("invalid model id: %w", err)
	}
	if adapterType != provider.TypeCustom {
		return nil, fmt.Errorf("generic adapter called for non-custom model %q", req.Model)
	}
	baseURL, err := baseURLFromConfig(providerConfig)
	if err != nil {
		return nil, err
	}
	if len(creds) == 0 {
		return nil, fmt.Errorf("no credentials available")
	}
	client := newClient(baseURL)
	var lastErr error
	for _, cred := range creds {
		var apiKey string
		if cred != nil {
			apiKey = cred.DataString("api_key")
		}
		resp, err := client.SubmitVideo(ctx, apiKey, modelName, req)
		if err == nil {
			return resp, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

// PollVideo routes one video status poll over the credential pool.
func (a *Adapter) PollVideo(
	ctx context.Context,
	creds []*models.Credential,
	model models.ModelId,
	upstreamJobID string,
	providerConfig map[string]any,
) (*models.VideoGenerationResponse, error) {
	adapterType, _, _, err := model.ParseFull()
	if err != nil {
		return nil, fmt.Errorf("invalid model id: %w", err)
	}
	if adapterType != provider.TypeCustom {
		return nil, fmt.Errorf("generic adapter called for non-custom model %q", model)
	}
	baseURL, err := baseURLFromConfig(providerConfig)
	if err != nil {
		return nil, err
	}
	if len(creds) == 0 {
		return nil, fmt.Errorf("no credentials available")
	}
	client := newClient(baseURL)
	var lastErr error
	for _, cred := range creds {
		var apiKey string
		if cred != nil {
			apiKey = cred.DataString("api_key")
		}
		resp, err := client.PollVideo(ctx, apiKey, upstreamJobID)
		if err == nil {
			return resp, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

// VideoContent routes one video asset download over the credential pool.
func (a *Adapter) VideoContent(
	ctx context.Context,
	creds []*models.Credential,
	model models.ModelId,
	upstreamJobID string,
	index int,
	providerConfig map[string]any,
) (*models.VideoContentResponse, error) {
	adapterType, _, _, err := model.ParseFull()
	if err != nil {
		return nil, fmt.Errorf("invalid model id: %w", err)
	}
	if adapterType != provider.TypeCustom {
		return nil, fmt.Errorf("generic adapter called for non-custom model %q", model)
	}
	baseURL, err := baseURLFromConfig(providerConfig)
	if err != nil {
		return nil, err
	}
	if len(creds) == 0 {
		return nil, fmt.Errorf("no credentials available")
	}
	client := newClient(baseURL)
	var lastErr error
	for _, cred := range creds {
		var apiKey string
		if cred != nil {
			apiKey = cred.DataString("api_key")
		}
		resp, err := client.VideoContent(ctx, apiKey, upstreamJobID, index)
		if err == nil {
			return resp, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

// classifyHTTPError maps upstream status codes to terminal OpenAI-shaped
// errors. The body rides the message (bounded); message text never decides
// the code.
func classifyHTTPError(status int, body string) error {
	message := strings.TrimSpace(body)
	if message == "" {
		message = fmt.Sprintf("unexpected status %d", status)
	}
	if len(message) > 1024 {
		message = message[:1024] + "…[truncated]"
	}
	code := "server_error"
	switch status {
	case 400:
		code = "invalid_request_error"
	case 401:
		code = "authentication_error"
	case 402:
		code = "payment_required"
	case 404:
		code = "not_found"
	case 429:
		code = "rate_limit"
	}
	return &models.ProviderError{StatusCode: status, Message: message, Code: code}
}
