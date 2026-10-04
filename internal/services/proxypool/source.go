package proxypool

import (
	"context"
	"time"
)

// pluginSource bridges registered Lua proxy-source feeds into the library
// pool. Only http/https/socks4/socks5 candidates are accepted; anything
// else counts as unsupported. Feeds back databases with 304-style responses
// by returning no candidates.
type pluginSource struct {
	service *Service
}

// Name identifies the aggregate Lua feed to the library.
func (p *pluginSource) Name() string { return "lua-feeds" }

// FetchList aggregates every registered Lua feed into raw proxy URLs.
func (p *pluginSource) FetchList() []string {
	s := p.service
	s.mu.Lock()
	keys := s.keys
	fetch := s.fetch
	ctx := s.rootCtx
	s.mu.Unlock()
	if keys == nil || fetch == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	list, err := keys()
	if err != nil {
		return nil
	}
	var out []string
	for _, key := range list {
		cands, ferr := fetch(ctx, key)
		info := SourceInfo{Key: key, Name: sourceDisplayName(key), LastFetchAt: time.Now(), Total: len(cands)}
		if ferr != nil {
			info.LastError = ferr.Error()
			s.recordSource(info)
			continue
		}
		for _, c := range cands {
			if u, ok := candidateURL(c); ok {
				out = append(out, u)
			} else {
				info.Unsupported++
			}
		}
		s.recordSource(info)
	}
	return out
}

func (s *Service) recordSource(info SourceInfo) {
	s.sourceMu.Lock()
	s.sources[info.Key] = info
	s.sourceMu.Unlock()
}
