package proxypool

import (
	"fmt"

	"github.com/TheSlopMachine/llm-router/internal/models"
)

// ParseProxyMode reads the provider-level proxy config from
// ProviderInstance.Config. Unknown shapes fall back to disabled.
func ParseProxyMode(providerConfig map[string]any) (mode string, ids []string) {
	mode = models.ProxyModeDisabled
	raw, ok := providerConfig["proxy"].(map[string]any)
	if !ok {
		return mode, nil
	}
	if m, ok := raw["mode"].(string); ok {
		switch m {
		case models.ProxyModeAuto, models.ProxyModeManual:
			mode = m
		}
	}
	if list, ok := raw["ids"].([]any); ok {
		for _, v := range list {
			if s, ok := v.(string); ok {
				ids = append(ids, s)
			}
		}
	}
	return mode, ids
}

// ResolveProxy picks the pool proxy for one provider call and reports it as
// (id, url). It returns ("", "", nil) when the call goes direct: proxying
// disabled, or auto mode with no usable proxy pooled. A nil proxy is a
// legitimate direct route, never a silent fallback: every explicit proxy
// demand that cannot be satisfied (manual pick, forced exit with an empty
// pool) fails loudly instead.
//
// The plugin's preferred location is a preference, never a gate: matching
// proxies lead the ranking, others follow, and the plugin's geo-rotation
// falls through them when the upstream rejects one.
func ResolveProxy(s *Service, mode string, ids []string, location string, forceOnMismatch bool, serverCountry, providerType string) (proxyID, proxyURL string, err error) {
	// Normalize at use time: stored plugin rows may predate canonical codes.
	location = NormalizeCountryCode(location)
	serverCountry = NormalizeCountryCode(serverCountry)
	force := forceOnMismatch && location != "" && serverCountry != "" && serverCountry != location
	var p *models.Proxy
	switch {
	case mode == models.ProxyModeManual:
		p = s.SelectManual(ids, providerType)
		if p == nil {
			return "", "", fmt.Errorf("provider proxy: no usable proxy among %d selected", len(ids))
		}
	case force:
		// A forced exit demands proxied traffic but settles for any country
		// when none match: the provider answer decides, not pool metadata.
		p = s.Select(models.ProxyPreferences{Location: location}, providerType)
		if p == nil {
			return "", "", fmt.Errorf("provider proxy: plugin requires a proxied exit (prefers %s, server is %s) but the pool is empty", location, serverCountry)
		}
	case mode == models.ProxyModeAuto:
		p = s.Select(models.ProxyPreferences{Location: location}, providerType)
	}
	if p == nil {
		return "", "", nil
	}
	return p.ID, p.URL, nil
}
