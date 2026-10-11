package provider

// RegisterGoAdapter registers a built-in Go backend.
func (s *Service) RegisterGoAdapter(adapter GoAdapter) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.goAdapters[adapter.TypeKey()] = adapter
}

// GoAdapterFor returns the Go backend for a type key, if one is registered.
func (s *Service) GoAdapterFor(typeKey string) (GoAdapter, bool) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	adapter, ok := s.goAdapters[typeKey]
	return adapter, ok
}
