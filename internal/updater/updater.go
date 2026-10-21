// Package updater checks GitHub releases and replaces the running binary.
// It owns the release asset naming (see scripts/build) and never touches
// processes or databases: callers stop and start the server around Swap.
package updater

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/version"
)

const (
	// Repository is the GitHub repository that publishes releases.
	Repository = "TheSlopMachine/llm-router"
	// DefaultAPIBase is the GitHub REST API root.
	DefaultAPIBase = "https://api.github.com"
	// ChecksumsAssetName is the checksum list published with each release.
	ChecksumsAssetName = "checksums.txt"

	MaxArchiveBytes   int64 = 256 << 20
	MaxChecksumBytes  int64 = 1 << 20
	maxBinaryBytes    int64 = 512 << 20
	requestTimeout          = 60 * time.Second
	downloadTimeout         = 10 * time.Minute
	userAgent               = "llm-router-updater"
	binaryFileName          = "llm-router"
	stagingTag              = "update"
	previousTag             = "prev"
	executableFileBit       = 0o755
)

// ErrNoRelease reports a repository without a published release.
var ErrNoRelease = errors.New("no release published")

// Release is the latest published release.
type Release struct {
	Tag    string
	Assets map[string]string
}

// Client talks to the GitHub API. The zero value uses the public API.
type Client struct {
	HTTPClient *http.Client
	APIBase    string
	Repository string
}

// Latest fetches the newest non-draft, non-prerelease release.
func (client Client) Latest(ctx context.Context) (Release, error) {
	base := client.APIBase
	if base == "" {
		base = DefaultAPIBase
	}
	repository := client.Repository
	if repository == "" {
		repository = Repository
	}
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/repos/"+repository+"/releases/latest", nil)
	if err != nil {
		return Release{}, fmt.Errorf("build release request: %w", err)
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	request.Header.Set("User-Agent", userAgent)
	response, err := client.httpClient().Do(request)
	if err != nil {
		return Release{}, fmt.Errorf("query latest release: %w", err)
	}
	defer response.Body.Close()
	switch response.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return Release{}, ErrNoRelease
	case http.StatusForbidden, http.StatusTooManyRequests:
		return Release{}, rateLimitError(response)
	default:
		return Release{}, fmt.Errorf("query latest release: status %d", response.StatusCode)
	}
	var payload struct {
		Tag    string `json:"tag_name"`
		Assets []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(&payload); err != nil {
		return Release{}, fmt.Errorf("decode latest release: %w", err)
	}
	if strings.TrimSpace(payload.Tag) == "" {
		return Release{}, errors.New("latest release has no tag")
	}
	release := Release{Tag: payload.Tag, Assets: make(map[string]string, len(payload.Assets))}
	for _, asset := range payload.Assets {
		release.Assets[asset.Name] = asset.URL
	}
	return release, nil
}

// Download fetches url into memory and fails above limit bytes.
func (client Client) Download(ctx context.Context, url string, limit int64) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, downloadTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build download request: %w", err)
	}
	request.Header.Set("User-Agent", userAgent)
	response, err := client.httpClient().Do(request)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", url, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: status %d", url, response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", url, err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("download %s: larger than %d bytes", url, limit)
	}
	return data, nil
}

func (client Client) httpClient() *http.Client {
	if client.HTTPClient != nil {
		return client.HTTPClient
	}
	return http.DefaultClient
}

func rateLimitError(response *http.Response) error {
	if reset := response.Header.Get("X-RateLimit-Reset"); reset != "" {
		if seconds, err := strconv.ParseInt(reset, 10, 64); err == nil {
			return fmt.Errorf("GitHub API rate limit reached, retry after %s", time.Unix(seconds, 0).Format(time.TimeOnly))
		}
	}
	return fmt.Errorf("GitHub API denied the request: status %d", response.StatusCode)
}

// AssetName returns the release archive name for a platform.
func AssetName(goos, goarch string) string {
	return fmt.Sprintf("%s_%s_%s.zip", binaryFileName, goos, goarch)
}

// Compare orders two versions: negative when current is older than latest,
// zero when equal, positive when newer.
func Compare(current, latest version.Version) int {
	for _, pair := range [][2]int{{current.Major, latest.Major}, {current.Minor, latest.Minor}, {current.Fix, latest.Fix}} {
		if pair[0] != pair[1] {
			if pair[0] < pair[1] {
				return -1
			}
			return 1
		}
	}
	return 0
}

// VerifyChecksum checks data against the entry for assetName in a
// checksums.txt body ("<sha256>  <dir>/<name>" per line).
func VerifyChecksum(checksums []byte, assetName string, data []byte) error {
	sum := sha256.Sum256(data)
	actual := hex.EncodeToString(sum[:])
	for _, line := range strings.Split(string(checksums), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		name := path.Base(strings.TrimPrefix(fields[len(fields)-1], "*"))
		if name != assetName {
			continue
		}
		if !strings.EqualFold(fields[0], actual) {
			return fmt.Errorf("checksum mismatch for %s", assetName)
		}
		return nil
	}
	return fmt.Errorf("%s has no entry for %s", ChecksumsAssetName, assetName)
}

// ExtractBinary returns the named executable from a release archive.
func ExtractBinary(archive []byte, binaryName string) ([]byte, error) {
	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return nil, fmt.Errorf("open archive: %w", err)
	}
	for _, entry := range reader.File {
		if entry.FileInfo().IsDir() || path.Base(entry.Name) != binaryName {
			continue
		}
		if entry.UncompressedSize64 > uint64(maxBinaryBytes) {
			return nil, fmt.Errorf("archive entry %s is larger than %d bytes", entry.Name, maxBinaryBytes)
		}
		opened, err := entry.Open()
		if err != nil {
			return nil, fmt.Errorf("open archive entry %s: %w", entry.Name, err)
		}
		data, err := io.ReadAll(io.LimitReader(opened, maxBinaryBytes+1))
		_ = opened.Close()
		if err != nil {
			return nil, fmt.Errorf("read archive entry %s: %w", entry.Name, err)
		}
		if int64(len(data)) > maxBinaryBytes {
			return nil, fmt.Errorf("archive entry %s is larger than %d bytes", entry.Name, maxBinaryBytes)
		}
		return data, nil
	}
	return nil, fmt.Errorf("archive has no %s", binaryName)
}

// Stage writes the new binary next to executable and returns its path.
// Callers run it once to confirm it works before Swap.
func Stage(executable string, binary []byte) (string, error) {
	staging := sibling(executable, stagingTag)
	if err := os.WriteFile(staging, binary, executableFileBit); err != nil {
		return "", fmt.Errorf("write %s: %w", staging, err)
	}
	if err := os.Chmod(staging, executableFileBit); err != nil {
		_ = os.Remove(staging)
		return "", fmt.Errorf("chmod %s: %w", staging, err)
	}
	return staging, nil
}

// Swap replaces executable with the staged binary and keeps the old one as
// the previous version. Renaming a running executable is allowed on every
// supported OS, so the running process keeps its old code until restart.
func Swap(executable, staging string) (string, error) {
	previous := sibling(executable, previousTag)
	if err := os.Remove(previous); err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("remove %s: %w", previous, err)
	}
	if err := os.Rename(executable, previous); err != nil {
		return "", fmt.Errorf("move %s aside: %w", executable, err)
	}
	if err := os.Rename(staging, executable); err != nil {
		if restoreErr := os.Rename(previous, executable); restoreErr != nil {
			return "", fmt.Errorf("install %s: %w (restoring the old binary failed: %v)", executable, err, restoreErr)
		}
		return "", fmt.Errorf("install %s: %w", executable, err)
	}
	return previous, nil
}

// Discard removes a staged binary.
func Discard(staging string) {
	_ = os.Remove(staging)
}

// sibling names a file next to executable, keeping the extension so
// Windows still treats it as an executable.
func sibling(executable, tag string) string {
	extension := filepath.Ext(executable)
	return strings.TrimSuffix(executable, extension) + "." + tag + extension
}
