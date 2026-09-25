package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

// provision ensures the instance is bootstrapped, installs the plugin
// source and resolves usable credentials from the dev database. The pool
// fails over across keys inside every request, so the matrix needs no
// per-credential loop; the credential-test below iterates them instead.
// Cleanup is non-nil only for the ephemeral mock credential. No usable
// credential is errNoAccount.
func provision(cfg config, pluginType string) (providerID string, creds []credCandidate, cleanup func(), err error) {
	if err := ensureBootstrapped(cfg); err != nil {
		return "", nil, nil, err
	}
	source, err := pluginSource(cfg, pluginType)
	if err != nil {
		return "", nil, nil, err
	}
	if err := installPlugin(cfg, pluginType, source); err != nil {
		return "", nil, nil, err
	}
	providerID, err = findProvider(cfg, pluginType)
	if err != nil {
		return "", nil, nil, err
	}
	if pluginType == "mock" {
		credID, err := addCredential(cfg, providerID, map[string]any{})
		if err != nil {
			return "", nil, nil, err
		}
		me := []credCandidate{{ID: credID, Label: "smoke", ProviderID: providerID}}
		if !cfg.cleanup {
			return providerID, me, nil, nil
		}
		return providerID, me, func() { deleteCredential(cfg, credID) }, nil
	}
	creds, err = listUsableCredentials(cfg, providerID)
	if err != nil {
		return "", nil, nil, err
	}
	if len(creds) == 0 {
		return "", nil, nil, errNoAccount
	}
	return providerID, creds, nil, nil
}

// waitReady polls status until the backend listens or the deadline passes.
// Restart returns before the backend accepts connections.
func waitReady(cfg config) error {
	deadline := time.Now().Add(120 * time.Second)
	for {
		status, raw, err := doJSON("GET", cfg.web+"/api/llm-router/status", nil)
		if err == nil {
			var st struct {
				Bootstrapped bool `json:"bootstrapped"`
			}
			if jerr := json.Unmarshal(raw, &st); jerr == nil && status == 200 {
				return nil
			}
		}
		if time.Now().After(deadline) {
			if err != nil {
				return fmt.Errorf("backend not ready: %w", err)
			}
			return fmt.Errorf("backend not ready: status %d", status)
		}
		time.Sleep(2 * time.Second)
	}
}

func ensureBootstrapped(cfg config) error {
	status, raw, err := doJSON("GET", cfg.web+"/api/llm-router/status", nil)
	if err != nil {
		return fmt.Errorf("status: %w", err)
	}
	var st struct {
		Bootstrapped bool `json:"bootstrapped"`
	}
	if err := requireOK(status, raw, &st); err != nil {
		return err
	}
	if st.Bootstrapped {
		return nil
	}
	var rnd [16]byte
	if _, err := rand.Read(rnd[:]); err != nil {
		return err
	}
	status, raw, err = doJSON("POST", cfg.web+"/api/llm-router/bootstrap", map[string]any{
		"username": "smoke",
		"password": hex.EncodeToString(rnd[:]),
	})
	if err != nil {
		return fmt.Errorf("bootstrap: %w", err)
	}
	return requireOK(status, raw, nil)
}

// pluginSource reads the .lua source: the bundled mock or storeDir/<type>.lua.
func pluginSource(cfg config, pluginType string) ([]byte, error) {
	if pluginType == "mock" {
		_, thisFile, _, _ := runtime.Caller(0)
		return os.ReadFile(filepath.Join(filepath.Dir(thisFile), "testdata", "mock.lua"))
	}
	src, err := os.ReadFile(filepath.Join(cfg.store, pluginType+".lua"))
	if err != nil {
		return nil, fmt.Errorf("plugin source for %q: %w", pluginType, err)
	}
	return src, nil
}

func installPlugin(cfg config, pluginType string, source []byte) error {
	if v := manifestVersion(source); v != "" {
		if ok, err := installedCurrent(cfg, pluginType, v); err != nil {
			return err
		} else if ok {
			return nil
		}
	}
	// A store-installed copy claims the type key: replace it with the
	// local source under test, then install. Dev DB only.
	if err := deleteTypeClaimants(cfg, pluginType); err != nil {
		return err
	}
	status, raw, err := doRaw("POST", cfg.web+"/api/llm-router/dashboard/plugins/install-file", source)
	if err != nil {
		return fmt.Errorf("install plugin: %w", err)
	}
	return requireOK(status, raw, nil)
}

// manifestVersion reads --- @version from a plugin source.
func manifestVersion(source []byte) string {
	for _, line := range strings.Split(string(source), "\n") {
		line = strings.TrimSpace(line)
		if v, ok := strings.CutPrefix(line, "--- @version "); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

type pluginRow struct {
	ID       string   `json:"id"`
	Version  string   `json:"version"`
	TypeKeys []string `json:"type_keys"`
}

func listPlugins(cfg config) ([]pluginRow, error) {
	status, raw, err := doJSON("GET", cfg.web+"/api/llm-router/dashboard/plugins", nil)
	if err != nil {
		return nil, fmt.Errorf("list plugins: %w", err)
	}
	var rows []pluginRow
	if err := requireOK(status, raw, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

// deleteTypeClaimants removes installed plugins claiming the type key so a
// local source can take it over. Version drift already failed above; this
// runs only when testing a different copy of the same type.
func deleteTypeClaimants(cfg config, pluginType string) error {
	rows, err := listPlugins(cfg)
	if err != nil {
		return err
	}
	for _, p := range rows {
		for _, k := range p.TypeKeys {
			if k != pluginType {
				continue
			}
			// Record IDs contain slashes: encode like the dashboard
			// frontend does, the API matches single encoded segments.
			target := cfg.web + "/api/llm-router/dashboard/plugins/" + url.PathEscape(p.ID)
			status, raw, derr := doJSON("DELETE", target, nil)
			if derr != nil {
				return fmt.Errorf("delete plugin %s: %w", p.ID, derr)
			}
			if derr := requireOK(status, raw, nil); derr != nil {
				return fmt.Errorf("delete plugin %s: %w", p.ID, derr)
			}
		}
	}
	return nil
}

// installedCurrent reports whether the type is already installed at the
// source version: reinstalling would only churn history. A version drift
// falls through to replacement below.
func installedCurrent(cfg config, pluginType, version string) (bool, error) {
	rows, err := listPlugins(cfg)
	if err != nil {
		return false, err
	}
	for _, p := range rows {
		for _, k := range p.TypeKeys {
			if k == pluginType {
				return p.Version == version, nil
			}
		}
	}
	return false, nil
}

type providerRow struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	TypeKey string `json:"type_key"`
}

func findProvider(cfg config, pluginType string) (string, error) {
	status, raw, err := doJSON("GET", cfg.web+"/api/llm-router/dashboard/providers", nil)
	if err != nil {
		return "", fmt.Errorf("list providers: %w", err)
	}
	var rows []providerRow
	if err := requireOK(status, raw, &rows); err != nil {
		return "", err
	}
	for _, p := range rows {
		if p.TypeKey == pluginType {
			return p.ID, nil
		}
	}
	return "", fmt.Errorf("no provider instance for type %q after install", pluginType)
}

// errNoAccount reports no usable credential in the dev database: skip the
// plugin, never fail.
var errNoAccount = fmt.Errorf("no credential in db")

type credRow struct {
	ID         string `json:"id"`
	ProviderID string `json:"provider_id"`
	Label      string `json:"label"`
	Disabled   bool   `json:"disabled"`
	IsExpired  bool   `json:"is_expired"`
	UpdatedAt  string `json:"updated_at"`
}

// credCandidate is one enabled, non-expired credential row.
type credCandidate struct {
	ID         string
	Label      string
	ProviderID string
	UpdatedAt  string
}

// listUsableCredentials returns enabled, non-expired credentials of the
// provider, most recently updated first.
func listUsableCredentials(cfg config, providerID string) ([]credCandidate, error) {
	status, raw, err := doJSON("GET", cfg.web+"/api/llm-router/dashboard/credentials", nil)
	if err != nil {
		return nil, fmt.Errorf("list credentials: %w", err)
	}
	var rows []credRow
	if err := requireOK(status, raw, &rows); err != nil {
		return nil, err
	}
	var out []credCandidate
	for _, c := range rows {
		if c.ProviderID == providerID && !c.Disabled && !c.IsExpired {
			out = append(out, credCandidate{ID: c.ID, Label: c.Label, ProviderID: c.ProviderID, UpdatedAt: c.UpdatedAt})
		}
	}
	// Most recently touched first (RFC3339 sorts lexically); stable IDs
	// break ties deterministically.
	sort.Slice(out, func(i, j int) bool {
		if out[i].UpdatedAt != out[j].UpdatedAt {
			return out[i].UpdatedAt > out[j].UpdatedAt
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func addCredential(cfg config, providerID string, data map[string]any) (string, error) {
	status, raw, err := doJSON("POST", cfg.web+"/api/llm-router/dashboard/credentials", map[string]any{
		"provider_id": providerID,
		"label":       "smoke",
		"data":        data,
	})
	if err != nil {
		return "", fmt.Errorf("add credential: %w", err)
	}
	var cred struct {
		ID string `json:"id"`
	}
	if err := requireOK(status, raw, &cred); err != nil {
		return "", err
	}
	if cred.ID == "" {
		return "", fmt.Errorf("add credential: empty id")
	}
	return cred.ID, nil
}

func deleteCredential(cfg config, credID string) {
	status, raw, err := doJSON("DELETE", cfg.web+"/api/llm-router/dashboard/credentials/"+credID, nil)
	if err == nil {
		_ = requireOK(status, raw, nil)
	}
}
