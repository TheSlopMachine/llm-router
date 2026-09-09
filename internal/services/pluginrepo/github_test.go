package pluginrepo

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func newRawTestServer() *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/o/r/main/llm-router-plugins/index.json", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	mux.HandleFunc("/o/r/master/llm-router-plugins/index.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"plugins":["a.lua","b.txt","sub/c.lua"," a.lua ",""]}`))
	})
	mux.HandleFunc("/o/r/master/llm-router-plugins/a.lua", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("-- plugin a"))
	})
	mux.HandleFunc("/o/r/master/README.md", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("# readme"))
	})
	mux.HandleFunc("/o/empty/main/llm-router-plugins/index.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"plugins":[]}`))
	})
	mux.HandleFunc("/o/empty/master/llm-router-plugins/index.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"plugins":[]}`))
	})
	mux.HandleFunc("/o/broken/main/llm-router-plugins/index.json", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`not json`))
	})
	return httptest.NewServer(mux)
}

func newRawTestProvider(srv *httptest.Server) *githubProvider {
	return &githubProvider{client: srv.Client(), rawBase: srv.URL, branches: map[string]string{}}
}

func TestGitHubListFiltersAndFallback(t *testing.T) {
	srv := newRawTestServer()
	defer srv.Close()
	g := newRawTestProvider(srv)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	files, err := g.ListPluginFiles(ctx, RepoRef{ID: "github/o/r", Kind: "github", Owner: "o", Repo: "r"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(files) != 1 || files[0].Path != "llm-router-plugins/a.lua" {
		t.Fatalf("files: %+v", files)
	}
	if g.branches["github/o/r"] != "master" {
		t.Fatalf("resolved branch: %q", g.branches["github/o/r"])
	}
}

func TestGitHubFetchFile(t *testing.T) {
	srv := newRawTestServer()
	defer srv.Close()
	g := newRawTestProvider(srv)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ref := RepoRef{ID: "github/o/r", Kind: "github", Owner: "o", Repo: "r"}

	body, err := g.FetchFile(ctx, ref, "llm-router-plugins/a.lua")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if string(body) != "-- plugin a" {
		t.Fatalf("body: %q", body)
	}
	if _, err := g.FetchFile(ctx, ref, "other/a.lua"); err == nil {
		t.Fatalf("expected error for path outside llm-router-plugins/")
	}
	if _, err := g.FetchFile(ctx, ref, "llm-router-plugins/sub/a.lua"); err == nil {
		t.Fatalf("expected error for nested path")
	}
	if _, err := g.FetchFile(ctx, ref, "llm-router-plugins/missing.lua"); err == nil {
		t.Fatalf("expected error for missing file")
	}
}

func TestGitHubReadMeLicense(t *testing.T) {
	srv := newRawTestServer()
	defer srv.Close()
	g := newRawTestProvider(srv)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ref := RepoRef{ID: "github/o/r", Kind: "github", Owner: "o", Repo: "r"}

	content, ok, err := g.ReadMe(ctx, ref)
	if err != nil || !ok || content != "# readme" {
		t.Fatalf("readme: %q %v %v", content, ok, err)
	}
	if _, ok, _ := g.License(ctx, ref); ok {
		t.Fatalf("license must be absent")
	}
}

func TestGitHubIndexErrors(t *testing.T) {
	srv := newRawTestServer()
	defer srv.Close()
	g := newRawTestProvider(srv)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := g.ListPluginFiles(ctx, RepoRef{ID: "github/o/missing", Kind: "github", Owner: "o", Repo: "missing"}); err == nil {
		t.Fatalf("expected error for missing index")
	}
	if _, err := g.ListPluginFiles(ctx, RepoRef{ID: "github/o/empty", Kind: "github", Owner: "o", Repo: "empty"}); err == nil {
		t.Fatalf("expected error for empty index")
	}
	if _, err := g.ListPluginFiles(ctx, RepoRef{ID: "github/o/broken", Kind: "github", Owner: "o", Repo: "broken"}); err == nil {
		t.Fatalf("expected error for broken index")
	}
}
