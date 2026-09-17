package repository

// Transient scratch: inspect groq provider + credential hygiene in
// ./router.db (read-only). Deleted after the run; never committed.

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
)

func TestScratchInspectGroq(t *testing.T) {
	path := "/Users/toli/playground/llm-router/router.db"
	if _, err := os.Stat(path); err != nil {
		t.Skipf("db not present: %v", err)
	}
	database, err := bolt.Open(path, 0600, &bolt.Options{ReadOnly: true, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer database.Close()

	err = database.View(func(tx *bolt.Tx) error {
		pi := tx.Bucket([]byte("provider_instances"))
		if pi == nil {
			return fmt.Errorf("no provider_instances bucket")
		}
		c := pi.Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			if !strings.Contains(string(k), "groq") {
				continue
			}
			var inst map[string]any
			if err := json.Unmarshal(v, &inst); err != nil {
				return err
			}
			cfg, _ := json.Marshal(inst["config"])
			fmt.Printf("PROVIDER %s: name=%v type=%v disabled=%v config=%s\n",
				k, inst["name"], inst["type_key"], inst["disabled"], cfg)
		}

		creds := tx.Bucket([]byte("credentials"))
		if creds == nil {
			return fmt.Errorf("no credentials bucket")
		}
		cc := creds.Cursor()
		n := 0
		for k, v := cc.First(); k != nil; k, v = cc.Next() {
			var cred map[string]any
			if err := json.Unmarshal(v, &cred); err != nil {
				continue
			}
			if cred["provider_id"] != "groq" {
				continue
			}
			n++
			data, _ := cred["data"].(map[string]any)
			for dk, dv := range data {
				s, _ := dv.(string)
				prefix := s
				if len(prefix) > 7 {
					prefix = prefix[:7]
				}
				suffix := ""
				if len(s) >= 4 {
					suffix = s[len(s)-4:]
				}
				fmt.Printf("CRED %s field=%s len=%d prefix=%q suffix=%q ws=%v crlf=%v\n",
					k, dk, len(s), prefix, suffix,
					strings.TrimSpace(s) != s, strings.ContainsAny(s, "\r\n"))
			}
			fmt.Printf("CRED %s label=%v disabled=%v created=%v\n", k, cred["label"], cred["disabled"], cred["created_at"])
		}
		fmt.Printf("groq credentials: %d\n", n)
		return nil
	})
	if err != nil {
		t.Fatalf("view: %v", err)
	}
}
