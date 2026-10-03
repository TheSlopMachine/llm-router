package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"time"

	"github.com/TheSlopMachine/llm-router/scripts/shared"
)

// provision ensures the instance is bootstrapped, installs the plugin
// source and resolves usable credentials from the dev database. The pool
// fails over across keys inside every request, so the matrix needs no
// per-credential loop; the credential-test below iterates them instead.
// Cleanup is non-nil only for the ephemeral mock credential. No usable
// credential is errNoCredential.
func provision(cfg config, pluginType string) (providerID string, creds []credCandidate, cleanup func(), err error) {
	if err := ensureBootstrapped(cfg); err != nil {
		return "", nil, nil, err
	}
	source, err := pluginSource(cfg, pluginType)
	if err != nil {
		return "", nil, nil, err
	}
	if _, err := shared.InstallPlugin(cfg.web, pluginType, source); err != nil {
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
	if enabled, err := credentialsEnabled(cfg, providerID); err != nil {
		return "", nil, nil, err
	} else if !enabled {
		// Credential-less provider types (anonymous tiers) run the
		// matrix without accounts; keyed types still skip below.
		return providerID, nil, nil, nil
	}
	creds, err = listUsableCredentials(cfg, providerID)
	if err != nil {
		return "", nil, nil, err
	}
	if len(creds) == 0 {
		return "", nil, nil, errNoCredential
	}
	return providerID, creds, nil, nil
}

// credentialsEnabled reports the provider's credential-schema capability
// flag: false means the type serves no credential table. Unknown endpoints
// fail loudly: the harness tracks the in-tree router, so a missing flag
// is incompatibility, never a skip.
func credentialsEnabled(cfg config, providerID string) (bool, error) {
	status, raw, err := doJSON("GET", cfg.web+"/api/llm-router/dashboard/providers/"+providerID+"/credential-schema", nil)
	if err != nil {
		return false, fmt.Errorf("credential schema: %w", err)
	}
	var view struct {
		Enabled bool `json:"enabled"`
	}
	if err := requireOK(status, raw, &view); err != nil {
		return false, err
	}
	return view.Enabled, nil
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
	return shared.PluginSource(cfg.store, pluginType)
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

// errNoCredential reports no usable credential in the dev database: skip the
// plugin, never fail.
var errNoCredential = fmt.Errorf("no credential in db")

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
