package doctor

import (
	"testing"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/credential"
	"github.com/TheSlopMachine/llm-router/internal/services/geoban"
	"github.com/TheSlopMachine/llm-router/internal/services/luaplugin"
	"github.com/TheSlopMachine/llm-router/internal/services/modelinfo"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
	"github.com/TheSlopMachine/llm-router/internal/services/token"
	"github.com/TheSlopMachine/llm-router/internal/services/virtual"
	"github.com/TheSlopMachine/llm-router/internal/testutil"
	bolt "go.etcd.io/bbolt"
)

const docPluginSource = `--- @plugin Doc Plugin
--- @author tester
--- @version 1.0.0
--- @router_version 0.3.0
--- @allow_host example.com

llm_router.register("doc-type", {
  complete = function(ctx, credential, request)
    return nil, { type = "upstream", message = "doc stub" }
  end,
})
`

// docStack wires a doctor service with real stores: one qualified provider
// (ID "doc-type:eu") carrying an override whose model name holds a slash,
// plus one installed plugin. Both shapes once false-flagged as orphans.
func docStack(t *testing.T) (*Service, string, string) {
	t.Helper()
	database := testutil.SetupTestDB(t)
	providerSvc := provider.NewService(database)
	luaSvc, err := luaplugin.New(database, nil)
	if err != nil {
		t.Fatalf("new plugin service: %v", err)
	}
	if _, err := luaSvc.Install([]byte(docPluginSource), luaplugin.PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install plugin: %v", err)
	}
	rec, err := luaSvc.Lookup("doc-type")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	inst, err := providerSvc.Create(provider.CreateOptions{Name: "Doc EU", TypeKey: "doc-type", Qualifier: "EU"})
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}
	credSvc := credential.New(database, providerSvc)
	modelInfoSvc := modelinfo.New(database, providerSvc, credSvc, time.Hour)
	if err := modelInfoSvc.SetOverride(models.ModelOverride{
		ProviderID: inst.ID,
		Name:       "openai/gpt-oss-20b",
	}); err != nil {
		t.Fatalf("set override: %v", err)
	}
	svc := New(database, providerSvc, credSvc,
		virtual.New(database, providerSvc, modelInfoSvc),
		token.New(database), nil, luaSvc, modelInfoSvc, geoban.New(database))
	return svc, inst.ID, rec.ID
}

func hasCategory(rep *InspectionReport, cat IssueCategory) bool {
	for _, issue := range rep.Issues {
		if issue.Category == cat {
			return true
		}
	}
	return false
}

func TestInspect_LiveOverrideNotFlagged(t *testing.T) {
	svc, _, _ := docStack(t)
	rep, err := svc.Inspect()
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if hasCategory(rep, CategoryOrphanModelOverrides) {
		t.Fatalf("live override flagged orphan: %+v", rep.Issues)
	}
}

func TestInspect_OrphanOverrideFlaggedAndFixed(t *testing.T) {
	svc, providerID, _ := docStack(t)
	if err := svc.providerSvc.Delete(providerID); err != nil {
		t.Fatalf("delete provider: %v", err)
	}
	rep, err := svc.Inspect()
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if !hasCategory(rep, CategoryOrphanModelOverrides) {
		t.Fatal("deleted provider's override must flag orphan")
	}
	fixed, err := svc.Fix([]IssueCategory{CategoryOrphanModelOverrides})
	if err != nil {
		t.Fatalf("fix: %v", err)
	}
	if fixed != 1 {
		t.Fatalf("fixed %d rows, want 1", fixed)
	}
	rep, err = svc.Inspect()
	if err != nil {
		t.Fatalf("re-inspect: %v", err)
	}
	if hasCategory(rep, CategoryOrphanModelOverrides) {
		t.Fatalf("override still flagged after fix: %+v", rep.Issues)
	}
}

// TestInspect_PluginStorageNamespaces covers the second separator bug:
// storage rows live under pluginID NUL scope NUL key. A row for the
// installed plugin passes, a removed plugin's row and an unparseable row
// flag, and fix keeps the live row.
func TestInspect_PluginStorageNamespaces(t *testing.T) {
	svc, _, pluginID := docStack(t)
	put := func(key string) {
		t.Helper()
		if err := svc.db.Update(func(tx *bolt.Tx) error {
			return tx.Bucket(db.BucketPluginStorage).Put([]byte(key), []byte(`{}`))
		}); err != nil {
			t.Fatalf("put storage row: %v", err)
		}
	}
	// Literal NUL-joined keys; luaplugin.ParseStorageKey owns the format,
	// the literals here only seed rows for the inspector under test.
	put(pluginID + "\x00" + "flow:abc" + "\x00" + "device")
	put("removed-plugin" + "\x00" + "s" + "\x00" + "k")
	put("garbage-without-separator")

	rep, err := svc.Inspect()
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	var keys []string
	for _, issue := range rep.Issues {
		if issue.Category == CategoryOrphanPluginStorage {
			keys = issue.Keys
		}
	}
	if len(keys) != 2 {
		t.Fatalf("flagged storage rows = %v, want the removed-plugin and garbage rows", keys)
	}
	if _, err := svc.Fix([]IssueCategory{CategoryOrphanPluginStorage}); err != nil {
		t.Fatalf("fix: %v", err)
	}
	rep, err = svc.Inspect()
	if err != nil {
		t.Fatalf("re-inspect: %v", err)
	}
	if hasCategory(rep, CategoryOrphanPluginStorage) {
		t.Fatalf("storage still flagged after fix: %+v", rep.Issues)
	}
	live, err := svc.luaSvc.List()
	if err != nil || len(live) == 0 {
		t.Fatalf("installed plugin must survive storage fix: %v", live)
	}
}
