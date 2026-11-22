package metrics

import (
	"log/slog"
	"testing"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/testutil"
)

func TestQueryLastActivity(t *testing.T) {
	database := testutil.SetupTestSQLite(t)
	service := New(database, slog.Default())

	empty, err := service.QueryLastActivity(models.MetricsFilters{})
	if err != nil {
		t.Fatalf("empty: %v", err)
	}
	if empty != nil {
		t.Fatalf("empty: got %v, want nil", *empty)
	}

	now := time.Now().UTC().Truncate(time.Minute)
	older := now.Add(-10 * 24 * time.Hour)
	newer := now.Add(-3 * time.Hour)
	expired := now.Add(-RetentionPeriod - 24*time.Hour)
	for _, stamp := range []time.Time{expired, older, newer} {
		if err := service.persistBucket(buildDimensionBucket(stamp), "1m"); err != nil {
			t.Fatalf("persist %v: %v", stamp, err)
		}
	}
	idle := &MetricBucket{Timestamp: now.Add(-time.Hour)}
	if err := service.persistBucket(idle, "1m"); err != nil {
		t.Fatalf("persist idle: %v", err)
	}

	tests := []struct {
		name    string
		filters models.MetricsFilters
		want    *time.Time
	}{
		{name: "unfiltered", filters: models.MetricsFilters{}, want: &newer},
		{name: "provider", filters: models.MetricsFilters{ProviderID: "demo"}, want: &newer},
		{name: "model", filters: models.MetricsFilters{Model: "demo/chat-a"}, want: &newer},
		{name: "unknown provider", filters: models.MetricsFilters{ProviderID: "missing"}, want: nil},
		{name: "unknown model", filters: models.MetricsFilters{Model: "demo/missing"}, want: nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := service.QueryLastActivity(test.filters)
			if err != nil {
				t.Fatalf("query: %v", err)
			}
			if test.want == nil {
				if got != nil {
					t.Fatalf("got %v, want nil", *got)
				}
				return
			}
			if got == nil || !got.Equal(*test.want) {
				t.Fatalf("got %v, want %v", got, *test.want)
			}
		})
	}
}
