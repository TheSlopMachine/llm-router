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

// WebBaseURL builds http://HOST:port from the Makefile-exported HOST and
// port env (WEB_PORT default 38080).
func WebBaseURL(portEnv, defPort string) string {
	return fmt.Sprintf("http://%s:%s", Getenv("HOST", "localhost"), Getenv(portEnv, defPort))
}

// DefaultStoreDir is the sibling plugin store checkout layout.
func DefaultStoreDir() string { return "../../llm-router-store/llm-router-plugins" }

// WaitReady polls status until the backend listens or the deadline passes.
func WaitReady(web string) error {
	deadline := time.Now().Add(120 * time.Second)
	for {
		status, raw, err := uploadJSON("GET", web+"/api/llm-router/status", nil)
		if err == nil && status == 200 {
			var st struct {
				Bootstrapped bool `json:"bootstrapped"`
			}
			if jerr := json.Unmarshal(raw, &st); jerr == nil {
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
	status, raw, err := uploadJSON("GET", web+"/api/llm-router/dashboard/plugins", nil)
	if err != nil {
		return nil, fmt.Errorf("list plugins: %w", err)
	}
	var rows []PluginRow
	if err := RequireUploadOK(status, raw, &rows); err != nil {
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
	for _, p := range rows {
		if len(p.TypeKeys) == 0 && p.DisplayName == name && p.Version == version {
			return true
		}
	}
	return false
}

func deletePlugin(web, id string) error {
	// Record IDs contain slashes: encode like the dashboard
	// frontend does, the API matches single encoded segments.
	target := web + "/api/llm-router/dashboard/plugins/" + url.PathEscape(id)
	status, raw, err := uploadJSON("DELETE", target, nil)
	if err != nil {
		return fmt.Errorf("delete plugin %s: %w", id, err)
	}
	if err := RequireUploadOK(status, raw, nil); err != nil {
		return fmt.Errorf("delete plugin %s: %w", id, err)
	}
	return nil
}

// InstallPlugin installs one plugin source, skipping reinstalls when the
// installed version already matches. Returns true when an install ran.
func InstallPlugin(web, pluginType string, source []byte) (bool, error) {
	v := ManifestVersion(source)
	name := ManifestName(source)
	rows, err := ListPlugins(web)
	if err != nil {
		return false, err
	}
	if v != "" {
		for _, p := range rows {
			for _, k := range p.TypeKeys {
				if k == pluginType && p.Version == v {
					return false, nil
				}
			}
		}
		if installedCurrentName(rows, name, v) {
			return false, nil
		}
	}
	for _, p := range rows {
		matched := false
		for _, k := range p.TypeKeys {
			if k == pluginType {
				matched = true
				break
			}
		}
		if !matched && name != "" && len(p.TypeKeys) == 0 && p.DisplayName == name {
			matched = true
		}
		if matched {
			if err := deletePlugin(web, p.ID); err != nil {
				return false, err
			}
		}
	}
	status, raw, rerr := uploadRaw("POST", web+"/api/llm-router/dashboard/plugins/install-file", source)
	if rerr != nil {
		return false, fmt.Errorf("install plugin: %w", rerr)
	}
	if err := RequireUploadOK(status, raw, nil); err != nil {
		return false, err
	}
	return true, nil
}

// uploadJSON sends a JSON request and returns status plus body.
func uploadJSON(method, url string, body any) (int, []byte, error) {
	var rdr io.Reader
	if body != nil {
		enc, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		rdr = bytes.NewReader(enc)
	}
	req, err := http.NewRequest(method, url, rdr)
	if err != nil {
		return 0, nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := uploadClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return 0, nil, err
	}
	return resp.StatusCode, raw, nil
}

// uploadRaw POSTs raw bytes (plugin sources) and returns status plus body.
func uploadRaw(method, url string, body []byte) (int, []byte, error) {
	req, err := http.NewRequest(method, url, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err := uploadClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return 0, nil, err
	}
	return resp.StatusCode, raw, nil
}

// RequireUploadOK decodes a 2xx JSON body into out, else returns an error.
func RequireUploadOK(status int, raw []byte, out any) error {
	if status < 200 || status >= 300 {
		return fmt.Errorf("http %d: %s", status, strings.TrimSpace(string(raw)))
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}
