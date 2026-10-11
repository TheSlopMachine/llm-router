package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/db/record"
	"github.com/TheSlopMachine/llm-router/internal/repository"
	"github.com/TheSlopMachine/llm-router/internal/services/luaplugin"
	"github.com/TheSlopMachine/llm-router/internal/services/pluginrepo"
	"github.com/TheSlopMachine/llm-router/internal/testutil"
)

// seedPluginRepo inserts one plugin_repositories row directly through the
// typed repository: the dashboard tests fixture repository state without
// network fetches.
func seedPluginRepo(t *testing.T, database *db.Database, identifier, kind, indexURL string) {
	t.Helper()
	rows := []record.PluginRepository{{
		ID:       identifier,
		Kind:     kind,
		IndexURL: indexURL,
		AddedAt:  time.Now().UTC(),
	}}
	if err := repository.NewGormRepository[record.PluginRepository](database.Handle, "id").Upsert(context.Background(), rows...); err != nil {
		t.Fatalf("seed repository: %v", err)
	}
}

// A repository that fails to list must still report files as an empty
// array, never null: the frontend reads entry.files.length unconditionally.
func TestStoreSearchErrorRepoReturnsEmptyFiles(t *testing.T) {
	database := testutil.SetupTestSQLite(t)
	repositoryService := pluginrepo.New(database)
	luaService, err := luaplugin.New(database, nil)
	if err != nil {
		t.Fatalf("lua service: %v", err)
	}
	seedPluginRepo(t, database, "index/unreachable", "index", "http://127.0.0.1:1/index.json")

	h := &Handler{repositoryService: repositoryService, luaService: luaService}
	request := httptest.NewRequest(http.MethodGet, "/api/llm-router/dashboard/plugin-store/search", nil)
	recorder := httptest.NewRecorder()
	h.apiStoreSearch(recorder, request)

	var body struct {
		Repos []map[string]json.RawMessage `json:"repos"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
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
	server := httptest.NewServer(mux)
	defer server.Close()

	database := testutil.SetupTestSQLite(t)
	repositoryService := pluginrepo.New(database)
	luaService, err := luaplugin.New(database, nil)
	if err != nil {
		t.Fatalf("lua service: %v", err)
	}
	seedPluginRepo(t, database, "index/upd", "index", server.URL+"/files/index.json")
	ctx := context.Background()
	installed := "--- @plugin Upd\n--- @author tester\n--- @version 1.0.0\n--- @plugin_api 1.0\n--- @allow_host old.example.com\n\nllm_router.register(\"upd-type\", {\n  complete = function() end,\n})\n"
	if _, err := luaService.InstallSQLite(ctx, []byte(installed), luaplugin.PluginOrigin{RepoID: "index/upd", Path: "llm-router-plugins/a.lua"}); err != nil {
		t.Fatalf("install: %v", err)
	}

	h := &Handler{repositoryService: repositoryService, luaService: luaService}
	request := httptest.NewRequest(http.MethodGet, "/api/llm-router/dashboard/plugin-store/updates", nil)
	recorder := httptest.NewRecorder()
	h.apiStoreUpdates(recorder, request)

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
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Updates) != 1 {
		t.Fatalf("updates: got %d, want 1", len(body.Updates))
	}
	update := body.Updates[0]
	if !update.Available || update.Current != "1.0.0" || update.Latest != "1.1.0" {
		t.Fatalf("version: %+v", update)
	}
	if len(update.NewAllowHosts) != 2 || update.NewAllowHosts[0] != "old.example.com" || update.NewAllowHosts[1] != "new.example.com" {
		t.Fatalf("new hosts: %+v", update)
	}
	if update.NewUnsafe {
		t.Fatalf("unsafe: %+v", update)
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
	server := httptest.NewServer(mux)
	defer server.Close()

	database := testutil.SetupTestSQLite(t)
	repositoryService := pluginrepo.New(database)
	luaService, err := luaplugin.New(database, nil)
	if err != nil {
		t.Fatalf("lua service: %v", err)
	}
	seedPluginRepo(t, database, "index/titled", "index", server.URL+"/files/index.json")

	h := &Handler{repositoryService: repositoryService, luaService: luaService}
	request := httptest.NewRequest(http.MethodGet, "/api/llm-router/dashboard/plugin-store/search", nil)
	recorder := httptest.NewRecorder()
	h.apiStoreSearch(recorder, request)

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
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Repos) != 1 {
		t.Fatalf("repos: got %d, want 1", len(body.Repos))
	}
	if body.Repos[0].Repo.Title != "Titled store" || body.Repos[0].Repo.Description != "Titled plugins" {
		t.Fatalf("repository view: %+v", body.Repos[0].Repo)
	}
	if len(body.Repos[0].Files) != 1 || body.Repos[0].Files[0].Path != "llm-router-plugins/a.lua" {
		t.Fatalf("files: %+v", body.Repos[0].Files)
	}
}
