package server

import (
	"log/slog"

	"github.com/TheSlopMachine/llm-router/internal/services/credential"
	"github.com/TheSlopMachine/llm-router/internal/services/luaplugin"
	"github.com/TheSlopMachine/llm-router/internal/services/modelinfo"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
)

// startupCleanup removes credential rows that can never route and drops
// orphaned credentials left by interrupted deletes. Failures only warn:
// cleanup must never fail startup.
func startupCleanup(logger *slog.Logger, credSvc *credential.Service, providerSvc *provider.Service) {
	// Virtual models resolve from the model name and need no credentials;
	// rows left over from the credential-bound era are dead weight.
	for _, key := range []string{"agents", provider.TypeVirtual} {
		if n, err := credSvc.DeleteByProvider(key); err != nil {
			logger.Warn("virtual credential cleanup failed", "provider", key, "err", err)
		} else if n > 0 {
			logger.Info("virtual credential cleanup completed", "provider", key, "count", n)
		}
	}

	if n, err := providerSvc.CleanupOrphanedCredentials(); err != nil {
		logger.Warn("orphan credential GC failed", "err", err)
	} else if n > 0 {
		logger.Info("orphan credential GC completed", "count", n)
	}
}

// wireInvalidation refreshes cached model lists when providers or plugins
// change. Credential changes never invalidate: discovery uses the first
// answering key, so the upstream list does not depend on the pool.
func wireInvalidation(logger *slog.Logger, providerSvc *provider.Service, modelInfoSvc *modelinfo.Service, luaSvc *luaplugin.Service) {
	invalidate := func(providerID string) {
		_ = modelInfoSvc.InvalidateProvider(providerID)
	}
	providerSvc.SetOnChanged(invalidate)
	luaSvc.SetOnChanged(func(typeKey string) {
		// Runtime plugin changes (install/update/rollback/enable) must
		// create default provider rows, previously seeded at startup.
		if err := providerSvc.SyncDefaultProviders(); err != nil {
			logger.Warn("sync default providers failed", "err", err)
		}
		providers, err := providerSvc.GetByType(typeKey)
		if err != nil {
			return
		}
		for _, p := range providers {
			_ = modelInfoSvc.InvalidateProvider(p.ID)
		}
	})
}
