package proxypool

import (
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/util"
)

// NoteDemand records whitelisted regions as demanded. Throttled to one
// bucket write per region per hour; reads stay in the bucket.
func (s *Service) NoteDemand(regions []string) {
	now := util.Now()
	for _, r := range regions {
		r = NormalizeCountryCode(r)
		if r == "" {
			continue
		}
		if existing, err := s.regions.Get(r); err == nil && existing != nil {
			if now.Sub(existing.LastSeen) < demandWriteThrottle {
				continue
			}
		}
		_ = s.regions.Put(r, &models.ActiveRegion{Region: r, LastSeen: now})
	}
}

// demandSet returns defaults merged with fresh observed demand.
func (s *Service) demandSet() map[string]bool {
	demand := map[string]bool{}
	for _, r := range DefaultRegions {
		demand[r] = true
	}
	now := util.Now()
	rows, err := s.regions.List()
	if err != nil {
		return demand
	}
	for _, row := range rows {
		if now.Sub(row.LastSeen) < demandExpiry {
			demand[row.Region] = true
		}
	}
	return demand
}

// fastCount counts pooled proxies of a location at or above the speed
// floor, manual ones included: demand is satisfied by anything choosable.
func (s *Service) fastCount(location string, minSpeedKbps int64) int {
	pooled, err := s.proxies.ListFiltered(func(p *models.Proxy) bool {
		return p.Location == location && p.SpeedKbps >= minSpeedKbps
	})
	if err != nil {
		return 0
	}
	return len(pooled)
}

// NeedsSearch reports whether any demanded region lacks fast proxies.
// True means the next scheduled fetch should run; false pauses it.
func (s *Service) NeedsSearch() bool {
	minSpeedKbps, maxPerLocation := s.settings()
	for region := range s.demandSet() {
		if s.fastCount(region, minSpeedKbps) < maxPerLocation {
			return true
		}
	}
	return false
}

// sweepDemand drops demand unseen past expiry. Defaults are not stored,
// so they never expire.
func (s *Service) sweepDemand() {
	now := util.Now()
	rows, err := s.regions.List()
	if err != nil {
		return
	}
	for _, row := range rows {
		if now.Sub(row.LastSeen) >= demandExpiry {
			_ = s.regions.Delete(row.Region)
		}
	}
}
