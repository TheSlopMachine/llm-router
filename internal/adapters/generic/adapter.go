// Package generic provides a generic OpenAI-compatible backend for custom providers.
package generic

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	apierrors "github.com/TheSlopMachine/llm-router/internal/errors"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/pool"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
)

const adapterTypeKey = "custom"

// UsageTracker records per-credential outcomes. It is implemented by the
// credential pool service and injected via SetUsageTracker.
type UsageTracker = pool.UsageTracker

// Adapter implements the generic OpenAI-compatible backend for "custom" providers.
type Adapter struct {
	usage UsageTracker
}

// SetUsageTracker wires per-credential usage accounting for pool calls.
// Unset (nil) disables accounting; attempts still run.
func (a *Adapter) SetUsageTracker(t UsageTracker) { a.usage = t }

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
	return pool.Run(ctx, nil, creds, a.usage, func(ctx context.Context, cred *models.Credential) (*models.ChatCompletionResponse, error) {
		var apiKey string
		if cred != nil {
			apiKey = cred.DataString("api_key")
		}
		return client.ChatCompletion(ctx, apiKey, modelName, req)
	}, nil)
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
	return pool.RunStream(ctx, nil, w, creds, a.usage, func(ctx context.Context, cred *models.Credential, w io.Writer) error {
		var apiKey string
		if cred != nil {
			apiKey = cred.DataString("api_key")
		}
		return client.ChatCompletionStream(ctx, apiKey, modelName, req, w)
	}, nil)
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
// Structured code/type fields of the upstream envelope decide; message
// text never does.
func classifyHTTPError(status int, body string) error {
	code, errType, message := parseUpstreamEnvelope(body)
	if message == "" {
		message = fmt.Sprintf("unexpected status %d: %s", status, body)
	}
	perr := apierrors.MapUpstream(status, code, errType, message)
	if perr.Type == models.ErrorTypeQuotaExceeded {
		retryAfter := time.Now().Add(time.Minute)
		perr.RetryAfter = &retryAfter
	}
	return perr
}

// parseUpstreamEnvelope extracts code/type/message from an OpenAI-style
// error envelope: {"error":{"code","type","message"}}. Unknown shapes
// yield empty values and fall back to status-based mapping.
func parseUpstreamEnvelope(body string) (code, errType, message string) {
	var envelope struct {
		Error struct {
			Code    any `json:"code"`
			Type    any `json:"type"`
			Message any `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(body), &envelope); err != nil {
		return "", "", ""
	}
	return asString(envelope.Error.Code), asString(envelope.Error.Type), asString(envelope.Error.Message)
}

func asString(v any) string {
	switch s := v.(type) {
	case string:
		return s
	case float64:
		return strings.TrimSuffix(fmt.Sprintf("%v", s), ".0")
	default:
		return ""
	}
}
