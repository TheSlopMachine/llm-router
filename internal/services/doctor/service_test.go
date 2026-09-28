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
// installed plugin passes, a removed plugin's row flags orphan and
// auto-fix deletes it, and an unparseable row flags corrupt and survives
// fix-all: no writer builds it, so no one can prove it orphaned.
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
	byCategory := map[IssueCategory][]string{}
	for _, issue := range rep.Issues {
		byCategory[issue.Category] = issue.Keys
	}
	if len(byCategory[CategoryOrphanPluginStorage]) != 1 {
		t.Fatalf("orphan storage rows = %v, want exactly the removed-plugin row", byCategory[CategoryOrphanPluginStorage])
	}
	if len(byCategory[CategoryCorruptPluginStorage]) != 1 {
		t.Fatalf("corrupt storage rows = %v, want exactly the garbage row", byCategory[CategoryCorruptPluginStorage])
	}
	if _, err := svc.Fix(nil); err != nil {
		t.Fatalf("fix-all: %v", err)
	}
	rep, err = svc.Inspect()
	if err != nil {
		t.Fatalf("re-inspect: %v", err)
	}
	if hasCategory(rep, CategoryOrphanPluginStorage) {
		t.Fatalf("orphan storage still flagged after fix: %+v", rep.Issues)
	}
	if !hasCategory(rep, CategoryCorruptPluginStorage) {
		t.Fatal("corrupt row must survive fix-all: no writer builds it, no one proves it orphaned")
	}
	live, err := svc.luaSvc.List()
	if err != nil || len(live) == 0 {
		t.Fatalf("installed plugin must survive storage fix: %v", live)
	}
}

// TestInspect_MissingBackendFlaggedAndDisabled covers providers whose type
// has no installed plugin or built-in backend: inspecting flags the
// provider ID, fixing disables it (first-wins, manual re-enable clears),
// and re-inspecting stays clean since parked providers skip.
func TestInspect_MissingBackendFlaggedAndDisabled(t *testing.T) {
	svc, _, _ := docStack(t)
	ghost, err := svc.providerSvc.Create(provider.CreateOptions{Name: "Ghost", TypeKey: "ghost-type"})
	if err != nil {
		t.Fatalf("create ghost provider: %v", err)
	}
	rep, err := svc.Inspect()
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	var keys []string
	for _, issue := range rep.Issues {
		if issue.Category == CategoryMissingProviderBackend {
			keys = issue.Keys
		}
	}
	if len(keys) != 1 || keys[0] != ghost.ID {
		t.Fatalf("missing-backend keys = %v, want [%q]", keys, ghost.ID)
	}
	fixed, err := svc.Fix([]IssueCategory{CategoryMissingProviderBackend})
	if err != nil {
		t.Fatalf("fix: %v", err)
	}
	if fixed != 1 {
		t.Fatalf("fixed %d providers, want 1", fixed)
	}
	got, err := svc.providerSvc.Get(ghost.ID)
	if err != nil || !got.Disabled || got.DisabledBy != "system" {
		t.Fatalf("ghost must disable by system: %+v %v", got, err)
	}
	rep, err = svc.Inspect()
	if err != nil {
		t.Fatalf("re-inspect: %v", err)
	}
	if hasCategory(rep, CategoryMissingProviderBackend) {
		t.Fatalf("disabled provider still flagged: %+v", rep.Issues)
	}
}

// TestInspect_MissingBackendSkipsParkedAndBacked proves the check stays
// quiet for intentional states: a disabled provider without backend and a
// Go-backed provider with no plugin both pass.
func TestInspect_MissingBackendSkipsParkedAndBacked(t *testing.T) {
	svc, _, _ := docStack(t)
	svc.providerSvc.RegisterGoAdapter(testutil.NewMockAdapter("go-type"))
	if _, err := svc.providerSvc.Create(provider.CreateOptions{Name: "Go", TypeKey: "go-type"}); err != nil {
		t.Fatalf("create go provider: %v", err)
	}
	parked, err := svc.providerSvc.Create(provider.CreateOptions{Name: "Parked", TypeKey: "parked-type"})
	if err != nil {
		t.Fatalf("create parked provider: %v", err)
	}
	if err := svc.providerSvc.SystemDisable(parked.ID, "test"); err != nil {
		t.Fatalf("disable parked: %v", err)
	}
	rep, err := svc.Inspect()
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if hasCategory(rep, CategoryMissingProviderBackend) {
		t.Fatalf("parked and Go-backed providers must pass: %+v", rep.Issues)
	}
}
