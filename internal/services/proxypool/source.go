package proxypool

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/models"
)

const sourceFetchTimeout = 15 * time.Minute

type pluginSource struct {
	service *Service
}

func (s *pluginSource) Name() string { return "llm-router-plugin-sources" }

func (s *pluginSource) FetchList() []string {
	s.service.mu.Lock()
	keysFn, fetchFn, rootCtx := s.service.keys, s.service.fetch, s.service.rootCtx
	s.service.mu.Unlock()
	if keysFn == nil || fetchFn == nil {
		s.service.recordSourceFailure("plugins", fmt.Errorf("proxy source handlers are not configured"))
		return []string{}
	}
	if rootCtx == nil {
		rootCtx = context.Background()
	}
	keys, err := keysFn()
	if err != nil {
		s.service.recordSourceFailure("plugins", err)
		return []string{}
	}
	active := make(map[string]bool, len(keys))
	for _, key := range keys {
		active[key] = true
	}
	s.service.sourceMu.Lock()
	for key := range s.service.sources {
		if !active[key] {
			delete(s.service.sources, key)
		}
	}
	s.service.sourceMu.Unlock()
	all := make([]string, 0)
	for _, key := range keys {
		ctx, cancel := context.WithTimeout(rootCtx, sourceFetchTimeout)
		candidates, fetchErr := fetchFn(ctx, key)
		cancel()
		if fetchErr != nil {
			s.service.recordSourceFailure(key, fetchErr)
			continue
		}
		info := SourceInfo{Key: key, Name: sourceDisplayName(key), LastFetchAt: time.Now(), Total: len(candidates)}
		for _, candidate := range candidates {
			proxyURL, ok := candidateURL(candidate)
			if !ok {
				info.Unsupported++
				continue
			}
			all = append(all, proxyURL)
		}
		s.service.sourceMu.Lock()
		s.service.sources[key] = info
		s.service.sourceMu.Unlock()
	}
	return all
}

func candidateURL(candidate models.ProxyCandidate) (string, bool) {
	protocol := strings.ToLower(strings.TrimSpace(candidate.Protocol))
	host := strings.TrimSpace(candidate.Host)
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		host = host[1 : len(host)-1]
	}
	if protocol != "http" || host == "" || strings.ContainsAny(host, " \t\r\n/@?#[]") || candidate.Port < 1 || candidate.Port > 65535 {
		return "", false
	}
	if strings.Contains(host, ":") && net.ParseIP(host) == nil {
		return "", false
	}
	return "http://" + net.JoinHostPort(host, fmt.Sprint(candidate.Port)), true
}

func (s *Service) recordSourceFailure(key string, err error) {
	info := SourceInfo{Key: key, Name: sourceDisplayName(key), LastFetchAt: time.Now(), LastError: err.Error()}
	s.sourceMu.Lock()
	s.sources[key] = info
	s.sourceMu.Unlock()
	if s.log != nil {
		s.log.Warn("proxy source refresh failed", "source", key, "error", err)
	}
}
