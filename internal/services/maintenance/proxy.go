package maintenance

import (
	"context"
	"errors"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/services/proxypool"
)

const proxySourceFetchInterval = time.Hour

// rotateProxyOnce runs one pool rotation pass outside the tick schedule.
func (s *Service) rotateProxyOnce(ctx context.Context) {
	if s.proxySvc == nil {
		return
	}
	s.lastProxyRotate = time.Now()
	if err := s.proxySvc.RotateAll(ctx); err != nil {
		s.logger.Warn("maintenance: startup proxy rotation failed", "err", err)
		return
	}
	s.logger.Info("maintenance: startup proxy rotation completed")
}

// maintainProxyPool rotates the pool on the configured tick and fetches new
// list candidates hourly. Fetching and rotation are independent: the tick
// re-probes pooled proxies, the fetch adds new ones. Fetch completion
// tracks per source: one failing source never delays the others.
func (s *Service) maintainProxyPool(ctx context.Context) {
	if s.proxySvc == nil || s.luaSvc == nil {
		return
	}
	if s.proxyTickInterval > 0 && (s.lastProxyRotate.IsZero() || time.Since(s.lastProxyRotate) >= s.proxyTickInterval) {
		s.lastProxyRotate = time.Now()
		if err := s.proxySvc.RotateAll(ctx); err != nil {
			s.logger.Warn("maintenance: proxy rotation failed", "err", err)
		}
	}
	if !s.proxySvc.NeedsSearch() {
		return
	}
	for _, key := range s.luaSvc.ProxySourceKeys() {
		if ctx.Err() != nil {
			return
		}
		if last, ok := s.lastProxyFetch[key]; ok && time.Since(last) < proxySourceFetchInterval {
			continue
		}
		s.fetchProxySource(ctx, key)
		s.lastProxyFetch[key] = time.Now()
	}
}

// fetchProxySource refreshes one proxy list source through fetch and add.
func (s *Service) fetchProxySource(ctx context.Context, key string) {
	s.proxySvc.SetSourceFetching(key)
	candidates, err := s.luaSvc.FetchProxies(ctx, key)
	if err != nil {
		s.proxySvc.SetSourceFailed(key, err)
		s.logger.Warn("maintenance: proxy source refresh failed", "source", key, "err", err)
		return
	}
	if err := s.proxySvc.AddCandidates(ctx, key, candidates); err != nil {
		// A busy pool means another rotation is already working;
		// not a source failure, so don't stain its status.
		if errors.Is(err, proxypool.ErrBusy) {
			s.proxySvc.SetSourceDone(key, len(candidates))
		} else {
			s.proxySvc.SetSourceFailed(key, err)
		}
		s.logger.Warn("maintenance: proxy pool refresh failed", "source", key, "err", err)
		return
	}
	s.proxySvc.SetSourceDone(key, len(candidates))
}
