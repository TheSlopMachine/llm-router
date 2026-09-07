package luaplugin

import (
	"context"
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/testutil"
)

func installBundled(t *testing.T, svc *Service, filename string) *PluginRecord {
	t.Helper()
	bundled, err := BundledPlugins()
	if err != nil {
		t.Fatalf("bundled plugins: %v", err)
	}
	for _, b := range bundled {
		if b.Filename != filename {
			continue
		}
		rec, err := svc.Install(b.Source, PluginOrigin{RepoID: "bundled", Path: "llm-router-plugins/" + b.Filename})
		if err != nil {
			t.Fatalf("install %s: %v", filename, err)
		}
		return rec
	}
	t.Fatalf("bundled plugin %s not found", filename)
	return nil
}

func TestBundledManifests(t *testing.T) {
	bundled, err := BundledPlugins()
	if err != nil {
		t.Fatalf("bundled: %v", err)
	}
	if len(bundled) != 3 {
		t.Fatalf("expected 3 bundled plugins, got %d", len(bundled))
	}
	for _, b := range bundled {
		if err := CheckRouterVersion(b.Manifest, models.CurrentVersion); err != nil {
			t.Fatalf("%s router version: %v", b.Filename, err)
		}
		if len(b.Manifest.AllowHosts) == 0 {
			t.Fatalf("%s has no allow hosts", b.Filename)
		}
	}
}

func TestBundledOpenCodeZenSchemas(t *testing.T) {
	svc, err := New(testutil.SetupTestDB(t), nil)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	installBundled(t, svc, "opencode-zen.lua")

	nodes, err := svc.Schema("opencode-zen", "credential_schema")
	if err != nil {
		t.Fatalf("credential schema: %v", err)
	}
	if len(nodes) == 0 {
		t.Fatal("expected credential schema nodes")
	}

	ok, err := svc.ValidateCredentials("opencode-zen", map[string]any{})
	if err != nil || !ok {
		t.Fatalf("empty creds must validate (free tier): ok=%v err=%v", ok, err)
	}
	ok, err = svc.ValidateCredentials("opencode-zen", map[string]any{"api_key": "short"})
	if err == nil || ok {
		t.Fatalf("short key must be rejected: ok=%v err=%v", ok, err)
	}
}

func TestBundledGoogleSchemas(t *testing.T) {
	svc, err := New(testutil.SetupTestDB(t), nil)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	installBundled(t, svc, "google.lua")

	nodes, err := svc.Schema("google", "credential_schema")
	if err != nil || len(nodes) == 0 {
		t.Fatalf("credential schema: %+v %v", nodes, err)
	}
	ok, err := svc.ValidateCredentials("google", map[string]any{"api_key": "AIza0123456789abcdef"})
	if err != nil || !ok {
		t.Fatalf("valid key rejected: ok=%v err=%v", ok, err)
	}
}

func TestBundledKiroModelInfos(t *testing.T) {
	svc, err := New(testutil.SetupTestDB(t), nil)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	installBundled(t, svc, "kiro.lua")

	infos, err := svc.GetModelInfos(context.Background(), "kiro",
		&models.Credential{ID: "c1", Data: map[string]any{"access_token": "token"}}, nil)
	if err != nil {
		t.Fatalf("model infos: %v", err)
	}
	if len(infos) != 5 {
		t.Fatalf("expected 5 kiro models, got %d", len(infos))
	}

	nodes, err := svc.Schema("kiro", "config_schema")
	if err != nil || len(nodes) == 0 {
		t.Fatalf("config schema: %+v %v", nodes, err)
	}

	needs, err := svc.NeedsRefresh("kiro", &models.Credential{
		ID:   "c1",
		Data: map[string]any{"refresh_token": "refresh-token-value-long-enough"},
	})
	if err != nil || !needs {
		t.Fatalf("missing access token must need refresh: needs=%v err=%v", needs, err)
	}

	result, err := svc.AuthInitiate(context.Background(), "kiro", "flow-test")
	if err != nil {
		t.Fatalf("auth initiate: %v", err)
	}
	if len(result.Render) == 0 {
		t.Fatal("expected start page nodes")
	}

	step, err := svc.AuthStep(context.Background(), "kiro", "flow-test", "bogus-action", map[string]any{})
	if err != nil {
		t.Fatalf("auth step: %v", err)
	}
	if len(step.Render) == 0 {
		t.Fatal("expected fallback start page nodes")
	}
}

func TestBundledHandlerSets(t *testing.T) {
	svc, err := New(testutil.SetupTestDB(t), nil)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if err := svc.EnsureBundled(); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	expected := map[string][]string{
		"opencode-zen": {"complete", "validate_credentials", "get_model_infos", "credential_schema"},
		"google":       {"complete", "complete_stream", "validate_credentials", "get_model_infos", "credential_schema"},
		"kiro":         {"complete", "complete_stream", "validate_credentials", "get_model_infos", "needs_refresh", "refresh_credential", "config_schema", "credential_schema", "auth_initiate", "auth_step"},
	}
	for typeKey, handlers := range expected {
		for _, h := range handlers {
			if !svc.HasHandler(typeKey, h) {
				t.Errorf("type %q missing handler %q", typeKey, h)
			}
		}
	}
	if svc.HasHandler("opencode-zen", "complete_stream") {
		t.Error("opencode-zen must not declare complete_stream (emulated by router)")
	}
}

func TestEnsureBundledIdempotent(t *testing.T) {	svc, err := New(testutil.SetupTestDB(t), nil)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if err := svc.EnsureBundled(); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	first, err := svc.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(first) != 3 {
		t.Fatalf("expected 3 plugins, got %d", len(first))
	}
	if err := svc.EnsureBundled(); err != nil {
		t.Fatalf("second ensure: %v", err)
	}
	second, err := svc.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(second) != 3 {
		t.Fatalf("ensure must be idempotent, got %d plugins", len(second))
	}
	for _, rec := range second {
		if len(rec.History) != 0 {
			t.Fatalf("idempotent ensure must not append history for %s", rec.ID)
		}
	}
}
