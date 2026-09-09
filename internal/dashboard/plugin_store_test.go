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
	rec := pluginrepo.RepoRecord{ID: "generic/unreachable", Kind: "generic-index", IndexURL: "http://127.0.0.1:1/index.json"}
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
