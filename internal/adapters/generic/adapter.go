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

func (a *Adapter) currentLogger() *slog.Logger {
	if a == nil || a.logger == nil {
		return slog.Default()
	}
	return a.logger
}

func (a *Adapter) TypeKey() string { return provider.TypeCustom }

func (a *Adapter) ValidateCredentials(data map[string]any) error {
	apiKey, _ := data["api_key"].(string)
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		err := fmt.Errorf("custom provider: api_key is required")
		a.currentLogger().Error("Validating credentials failed", "endpoint", "/models", "error", err)
		return err
	}
	if len(apiKey) < 8 {
		err := fmt.Errorf("custom provider: api_key appears invalid (too short)")
		a.currentLogger().Error("Validating credentials failed", "endpoint", "/models", "error", err)
		return err
	}
	a.currentLogger().Debug("Validating credentials", "endpoint", "/models")
	return nil
}

func baseURLFromConfig(config map[string]any) (string, error) {
	raw, _ := config["base_url"].(string)
	return models.NormalizeBaseURL(raw)
}

// withCredentialFailover tries attempt once per credential and returns the
// first success. The last error wins when every attempt fails. An empty pool
// fails closed with "no credentials available".
func withCredentialFailover[Response any](
	credentials []*models.Credential,
	attempt func(apiKey string) (Response, error),
) (Response, error) {
	var empty Response
	var lastErr error
	for _, credential := range credentials {
		var apiKey string
		if credential != nil {
			apiKey = credential.DataString("api_key")
		}
		response, err := attempt(apiKey)
		if err == nil {
			return response, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no credentials available")
	}
	return empty, lastErr
}

func withCredentialFailoverLogged[Response any](
	logger *slog.Logger,
	model string,
	endpoint string,
	credentials []*models.Credential,
	attempt func(apiKey string) (Response, error),
) (Response, error) {
	if logger == nil {
		logger = slog.Default()
	}
	var empty Response
	var lastErr error
	for attemptIndex, credential := range credentials {
		var apiKey string
		if credential != nil {
			apiKey = credential.DataString("api_key")
		}
		logger.Debug("Trying credential", "model", model, "endpoint", endpoint, "attempt", attemptIndex, "count", len(credentials))
		response, err := attempt(apiKey)
		if err == nil {
			return response, nil
		}
		lastErr = err
		if attemptIndex < len(credentials)-1 {
			logger.Warn("Credential attempt failed, trying next", "model", model, "endpoint", endpoint, "attempt", attemptIndex, "error", err)
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no credentials available")
	}
	logger.Error("All credentials failed", "model", model, "endpoint", endpoint, "count", len(credentials), "error", lastErr)
	return empty, lastErr
}

func (a *Adapter) clientFor(baseURL string) *Client {
	client := newClient(baseURL)
	client.SetLogger(a.currentLogger())
	return client
}

func (a *Adapter) Complete(
	ctx context.Context,
	credentials []*models.Credential,
	request *models.ChatCompletionRequest,
	providerConfig map[string]any,
) (*models.ChatCompletionResponse, error) {
	adapterType, _, modelName, err := request.Model.ParseFull()
	if err != nil {
		a.currentLogger().Error("Validating model failed", "endpoint", "/chat/completions", "error", err)
		return nil, fmt.Errorf("invalid model id: %w", err)
	}
	if adapterType != provider.TypeCustom {
		err := fmt.Errorf("generic adapter called for non-custom model %q", request.Model)
		a.currentLogger().Error("Validating model failed", "endpoint", "/chat/completions", "model", request.Model.String(), "error", err)
		return nil, err
	}
	baseURL, err := baseURLFromConfig(providerConfig)
	if err != nil {
		a.currentLogger().Error("Reading provider configuration failed", "endpoint", "/chat/completions", "model", request.Model.String(), "error", err)
		return nil, err
	}
	if len(credentials) == 0 {
		err := fmt.Errorf("no credentials available")
		a.currentLogger().Error("All credentials failed", "endpoint", "/chat/completions", "model", request.Model.String(), "count", 0, "error", err)
		return nil, err
	}
	client := a.clientFor(baseURL)
	return withCredentialFailoverLogged(a.currentLogger(), request.Model.String(), "/chat/completions", credentials, func(apiKey string) (*models.ChatCompletionResponse, error) {
		return client.ChatCompletion(ctx, apiKey, modelName, request)
	})
}

func (a *Adapter) CompleteStream(
	ctx context.Context,
	credentials []*models.Credential,
	request *models.ChatCompletionRequest,
	writer io.Writer,
	providerConfig map[string]any,
) error {
	adapterType, _, modelName, err := request.Model.ParseFull()
	if err != nil {
		a.currentLogger().Error("Validating model failed", "endpoint", "/chat/completions", "error", err)
		return fmt.Errorf("invalid model id: %w", err)
	}
	if adapterType != provider.TypeCustom {
		fault := fmt.Errorf("generic adapter called for non-custom model %q", request.Model)
		a.currentLogger().Error("Validating model failed", "endpoint", "/chat/completions", "model", request.Model.String(), "error", fault)
		return fault
	}
	baseURL, err := baseURLFromConfig(providerConfig)
	if err != nil {
		a.currentLogger().Error("Reading provider configuration failed", "endpoint", "/chat/completions", "model", request.Model.String(), "error", err)
		return err
	}
	if len(credentials) == 0 {
		fault := fmt.Errorf("no credentials available")
		a.currentLogger().Error("All credentials failed", "endpoint", "/chat/completions", "model", request.Model.String(), "count", 0, "error", fault)
		return fault
	}
	client := a.clientFor(baseURL)
	_, err = withCredentialFailoverLogged(a.currentLogger(), request.Model.String(), "/chat/completions", credentials, func(apiKey string) (struct{}, error) {
		return struct{}{}, client.ChatCompletionStream(ctx, apiKey, modelName, request, writer)
	})
	return err
}

func (a *Adapter) NeedsRefresh(credential *models.Credential) bool { return false }

func (a *Adapter) RefreshCredential(ctx context.Context, credential *models.Credential) (map[string]any, error) {
	return nil, provider.ErrNotRefreshable
}

func (a *Adapter) GetModelInfos(
	ctx context.Context,
	credential *models.Credential,
	providerConfig map[string]any,
) ([]models.ModelInfo, error) {
	baseURL, err := baseURLFromConfig(providerConfig)
	if err != nil {
		a.currentLogger().Error("Reading provider configuration failed", "endpoint", "/models", "error", err)
		return nil, err
	}
	apiKey := ""
	if credential != nil {
		apiKey = credential.DataString("api_key")
	}
	client := a.clientFor(baseURL)
	infos, err := client.ListModels(ctx, apiKey)
	if err != nil {
		a.currentLogger().Error("Listing models failed", "endpoint", "/models", "error", err)
		return nil, err
	}
	a.currentLogger().Debug("Listing models", "endpoint", "/models", "count", len(infos))
	return infos, nil
}

// SubmitVideo routes one video generation submit over the credential pool.
func (a *Adapter) SubmitVideo(
	ctx context.Context,
	credentials []*models.Credential,
	request *models.VideoGenerationRequest,
	providerConfig map[string]any,
) (*models.VideoGenerationResponse, error) {
	adapterType, _, modelName, err := request.Model.ParseFull()
	if err != nil {
		a.currentLogger().Error("Validating model failed", "endpoint", "/videos", "error", err)
		return nil, fmt.Errorf("invalid model id: %w", err)
	}
	if adapterType != provider.TypeCustom {
		fault := fmt.Errorf("generic adapter called for non-custom model %q", request.Model)
		a.currentLogger().Error("Validating model failed", "endpoint", "/videos", "model", request.Model.String(), "error", fault)
		return nil, fault
	}
	baseURL, err := baseURLFromConfig(providerConfig)
	if err != nil {
		a.currentLogger().Error("Reading provider configuration failed", "endpoint", "/videos", "model", request.Model.String(), "error", err)
		return nil, err
	}
	if len(credentials) == 0 {
		fault := fmt.Errorf("no credentials available")
		a.currentLogger().Error("All credentials failed", "endpoint", "/videos", "model", request.Model.String(), "count", 0, "error", fault)
		return nil, fault
	}
	client := a.clientFor(baseURL)
	return withCredentialFailoverLogged(a.currentLogger(), request.Model.String(), "/videos", credentials, func(apiKey string) (*models.VideoGenerationResponse, error) {
		return client.SubmitVideo(ctx, apiKey, modelName, request)
	})
}

// PollVideo routes one video status poll over the credential pool.
func (a *Adapter) PollVideo(
	ctx context.Context,
	credentials []*models.Credential,
	model models.ModelId,
	upstreamJobID string,
	providerConfig map[string]any,
) (*models.VideoGenerationResponse, error) {
	adapterType, _, _, err := model.ParseFull()
	if err != nil {
		a.currentLogger().Error("Validating model failed", "endpoint", "/videos", "error", err)
		return nil, fmt.Errorf("invalid model id: %w", err)
	}
	if adapterType != provider.TypeCustom {
		fault := fmt.Errorf("generic adapter called for non-custom model %q", model)
		a.currentLogger().Error("Validating model failed", "endpoint", "/videos", "model", model.String(), "error", fault)
		return nil, fault
	}
	baseURL, err := baseURLFromConfig(providerConfig)
	if err != nil {
		a.currentLogger().Error("Reading provider configuration failed", "endpoint", "/videos", "model", model.String(), "error", err)
		return nil, err
	}
	if len(credentials) == 0 {
		fault := fmt.Errorf("no credentials available")
		a.currentLogger().Error("All credentials failed", "endpoint", "/videos", "model", model.String(), "count", 0, "error", fault)
		return nil, fault
	}
	client := a.clientFor(baseURL)
	return withCredentialFailoverLogged(a.currentLogger(), model.String(), "/videos", credentials, func(apiKey string) (*models.VideoGenerationResponse, error) {
		return client.PollVideo(ctx, apiKey, upstreamJobID)
	})
}

// VideoContent routes one video asset download over the credential pool.
func (a *Adapter) VideoContent(
	ctx context.Context,
	credentials []*models.Credential,
	model models.ModelId,
	upstreamJobID string,
	index int,
	providerConfig map[string]any,
) (*models.VideoContentResponse, error) {
	adapterType, _, _, err := model.ParseFull()
	if err != nil {
		a.currentLogger().Error("Validating model failed", "endpoint", "/videos/content", "error", err)
		return nil, fmt.Errorf("invalid model id: %w", err)
	}
	if adapterType != provider.TypeCustom {
		fault := fmt.Errorf("generic adapter called for non-custom model %q", model)
		a.currentLogger().Error("Validating model failed", "endpoint", "/videos/content", "model", model.String(), "error", fault)
		return nil, fault
	}
	baseURL, err := baseURLFromConfig(providerConfig)
	if err != nil {
		a.currentLogger().Error("Reading provider configuration failed", "endpoint", "/videos/content", "model", model.String(), "error", err)
		return nil, err
	}
	if len(credentials) == 0 {
		fault := fmt.Errorf("no credentials available")
		a.currentLogger().Error("All credentials failed", "endpoint", "/videos/content", "model", model.String(), "count", 0, "error", fault)
		return nil, fault
	}
	client := a.clientFor(baseURL)
	return withCredentialFailoverLogged(a.currentLogger(), model.String(), "/videos/content", credentials, func(apiKey string) (*models.VideoContentResponse, error) {
		return client.VideoContent(ctx, apiKey, upstreamJobID, index)
	})
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
