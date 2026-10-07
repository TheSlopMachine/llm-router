package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	bolt "go.etcd.io/bbolt"

	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/services/luaplugin"
	"github.com/TheSlopMachine/llm-router/internal/services/pluginrepo"
	"github.com/TheSlopMachine/llm-router/internal/testutil"
)

// A repository that fails to list must still report files as an empty
// array, never null: the frontend reads entry.files.length unconditionally.
func TestStoreSearchErrorRepoReturnsEmptyFiles(t *testing.T) {
	database := testutil.SetupTestDB(t)
	repoSvc := pluginrepo.New(database)
	luaSvc, err := luaplugin.New(database, nil)
	if err != nil {
		t.Fatalf("lua service: %v", err)
	}
	rec := pluginrepo.RepoRecord{ID: "index/unreachable", Kind: "index", IndexURL: "http://127.0.0.1:1/index.json"}
	raw, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := database.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(db.BucketPluginRepos).Put([]byte(rec.ID), raw)
	}); err != nil {
		t.Fatalf("put repo: %v", err)
	}

	h := &Handler{repoSvc: repoSvc, luaSvc: luaSvc}
	req := httptest.NewRequest(http.MethodGet, "/api/llm-router/dashboard/plugin-store/search", nil)
	resp := httptest.NewRecorder()
	h.apiStoreSearch(resp, req)

	var body struct {
		Repos []map[string]json.RawMessage `json:"repos"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Repos) != 1 {
		t.Fatalf("repos: got %d, want 1", len(body.Repos))
	}
	if got := strings.TrimSpace(string(body.Repos[0]["files"])); got != "[]" {
		t.Fatalf("files: got %s, want []", got)
	}
	if len(body.Repos[0]["error"]) <= 2 {
		t.Fatalf("error must describe the failure, got %s", body.Repos[0]["error"])
	}
}

// Updates expose the upstream allow list so the UI confirms host changes.
func TestStoreUpdatesExposeNewAllowHosts(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/files/index.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"title":"Upd store","description":"Upd plugins","plugins":["a.lua"]}`))
	})
	mux.HandleFunc("/files/a.lua", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("--- @plugin Upd\n--- @author tester\n--- @version 1.1.0\n--- @plugin_api 1.0\n--- @allow_host old.example.com\n--- @allow_host new.example.com\n\nllm_router.register(\"upd-type\", {\n  complete = function() end,\n})\n"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	database := testutil.SetupTestDB(t)
	repoSvc := pluginrepo.New(database)
	luaSvc, err := luaplugin.New(database, nil)
	if err != nil {
		t.Fatalf("lua service: %v", err)
	}
	rec := pluginrepo.RepoRecord{ID: "index/upd", Kind: "index", IndexURL: srv.URL + "/files/index.json"}
	raw, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := database.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(db.BucketPluginRepos).Put([]byte(rec.ID), raw)
	}); err != nil {
		t.Fatalf("put repo: %v", err)
	}
	installed := "--- @plugin Upd\n--- @author tester\n--- @version 1.0.0\n--- @plugin_api 1.0\n--- @allow_host old.example.com\n\nllm_router.register(\"upd-type\", {\n  complete = function() end,\n})\n"
	if _, err := luaSvc.Install([]byte(installed), luaplugin.PluginOrigin{RepoID: rec.ID, Path: "llm-router-plugins/a.lua"}); err != nil {
		t.Fatalf("install: %v", err)
	}

	h := &Handler{repoSvc: repoSvc, luaSvc: luaSvc}
	req := httptest.NewRequest(http.MethodGet, "/api/llm-router/dashboard/plugin-store/updates", nil)
	resp := httptest.NewRecorder()
	h.apiStoreUpdates(resp, req)

	var body struct {
		Updates []struct {
			PluginID      string   `json:"plugin_id"`
			Current       string   `json:"current"`
			Latest        string   `json:"latest"`
			NewAllowHosts []string `json:"new_allow_hosts"`
			NewUnsafe     bool     `json:"new_unsafe"`
			Available     bool     `json:"update_available"`
		} `json:"updates"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Updates) != 1 {
		t.Fatalf("updates: got %d, want 1", len(body.Updates))
	}
	u := body.Updates[0]
	if !u.Available || u.Current != "1.0.0" || u.Latest != "1.1.0" {
		t.Fatalf("version: %+v", u)
	}
	if len(u.NewAllowHosts) != 2 || u.NewAllowHosts[0] != "old.example.com" || u.NewAllowHosts[1] != "new.example.com" {
		t.Fatalf("new hosts: %+v", u)
	}
	if u.NewUnsafe {
		t.Fatalf("unsafe: %+v", u)
	}
}

// Search returns live index titles and descriptions with the files.
func TestStoreSearchReturnsIndexTitle(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/files/index.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"title":"Titled store","description":"Titled plugins","plugins":["a.lua"]}`))
	})
	mux.HandleFunc("/files/a.lua", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("--- @plugin Titled\n--- @author tester\n--- @version 1.0.0\n--- @plugin_api 1.0\n--- @allow_host example.com\n\nllm_router.register(\"titled-type\", {\n  complete = function() end,\n})\n"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	database := testutil.SetupTestDB(t)
	repoSvc := pluginrepo.New(database)
	luaSvc, err := luaplugin.New(database, nil)
	if err != nil {
		t.Fatalf("lua service: %v", err)
	}
	rec := pluginrepo.RepoRecord{ID: "index/titled", Kind: "index", IndexURL: srv.URL + "/files/index.json"}
	raw, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := database.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(db.BucketPluginRepos).Put([]byte(rec.ID), raw)
	}); err != nil {
		t.Fatalf("put repo: %v", err)
	}

	h := &Handler{repoSvc: repoSvc, luaSvc: luaSvc}
	req := httptest.NewRequest(http.MethodGet, "/api/llm-router/dashboard/plugin-store/search", nil)
	resp := httptest.NewRecorder()
	h.apiStoreSearch(resp, req)

	var body struct {
		Repos []struct {
			Repo struct {
				Title       string `json:"title"`
				Description string `json:"description"`
			} `json:"repo"`
			Files []struct {
				Path string `json:"path"`
			} `json:"files"`
		} `json:"repos"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Repos) != 1 {
		t.Fatalf("repos: got %d, want 1", len(body.Repos))
	}
	if body.Repos[0].Repo.Title != "Titled store" || body.Repos[0].Repo.Description != "Titled plugins" {
		t.Fatalf("repo view: %+v", body.Repos[0].Repo)
	}
	if len(body.Repos[0].Files) != 1 || body.Repos[0].Files[0].Path != "llm-router-plugins/a.lua" {
		t.Fatalf("files: %+v", body.Repos[0].Files)
	}
}
