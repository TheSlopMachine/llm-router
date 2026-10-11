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

// moderateOne runs a single moderation pass. Lua plugins iterate
// internally; Go adapters receive the gated pool.
func (s *Service) moderateOne(ctx context.Context, resolved *provider.Resolved, credentials []*models.Credential, allowed []string, request *models.ModerationRequest) (*models.ModerationResponse, string, error) {
	if resolved.IsLua() {
		response, err := s.providerService.LuaService().Moderate(ctx, s.handlerMetadata(resolved, request.Model, allowed), request)
		if errors.Is(err, luaplugin.ErrHandlerNotFound) {
			return nil, "", fmt.Errorf("%w: provider %q has no moderate handler", apierrors.ErrEndpointNotSupported, resolved.Instance.Name)
		}
		return response, "", err
	}
	moderator, ok := resolved.Go.(provider.Moderator)
	if !ok {
		return nil, "", fmt.Errorf("%w: provider %q does not support moderations", apierrors.ErrEndpointNotSupported, resolved.Instance.Name)
	}
	response, err := moderator.Moderate(ctx, credentials, request, resolved.Instance.Config)
	return response, "", err
}

// Moderate routes a POST /v1/moderations request in a single backend pass
// over the credential pool.
func (s *Service) Moderate(
	ctx context.Context,
	request *models.ModerationRequest,
	token *models.RouterToken,
) (*models.ModerationResponse, error) {
	return s.moderate(ctx, request, token, false)
}

func (s *Service) moderate(
	ctx context.Context,
	request *models.ModerationRequest,
	token *models.RouterToken,
	allowDisabled bool,
) (*models.ModerationResponse, error) {
	response, _, err := s.moderateWithProxy(ctx, request, token, allowDisabled)
	return response, err
}

// moderateWithProxy is moderate plus the redacted proxy host:port of the
// last attempt ("" = direct).
func (s *Service) moderateWithProxy(
	ctx context.Context,
	request *models.ModerationRequest,
	token *models.RouterToken,
	allowDisabled bool,
) (*models.ModerationResponse, string, error) {
	ctx, resolvedRequest, err := s.resolveRequest(ctx, request.Model, token, allowDisabled, models.EndpointChatCompletions)
	if err != nil {
		return nil, "", err
	}
	resolved := resolvedRequest.resolved
	if resolved.Instance.TypeKey == provider.TypeVirtual {
		return nil, "", fmt.Errorf("%w: virtual models do not serve moderations", apierrors.ErrEndpointNotSupported)
	}
	// Capability pre-check: fail loudly before touching the credential gate.
	if resolved.IsLua() {
		if !s.providerService.LuaService().HasHandler(resolved.Instance.TypeKey, "moderate") {
			return nil, "", fmt.Errorf("%w: provider %q has no moderate handler", apierrors.ErrEndpointNotSupported, resolved.Instance.Name)
		}
	} else if _, ok := resolved.Go.(provider.Moderator); !ok {
		return nil, "", fmt.Errorf("%w: provider %q does not support moderations", apierrors.ErrEndpointNotSupported, resolved.Instance.Name)
	}
	allowed, credentials, err := s.gateCredentials(ctx, resolved, request.Model, token)
	if err != nil {
		return nil, "", err
	}
	response, _, err := s.moderateOne(ctx, resolved, credentials, allowed, request)
	s.dropMissingModel(resolvedRequest.providerID, resolvedRequest.modelName, err)
	return response, "", err
}
