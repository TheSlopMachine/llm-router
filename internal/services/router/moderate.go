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

// moderateOne runs a single moderation pass against the credential pool.
func (s *Service) moderateOne(ctx context.Context, resolved *provider.Resolved, creds []*models.Credential, req *models.ModerationRequest) (*models.ModerationResponse, string, error) {
	if resolved.IsLua() {
		resp, proxy, err := s.providerSvc.LuaService().ModeratePool(ctx, s.meta(resolved, req.Model), creds, req)
		if errors.Is(err, luaplugin.ErrHandlerNotFound) {
			return nil, proxy, fmt.Errorf("%w: provider %q has no moderate handler", apierrors.ErrEndpointNotSupported, resolved.Instance.Name)
		}
		return resp, proxy, err
	}
	mo, ok := resolved.Go.(provider.Moderator)
	if !ok {
		return nil, "", fmt.Errorf("%w: provider %q does not support moderations", apierrors.ErrEndpointNotSupported, resolved.Instance.Name)
	}
	resp, err := mo.Moderate(ctx, creds, req, resolved.Instance.Config)
	return resp, "", err
}

// Moderate routes a POST /v1/moderations request in a single backend pass
// over the credential pool.
func (s *Service) Moderate(
	ctx context.Context,
	req *models.ModerationRequest,
	token *models.RouterToken,
) (*models.ModerationResponse, error) {
	return s.moderate(ctx, req, token, false)
}

func (s *Service) moderate(
	ctx context.Context,
	req *models.ModerationRequest,
	token *models.RouterToken,
	allowDisabled bool,
) (*models.ModerationResponse, error) {
	resp, _, err := s.moderateWithProxy(ctx, req, token, allowDisabled)
	return resp, err
}

// moderateWithProxy is moderate plus the redacted proxy host:port of the
// last attempt ("" = direct).
func (s *Service) moderateWithProxy(
	ctx context.Context,
	req *models.ModerationRequest,
	token *models.RouterToken,
	allowDisabled bool,
) (*models.ModerationResponse, string, error) {
	ctx, rr, err := s.resolveRequest(ctx, req.Model, token, allowDisabled, models.EndpointChatCompletions)
	if err != nil {
		return nil, "", err
	}
	resolved := rr.resolved
	if resolved.Instance.TypeKey == provider.TypeVirtual {
		return nil, "", fmt.Errorf("%w: virtual models do not serve moderations", apierrors.ErrEndpointNotSupported)
	}
	// Capability pre-check: fail loudly before touching the credential pool.
	if resolved.IsLua() {
		if !s.providerSvc.LuaService().HasHandler(resolved.Instance.TypeKey, "moderate") {
			return nil, "", fmt.Errorf("%w: provider %q has no moderate handler", apierrors.ErrEndpointNotSupported, resolved.Instance.Name)
		}
	} else if _, ok := resolved.Go.(provider.Moderator); !ok {
		return nil, "", fmt.Errorf("%w: provider %q does not support moderations", apierrors.ErrEndpointNotSupported, resolved.Instance.Name)
	}
	creds, err := s.loadCredentials(ctx, resolved, req.Model, token)
	if err != nil {
		return nil, "", err
	}
	resp, proxy, err := s.moderateOne(ctx, resolved, creds, req)
	s.dropMissingModel(rr.providerID, rr.modelName, err)
	return resp, proxy, err
}
