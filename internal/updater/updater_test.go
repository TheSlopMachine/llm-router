package updater

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/version"
)

func TestLatestParsesRelease(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/TheSlopMachine/llm-router/releases/latest" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if r.Header.Get("User-Agent") == "" {
			t.Error("missing User-Agent")
		}
		_, _ = w.Write([]byte(`{"tag_name":"v1.2.3","assets":[{"name":"checksums.txt","browser_download_url":"http://x/checksums.txt"},{"name":"llm-router_linux_amd64.zip","browser_download_url":"http://x/a.zip"}]}`))
	}))
	defer server.Close()
	release, err := Client{APIBase: server.URL}.Latest(context.Background())
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if release.Tag != "v1.2.3" || release.Assets["llm-router_linux_amd64.zip"] != "http://x/a.zip" || len(release.Assets) != 2 {
		t.Errorf("release = %+v", release)
	}
}

func TestLatestErrors(t *testing.T) {
	cases := []struct {
		status int
		header map[string]string
		check  func(error) bool
	}{
		{http.StatusNotFound, nil, func(err error) bool { return errors.Is(err, ErrNoRelease) }},
		{http.StatusForbidden, map[string]string{"X-RateLimit-Reset": "1700000000"}, func(err error) bool { return strings.Contains(err.Error(), "rate limit") }},
		{http.StatusInternalServerError, nil, func(err error) bool { return strings.Contains(err.Error(), "500") }},
	}
	for _, testCase := range cases {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			for name, value := range testCase.header {
				w.Header().Set(name, value)
			}
			w.WriteHeader(testCase.status)
		}))
		_, err := Client{APIBase: server.URL}.Latest(context.Background())
		server.Close()
		if err == nil || !testCase.check(err) {
			t.Errorf("status %d: error = %v", testCase.status, err)
		}
	}
}

func TestDownloadLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(bytes.Repeat([]byte("a"), 100))
	}))
	defer server.Close()
	if data, err := (Client{}).Download(context.Background(), server.URL, 100); err != nil || len(data) != 100 {
		t.Errorf("Download at limit = %d bytes, %v", len(data), err)
	}
	if _, err := (Client{}).Download(context.Background(), server.URL, 99); err == nil {
		t.Error("Download above limit succeeded")
	}
}

func TestCompare(t *testing.T) {
	parse := func(text string) version.Version {
		parsed, err := version.Parse(text)
		if err != nil {
			t.Fatal(err)
		}
		return parsed
	}
	cases := []struct {
		current, latest string
		want            int
	}{
		{"0.4.3", "0.4.4", -1},
		{"0.4.3", "v0.5", -1},
		{"0.4.3", "0.4.3", 0},
		{"1.0", "0.9.9", 1},
		{"0.4", "0.4.0", 0},
	}
	for _, testCase := range cases {
		if got := Compare(parse(testCase.current), parse(testCase.latest)); got != testCase.want {
			t.Errorf("Compare(%s, %s) = %d, want %d", testCase.current, testCase.latest, got, testCase.want)
		}
	}
}

func TestAssetName(t *testing.T) {
	if got := AssetName("windows", "arm64"); got != "llm-router_windows_arm64.zip" {
		t.Errorf("AssetName = %s", got)
	}
}

func TestVerifyChecksum(t *testing.T) {
	data := []byte("archive bytes")
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	checksums := []byte("deadbeef  darwin_arm64/llm-router_darwin_arm64.zip\n" + hash + "  linux_amd64/llm-router_linux_amd64.zip\n")
	if err := VerifyChecksum(checksums, "llm-router_linux_amd64.zip", data); err != nil {
		t.Errorf("valid checksum: %v", err)
	}
	if err := VerifyChecksum(checksums, "llm-router_darwin_arm64.zip", data); err == nil || !strings.Contains(err.Error(), "mismatch") {
		t.Errorf("mismatch: error = %v", err)
	}
	if err := VerifyChecksum(checksums, "llm-router_freebsd_amd64.zip", data); err == nil || !strings.Contains(err.Error(), "no entry") {
		t.Errorf("missing entry: error = %v", err)
	}
}

func buildArchive(t *testing.T, name string, content []byte) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	entry, err := writer.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func TestExtractBinary(t *testing.T) {
	archive := buildArchive(t, "linux_amd64/llm-router", []byte("binary"))
	data, err := ExtractBinary(archive, "llm-router")
	if err != nil || string(data) != "binary" {
		t.Fatalf("ExtractBinary = %q, %v", data, err)
	}
	if _, err := ExtractBinary(archive, "other"); err == nil {
		t.Error("missing entry succeeded")
	}
	if _, err := ExtractBinary([]byte("not a zip"), "llm-router"); err == nil {
		t.Error("invalid archive succeeded")
	}
}

func TestStageAndSwapKeepOnePrevious(t *testing.T) {
	directory := t.TempDir()
	executable := filepath.Join(directory, "llm-router")
	if err := os.WriteFile(executable, []byte("v1"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, release := range []string{"v2", "v3"} {
		staging, err := Stage(executable, []byte(release))
		if err != nil {
			t.Fatalf("Stage: %v", err)
		}
		previous, err := Swap(executable, staging)
		if err != nil {
			t.Fatalf("Swap: %v", err)
		}
		if previous != filepath.Join(directory, "llm-router.prev") {
			t.Errorf("previous = %s", previous)
		}
	}
	current, _ := os.ReadFile(executable)
	previous, _ := os.ReadFile(filepath.Join(directory, "llm-router.prev"))
	if string(current) != "v3" || string(previous) != "v2" {
		t.Errorf("current = %q, previous = %q, want v3 and v2", current, previous)
	}
	info, err := os.Stat(executable)
	if err != nil || info.Mode().Perm() != 0o755 {
		t.Errorf("mode = %v, %v, want 0755", info.Mode().Perm(), err)
	}
	if _, err := os.Stat(filepath.Join(directory, "llm-router.update")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("staging file left behind: %v", err)
	}
}

func TestSwapRestoresOnFailure(t *testing.T) {
	directory := t.TempDir()
	executable := filepath.Join(directory, "llm-router")
	if err := os.WriteFile(executable, []byte("v1"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Swap(executable, filepath.Join(directory, "missing")); err == nil {
		t.Fatal("Swap with missing staging succeeded")
	}
	current, err := os.ReadFile(executable)
	if err != nil || string(current) != "v1" {
		t.Errorf("executable after failed Swap = %q, %v, want v1 restored", current, err)
	}
}

func TestSiblingKeepsExtension(t *testing.T) {
	if got := sibling(`C:\bin\llm-router.exe`, "update"); got != `C:\bin\llm-router.update.exe` {
		t.Errorf("sibling = %s", got)
	}
}
