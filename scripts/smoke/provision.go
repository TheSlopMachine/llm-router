package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"

	"github.com/TheSlopMachine/llm-router/scripts/shared"
)

// provision ensures the instance is bootstrapped, installs the plugin
// source and resolves usable credentials from the dev database. The pool
// fails over across keys inside every request, so the matrix needs no
// per-credential loop; the credential-test below iterates them instead.
// Cleanup is non-nil only for the ephemeral mock credential. No usable
// credential is errNoCredential.
func provision(testConfiguration configuration, pluginType string) (providerID string, credentials []credentialCandidate, cleanup func(), err error) {
	if err := ensureBootstrapped(testConfiguration); err != nil {
		return "", nil, nil, err
	}
	source, err := pluginSource(testConfiguration, pluginType)
	if err != nil {
		return "", nil, nil, err
	}
	if _, err := shared.InstallPlugin(testConfiguration.web, pluginType, source); err != nil {
		return "", nil, nil, err
	}
	providerID, err = findProvider(testConfiguration, pluginType)
	if err != nil {
		return "", nil, nil, err
	}
	if pluginType == "mock" {
		credentialID, err := addCredential(testConfiguration, providerID, map[string]any{})
		if err != nil {
			return "", nil, nil, err
		}
		mockCredentials := []credentialCandidate{{ID: credentialID, Label: "smoke", ProviderID: providerID}}
		if !testConfiguration.cleanup {
			return providerID, mockCredentials, nil, nil
		}
		return providerID, mockCredentials, func() { deleteCredential(testConfiguration, credentialID) }, nil
	}
	if enabled, err := credentialsEnabled(testConfiguration, providerID); err != nil {
		return "", nil, nil, err
	} else if !enabled {
		// Credential-less provider types (anonymous tiers) run the
		// matrix without accounts; keyed types still skip below.
		return providerID, nil, nil, nil
	}
	credentials, err = listUsableCredentials(testConfiguration, providerID)
	if err != nil {
		return "", nil, nil, err
	}
	if len(credentials) == 0 {
		return "", nil, nil, errNoCredential
	}
	return providerID, credentials, nil, nil
}

// credentialsEnabled reports the provider's credential-schema capability
// flag: false means the type serves no credential table. Unknown endpoints
// fail loudly: the harness tracks the in-tree router, so a missing flag
// is incompatibility, never a skip.
func credentialsEnabled(testConfiguration configuration, providerID string) (bool, error) {
	statusCode, raw, err := doJSON("GET", testConfiguration.web+"/api/llm-router/dashboard/providers/"+providerID+"/credential-schema", nil)
	if err != nil {
		return false, fmt.Errorf("credential schema: %w", err)
	}
	var view struct {
		Enabled bool `json:"enabled"`
	}
	if err := requireOK(statusCode, raw, &view); err != nil {
		return false, err
	}
	return view.Enabled, nil
}

// waitReady polls status until the backend listens or the deadline passes.
// Restart returns before the backend accepts connections. Delegates to
// shared.WaitReady; behavior is identical.
func waitReady(testConfiguration configuration) error {
	return shared.WaitReady(testConfiguration.web)
}

func ensureBootstrapped(testConfiguration configuration) error {
	statusCode, raw, err := doJSON("GET", testConfiguration.web+"/api/llm-router/status", nil)
	if err != nil {
		return fmt.Errorf("status: %w", err)
	}
	var bootstrapStatus struct {
		Bootstrapped bool `json:"bootstrapped"`
	}
	if err := requireOK(statusCode, raw, &bootstrapStatus); err != nil {
		return err
	}
	if bootstrapStatus.Bootstrapped {
		return nil
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return err
	}
	statusCode, raw, err = doJSON("POST", testConfiguration.web+"/api/llm-router/bootstrap", map[string]any{
		"username": "smoke",
		"password": hex.EncodeToString(random[:]),
	})
	if err != nil {
		return fmt.Errorf("bootstrap: %w", err)
	}
	return requireOK(statusCode, raw, nil)
}

// pluginSource reads the .lua source: the bundled mock or storeDir/<type>.lua.
func pluginSource(testConfiguration configuration, pluginType string) ([]byte, error) {
	if pluginType == "mock" {
		_, thisFile, _, _ := runtime.Caller(0)
		return os.ReadFile(filepath.Join(filepath.Dir(thisFile), "testdata", "mock.lua"))
	}
	return shared.PluginSource(testConfiguration.store, pluginType)
}

type providerRow struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	TypeKey string `json:"type_key"`
}

func findProvider(testConfiguration configuration, pluginType string) (string, error) {
	statusCode, raw, err := doJSON("GET", testConfiguration.web+"/api/llm-router/dashboard/providers", nil)
	if err != nil {
		return "", fmt.Errorf("list providers: %w", err)
	}
	var rows []providerRow
	if err := requireOK(statusCode, raw, &rows); err != nil {
		return "", err
	}
	for _, provider := range rows {
		if provider.TypeKey == pluginType {
			return provider.ID, nil
		}
	}
	return "", fmt.Errorf("no provider instance for type %q after install", pluginType)
}

// errNoCredential reports no usable credential in the dev database: skip the
// plugin, never fail.
var errNoCredential = fmt.Errorf("no credential in db")

type credentialRow struct {
	ID         string `json:"id"`
	ProviderID string `json:"provider_id"`
	Label      string `json:"label"`
	Disabled   bool   `json:"disabled"`
	IsExpired  bool   `json:"is_expired"`
	UpdatedAt  string `json:"updated_at"`
}

// credentialCandidate is one enabled, non-expired credential row.
type credentialCandidate struct {
	ID         string
	Label      string
	ProviderID string
	UpdatedAt  string
}

// listUsableCredentials returns enabled, non-expired credentials of the
// provider, most recently updated first.
func listUsableCredentials(testConfiguration configuration, providerID string) ([]credentialCandidate, error) {
	statusCode, raw, err := doJSON("GET", testConfiguration.web+"/api/llm-router/dashboard/credentials", nil)
	if err != nil {
		return nil, fmt.Errorf("list credentials: %w", err)
	}
	var rows []credentialRow
	if err := requireOK(statusCode, raw, &rows); err != nil {
		return nil, err
	}
	var output []credentialCandidate
	for _, credential := range rows {
		if credential.ProviderID == providerID && !credential.Disabled && !credential.IsExpired {
			output = append(output, credentialCandidate{ID: credential.ID, Label: credential.Label, ProviderID: credential.ProviderID, UpdatedAt: credential.UpdatedAt})
		}
	}
	// Most recently touched first (RFC3339 sorts lexically); stable IDs
	// break ties deterministically.
	sort.Slice(output, func(i, j int) bool {
		if output[i].UpdatedAt != output[j].UpdatedAt {
			return output[i].UpdatedAt > output[j].UpdatedAt
		}
		return output[i].ID < output[j].ID
	})
	return output, nil
}

func addCredential(testConfiguration configuration, providerID string, data map[string]any) (string, error) {
	statusCode, raw, err := doJSON("POST", testConfiguration.web+"/api/llm-router/dashboard/credentials", map[string]any{
		"provider_id": providerID,
		"label":       "smoke",
		"data":        data,
	})
	if err != nil {
		return "", fmt.Errorf("add credential: %w", err)
	}
	var credential struct {
		ID string `json:"id"`
	}
	if err := requireOK(statusCode, raw, &credential); err != nil {
		return "", err
	}
	if credential.ID == "" {
		return "", fmt.Errorf("add credential: empty id")
	}
	return credential.ID, nil
}

func deleteCredential(testConfiguration configuration, credentialID string) {
	statusCode, raw, err := doJSON("DELETE", testConfiguration.web+"/api/llm-router/dashboard/credentials/"+credentialID, nil)
	if err == nil {
		_ = requireOK(statusCode, raw, nil)
	}
}
