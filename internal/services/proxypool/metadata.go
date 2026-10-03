package proxypool

import (
	"encoding/json"
	"fmt"
	"time"

	proxypoollib "github.com/TheSlopMachine/proxypool"
)

const limitMetadataPrefix = "llm-router.limit."

type proxyLimit struct {
	ResetsAt time.Time `json:"resets_at"`
	Reason   string    `json:"reason,omitempty"`
}

// MarkLimit stores one proxy-scoped limit in the library-managed metadata.
func (s *Service) MarkLimit(id, key string, resetsAt time.Time, reason string) error {
	if id == "" || key == "" {
		return fmt.Errorf("proxy limit requires proxy ID and key")
	}
	state, ok, err := s.stateForID(id)
	if err != nil {
		return fmt.Errorf("load proxy limit state: %w", err)
	}
	if !ok {
		return fmt.Errorf("proxy %q not found", id)
	}
	encoded, err := json.Marshal(proxyLimit{ResetsAt: resetsAt, Reason: reason})
	if err != nil {
		return fmt.Errorf("encode proxy limit: %w", err)
	}
	s.refreshMu.Lock()
	s.pool.UpdateMetadata(state.URL, func(metadata map[string]string) {
		metadata[limitMetadataPrefix+key] = string(encoded)
	})
	err = s.cache.takeError()
	s.refreshMu.Unlock()
	if err != nil {
		return fmt.Errorf("persist proxy limit: %w", err)
	}
	return nil
}

// IsLimited checks and lazily removes an expired proxy-scoped limit.
func (s *Service) IsLimited(id, key string, now time.Time) (bool, error) {
	_, limited, err := s.limitExpiry(id, key, now)
	return limited, err
}

// limitExpiry reports the reset time of one proxy-scoped limit key.
func (s *Service) limitExpiry(id, key string, now time.Time) (time.Time, bool, error) {
	state, ok, err := s.stateForID(id)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("load proxy limit state: %w", err)
	}
	if !ok {
		return time.Time{}, false, fmt.Errorf("proxy %q not found", id)
	}
	metadata := s.pool.GetMetadata(state.URL)
	metadataKey := limitMetadataPrefix + key
	raw, ok := metadata[metadataKey]
	if !ok {
		return time.Time{}, false, nil
	}
	var limit proxyLimit
	if err := json.Unmarshal([]byte(raw), &limit); err != nil {
		return time.Time{}, false, fmt.Errorf("decode proxy limit: %w", err)
	}
	if now.Before(limit.ResetsAt) {
		return limit.ResetsAt, true, nil
	}
	s.refreshMu.Lock()
	s.pool.UpdateMetadata(state.URL, func(values map[string]string) {
		delete(values, metadataKey)
	})
	err = s.cache.takeError()
	s.refreshMu.Unlock()
	if err != nil {
		return time.Time{}, false, fmt.Errorf("remove expired proxy limit: %w", err)
	}
	return time.Time{}, false, nil
}

func (s *Service) stateForID(id string) (proxypoollib.ProxyState, bool, error) {
	if err := s.cache.peekError(); err != nil {
		return proxypoollib.ProxyState{}, false, err
	}
	for _, state := range s.cache.All() {
		if proxyID(state.URL) == id {
			return state, true, nil
		}
	}
	return proxypoollib.ProxyState{}, false, nil
}
