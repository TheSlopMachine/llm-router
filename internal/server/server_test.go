package server

import (
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

func startListener(t *testing.T, handler http.Handler) (string, *http.Server) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	created := &http.Server{Handler: handler}
	go func() {
		_ = created.Serve(listener)
	}()
	t.Cleanup(func() {
		_ = created.Close()
	})
	return "http://" + listener.Addr().String(), created
}

func TestShutdownListenersIdle(t *testing.T) {
	address, publicServer := startListener(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	_, adminServer := startListener(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	response, err := http.Get(address)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
	start := time.Now()
	shutdownListeners(publicServer, adminServer, nil, 5*time.Second)
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("idle shutdown took %v", elapsed)
	}
}

func TestShutdownListenersStuckRequest(t *testing.T) {
	entered := make(chan struct{})
	var once bool
	address, publicServer := startListener(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !once {
			once = true
			close(entered)
		}
		select {}
	}))
	go func() {
		response, err := http.Get(address)
		if err != nil {
			return
		}
		_, _ = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("handler never ran")
	}
	start := time.Now()
	shutdownListeners(publicServer, nil, nil, 300*time.Millisecond)
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("stuck shutdown took %v", elapsed)
	}
}
