package proxypool

import (
	"context"
	"time"

	proxypoollib "github.com/TheSlopMachine/proxypool"
)

// pluginSource bridges registered Lua proxy-source feeds into the library
// pool. Only http/https/socks4/socks5 candidates are accepted; anything else
// counts as unsupported. Feeds back databases with 304-style responses
// by returning no candidates.
type pluginSource struct {
	service *Service
}

// Name identifies the aggregate Lua feed to the library.
func (p *pluginSource) Name() string { return "lua-feeds" }

// FetchTagged aggregates registered Lua feeds with per-source identity.
func (p *pluginSource) FetchTagged() []proxypoollib.TaggedURL {
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
	active := make(map[string]struct{}, len(list))
	for _, key := range list {
		active[key] = struct{}{}
	}
	s.sourceMu.Lock()
	for key := range s.sources {
		if _, ok := active[key]; !ok {
			delete(s.sources, key)
		}
	}
	s.sourceMu.Unlock()
	var out []proxypoollib.TaggedURL
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
				out = append(out, proxypoollib.TaggedURL{URL: u, Source: info.Name})
			} else {
				info.Unsupported++
			}
		}
		s.recordSource(info)
	}
	return out
}

// FetchList preserves the library ProxySource contract for callers that do not
// use TaggedSource.
func (p *pluginSource) FetchList() []string {
	tagged := p.FetchTagged()
	out := make([]string, 0, len(tagged))
	for _, item := range tagged {
		out = append(out, item.URL)
	}
	return out
}

func (s *Service) recordSource(info SourceInfo) {
	s.sourceMu.Lock()
	s.sources[info.Key] = info
	s.sourceMu.Unlock()
}
