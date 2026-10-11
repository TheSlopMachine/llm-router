package server

import (
	"context"
	"log/slog"

	"github.com/TheSlopMachine/llm-router/internal/services/credential"
	"github.com/TheSlopMachine/llm-router/internal/services/luaplugin"
	"github.com/TheSlopMachine/llm-router/internal/services/modelinfo"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
)

// startupCleanup removes credential rows that can never route. Fresh
// databases hold none; the pass stays warn-only so cleanup never fails
// startup.
func startupCleanup(logger *slog.Logger, credentialService *credential.GormService, providerService *provider.GormService) {
	ctx := context.Background()
	// Virtual models resolve from the model name and need no credentials;
	// rows left over from the credential-bound era are dead weight.
	for _, key := range []string{"agents", provider.TypeVirtual} {
		if count, err := credentialService.DeleteByProvider(ctx, key); err != nil {
			logger.Warn("virtual credential cleanup failed", "provider", key, "err", err)
		} else if count > 0 {
			logger.Info("virtual credential cleanup completed", "provider", key, "count", count)
		}
	}
}

// wireInvalidation refreshes cached model lists when providers or plugins
// change. Credential changes never invalidate: discovery uses the first
// answering key, so the upstream list does not depend on the pool.
func wireInvalidation(logger *slog.Logger, providerService *provider.GormService, modelInfoService *modelinfo.GormService, luaService *luaplugin.Service) {
	ctx := context.Background()
	invalidate := func(providerID string) {
		_ = modelInfoService.InvalidateProvider(ctx, providerID)
	}
	providerService.SetOnChanged(invalidate)
	luaService.SetOnChanged(func(typeKey string) {
		// Runtime plugin changes (install/update/rollback/enable) must
		// create default provider rows, previously seeded at startup.
		if err := providerService.SyncDefaultProviders(ctx); err != nil {
			logger.Warn("sync default providers failed", "err", err)
		}
		providers, err := providerService.GetByType(ctx, typeKey)
		if err != nil {
			return
		}
		for _, provider := range providers {
			_ = modelInfoService.InvalidateProvider(ctx, provider.ID)
		}
	})
}
