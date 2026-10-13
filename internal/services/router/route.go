package router

import (
	"context"
	"errors"
	"fmt"

	apierrors "github.com/TheSlopMachine/llm-router/internal/errors"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/luaplugin"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
)

// resolvedRequest is the endpoint-independent resolution of a routed request:
// token snapshot, ModelId parse, provider resolve, disabled gate,
// model-override gate and endpoint gate. Endpoint-specific capability
// checks, credential loading and backend invocation stay with the caller.
type resolvedRequest struct {
	providerID string
	modelName  string
	resolved   *provider.Resolved
}

// resolveRequest runs the shared routing pipeline for one request model.
func (s *Service) resolveRequest(ctx context.Context, model models.ModelId, token *models.RouterToken, allowDisabled bool, endpoint string) (context.Context, resolvedRequest, error) {
	var resolution resolvedRequest
	ctx = withTokenRules(ctx, token)
	providerID, modelName, err := model.Parse()
	if err != nil {
		s.debug("Denying request with invalid model identifier",
			"model", model.String(), "endpoint", endpoint,
			"reason", "invalid_model_id", "token_id", tokenIdentifier(token), "error", err)
		return ctx, resolution, fmt.Errorf("invalid model id: %w", err)
	}
	resolved, err := provider.Resolve(s.providerService, ctx, providerID)
	if err != nil {
		if errors.Is(err, luaplugin.ErrPluginDisabled) {
			s.warn("Denying request for disabled plugin",
				"provider_id", providerID, "model", model.String(), "endpoint", endpoint,
				"reason", "provider_disabled", "token_id", tokenIdentifier(token), "error", err)
			return ctx, resolution, fmt.Errorf("%w: %s", apierrors.ErrProviderDisabled, providerID)
		}
		s.debug("Denying request for unknown provider",
			"provider_id", providerID, "model", model.String(), "endpoint", endpoint,
			"reason", "provider_not_found", "token_id", tokenIdentifier(token), "error", err)
		return ctx, resolution, fmt.Errorf("%w: %s", apierrors.ErrProviderNotFound, providerID)
	}
	if resolved.Instance.Disabled {
		s.warn("Denying request for disabled provider",
			"provider_id", providerID, "model", model.String(), "endpoint", endpoint,
			"reason", "provider_disabled", "token_id", tokenIdentifier(token))
		return ctx, resolution, fmt.Errorf("%w: %s", apierrors.ErrProviderDisabled, providerID)
	}
	if err := s.requireModelEnabled(ctx, providerID, modelName, model, allowDisabled); err != nil {
		reason := "model_gate_failed"
		if errors.Is(err, apierrors.ErrModelDisabled) {
			reason = "model_disabled"
		}
		s.warn("Denying request for disabled model",
			"provider_id", providerID, "model", model.String(), "endpoint", endpoint,
			"reason", reason, "token_id", tokenIdentifier(token), "error", err)
		return ctx, resolution, err
	}
	if err := s.checkEndpoint(ctx, providerID, modelName, endpoint); err != nil {
		s.debug("Denying request for unsupported endpoint",
			"provider_id", providerID, "model", model.String(), "endpoint", endpoint,
			"reason", "endpoint_not_supported", "token_id", tokenIdentifier(token), "error", err)
		return ctx, resolution, err
	}
	resolution = resolvedRequest{providerID: providerID, modelName: modelName, resolved: resolved}
	return ctx, resolution, nil
}
