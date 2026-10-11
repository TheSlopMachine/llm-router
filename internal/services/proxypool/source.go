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
func (source *pluginSource) Name() string { return "lua-feeds" }

// FetchTagged aggregates registered Lua feeds with per-source identity.
func (source *pluginSource) FetchTagged() []proxypoollib.TaggedURL {
	service := source.service
	service.mu.Lock()
	keys := service.keys
	fetch := service.fetch
	ctx := service.rootCtx
	service.mu.Unlock()
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
	service.sourceMu.Lock()
	for key := range service.sources {
		if _, ok := active[key]; !ok {
			delete(service.sources, key)
		}
	}
	service.sourceMu.Unlock()
	var output []proxypoollib.TaggedURL
	for _, key := range list {
		candidates, fetchError := fetch(ctx, key)
		sourceInfo := SourceInfo{Key: key, Name: sourceDisplayName(key), LastFetchAt: time.Now(), Total: len(candidates)}
		if fetchError != nil {
			sourceInfo.LastError = fetchError.Error()
			service.recordSource(sourceInfo)
			continue
		}
		for _, candidate := range candidates {
			if validatedURL, ok := candidateURL(candidate); ok {
				output = append(output, proxypoollib.TaggedURL{URL: validatedURL, Source: sourceInfo.Name, Country: candidate.Country})
			} else {
				sourceInfo.Unsupported++
			}
		}
		service.recordSource(sourceInfo)
	}
	return output
}

// FetchList preserves the library ProxySource contract for callers that do not
// use TaggedSource.
func (source *pluginSource) FetchList() []string {
	tagged := source.FetchTagged()
	output := make([]string, 0, len(tagged))
	for _, item := range tagged {
		output = append(output, item.URL)
	}
	return output
}

func (s *Service) recordSource(sourceInfo SourceInfo) {
	s.sourceMu.Lock()
	s.sources[sourceInfo.Key] = sourceInfo
	s.sourceMu.Unlock()
}
