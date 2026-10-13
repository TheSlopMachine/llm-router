package shared

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// uploadClient bounds one plugin upload attempt, sequential calls only.
var uploadClient = &http.Client{Timeout: 120 * time.Second}

// PluginRow is one installed plugin record: ID, version, claimed type keys.
type PluginRow struct {
	ID          string   `json:"id"`
	DisplayName string   `json:"display_name"`
	Version     string   `json:"version"`
	TypeKeys    []string `json:"type_keys"`
}

// AdminBaseURL builds the admin listener URL. The admin listener binds
// loopback regardless of HOST, so this never uses HOST.
func AdminBaseURL() string {
	return fmt.Sprintf("http://127.0.0.1:%s", Getenv("ADMIN_PORT", DevAdminPort))
}

// DefaultStoreDir is the sibling plugin store checkout layout.
func DefaultStoreDir() string { return "../../llm-router-store/llm-router-plugins" }

// WaitReady polls status until the backend listens or 120 seconds pass.
func WaitReady(web string) error {
	return WaitReadyTimeout(web, 120*time.Second)
}

// WaitReadyTimeout polls status until the backend listens or timeout
// passes.
func WaitReadyTimeout(web string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		statusCode, raw, err := uploadJSON("GET", web+"/api/llm-router/status", nil)
		if err == nil && statusCode == 200 {
			var statusBody struct {
				Bootstrapped bool `json:"bootstrapped"`
			}
			if jerr := json.Unmarshal(raw, &statusBody); jerr == nil {
				return nil
			}
		}
		if time.Now().After(deadline) {
			if err != nil {
				return fmt.Errorf("backend not ready: %w", err)
			}
			return fmt.Errorf("backend not ready: status %d", statusCode)
		}
		time.Sleep(2 * time.Second)
	}
}

// PluginSource reads the .lua source for a type key from the store dir.
func PluginSource(storeDir, pluginType string) ([]byte, error) {
	src, err := os.ReadFile(filepath.Join(storeDir, pluginType+".lua"))
	if err != nil {
		return nil, fmt.Errorf("plugin source for %q: %w", pluginType, err)
	}
	return src, nil
}

// ManifestVersion reads --- @version from a plugin source.
func ManifestVersion(source []byte) string {
	for _, line := range strings.Split(string(source), "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "--- @version "); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// ListPlugins returns installed plugin records.
func ListPlugins(web string) ([]PluginRow, error) {
	statusCode, raw, err := uploadJSON("GET", web+"/api/llm-router/dashboard/plugins", nil)
	if err != nil {
		return nil, fmt.Errorf("list plugins: %w", err)
	}
	var rows []PluginRow
	if err := RequireUploadOK(statusCode, raw, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

// ManifestName reads --- @plugin from a plugin source.
func ManifestName(source []byte) string {
	for _, line := range strings.Split(string(source), "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "--- @plugin "); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// installedCurrentName reports whether a keyless plugin with the display
// name is already installed at the source version.
func installedCurrentName(rows []PluginRow, name, version string) bool {
	if name == "" {
		return false
	}
	for _, plugin := range rows {
		if len(plugin.TypeKeys) == 0 && plugin.DisplayName == name && plugin.Version == version {
			return true
		}
	}
	return false
}

func deletePlugin(web, id string) error {
	// Record IDs contain slashes: encode like the dashboard
	// frontend does, the API matches single encoded segments.
	target := web + "/api/llm-router/dashboard/plugins/" + url.PathEscape(id)
	statusCode, raw, err := uploadJSON("DELETE", target, nil)
	if err != nil {
		return fmt.Errorf("delete plugin %s: %w", id, err)
	}
	if err := RequireUploadOK(statusCode, raw, nil); err != nil {
		return fmt.Errorf("delete plugin %s: %w", id, err)
	}
	return nil
}

// InstallPlugin installs one plugin source, skipping reinstalls when the
// installed version already matches. Returns true when an install ran.
func InstallPlugin(web, pluginType string, source []byte) (bool, error) {
	version := ManifestVersion(source)
	name := ManifestName(source)
	rows, err := ListPlugins(web)
	if err != nil {
		return false, err
	}
	if version != "" {
		for _, plugin := range rows {
			for _, key := range plugin.TypeKeys {
				if key == pluginType && plugin.Version == version {
					return false, nil
				}
			}
		}
		if installedCurrentName(rows, name, version) {
			return false, nil
		}
	}
	for _, plugin := range rows {
		matched := false
		for _, key := range plugin.TypeKeys {
			if key == pluginType {
				matched = true
				break
			}
		}
		if !matched && name != "" && len(plugin.TypeKeys) == 0 && plugin.DisplayName == name {
			matched = true
		}
		if matched {
			if err := deletePlugin(web, plugin.ID); err != nil {
				return false, err
			}
		}
	}
	statusCode, raw, installError := uploadRaw("POST", web+"/api/llm-router/dashboard/plugins/install-file", source)
	if installError != nil {
		return false, fmt.Errorf("install plugin: %w", installError)
	}
	if err := RequireUploadOK(statusCode, raw, nil); err != nil {
		return false, err
	}
	return true, nil
}

// uploadJSON sends a JSON request and returns status plus body.
func uploadJSON(method, url string, body any) (int, []byte, error) {
	var bodyReader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		bodyReader = bytes.NewReader(encoded)
	}
	request, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		return 0, nil, err
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := uploadClient.Do(request)
	if err != nil {
		return 0, nil, err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil {
		return 0, nil, err
	}
	return response.StatusCode, raw, nil
}

// uploadRaw POSTs raw bytes (plugin sources) and returns status plus body.
func uploadRaw(method, url string, body []byte) (int, []byte, error) {
	request, err := http.NewRequest(method, url, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	request.Header.Set("Content-Type", "application/octet-stream")
	response, err := uploadClient.Do(request)
	if err != nil {
		return 0, nil, err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil {
		return 0, nil, err
	}
	return response.StatusCode, raw, nil
}

// RequireUploadOK decodes a 2xx JSON body into output, else returns an error.
func RequireUploadOK(status int, raw []byte, output any) error {
	if status < 200 || status >= 300 {
		return fmt.Errorf("http %d: %s", status, strings.TrimSpace(string(raw)))
	}
	if output == nil {
		return nil
	}
	if err := json.Unmarshal(raw, output); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}
