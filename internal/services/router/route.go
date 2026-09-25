package router

import (
	"context"
	"fmt"

	apierrors "github.com/TheSlopMachine/llm-router/internal/errors"
	"github.com/TheSlopMachine/llm-router/internal/models"
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
	var out resolvedRequest
	ctx = withTokenRules(ctx, token)
	providerID, modelName, err := model.Parse()
	if err != nil {
		return ctx, out, fmt.Errorf("invalid model id: %w", err)
	}
	resolved, err := provider.Resolve(s.providerSvc, providerID)
	if err != nil {
		return ctx, out, fmt.Errorf("%w: %s", apierrors.ErrProviderNotFound, providerID)
	}
	if resolved.Instance.Disabled {
		return ctx, out, fmt.Errorf("%w: %s", apierrors.ErrProviderDisabled, providerID)
	}
	if err := s.requireModelEnabled(providerID, modelName, model, allowDisabled); err != nil {
		return ctx, out, err
	}
	if err := s.checkEndpoint(providerID, modelName, endpoint); err != nil {
		return ctx, out, err
	}
	out = resolvedRequest{providerID: providerID, modelName: modelName, resolved: resolved}
	return ctx, out, nil
}
