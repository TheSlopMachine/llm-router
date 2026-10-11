package metrics

import (
	"log/slog"
	"testing"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/testutil"
)

func buildDimensionBucket(stamp time.Time) *MetricBucket {
	lastUsed := stamp.Add(time.Minute)
	return &MetricBucket{
		Timestamp:     stamp,
		TotalRequests: 4,
		TotalErrors:   1,
		TokensInput:   100,
		TokensOutput:  50,
		ByProviderID: map[string]*ProviderMetrics{
			"demo": {Requests: 4, Errors: 1, TokensInput: 100, TokensOutput: 50},
		},
		ByProviderType: map[string]*ProviderMetrics{
			"mock": {Requests: 4, Errors: 1, TokensInput: 100, TokensOutput: 50},
		},
		ByModel: map[string]*ModelMetrics{
			"demo/chat-a": {Requests: 3, TokensInput: 80, TokensOutput: 40},
			"demo/chat-b": {Requests: 1, Errors: 1, TokensInput: 20, TokensOutput: 10},
		},
		ByTokenID: map[string]*TokenMetrics{
			"tok-1": {Requests: 4, TokensInput: 100, TokensOutput: 50, LastUsed: &lastUsed},
		},
		ErrorsByType: map[string]int64{"rate_limit": 1},
	}
}

func TestDimensionsRoundTripThroughSQLite(t *testing.T) {
	database := testutil.SetupTestSQLite(t)
	service := New(database, slog.Default())
	stamp := time.Now().UTC().Truncate(time.Minute)

	if err := service.persistBucket(buildDimensionBucket(stamp), "1m"); err != nil {
		t.Fatalf("persist: %v", err)
	}
	loaded, err := service.loadBucket(stamp, "1m")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.TotalRequests != 4 || loaded.TokensInput != 100 {
		t.Fatalf("base row: %+v", loaded)
	}
	if loaded.ByProviderID["demo"] == nil || loaded.ByProviderID["demo"].Requests != 4 {
		t.Fatalf("provider dimension: %+v", loaded.ByProviderID)
	}
	if loaded.ByProviderType["mock"] == nil || loaded.ByProviderType["mock"].Requests != 4 {
		t.Fatalf("provider type dimension: %+v", loaded.ByProviderType)
	}
	if loaded.ByModel["demo/chat-a"] == nil || loaded.ByModel["demo/chat-a"].Requests != 3 {
		t.Fatalf("model dimension: %+v", loaded.ByModel)
	}
	if loaded.ByTokenID["tok-1"] == nil || loaded.ByTokenID["tok-1"].Requests != 4 {
		t.Fatalf("token dimension: %+v", loaded.ByTokenID)
	}
	if loaded.ErrorsByType["rate_limit"] != 1 {
		t.Fatalf("error dimension: %+v", loaded.ErrorsByType)
	}

	ranged, err := service.loadBucketsInRange(stamp.Add(-time.Hour), stamp.Add(time.Hour))
	if err != nil {
		t.Fatalf("range: %v", err)
	}
	if len(ranged) != 1 || len(ranged[0].ByModel) != 2 {
		t.Fatalf("range hydration: %+v", ranged)
	}

	distinct, err := service.GetDistinctModels()
	if err != nil {
		t.Fatalf("distinct: %v", err)
	}
	if len(distinct) != 2 {
		t.Fatalf("distinct models: %v", distinct)
	}

	usage, err := service.GetTokenUsage()
	if err != nil {
		t.Fatalf("usage: %v", err)
	}
	if usage["tok-1"] == nil || usage["tok-1"].Requests != 4 || usage["tok-1"].LastUsed == nil {
		t.Fatalf("token usage: %+v", usage)
	}
}

func TestRecordRequestSurfacesInOverview(t *testing.T) {
	database := testutil.SetupTestSQLite(t)
	service := New(database, slog.Default())
	service.RecordRequest(models.MetricEvent{
		Timestamp:   time.Now().UTC(),
		ProviderID:  "demo",
		Model:       "demo/chat-a",
		TokenID:     "tok-1",
		TokensInput: 10,
	})
	service.drainEvents()
	if err := service.flushAll(); err != nil {
		t.Fatalf("flush: %v", err)
	}
	overview, err := service.QueryOverview(models.MetricsFilters{})
	if err != nil {
		t.Fatalf("overview: %v", err)
	}
	if overview.TotalRequests != 1 || overview.PeakRequests != 1 {
		t.Fatalf("overview: %+v", overview)
	}
}
