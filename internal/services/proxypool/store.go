package proxypool

import (
	"sort"
	"strings"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/util"
)

// Get returns one proxy.
func (s *Service) Get(id string) (*models.Proxy, error) {
	return s.proxies.Get(id)
}

// Delete removes a proxy.
func (s *Service) Delete(id string) error {
	if err := s.proxies.Delete(id); err != nil {
		return err
	}
	s.notify()
	return nil
}

// List returns all proxies, manual first, then fastest first.
func (s *Service) List() ([]*models.Proxy, error) {
	all, err := s.proxies.List()
	if err != nil {
		return nil, err
	}
	sort.Slice(all, func(i, j int) bool {
		mi, mj := all[i].Source == ManualSource, all[j].Source == ManualSource
		if mi != mj {
			return mi
		}
		return lessProxy(all[i], all[j])
	})
	return all, nil
}

// PoolTotals reports the pooled proxy count.
func (s *Service) PoolTotals() (int, error) {
	all, err := s.proxies.List()
	if err != nil {
		return 0, err
	}
	return len(all), nil
}

// SourceProxies returns pooled proxies of one source, fastest first.
func (s *Service) SourceProxies(sourceKey string, offset, limit int) ([]*models.Proxy, int, error) {
	source := ListSource(sourceKey)
	pooled, err := s.proxies.ListFiltered(func(p *models.Proxy) bool {
		return p.Source == source
	})
	if err != nil {
		return nil, 0, err
	}
	sort.Slice(pooled, func(i, j int) bool { return lessProxy(pooled[i], pooled[j]) })
	total := len(pooled)
	if offset > total {
		offset = total
	}
	end := offset + limit
	if limit <= 0 || end > total {
		end = total
	}
	page := pooled[offset:end]
	if page == nil {
		page = []*models.Proxy{}
	}
	return page, total, nil
}

// SourceInfos snapshots the runtime state of the given source keys.
// Status copies take the lock; bucket reads run unlocked so slow storage
// never stalls ranking waiters.
func (s *Service) SourceInfos(keys []string) []SourceInfo {
	s.mu.Lock()
	status := make(map[string]sourceState, len(keys))
	for _, key := range keys {
		if st, ok := s.sourceStatus[ListSource(key)]; ok {
			status[key] = *st
		}
	}
	s.mu.Unlock()
	out := make([]SourceInfo, 0, len(keys))
	for _, key := range keys {
		info := SourceInfo{Key: key, Name: sourceDisplayName(key), Status: SourceStatusIdle}
		if st, ok := status[key]; ok {
			info.Status = st.status
			info.Total = st.total
			info.LastFetchAt = st.lastFetchAt
			info.LastError = st.lastError
		} else if meta, err := s.meta.Get(ListSource(key)); err == nil && meta != nil {
			info.Total = meta.Total
			info.LastFetchAt = meta.LastFetchAt
			info.LastError = meta.LastError
		}
		pooled, err := s.proxies.ListFiltered(func(p *models.Proxy) bool {
			return p.Source == ListSource(key)
		})
		if err == nil {
			info.Pooled = len(pooled)
		}
		out = append(out, info)
	}
	return out
}

// sourceDisplayName is the bare declared name of a (possibly qualified)
// source key: the segment after the last slash. Declared names never
// contain slashes, so this is exact, not heuristic.
func sourceDisplayName(key string) string {
	if idx := strings.LastIndex(key, "/"); idx != -1 {
		return key[idx+1:]
	}
	return key
}

func (s *Service) setSourceStatus(source, status string, total int, lastError string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.sourceStatus[source]
	if !ok {
		st = &sourceState{}
		s.sourceStatus[source] = st
	}
	st.status = status
	st.total = total
	st.lastError = lastError
	if status != SourceStatusIdle {
		st.lastFetchAt = util.Now()
	}
}

// RekeySource moves pooled proxies and fetch metadata from one source tag
// to another. Used once when source keys change scheme: rows keep serving
// under the new key instead of orphaning.
func (s *Service) RekeySource(oldSource, newSource string) error {
	if oldSource == newSource {
		return nil
	}
	pooled, err := s.proxies.ListFiltered(func(p *models.Proxy) bool {
		return p.Source == oldSource
	})
	if err != nil {
		return err
	}
	for _, p := range pooled {
		p.Source = newSource
		if err := s.proxies.Put(p.ID, p); err != nil {
			return err
		}
	}
	if meta, err := s.meta.Get(oldSource); err == nil && meta != nil {
		if err := s.meta.Put(newSource, meta); err != nil {
			return err
		}
		_ = s.meta.Delete(oldSource)
	}
	return nil
}

// StoredSources lists distinct source tags present in pooled rows.
// Used once to find legacy keys needing a scheme migration. Stale fetch
// metadata without pooled rows needs no migration: offsets restart from
// zero, which only refetches one window.
func (s *Service) StoredSources() ([]string, error) {
	pooled, err := s.proxies.List()
	if err != nil {
		return nil, err
	}
	set := map[string]bool{}
	for _, p := range pooled {
		set[p.Source] = true
	}
	out := make([]string, 0, len(set))
	for source := range set {
		out = append(out, source)
	}
	sort.Strings(out)
	return out, nil
}

// SetSourceRotating marks a source pool check in progress for the dashboard.
func (s *Service) SetSourceRotating(sourceKey string) {
	s.setSourceStatus(ListSource(sourceKey), SourceStatusRotating, 0, "")
}

// SetSourceDone returns a source to idle without an error, keeping the
// fetched total. Used when the adds succeeded but a later step did not run.
func (s *Service) SetSourceDone(sourceKey string, total int) {
	s.setSourceStatus(ListSource(sourceKey), SourceStatusIdle, total, "")
}

// SetSourceFetching marks a source fetch in progress for the dashboard.
func (s *Service) SetSourceFetching(sourceKey string) {
	s.setSourceStatus(ListSource(sourceKey), SourceStatusFetching, 0, "")
}

// SetSourceFailed records a source fetch error for the dashboard.
func (s *Service) SetSourceFailed(sourceKey string, err error) {
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	s.setSourceStatus(ListSource(sourceKey), SourceStatusIdle, 0, msg)
	_ = s.meta.Put(ListSource(sourceKey), &sourceFetchMeta{LastFetchAt: util.Now(), LastError: msg})
}
