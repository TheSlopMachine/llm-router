// Package generic provides a generic OpenAI-compatible backend for custom providers.
package generic

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
)

const adapterTypeKey = "custom"

// UsageTracker records per-credential outcomes. It is implemented by the
// credential pool service and injected via SetUsageTracker.
type UsageTracker interface {
	UpdateUsage(id string, success bool) error
	MarkQuotaExceeded(id string, resetAt time.Time) error
}

// Adapter implements the generic OpenAI-compatible backend for "custom" providers.
type Adapter struct {
	usage UsageTracker
}

// SetUsageTracker wires per-credential usage accounting for pool calls.
// Unset (nil) disables accounting; attempts still run.
func (a *Adapter) SetUsageTracker(t UsageTracker) { a.usage = t }

func (a *Adapter) trackSuccess(cred *models.Credential) {
	if a.usage != nil {
		_ = a.usage.UpdateUsage(cred.ID, true)
	}
}

func (a *Adapter) trackFailure(cred *models.Credential, err error) {
	if a.usage == nil {
		return
	}
	_ = a.usage.UpdateUsage(cred.ID, false)
	var perr *models.ProviderError
	if errors.As(err, &perr) && perr.Type == models.ErrorTypeQuotaExceeded && perr.RetryAfter != nil {
		_ = a.usage.MarkQuotaExceeded(cred.ID, *perr.RetryAfter)
	}
}

func (a *Adapter) TypeKey() string { return adapterTypeKey }

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
	baseURL, _ := config["base_url"].(string)
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return "", fmt.Errorf("custom provider has empty base_url")
	}
	return strings.TrimSuffix(baseURL, "/"), nil
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
	if adapterType != adapterTypeKey {
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
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var apiKey string
		if cred != nil {
			apiKey = cred.DataString("api_key")
		}
		resp, err := client.ChatCompletion(ctx, apiKey, modelName, req)
		if err == nil {
			a.trackSuccess(cred)
			return resp, nil
		}
		a.trackFailure(cred, err)
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
	if adapterType != adapterTypeKey {
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
		if err := ctx.Err(); err != nil {
			return err
		}
		var apiKey string
		if cred != nil {
			apiKey = cred.DataString("api_key")
		}
		err := client.ChatCompletionStream(ctx, apiKey, modelName, req, w)
		if err == nil {
			a.trackSuccess(cred)
			return nil
		}
		a.trackFailure(cred, err)
		lastErr = err
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

// classifyHTTPError maps upstream status codes to the error contract.
func classifyHTTPError(status int, body string) error {
	msg := fmt.Sprintf("unexpected status %d: %s", status, body)
	switch {
	case status == 401 || status == 403:
		return &models.ProviderError{StatusCode: status, Message: msg, Type: models.ErrorTypeAuth}
	case status == 429:
		if strings.Contains(strings.ToLower(msg), "quota") {
			retryAfter := time.Now().Add(time.Minute)
			return &models.ProviderError{StatusCode: status, Message: msg, Type: models.ErrorTypeQuotaExceeded, RetryAfter: &retryAfter}
		}
		return &models.ProviderError{StatusCode: status, Message: msg, Type: models.ErrorTypeRateLimit}
	case status == 408 || status == 504:
		return &models.ProviderError{StatusCode: status, Message: msg, Type: models.ErrorTypeTimeout}
	case status >= 500:
		return &models.ProviderError{StatusCode: status, Message: msg, Type: models.ErrorTypeUpstream}
	default:
		return &models.ProviderError{StatusCode: status, Message: msg, Type: models.ErrorTypeInvalidRequest}
	}
}
