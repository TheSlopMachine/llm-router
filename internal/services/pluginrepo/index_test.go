package pluginrepo

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/testutil"
)

func TestResolveIndexCandidates(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{
			name: "direct index url",
			in:   "https://example.com/files/index.json",
			want: []string{"https://example.com/files/index.json"},
		},
		{
			name: "github repo",
			in:   "https://github.com/o/r",
			want: []string{
				"https://raw.githubusercontent.com/o/r/main/llm-router-plugins/index.json",
				"https://raw.githubusercontent.com/o/r/master/llm-router-plugins/index.json",
			},
		},
		{
			name: "github repo with git suffix and slash",
			in:   "https://github.com/o/r.git/",
			want: []string{
				"https://raw.githubusercontent.com/o/r/main/llm-router-plugins/index.json",
				"https://raw.githubusercontent.com/o/r/master/llm-router-plugins/index.json",
			},
		},
		{
			name: "gitlab subgroups",
			in:   "https://gitlab.com/g/sub/r",
			want: []string{
				"https://gitlab.com/g/sub/r/-/raw/main/llm-router-plugins/index.json",
				"https://gitlab.com/g/sub/r/-/raw/master/llm-router-plugins/index.json",
			},
		},
		{
			name: "bitbucket",
			in:   "https://bitbucket.org/o/r",
			want: []string{
				"https://bitbucket.org/o/r/raw/main/llm-router-plugins/index.json",
				"https://bitbucket.org/o/r/raw/master/llm-router-plugins/index.json",
			},
		},
		{
			name: "codeberg",
			in:   "https://codeberg.org/o/r",
			want: []string{
				"https://codeberg.org/o/r/raw/branch/main/llm-router-plugins/index.json",
				"https://codeberg.org/o/r/raw/branch/master/llm-router-plugins/index.json",
			},
		},
	}
	for _, tc := range cases {
		got, err := resolveIndexCandidates(tc.in)
		if err != nil {
			t.Errorf("%s: unexpected error: %v", tc.name, err)
			continue
		}
		if len(got) != len(tc.want) {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
				break
			}
		}
	}

	invalid := []string{
		"",
		"not a url",
		"ftp://example.com/index.json",
		"https://github.com/owner",
		"https://github.com/o/r/tree/main/x",
		"https://example.com/files/plugins.json",
		"https://unknown.example/o/r",
	}
	for _, in := range invalid {
		if _, err := resolveIndexCandidates(in); err == nil {
			t.Errorf("resolve(%q): expected error", in)
		}
	}
}

func TestRepoIDForIndexStable(t *testing.T) {
	a := repoIDForIndex("https://example.com/files/index.json")
	b := repoIDForIndex("https://example.com/files/index.json")
	if a != b || !strings.HasPrefix(a, "index/") {
		t.Fatalf("unstable id: %q %q", a, b)
	}
	c := repoIDForIndex("https://example.com/other/index.json")
	if a == c {
		t.Fatalf("colliding ids: %q", a)
	}
}

func newIndexTestServer() *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/files/index.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"plugins":["a.lua","b.txt","sub/c.lua"," a.lua ",""]}`))
	})
	mux.HandleFunc("/files/a.lua", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("-- plugin a"))
	})
	mux.HandleFunc("/files/README.md", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("# dir readme"))
	})
	mux.HandleFunc("/README.md", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("# parent readme"))
	})
	mux.HandleFunc("/LICENSE", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("parent license"))
	})
	return httptest.NewServer(mux)
}

func TestIndexProvider(t *testing.T) {
	srv := newIndexTestServer()
	defer srv.Close()
	p := &indexProvider{client: srv.Client()}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ref := RepoRef{ID: "index/test", IndexURL: srv.URL + "/files/index.json"}

	files, err := p.ListPluginFiles(ctx, ref)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(files) != 1 || files[0].Path != "llm-router-plugins/a.lua" {
		t.Fatalf("files: %+v", files)
	}
	if files[0].URL != srv.URL+"/files/a.lua" {
		t.Fatalf("url: %q", files[0].URL)
	}

	body, err := p.FetchFile(ctx, ref, "llm-router-plugins/a.lua")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if string(body) != "-- plugin a" {
		t.Fatalf("body: %q", body)
	}
	if _, err := p.FetchFile(ctx, ref, "other/a.lua"); err == nil {
		t.Fatalf("expected error for path outside llm-router-plugins/")
	}
	if _, err := p.FetchFile(ctx, ref, "llm-router-plugins/missing.lua"); err == nil {
		t.Fatalf("expected error for file missing from index")
	}

	content, ok, err := p.ReadMe(ctx, ref)
	if err != nil || !ok || content != "# dir readme" {
		t.Fatalf("readme: %q %v %v", content, ok, err)
	}
	content, ok, err = p.License(ctx, ref)
	if err != nil || !ok || content != "parent license" {
		t.Fatalf("license: %q %v %v", content, ok, err)
	}
}

func TestAddRepoDirectIndex(t *testing.T) {
	srv := newIndexTestServer()
	defer srv.Close()
	svc := New(testutil.SetupTestDB(t))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	raw := srv.URL + "/files/index.json"
	rec, err := svc.AddRepo(ctx, raw)
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if rec.Kind != "index" || rec.IndexURL != raw || rec.SourceURL != raw {
		t.Fatalf("record: %+v", rec)
	}
	again, err := svc.AddRepo(ctx, raw)
	if err != nil {
		t.Fatalf("re-add: %v", err)
	}
	if again.ID != rec.ID {
		t.Fatalf("re-add changed id: %q -> %q", rec.ID, again.ID)
	}
	if _, err := svc.AddRepo(ctx, srv.URL+"/files/plugins.json"); err == nil {
		t.Fatalf("expected error for non-index url on unknown host")
	}
}
