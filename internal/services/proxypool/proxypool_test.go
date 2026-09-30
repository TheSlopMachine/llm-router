package proxypool

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/testutil"
	proxypoollib "github.com/TheSlopMachine/proxypool"
	bolt "go.etcd.io/bbolt"
)

func TestNextRefreshInterval(t *testing.T) {
	step := 0
	for _, expected := range []time.Duration{5 * time.Minute, 10 * time.Minute, 15 * time.Minute, 15 * time.Minute} {
		interval, nextStep := nextRefreshInterval(false, step)
		if interval != expected {
			t.Fatalf("idle interval = %s, want %s", interval, expected)
		}
		step = nextStep
	}

	interval, step := nextRefreshInterval(true, step)
	if interval != time.Minute || step != 0 {
		t.Fatalf("active schedule = (%s, %d), want (%s, 0)", interval, step, time.Minute)
	}
	interval, _ = nextRefreshInterval(false, step)
	if interval != 5*time.Minute {
		t.Fatalf("first idle interval after activity = %s, want %s", interval, 5*time.Minute)
	}
}

func TestCandidateURL(t *testing.T) {
	cases := []struct {
		name      string
		candidate models.ProxyCandidate
		want      string
		ok        bool
	}{
		{name: "http IPv4", candidate: models.ProxyCandidate{Protocol: "http", Host: "192.0.2.1", Port: 8080}, want: "http://192.0.2.1:8080", ok: true},
		{name: "bracketed IPv6", candidate: models.ProxyCandidate{Protocol: "http", Host: "[2001:db8::1]", Port: 3128}, want: "http://[2001:db8::1]:3128", ok: true},
		{name: "unsupported protocol", candidate: models.ProxyCandidate{Protocol: "socks5", Host: "proxy.example", Port: 1080}},
		{name: "invalid port", candidate: models.ProxyCandidate{Protocol: "http", Host: "proxy.example", Port: 65536}},
		{name: "invalid host", candidate: models.ProxyCandidate{Protocol: "http", Host: "proxy/path", Port: 8080}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := candidateURL(tc.candidate)
			if got != tc.want || ok != tc.ok {
				t.Fatalf("candidateURL() = (%q, %t), want (%q, %t)", got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestDBCachePersistsAndCopiesMetadata(t *testing.T) {
	database := testutil.SetupTestDB(t)
	cache, err := newDBCache(database)
	if err != nil {
		t.Fatalf("new cache: %v", err)
	}
	state := proxypoollib.ProxyState{
		URL:           "http://192.0.2.10:8080",
		IP:            "192.0.2.10",
		Port:          8080,
		Metadata:      map[string]string{"limit": "value"},
		LastCheckedAt: time.Now(),
	}
	cache.Set(state)
	if err := cache.peekError(); err != nil {
		t.Fatalf("cache write: %v", err)
	}

	got, ok := cache.Get(state.URL)
	if !ok {
		t.Fatal("cached state not found")
	}
	got.Metadata["limit"] = "changed"
	if again, _ := cache.Get(state.URL); again.Metadata["limit"] != "value" {
		t.Fatalf("cache metadata was mutated through returned state: %q", again.Metadata["limit"])
	}

	reloaded, err := newDBCache(database)
	if err != nil {
		t.Fatalf("reload cache: %v", err)
	}
	if persisted, ok := reloaded.Get(state.URL); !ok || persisted.Metadata["limit"] != "value" {
		t.Fatalf("persisted cache state = (%+v, %t)", persisted, ok)
	}
	reloaded.Clear()
	if states := reloaded.All(); len(states) != 0 {
		t.Fatalf("cache after clear = %d states, want none", len(states))
	}
}

func TestProxyLimitMetadataExpires(t *testing.T) {
	database := testutil.SetupTestDB(t)
	service, err := New(database)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	proxyURL := "http://192.0.2.20:8080"
	service.cache.Set(proxypoollib.ProxyState{
		URL:           proxyURL,
		IP:            "192.0.2.20",
		Port:          8080,
		LastCheckedAt: time.Now(),
	})
	id := proxyID(proxyURL)
	key := "p=plugin\x00pr=provider\x00x=" + id
	resetsAt := time.Now().Add(time.Minute)
	if err := service.MarkLimit(id, key, resetsAt, "quota"); err != nil {
		t.Fatalf("mark limit: %v", err)
	}
	if limited, err := service.IsLimited(id, key, time.Now()); err != nil || !limited {
		t.Fatalf("limit before expiry = (%t, %v), want (true, nil)", limited, err)
	}
	if limited, err := service.IsLimited(id, key, resetsAt.Add(time.Second)); err != nil || limited {
		t.Fatalf("limit after expiry = (%t, %v), want (false, nil)", limited, err)
	}
	if _, ok := service.pool.GetMetadata(proxyURL)[limitMetadataPrefix+key]; ok {
		t.Fatal("expired limit metadata was not removed")
	}
}

func TestManualDirectModeDoesNotTouchPool(t *testing.T) {
	service, err := New(testutil.SetupTestDB(t))
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	picks, err := service.Rank(nil, models.ProxyModeManual, nil, "")
	if err != nil || len(picks) != 0 {
		t.Fatalf("manual direct selection = (%v, %v), want no picks", picks, err)
	}
	service.mu.Lock()
	lastUse, refreshing := service.lastUse, service.refreshing
	service.mu.Unlock()
	if !lastUse.IsZero() || refreshing {
		t.Fatalf("manual direct selection touched pool: last_use=%s refreshing=%t", lastUse, refreshing)
	}
}

func TestMigrateLegacyRewritesProxyReferences(t *testing.T) {
	database := testutil.SetupTestDB(t)
	legacyKey := "p=plugin\x00pr=provider\x00x=old-proxy"
	resetsAt := time.Now().Add(time.Hour)
	err := database.Update(func(tx *bolt.Tx) error {
		if _, err := tx.CreateBucket([]byte("proxies")); err != nil {
			return err
		}
		proxies, err := tx.CreateBucket([]byte("proxies_v2"))
		if err != nil {
			return err
		}
		rows := []legacyProxy{
			{ID: "old-proxy", URL: "http://192.0.2.30:8080", Location: "DE"},
			{ID: "unsupported-proxy", URL: "https://192.0.2.31:8443", Location: "FR"},
		}
		for i, row := range rows {
			raw, err := json.Marshal(row)
			if err != nil {
				return err
			}
			if err := proxies.Put([]byte(fmt.Sprintf("row-%d", i)), raw); err != nil {
				return err
			}
		}

		provider := map[string]any{"config": map[string]any{"proxy": map[string]any{
			"mode": models.ProxyModeManual,
			"ids":  []string{"old-proxy", "unsupported-proxy", "missing-proxy"},
		}}}
		providerRaw, err := json.Marshal(provider)
		if err != nil {
			return err
		}
		if err := tx.Bucket(db.BucketProviderInstances).Put([]byte("provider-1"), providerRaw); err != nil {
			return err
		}
		configRaw, err := json.Marshal(map[string]any{
			"models_filter":            "all",
			"min_download_speed_kbps":  15000,
			"max_proxies_per_location": 10,
			"update_interval_minutes":  15,
		})
		if err != nil {
			return err
		}
		if err := tx.Bucket(db.BucketRouterConfiguration).Put([]byte("instance"), configRaw); err != nil {
			return err
		}

		limitRaw, err := json.Marshal(models.ExhaustedEntry{Key: legacyKey, ResetsAt: resetsAt, Reason: "quota"})
		if err != nil {
			return err
		}
		if err := tx.Bucket(db.BucketExhausted).Put([]byte(legacyKey), limitRaw); err != nil {
			return err
		}
		banRaw, err := json.Marshal(models.GeoBanEntry{
			Key: legacyKey, Plugin: "plugin", Provider: "provider", Proxy: "old-proxy",
		})
		if err != nil {
			return err
		}
		return tx.Bucket(db.BucketGeoBans).Put([]byte(legacyKey), banRaw)
	})
	if err != nil {
		t.Fatalf("seed legacy data: %v", err)
	}

	report, err := MigrateLegacy(database)
	if err != nil {
		t.Fatalf("migrate legacy data: %v", err)
	}
	if report.Imported != 1 || report.Skipped != 1 || report.Limits != 1 || report.GeoBans != 1 || report.ConfigFields != 3 || report.SelectedIDsRemoved != 2 {
		t.Fatalf("migration report = %+v", report)
	}
	newID := proxyID("http://192.0.2.30:8080")
	newKey := "p=plugin\x00pr=provider\x00x=" + newID
	secondReport, err := MigrateLegacy(database)
	if err != nil {
		t.Fatalf("repeat migration: %v", err)
	}
	if secondReport != (MigrationReport{}) {
		t.Fatalf("repeat migration report = %+v, want empty", secondReport)
	}
	if err := database.View(func(tx *bolt.Tx) error {
		cache := tx.Bucket(db.BucketProxyCache)
		raw := cache.Get([]byte("http://192.0.2.30:8080"))
		var state proxypoollib.ProxyState
		if err := json.Unmarshal(raw, &state); err != nil {
			return err
		}
		if state.Location != "DE" {
			return fmt.Errorf("migrated location = %q, want DE", state.Location)
		}
		var limit proxyLimit
		if err := json.Unmarshal([]byte(state.Metadata[limitMetadataPrefix+newKey]), &limit); err != nil {
			return err
		}
		if !limit.ResetsAt.Equal(resetsAt) || limit.Reason != "quota" {
			return fmt.Errorf("migrated limit = %+v", limit)
		}

		var provider map[string]any
		if err := json.Unmarshal(tx.Bucket(db.BucketProviderInstances).Get([]byte("provider-1")), &provider); err != nil {
			return err
		}
		config := provider["config"].(map[string]any)
		proxy := config["proxy"].(map[string]any)
		ids := proxy["ids"].([]any)
		if len(ids) != 1 || ids[0] != newID {
			return fmt.Errorf("migrated provider proxy IDs = %v", ids)
		}

		var ban models.GeoBanEntry
		if err := json.Unmarshal(tx.Bucket(db.BucketGeoBans).Get([]byte(newKey)), &ban); err != nil {
			return err
		}
		if ban.Proxy != newID {
			return fmt.Errorf("migrated geo-ban proxy = %q, want %q", ban.Proxy, newID)
		}
		for _, bucketName := range []string{"proxies", "proxies_v2"} {
			if tx.Bucket([]byte(bucketName)) != nil {
				return fmt.Errorf("legacy proxy bucket %q still exists", bucketName)
			}
		}
		if tx.Bucket(db.BucketExhausted).Get([]byte(legacyKey)) != nil {
			return fmt.Errorf("legacy proxy limit still exists in exhausted bucket")
		}
		var routerSettings map[string]any
		if err := json.Unmarshal(tx.Bucket(db.BucketRouterConfiguration).Get([]byte("instance")), &routerSettings); err != nil {
			return err
		}
		for _, field := range []string{"min_download_speed_kbps", "max_proxies_per_location", "update_interval_minutes"} {
			if _, exists := routerSettings[field]; exists {
				return fmt.Errorf("legacy proxy setting %q remains", field)
			}
		}
		if routerSettings["models_filter"] != "all" {
			return fmt.Errorf("non-proxy router setting changed: %v", routerSettings["models_filter"])
		}
		return nil
	}); err != nil {
		t.Fatalf("check migrated data: %v", err)
	}
}
