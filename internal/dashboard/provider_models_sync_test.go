package dashboard

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/services/provider"
	"github.com/TheSlopMachine/llm-router/internal/services/virtual"
)

// TestProviderModelsGetRebuildsManagedVirtuals proves viewing the provider
// models page heals missing managed virtual models: the view warms the
// cache from the upstream, then the warmed list rebuilds the managed rows.
// Before the fix the rebuild ran only on toggle, refresh, or explicit sync,
// so fresh providers showed no virtual models until an enable/disable flip.
func TestProviderModelsGetRebuildsManagedVirtuals(t *testing.T) {
	parameters := buildWiredParams(t)
	handler, err := New(parameters)
	if err != nil {
		t.Fatalf("New with wired params: %v", err)
	}
	ctx := context.Background()

	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/models" {
			writer.WriteHeader(http.StatusNotFound)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		fmt.Fprint(writer, `{"object":"list","data":[{"id":"chat-model"}]}`)
	}))
	defer upstream.Close()

	created, err := parameters.ProviderSvc.Create(ctx, provider.CreateOptions{
		Name:    "Demo",
		TypeKey: "custom",
		Config:  map[string]any{"base_url": upstream.URL},
	})
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}

	getModels := func() int {
		request := httptest.NewRequest(http.MethodGet, "/api/llm-router/dashboard/providers/"+created.ID+"/models", nil)
		request.SetPathValue("id", created.ID)
		recorder := httptest.NewRecorder()
		handler.apiProviderModels(recorder, request)
		return recorder.Code
	}

	if code := getModels(); code != http.StatusOK {
		t.Fatalf("first models view status: got %d, want %d", code, http.StatusOK)
	}

	managed, err := parameters.VirtualSvc.List(ctx)
	if err != nil {
		t.Fatalf("list virtual models: %v", err)
	}
	wantMarker := virtual.ProviderMarker(created.ID, "chat")
	found := false
	for _, virtualModel := range managed {
		if virtualModel.ManagedBy == wantMarker {
			found = true
			if len(virtualModel.Models) != 1 || string(virtualModel.Models[0].ModelID) != created.ID+"/chat-model" {
				t.Fatalf("managed members: got %+v, want one %s/chat-model entry", virtualModel.Models, created.ID)
			}
		}
	}
	if !found {
		t.Fatalf("managed virtual model %q missing after viewing provider models", wantMarker)
	}

	if code := getModels(); code != http.StatusOK {
		t.Fatalf("second models view status: got %d, want %d (sync must be idempotent)", code, http.StatusOK)
	}
}
