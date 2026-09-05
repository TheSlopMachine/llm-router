// Package generic provides a generic OpenAI-compatible adapter for custom providers.
// BaseURL is resolved centrally via a provider resolver — no per-call credential injection.
package generic

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"strings"

	sdk "github.com/TheSlopMachine/llm-router-sdk"
)

const adapterTypeKey = "custom"

// resolveBaseURL is injected once at startup (server.New) so the adapter
// owns its config. Signature: qualifier -> baseURL.
var resolveBaseURL func(qualifier string) (string, error)
var logger *slog.Logger

// SetResolver wires the single source of truth for custom provider config.
// Must be called once after provider.Service is constructed.
func SetResolver(fn func(qualifier string) (string, error)) {
	resolveBaseURL = fn
}

// SetLogger wires structured logging for discovery/route diagnostics.
func SetLogger(l *slog.Logger) { logger = l }

func baseURLHost(baseURL string) string {
	u, err := url.Parse(baseURL)
	if err != nil || u.Host == "" {
		return "<invalid-host>"
	}
	return u.Host
}

// Adapter implements the generic OpenAI-compatible adapter for "custom" providers.
type Adapter struct{}

func init() {
	sdk.Register(&Adapter{})
}

func (a *Adapter) TypeKey() string { return adapterTypeKey }

func (a *Adapter) AuthType() sdk.AuthType { return sdk.AuthTypeAPIKey }

func (a *Adapter) ValidateCredentials(data map[string]string) error {
	apiKey := strings.TrimSpace(data["api_key"])
	if apiKey == "" {
		return fmt.Errorf("custom provider: api_key is required")
	}
	if len(apiKey) < 8 {
		return fmt.Errorf("custom provider: api_key appears invalid (too short)")
	}
	return nil
}

func (a *Adapter) baseURLForQualifier(qualifier string) (string, error) {
	if resolveBaseURL == nil {
		err := fmt.Errorf("custom provider resolver not wired (server.New must call generic.SetResolver)")
		if logger != nil {
			logger.Error("custom provider resolver not wired", "qualifier", qualifier)
		}
		return "", err
	}
	if strings.TrimSpace(qualifier) == "" {
		err := fmt.Errorf("custom provider: missing qualifier (expected custom:<slug>/model)")
		if logger != nil {
			logger.Warn("custom provider missing qualifier", "qualifier", qualifier)
		}
		return "", err
	}
	baseURL, err := resolveBaseURL(qualifier)
	if err != nil {
		if logger != nil {
			logger.Warn("custom provider not found", "qualifier", qualifier, "err", err)
		}
		return "", fmt.Errorf("custom provider %q not found: %w", qualifier, err)
	}
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		err := fmt.Errorf("custom provider %q has empty base_url", qualifier)
		if logger != nil {
			logger.Warn("custom provider empty base_url", "qualifier", qualifier)
		}
		return "", err
	}
	return baseURL, nil
}

func (a *Adapter) baseURLForRequest(req *sdk.ChatCompletionRequest) (string, error) {
	_, qualifier, _, err := req.Model.ParseFull()
	if err != nil {
		return "", fmt.Errorf("invalid model id: %w", err)
	}
	// Only handle custom type here; ParseFull extracts adapterType as well.
	adapterType, _, _, _ := req.Model.ParseFull()
	if adapterType != adapterTypeKey {
		return "", fmt.Errorf("generic adapter called for non-custom model %q", req.Model)
	}
	return a.baseURLForQualifier(qualifier)
}

func (a *Adapter) Complete(
	ctx context.Context,
	cred *sdk.Credential,
	req *sdk.ChatCompletionRequest,
) (*sdk.ChatCompletionResponse, error) {
	baseURL, err := a.baseURLForRequest(req)
	if err != nil {
		return nil, err
	}
	apiKey := extractAPIKey(cred)
	_, _, modelName, err := req.Model.ParseFull()
	if err != nil {
		return nil, fmt.Errorf("invalid model id: %w", err)
	}
	client := newClient(baseURL)
	return client.ChatCompletion(ctx, apiKey, modelName, req)
}

func (a *Adapter) CompleteStream(
	ctx context.Context,
	cred *sdk.Credential,
	req *sdk.ChatCompletionRequest,
	w io.Writer,
) error {
	baseURL, err := a.baseURLForRequest(req)
	if err != nil {
		return err
	}
	apiKey := extractAPIKey(cred)
	_, _, modelName, err := req.Model.ParseFull()
	if err != nil {
		return fmt.Errorf("invalid model id: %w", err)
	}
	client := newClient(baseURL)
	return client.ChatCompletionStream(ctx, apiKey, modelName, req, w)
}

func (a *Adapter) NeedsRefresh(cred *sdk.Credential) bool { return false }

func (a *Adapter) RefreshCredential(ctx context.Context, cred *sdk.Credential) (*sdk.Credential, error) {
	return nil, sdk.ErrNoRefreshNeeded
}

func (a *Adapter) GetModelInfos(
	ctx context.Context,
	cred *sdk.Credential,
	providerQualifier string,
) ([]sdk.ModelInfo, error) {
	baseURL, err := a.baseURLForQualifier(providerQualifier)
	if err != nil {
		return nil, err
	}
	apiKey := extractAPIKey(cred)
	client := newClient(baseURL)
	return client.ListModels(ctx, apiKey)
}

func (a *Adapter) GetAuthFlow() sdk.AuthFlowHandler { return &AuthFlow{} }

func (a *Adapter) GetDefaultProviders() []sdk.ProviderInfo { return []sdk.ProviderInfo{} }

func extractAPIKey(cred *sdk.Credential) string {
	if cred == nil || cred.Data == nil {
		return ""
	}
	return strings.TrimSpace(cred.Data["api_key"])
}

var _ sdk.Adapter = (*Adapter)(nil)
