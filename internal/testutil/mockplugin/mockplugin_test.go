package mockplugin

import (
	"context"
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/services/luaplugin"
	"github.com/TheSlopMachine/llm-router/internal/testutil"
)

func installFixture(t *testing.T, service *luaplugin.Service, fixture Fixture) {
	t.Helper()
	if _, err := service.InstallSQLite(context.Background(), []byte(fixture.Source), luaplugin.PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install %s: %v", fixture.TypeKey, err)
	}
}

func TestAllFixturesInstall(t *testing.T) {
	for _, fixture := range All() {
		t.Run(fixture.TypeKey, func(t *testing.T) {
			service, err := luaplugin.New(testutil.SetupTestSQLite(t), nil)
			if err != nil {
				t.Fatalf("new plugin service: %v", err)
			}
			installFixture(t, service, fixture)
			record, err := service.Lookup(fixture.TypeKey)
			if err != nil {
				t.Fatalf("lookup: %v", err)
			}
			if len(record.TypeKeys) != 1 || record.TypeKeys[0] != fixture.TypeKey {
				t.Fatalf("type keys: got %v, want [%s]", record.TypeKeys, fixture.TypeKey)
			}
			for _, handler := range fixture.Handlers {
				if !service.HasHandler(fixture.TypeKey, handler) {
					t.Fatalf("handler %q must be registered", handler)
				}
			}
			infos, err := service.GetModelInfos(context.Background(), fixture.TypeKey, fixture.TypeKey, nil)
			if err != nil {
				t.Fatalf("model infos: %v", err)
			}
			if len(infos) != len(fixture.Models) {
				t.Fatalf("models: got %v, want %v", infos, fixture.Models)
			}
			for i, name := range fixture.Models {
				if infos[i].Name != name {
					t.Fatalf("model %d: got %q, want %q", i, infos[i].Name, name)
				}
			}
		})
	}
}
