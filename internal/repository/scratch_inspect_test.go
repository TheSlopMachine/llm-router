package repository

// Transient scratch: inspect groq provider + credential hygiene in
// ./router.db (read-only). Deleted after the run; never committed.

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
)

// scratchLive gates the live network probes: they hit Google through the
// proxy pool, take minutes, and flake with proxy health. Run them
// explicitly with SCRATCH_LIVE=1.
func scratchLive(t *testing.T) {
	t.Helper()
	if os.Getenv("SCRATCH_LIVE") != "1" {
		t.Skip("live probe: set SCRATCH_LIVE=1 to run")
	}
}

func TestScratchInspectGroq(t *testing.T) {
	scratchLive(t)
	path := "/Users/toli/playground/llm-router/router.db"
	if _, err := os.Stat(path); err != nil {
		t.Skipf("db not present: %v", err)
	}
	database, err := bolt.Open(path, 0600, &bolt.Options{ReadOnly: true, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer database.Close()

	out, err := os.Create("/Users/toli/playground/llm-router/test_shit/scratch_out.txt")
	if err != nil {
		t.Fatalf("create out: %v", err)
	}
	defer out.Close()

	var keys []string
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
			fmt.Fprintf(out, "PROVIDER %s: name=%v type=%v disabled=%v config=%s\n",
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
				if dk == "api_key" {
					keys = append(keys, s)
				}
				prefix := s
				if len(prefix) > 7 {
					prefix = prefix[:7]
				}
				suffix := ""
				if len(s) >= 4 {
					suffix = s[len(s)-4:]
				}
				fmt.Fprintf(out, "CRED %s field=%s len=%d prefix=%q suffix=%q ws=%v crlf=%v\n",
					k, dk, len(s), prefix, suffix,
					strings.TrimSpace(s) != s, strings.ContainsAny(s, "\r\n"))
			}
			fmt.Fprintf(out, "CRED %s label=%v disabled=%v created=%v\n", k, cred["label"], cred["disabled"], cred["created_at"])
		}
		fmt.Fprintf(out, "groq credentials: %d\n", n)

		if px := tx.Bucket([]byte("proxies")); px != nil {
			pc := px.Cursor()
			total, manual, listed := 0, 0, 0
			countries := map[string]int{}
			for k, v := pc.First(); k != nil; k, v = pc.Next() {
				var p map[string]any
				if err := json.Unmarshal(v, &p); err != nil {
					continue
				}
				total++
				if p["source"] == "manual" {
					manual++
				} else {
					listed++
				}
				if c, ok := p["country"].(string); ok && c != "" {
					countries[c]++
				}
			}
			fmt.Fprintf(out, "proxies: total=%d manual=%d list=%d countries=%v\n", total, manual, listed, countries)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("view: %v", err)
	}

	client := &http.Client{Timeout: 15 * time.Second}
	for i, key := range keys {
		req, _ := http.NewRequest("GET", "https://api.groq.com/openai/v1/models", nil)
		req.Header.Set("Authorization", "Bearer "+key)
		resp, err := client.Do(req)
		if err != nil {
			fmt.Fprintf(out, "KEY %d: direct error: %v\n", i, err)
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		resp.Body.Close()
		fmt.Fprintf(out, "KEY %d: direct status=%d body=%q\n", i, resp.StatusCode, string(body))
	}
}

// TestScratchGoogleModels lists Google models relevant to the new endpoints
// (tts / image generation) with their supportedGenerationMethods. The API
// key is read from the dev DB and never printed.
func TestScratchGoogleModels(t *testing.T) {
	scratchLive(t)
	path := "/Users/toli/playground/llm-router/router.db"
	database, err := bolt.Open(path, 0600, &bolt.Options{ReadOnly: true, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer database.Close()

	var key string
	var usProxies []string
	err = database.View(func(tx *bolt.Tx) error {
		creds := tx.Bucket([]byte("credentials"))
		cc := creds.Cursor()
		for k, v := cc.First(); k != nil; k, v = cc.Next() {
			var cred map[string]any
			if err := json.Unmarshal(v, &cred); err != nil {
				continue
			}
			if cred["provider_id"] != "google" || cred["disabled"] == true {
				continue
			}
			data, _ := cred["data"].(map[string]any)
			if s, _ := data["api_key"].(string); s != "" && key == "" {
				key = s
			}
		}
		if px := tx.Bucket([]byte("proxies")); px != nil {
			pc := px.Cursor()
			for k, v := pc.First(); k != nil; k, v = pc.Next() {
				var p map[string]any
				if err := json.Unmarshal(v, &p); err != nil {
					continue
				}
				if p["country"] != "US" || p["alive"] != true {
					continue
				}
				proto, _ := p["protocol"].(string)
				if proto != "http" && proto != "https" {
					continue
				}
				if u, _ := p["url"].(string); u != "" {
					usProxies = append(usProxies, u)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("view: %v", err)
	}
	if key == "" {
		t.Skip("no google credential in db")
	}

	out, err := os.Create("/Users/toli/playground/llm-router/test_shit/google_models.txt")
	if err != nil {
		t.Fatalf("create out: %v", err)
	}
	defer out.Close()

	clients := []*http.Client{{Timeout: 30 * time.Second}}
	if len(usProxies) > 40 {
		usProxies = usProxies[:40]
	}
	for _, pu := range usProxies {
		u, perr := url.Parse(pu)
		if perr != nil {
			continue
		}
		clients = append(clients, &http.Client{
			Timeout:   20 * time.Second,
			Transport: &http.Transport{Proxy: http.ProxyURL(u)},
		})
	}

	var lastErr error
	var client *http.Client
	for _, c := range clients {
		req, _ := http.NewRequest("GET", "https://generativelanguage.googleapis.com/v1beta/models?pageSize=1", nil)
		req.Header.Set("x-goog-api-key", key)
		resp, err := c.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode == 200 {
			client = c
			break
		}
		lastErr = fmt.Errorf("status %d: %s", resp.StatusCode, body[:min(len(body), 200)])
	}
	if client == nil {
		t.Fatalf("no working route to Google API: %v", lastErr)
	}
	url := "https://generativelanguage.googleapis.com/v1beta/models?pageSize=1000"
	for url != "" {
		req, _ := http.NewRequest("GET", url, nil)
		req.Header.Set("x-goog-api-key", key)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("status %d: %s", resp.StatusCode, body)
		}
		var page struct {
			Models []struct {
				Name                       string   `json:"name"`
				DisplayName                string   `json:"displayName"`
				SupportedGenerationMethods []string `json:"supportedGenerationMethods"`
				InputTokenLimit            int      `json:"inputTokenLimit"`
				OutputTokenLimit           int      `json:"outputTokenLimit"`
			} `json:"models"`
			NextPageToken string `json:"nextPageToken"`
		}
		if err := json.Unmarshal(body, &page); err != nil {
			t.Fatalf("decode: %v", err)
		}
		for _, m := range page.Models {
			name := strings.TrimPrefix(m.Name, "models/")
			fmt.Fprintf(out, "%s | %s | methods=%v | in=%d out=%d\n",
				name, m.DisplayName, m.SupportedGenerationMethods, m.InputTokenLimit, m.OutputTokenLimit)
		}
		if page.NextPageToken == "" {
			break
		}
		url = "https://generativelanguage.googleapis.com/v1beta/models?pageSize=1000&pageToken=" + page.NextPageToken
	}
}

// scratchGoogleKeys collects Google API keys: every google credential in the
// dev DB plus every key in test_shit/config.json ("google" array). Deduped.
func scratchGoogleKeys(t *testing.T, database *bolt.DB) []string {
	t.Helper()
	seen := map[string]bool{}
	var keys []string
	add := func(k string) {
		if k != "" && !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
	}
	_ = database.View(func(tx *bolt.Tx) error {
		creds := tx.Bucket([]byte("credentials"))
		cc := creds.Cursor()
		for k, v := cc.First(); k != nil; k, v = cc.Next() {
			var cred map[string]any
			if err := json.Unmarshal(v, &cred); err != nil {
				continue
			}
			if cred["provider_id"] != "google" || cred["disabled"] == true {
				continue
			}
			data, _ := cred["data"].(map[string]any)
			if s, _ := data["api_key"].(string); s != "" {
				add(s)
			}
		}
		return nil
	})
	raw, err := os.ReadFile("/Users/toli/playground/llm-router/test_shit/config.json")
	if err == nil {
		var cfg struct {
			Google []string `json:"google"`
		}
		if json.Unmarshal(raw, &cfg) == nil {
			for _, k := range cfg.Google {
				add(strings.TrimSpace(k))
			}
		}
	}
	if len(keys) == 0 {
		t.Skip("no google credentials available")
	}
	return keys
}

// scratchGoogleAccess opens the dev DB, collects Google API keys (DB plus
// test_shit/config.json) and builds up to 5 HTTP clients routed through
// working US pool proxies (direct egress is geo-blocked from this machine).
func scratchGoogleAccess(t *testing.T) ([]string, []*http.Client) {
	t.Helper()
	path := "/Users/toli/playground/llm-router/router.db"
	database, err := bolt.Open(path, 0600, &bolt.Options{ReadOnly: true, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer database.Close()

	keys := scratchGoogleKeys(t, database)

	var usProxies []string
	err = database.View(func(tx *bolt.Tx) error {
		if px := tx.Bucket([]byte("proxies")); px != nil {
			pc := px.Cursor()
			for k, v := pc.First(); k != nil; k, v = pc.Next() {
				var p map[string]any
				if err := json.Unmarshal(v, &p); err != nil {
					continue
				}
				if p["country"] != "US" || p["alive"] != true {
					continue
				}
				proto, _ := p["protocol"].(string)
				if proto != "http" && proto != "https" {
					continue
				}
				if u, _ := p["url"].(string); u != "" {
					usProxies = append(usProxies, u)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("view: %v", err)
	}

	clients := []*http.Client{{Timeout: 30 * time.Second}}
	if len(usProxies) > 40 {
		usProxies = usProxies[:40]
	}
	for _, pu := range usProxies {
		u, perr := url.Parse(pu)
		if perr != nil {
			continue
		}
		clients = append(clients, &http.Client{
			Timeout:   20 * time.Second,
			Transport: &http.Transport{Proxy: http.ProxyURL(u)},
		})
	}
	var lastErr error
	var working []*http.Client
	for _, c := range clients {
		if len(working) >= 5 {
			break
		}
		req, _ := http.NewRequest("GET", "https://generativelanguage.googleapis.com/v1beta/models?pageSize=1", nil)
		req.Header.Set("x-goog-api-key", keys[0])
		resp, err := c.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		_, _ = io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode == 200 {
			working = append(working, c)
			continue
		}
		lastErr = fmt.Errorf("status %d", resp.StatusCode)
	}
	if len(working) == 0 {
		t.Fatalf("no working route to Google API: %v", lastErr)
	}
	return keys, working
}

// scratchPostGenerateContent issues the same generateContent call the Lua
// plugin would send and returns the decoded body. Transport failures rotate
// to the next working client; an HTTP answer (even an error) is definitive.
func scratchPostGenerateContent(t *testing.T, clients []*http.Client, key, model string, payload map[string]any) (map[string]any, error) {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var lastErr error
	for _, client := range clients {
		req, _ := http.NewRequest("POST",
			"https://generativelanguage.googleapis.com/v1beta/models/"+model+":generateContent",
			strings.NewReader(string(raw)))
		req.Header.Set("x-goog-api-key", key)
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 {
			return nil, fmt.Errorf("%s status %d: %s", model, resp.StatusCode, body[:min(len(body), 500)])
		}
		var decoded map[string]any
		if err := json.Unmarshal(body, &decoded); err != nil {
			return nil, fmt.Errorf("decode: %v", err)
		}
		return decoded, nil
	}
	return nil, fmt.Errorf("%s: all clients failed: %v", model, lastErr)
}

func scratchInlineData(t *testing.T, g map[string]any, family string) (mime string, size int) {
	t.Helper()
	cands, _ := g["candidates"].([]any)
	for _, c := range cands {
		cand, _ := c.(map[string]any)
		content, _ := cand["content"].(map[string]any)
		parts, _ := content["parts"].([]any)
		for _, pv := range parts {
			part, _ := pv.(map[string]any)
			inline, _ := part["inlineData"].(map[string]any)
			if inline == nil {
				continue
			}
			m, _ := inline["mimeType"].(string)
			if strings.HasPrefix(m, family) {
				data, _ := inline["data"].(string)
				return m, len(data)
			}
		}
	}
	return "", 0
}

// TestScratchGoogleSpeechProbe verifies the exact TTS payload google.lua
// sends: AUDIO modality + prebuiltVoiceConfig must yield inlineData audio.
func TestScratchGoogleSpeechProbe(t *testing.T) {
	scratchLive(t)
	keys, clients := scratchGoogleAccess(t)
	var g map[string]any
	var lastErr error
	for i, key := range keys {
		var err error
		g, err = scratchPostGenerateContent(t, clients, key, "gemini-2.5-flash-preview-tts", map[string]any{
			"contents": []any{map[string]any{"role": "user", "parts": []any{map[string]any{"text": "Say cheerfully: probe ok"}}}},
			"generationConfig": map[string]any{
				"responseModalities": []string{"AUDIO"},
				"speechConfig": map[string]any{
					"voiceConfig": map[string]any{"prebuiltVoiceConfig": map[string]any{"voiceName": "Kore"}},
				},
			},
		})
		if err == nil {
			fmt.Printf("SPEECH PROBE: key #%d OK\n", i)
			break
		}
		lastErr = err
		t.Logf("speech probe key #%d: %v", i, err)
	}
	if g == nil {
		t.Fatalf("speech probe: all keys failed: %v", lastErr)
	}
	mime, size := scratchInlineData(t, g, "audio/")
	if mime == "" {
		raw, _ := json.Marshal(g)
		t.Fatalf("no audio inlineData in response: %s", raw[:min(len(raw), 500)])
	}
	t.Logf("speech ok: mime=%s b64len=%d", mime, size)
	fmt.Printf("SPEECH PROBE: mime=%s b64len=%d\n", mime, size)
}

// TestScratchGoogleImageProbe verifies the exact image payload google.lua
// sends: TEXT+IMAGE modalities must yield inlineData image bytes.
func TestScratchGoogleImageProbe(t *testing.T) {
	scratchLive(t)
	keys, clients := scratchGoogleAccess(t)
	var lastErr error
	succeeded := false
outer:
	for ki, key := range keys {
		for _, model := range []string{"gemini-3-pro-image-preview", "nano-banana-pro-preview", "gemini-3.1-flash-lite-image", "gemini-3.1-flash-image", "gemini-2.5-flash-image"} {
			g, err := scratchPostGenerateContent(t, clients, key, model, map[string]any{
				"contents": []any{map[string]any{"role": "user", "parts": []any{map[string]any{"text": "A small red circle on white background"}}}},
				"generationConfig": map[string]any{
					"responseModalities": []string{"TEXT", "IMAGE"},
					"imageConfig":        map[string]any{"aspectRatio": "1:1"},
				},
			})
			if err != nil {
				lastErr = err
				t.Logf("key #%d %s: %v", ki, model, err)
				continue
			}
			mime, size := scratchInlineData(t, g, "image/")
			fmt.Printf("IMAGE PROBE key #%d %s: mime=%s b64len=%d\n", ki, model, mime, size)
			if mime != "" {
				succeeded = true
				break outer
			}
			raw, _ := json.Marshal(g)
			lastErr = fmt.Errorf("%s: no image inlineData: %s", model, raw[:min(len(raw), 500)])
			t.Logf("key #%d %s", ki, lastErr)
		}
	}
	if !succeeded {
		t.Fatalf("no key/model combination yielded an image; last: %v", lastErr)
	}
}

// TestScratchGoogleImageQuota prints the full 429 body for one image model
// so the quota wall is visible verbatim (free-tier limit vs daily budget).
func TestScratchGoogleImageQuota(t *testing.T) {
	scratchLive(t)
	keys, clients := scratchGoogleAccess(t)
	payload := map[string]any{
		"contents": []any{map[string]any{"role": "user", "parts": []any{map[string]any{"text": "A small red circle"}}}},
		"generationConfig": map[string]any{
			"responseModalities": []string{"TEXT", "IMAGE"},
		},
	}
	for ki, key := range keys {
		raw, _ := json.Marshal(payload)
		for _, client := range clients {
			req, _ := http.NewRequest("POST",
				"https://generativelanguage.googleapis.com/v1beta/models/gemini-2.5-flash-image:generateContent",
				strings.NewReader(string(raw)))
			req.Header.Set("x-goog-api-key", key)
			req.Header.Set("Content-Type", "application/json")
			resp, err := client.Do(req)
			if err != nil {
				continue
			}
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			fmt.Printf("IMAGE QUOTA key #%d status=%d body=%s\n", ki, resp.StatusCode, body[:min(len(body), 1200)])
			break
		}
	}
}

// TestScratchGoogleEmbedProbe verifies the exact batchEmbedContents payload
// google.lua sends, rotating keys on quota errors.
func TestScratchGoogleEmbedProbe(t *testing.T) {
	scratchLive(t)
	keys, clients := scratchGoogleAccess(t)
	payload := map[string]any{
		"requests": []any{
			map[string]any{
				"model":                "models/gemini-embedding-001",
				"content":              map[string]any{"parts": []any{map[string]any{"text": "hello world"}}},
				"outputDimensionality": 256,
			},
			map[string]any{
				"model":                "models/gemini-embedding-001",
				"content":              map[string]any{"parts": []any{map[string]any{"text": "second input"}}},
				"outputDimensionality": 256,
			},
		},
	}
	var lastErr error
	for ki, key := range keys {
		raw, _ := json.Marshal(payload)
		var lastTransportErr error
		for _, client := range clients {
			req, _ := http.NewRequest("POST",
				"https://generativelanguage.googleapis.com/v1beta/models/gemini-embedding-001:batchEmbedContents",
				strings.NewReader(string(raw)))
			req.Header.Set("x-goog-api-key", key)
			req.Header.Set("Content-Type", "application/json")
			resp, err := client.Do(req)
			if err != nil {
				lastTransportErr = err
				continue
			}
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode != 200 {
				lastErr = fmt.Errorf("status %d: %s", resp.StatusCode, body[:min(len(body), 300)])
				break
			}
			var g struct {
				Embeddings []struct {
					Values []float64 `json:"values"`
				} `json:"embeddings"`
			}
			if err := json.Unmarshal(body, &g); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if len(g.Embeddings) != 2 || len(g.Embeddings[0].Values) == 0 {
				t.Fatalf("unexpected embeddings shape: %s", body[:min(len(body), 300)])
			}
			fmt.Printf("EMBED PROBE key #%d: vectors=%d dims=%d,%d first=%.4f\n",
				ki, len(g.Embeddings), len(g.Embeddings[0].Values), len(g.Embeddings[1].Values), g.Embeddings[0].Values[0])
			return
		}
		if lastErr == nil {
			lastErr = lastTransportErr
		}
		t.Logf("embed probe key #%d: %v", ki, lastErr)
		lastErr = nil
	}
	t.Fatal("embed probe: all keys failed")
}
