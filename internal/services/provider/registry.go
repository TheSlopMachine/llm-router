package provider

// RegisterGoAdapter registers a built-in Go backend.
func (s *Service) RegisterGoAdapter(a GoAdapter) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.goAdapters[a.TypeKey()] = a
}

// GoAdapterFor returns the Go backend for a type key, if one is registered.
func (s *Service) GoAdapterFor(typeKey string) (GoAdapter, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	a, ok := s.goAdapters[typeKey]
	return a, ok
}
