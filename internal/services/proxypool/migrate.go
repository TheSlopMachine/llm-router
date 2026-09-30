package proxypool

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/models"
	proxypoollib "github.com/TheSlopMachine/proxypool"
	bolt "go.etcd.io/bbolt"
)

type MigrationReport struct {
	Imported           int
	Skipped            int
	Limits             int
	GeoBans            int
	ConfigFields       int
	SelectedIDsRemoved int
}

type legacyProxy struct {
	ID       string `json:"id"`
	URL      string `json:"url"`
	Location string `json:"location"`
}

// MigrateLegacy imports supported router proxy records into the library cache
// and rewrites durable references before dropping router-owned pool buckets.
func MigrateLegacy(database *db.DB) (MigrationReport, error) {
	var report MigrationReport
	err := database.Update(func(tx *bolt.Tx) error {
		cache := tx.Bucket(db.BucketProxyCache)
		if cache == nil {
			return fmt.Errorf("proxy cache bucket is missing")
		}
		if err := migrateProxyPoolConfig(tx, &report); err != nil {
			return err
		}
		idMap := map[string]string{}
		if err := cache.ForEach(func(key, _ []byte) error {
			id := proxyID(string(key))
			idMap[id] = id
			return nil
		}); err != nil {
			return fmt.Errorf("read cached proxy IDs during migration: %w", err)
		}
		legacy := tx.Bucket([]byte("proxies_v2"))
		if legacy != nil {
			if err := legacy.ForEach(func(_, raw []byte) error {
				var old legacyProxy
				if err := json.Unmarshal(raw, &old); err != nil {
					return fmt.Errorf("decode legacy proxy: %w", err)
				}
				state, ok := legacyState(old)
				if !ok {
					report.Skipped++
					return nil
				}
				newID := proxyID(state.URL)
				if old.ID != "" {
					idMap[old.ID] = newID
				}
				idMap[proxyID(old.URL)] = newID
				if cache.Get([]byte(state.URL)) == nil {
					encoded, err := json.Marshal(state)
					if err != nil {
						return fmt.Errorf("encode migrated proxy: %w", err)
					}
					if err := cache.Put([]byte(state.URL), encoded); err != nil {
						return err
					}
					report.Imported++
				}
				return nil
			}); err != nil {
				return err
			}
		}
		if err := rewriteProviderProxyIDs(tx, idMap, &report); err != nil {
			return err
		}
		if err := migrateProxyLimits(tx, idMap, cache, &report); err != nil {
			return err
		}
		if err := migrateGeoBans(tx, idMap, &report); err != nil {
			return err
		}
		for _, bucketName := range [][]byte{
			[]byte("proxies"),
			[]byte("proxies_v2"),
			[]byte("active_regions"),
			[]byte("proxy_source_meta"),
			[]byte("proxy_limits"),
		} {
			if err := tx.DeleteBucket(bucketName); err != nil && err != bolt.ErrBucketNotFound {
				return fmt.Errorf("drop legacy bucket %q: %w", bucketName, err)
			}
		}
		return nil
	})
	return report, err
}

func migrateProxyPoolConfig(tx *bolt.Tx, report *MigrationReport) error {
	bucket := tx.Bucket(db.BucketRouterConfiguration)
	if bucket == nil {
		return nil
	}
	raw := bucket.Get([]byte("instance"))
	if raw == nil {
		return nil
	}
	var config map[string]any
	if err := json.Unmarshal(raw, &config); err != nil {
		return fmt.Errorf("decode router configuration during proxy migration: %w", err)
	}
	fields := []string{"min_download_speed_kbps", "max_proxies_per_location", "update_interval_minutes"}
	for _, field := range fields {
		if _, exists := config[field]; exists {
			delete(config, field)
			report.ConfigFields++
		}
	}
	if report.ConfigFields == 0 {
		return nil
	}
	updated, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("encode router configuration during proxy migration: %w", err)
	}
	return bucket.Put([]byte("instance"), updated)
}

func legacyState(old legacyProxy) (proxypoollib.ProxyState, bool) {
	raw := strings.TrimSpace(old.URL)
	if raw == "" {
		return proxypoollib.ProxyState{}, false
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || strings.ToLower(u.Scheme) != "http" || u.Opaque != "" || u.RawQuery != "" || (u.Path != "" && u.Path != "/") || u.Fragment != "" {
		return proxypoollib.ProxyState{}, false
	}
	host := strings.TrimSpace(u.Hostname())
	if host == "" || strings.ContainsAny(host, " \t\r\n") {
		return proxypoollib.ProxyState{}, false
	}
	portText := u.Port()
	port := 80
	if portText != "" {
		port, err = strconv.Atoi(portText)
		if err != nil || port < 1 || port > 65535 {
			return proxypoollib.ProxyState{}, false
		}
	} else {
		if strings.HasSuffix(u.Host, ":") {
			return proxypoollib.ProxyState{}, false
		}
	}
	canonical := "http://" + net.JoinHostPort(host, strconv.Itoa(port))
	return proxypoollib.ProxyState{
		URL:      canonical,
		IP:       host,
		Port:     port,
		Location: NormalizeCountryCode(old.Location),
		Metadata: map[string]string{},
	}, true
}

func rewriteProviderProxyIDs(tx *bolt.Tx, idMap map[string]string, report *MigrationReport) error {
	bucket := tx.Bucket(db.BucketProviderInstances)
	if bucket == nil {
		return nil
	}
	return bucket.ForEach(func(key, raw []byte) error {
		var provider map[string]any
		if err := json.Unmarshal(raw, &provider); err != nil {
			return fmt.Errorf("decode provider during proxy migration: %w", err)
		}
		config, ok := provider["config"].(map[string]any)
		if !ok {
			return nil
		}
		proxyConfig, ok := config["proxy"].(map[string]any)
		if !ok {
			return nil
		}
		ids, ok := proxyConfig["ids"].([]any)
		if !ok {
			return nil
		}
		updated := make([]any, 0, len(ids))
		changed := false
		for _, value := range ids {
			oldID, ok := value.(string)
			if !ok {
				updated = append(updated, value)
				continue
			}
			newID, exists := idMap[oldID]
			if !exists {
				report.SelectedIDsRemoved++
				changed = true
				continue
			}
			if newID != oldID {
				changed = true
			}
			updated = append(updated, newID)
		}
		if !changed {
			return nil
		}
		proxyConfig["ids"] = updated
		encoded, err := json.Marshal(provider)
		if err != nil {
			return fmt.Errorf("encode provider during proxy migration: %w", err)
		}
		return bucket.Put(key, encoded)
	})
}

func migrateProxyLimits(tx *bolt.Tx, idMap map[string]string, cache *bolt.Bucket, report *MigrationReport) error {
	bucket := tx.Bucket(db.BucketExhausted)
	if bucket == nil {
		return nil
	}
	var remove [][]byte
	var write []models.ExhaustedEntry
	if err := bucket.ForEach(func(key, raw []byte) error {
		var entry models.ExhaustedEntry
		if err := json.Unmarshal(raw, &entry); err != nil {
			return fmt.Errorf("decode exhausted entry during proxy migration: %w", err)
		}
		oldID, ok := proxyIDFromKey(entry.Key)
		if !ok {
			return nil
		}
		remove = append(remove, append([]byte(nil), key...))
		newID, mapped := idMap[oldID]
		if !mapped || !time.Now().Before(entry.ResetsAt) {
			return nil
		}
		entry.Key = strings.Replace(entry.Key, "x="+oldID, "x="+newID, 1)
		write = append(write, entry)
		return nil
	}); err != nil {
		return err
	}
	for _, key := range remove {
		if err := bucket.Delete(key); err != nil {
			return err
		}
	}
	for _, entry := range write {
		proxyID, _ := proxyIDFromKey(entry.Key)
		url, ok := urlForID(cache, proxyID)
		if !ok {
			continue
		}
		var state proxypoollib.ProxyState
		if err := json.Unmarshal(cache.Get([]byte(url)), &state); err != nil {
			return fmt.Errorf("decode migrated proxy state: %w", err)
		}
		if state.Metadata == nil {
			state.Metadata = map[string]string{}
		}
		value, err := json.Marshal(proxyLimit{ResetsAt: entry.ResetsAt, Reason: entry.Reason})
		if err != nil {
			return err
		}
		state.Metadata[limitMetadataPrefix+entry.Key] = string(value)
		encoded, err := json.Marshal(state)
		if err != nil {
			return err
		}
		if err := cache.Put([]byte(url), encoded); err != nil {
			return err
		}
		report.Limits++
	}
	return nil
}

func migrateGeoBans(tx *bolt.Tx, idMap map[string]string, report *MigrationReport) error {
	bucket := tx.Bucket(db.BucketGeoBans)
	if bucket == nil {
		return nil
	}
	type geoBan struct {
		Key      string    `json:"key"`
		Plugin   string    `json:"plugin"`
		Provider string    `json:"provider"`
		Proxy    string    `json:"proxy"`
		Reason   string    `json:"reason,omitempty"`
		BannedAt time.Time `json:"banned_at"`
	}
	var remove [][]byte
	var write []geoBan
	if err := bucket.ForEach(func(key, raw []byte) error {
		var entry geoBan
		if err := json.Unmarshal(raw, &entry); err != nil {
			return fmt.Errorf("decode geo ban during proxy migration: %w", err)
		}
		newID, ok := idMap[entry.Proxy]
		if !ok {
			remove = append(remove, append([]byte(nil), key...))
			return nil
		}
		canonicalKey := strings.Join([]string{"p=" + entry.Plugin, "pr=" + entry.Provider, "x=" + newID}, "\x00")
		if entry.Proxy == newID && entry.Key == canonicalKey && string(key) == canonicalKey {
			return nil
		}
		remove = append(remove, append([]byte(nil), key...))
		entry.Proxy = newID
		entry.Key = canonicalKey
		write = append(write, entry)
		return nil
	}); err != nil {
		return err
	}
	for _, key := range remove {
		if err := bucket.Delete(key); err != nil {
			return err
		}
	}
	for _, entry := range write {
		encoded, err := json.Marshal(entry)
		if err != nil {
			return err
		}
		if err := bucket.Put([]byte(entry.Key), encoded); err != nil {
			return err
		}
		report.GeoBans++
	}
	return nil
}

func proxyIDFromKey(key string) (string, bool) {
	for _, part := range strings.Split(key, "\x00") {
		if strings.HasPrefix(part, "x=") && len(part) > 2 {
			return strings.TrimPrefix(part, "x="), true
		}
	}
	return "", false
}

func urlForID(cache *bolt.Bucket, id string) (string, bool) {
	var states []proxypoollib.ProxyState
	if err := cache.ForEach(func(_, raw []byte) error {
		var state proxypoollib.ProxyState
		if err := json.Unmarshal(raw, &state); err != nil {
			return err
		}
		if proxyID(state.URL) == id {
			states = append(states, state)
		}
		return nil
	}); err != nil || len(states) == 0 {
		return "", false
	}
	return states[0].URL, true
}
