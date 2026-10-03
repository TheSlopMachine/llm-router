package models

import (
	"testing"
)

func TestProxyPoolDefaults(t *testing.T) {
	if DefaultProxyPool == "" {
		t.Fatal("default proxy pool must be named")
	}
	ref, err := ParseProxyPoolRef(map[string]any{"proxy": "not-a-map"})
	if err != nil || ref.Pool != "" {
		t.Fatalf("non-map proxy section: %+v %v", ref, err)
	}
}
