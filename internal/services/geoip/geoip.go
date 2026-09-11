// Package geoip resolves the server's country for proxy preference matching.
//
// Detection: ip-api.com (free, keyless, 45 req/min). The result is cached
// for an hour; a manual override in RouterConfiguration always wins.
package geoip

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

const detectURL = "http://ip-api.com/json/?fields=status,countryCode"
const cacheTTL = time.Hour

// Service resolves and caches the server country code.
type Service struct {
	logger *slog.Logger

	mu        sync.Mutex
	country   string
	fetchedAt time.Time
	override  string
}

// New constructs the geoip service.
func New(logger *slog.Logger) *Service {
	return &Service{logger: logger}
}

// SetOverride installs the manual country override (empty clears).
func (s *Service) SetOverride(country string) {
	s.mu.Lock()
	s.override = strings.ToUpper(strings.TrimSpace(country))
	s.mu.Unlock()
}

// Country returns the server country code ("" when undetectable).
func (s *Service) Country(ctx context.Context) string {
	s.mu.Lock()
	if s.override != "" {
		c := s.override
		s.mu.Unlock()
		return c
	}
	cached, at := s.country, s.fetchedAt
	s.mu.Unlock()
	if cached != "" && time.Since(at) < cacheTTL {
		return cached
	}
	c, err := s.detect(ctx)
	if err != nil {
		if s.logger != nil {
			s.logger.Warn("geoip: detection failed", "err", err)
		}
		return cached // stale cache beats nothing
	}
	s.mu.Lock()
	s.country = c
	s.fetchedAt = time.Now()
	s.mu.Unlock()
	return c
}

func (s *Service) detect(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, detectURL, nil)
	if err != nil {
		return "", err
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var out struct {
		Status      string `json:"status"`
		CountryCode string `json:"countryCode"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if out.Status != "success" || out.CountryCode == "" {
		return "", fmt.Errorf("geoip: detection unsuccessful")
	}
	return out.CountryCode, nil
}
