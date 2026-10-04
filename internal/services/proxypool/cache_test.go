package proxypool

import (
	"fmt"
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/testutil"
	proxypoollib "github.com/TheSlopMachine/proxypool"
)

func TestDBCacheBuffersUntilFlush(t *testing.T) {
	service, err := New(testutil.SetupTestDB(t))
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	service.cache.Set(proxypoollib.ProxyState{URL: "http://192.0.2.10:8080", IP: "192.0.2.10", Port: 8080})
	service.cache.Set(proxypoollib.ProxyState{URL: "socks5://192.0.2.11:1080", IP: "192.0.2.11", Port: 1080})

	if got := len(service.cache.All()); got != 2 {
		t.Fatalf("All holds %d states, want 2", got)
	}
	stored, err := service.cache.repo.List()
	if err != nil {
		t.Fatalf("repo list: %v", err)
	}
	if len(stored) != 0 {
		t.Fatalf("bbolt holds %d rows before flush, want 0", len(stored))
	}

	if err := service.cache.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}
	stored, err = service.cache.repo.List()
	if err != nil {
		t.Fatalf("repo list: %v", err)
	}
	if len(stored) != 2 {
		t.Fatalf("bbolt holds %d rows after flush, want 2", len(stored))
	}

	if err := service.cache.Flush(); err != nil {
		t.Fatalf("empty flush: %v", err)
	}
	stored, err = service.cache.repo.List()
	if err != nil {
		t.Fatalf("repo list: %v", err)
	}
	if len(stored) != 2 {
		t.Fatalf("bbolt holds %d rows after empty flush, want 2", len(stored))
	}
}

func TestDBCacheClearDropsDirty(t *testing.T) {
	service, err := New(testutil.SetupTestDB(t))
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	service.cache.Set(proxypoollib.ProxyState{URL: "http://192.0.2.12:8080", IP: "192.0.2.12", Port: 8080})
	service.cache.Clear()
	if err := service.cache.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if got := len(service.cache.All()); got != 0 {
		t.Fatalf("All holds %d states, want 0", got)
	}
	stored, err := service.cache.repo.List()
	if err != nil {
		t.Fatalf("repo list: %v", err)
	}
	if len(stored) != 0 {
		t.Fatalf("bbolt holds %d rows, want 0", len(stored))
	}
}

func TestDBCacheFlushChunks(t *testing.T) {
	service, err := New(testutil.SetupTestDB(t))
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	const total = 2*flushChunkSize + 1
	for i := 0; i < total; i++ {
		url := fmt.Sprintf("http://192.0.2.1:%d", 10000+i)
		service.cache.Set(proxypoollib.ProxyState{URL: url, IP: "192.0.2.1", Port: 10000 + i})
	}
	if err := service.cache.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}
	stored, err := service.cache.repo.List()
	if err != nil {
		t.Fatalf("repo list: %v", err)
	}
	if len(stored) != total {
		t.Fatalf("bbolt holds %d rows, want %d", len(stored), total)
	}
}
