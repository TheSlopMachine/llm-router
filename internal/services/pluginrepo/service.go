// Package pluginrepo manages external plugin repositories (the plugin store).
// It knows about GitHub and generic index endpoints; the luaplugin service
// owns installation from downloaded bytes.
package pluginrepo

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/repository"
)

// RepoRef identifies one repository.
type RepoRef struct {
	ID       string
	Kind     string
	Owner    string
	Repo     string
	IndexURL string
}

// RepoFile is one plugin file inside a repository.
type RepoFile struct {
	Path string
	URL  string
}

// RepoRecord is the stored repository row in BucketPluginRepos.
type RepoRecord struct {
	ID       string    `json:"id"`
	Kind     string    `json:"kind"`
	Owner    string    `json:"owner"`
	Repo     string    `json:"repo"`
	IndexURL string    `json:"index_url"`
	AddedAt  time.Time `json:"added_at"`
}

// RepoProvider fetches plugin files from one repository kind.
type RepoProvider interface {
	Kind() string
	ListPluginFiles(ctx context.Context, ref RepoRef) ([]RepoFile, error)
	FetchFile(ctx context.Context, ref RepoRef, path string) ([]byte, error)
	ReadMe(ctx context.Context, ref RepoRef) (content string, ok bool, err error)
	License(ctx context.Context, ref RepoRef) (content string, ok bool, err error)
}

// Service owns repository records and dispatches to kind providers.
type Service struct {
	repo      *repository.Repository[RepoRecord]
	providers map[string]RepoProvider
	client    *http.Client
}

// New constructs a pluginrepo Service with GitHub and generic providers.
func New(database *db.DB) *Service {
	s := &Service{
		repo:   repository.New[RepoRecord](database, db.BucketPluginRepos, "plugin repo"),
		client: &http.Client{Timeout: 30 * time.Second},
		providers: map[string]RepoProvider{},
	}
	gh := &githubProvider{client: s.client}
	gen := &genericProvider{client: s.client}
	s.providers[gh.Kind()] = gh
	s.providers[gen.Kind()] = gen
	return s
}

func (s *Service) refOf(rec *RepoRecord) RepoRef {
	return RepoRef{ID: rec.ID, Kind: rec.Kind, Owner: rec.Owner, Repo: rec.Repo, IndexURL: rec.IndexURL}
}

func (s *Service) providerFor(kind string) (RepoProvider, error) {
	p, ok := s.providers[kind]
	if !ok {
		return nil, fmt.Errorf("unknown repo kind %q", kind)
	}
	return p, nil
}

// List returns all added repositories sorted by ID.
func (s *Service) List() ([]*RepoRecord, error) {
	items, err := s.repo.List()
	if err != nil {
		return nil, err
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

// Get returns one repository by ID.
func (s *Service) Get(id string) (*RepoRecord, error) {
	return s.repo.Get(id)
}

// AddGitHub validates owner/repo via the Contents API and stores the repo.
func (s *Service) AddGitHub(ctx context.Context, owner, repo string) (*RepoRecord, error) {
	owner = strings.TrimSpace(owner)
	repo = strings.TrimSpace(repo)
	if owner == "" || repo == "" {
		return nil, fmt.Errorf("owner and repo are required")
	}
	if strings.Contains(owner, "/") || strings.Contains(repo, "/") {
		return nil, fmt.Errorf("invalid owner/repo")
	}
	id := "github/" + owner + "/" + repo
	if existing, err := s.repo.Get(id); err == nil && existing != nil {
		return existing, nil
	}
	rec := &RepoRecord{ID: id, Kind: "github", Owner: owner, Repo: repo, AddedAt: time.Now()}
	p, _ := s.providerFor("github")
	if _, err := p.ListPluginFiles(ctx, s.refOf(rec)); err != nil {
		return nil, fmt.Errorf("validate repo: %w", err)
	}
	if err := s.repo.Put(id, rec); err != nil {
		return nil, err
	}
	return rec, nil
}

// AddGeneric validates an index URL and stores the repo.
func (s *Service) AddGeneric(ctx context.Context, indexURL string) (*RepoRecord, error) {
	indexURL = strings.TrimSpace(indexURL)
	u, err := url.Parse(indexURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("invalid index url")
	}
	id := "generic/" + slugURL(indexURL)
	if existing, err := s.repo.Get(id); err == nil && existing != nil {
		existing.IndexURL = indexURL
		_ = s.repo.Put(id, existing)
		return existing, nil
	}
	rec := &RepoRecord{ID: id, Kind: "generic-index", IndexURL: indexURL, AddedAt: time.Now()}
	p, _ := s.providerFor("generic-index")
	if _, err := p.ListPluginFiles(ctx, s.refOf(rec)); err != nil {
		return nil, fmt.Errorf("validate index: %w", err)
	}
	if err := s.repo.Put(id, rec); err != nil {
		return nil, err
	}
	return rec, nil
}

// Remove deletes a repository record.
func (s *Service) Remove(id string) error {
	return s.repo.Delete(id)
}

// ListPluginFiles lists plugin files for one repository.
func (s *Service) ListPluginFiles(ctx context.Context, id string) ([]RepoFile, error) {
	rec, err := s.repo.Get(id)
	if err != nil {
		return nil, err
	}
	p, err := s.providerFor(rec.Kind)
	if err != nil {
		return nil, err
	}
	return p.ListPluginFiles(ctx, s.refOf(rec))
}

// FetchFile downloads one plugin file.
func (s *Service) FetchFile(ctx context.Context, id, path string) ([]byte, error) {
	rec, err := s.repo.Get(id)
	if err != nil {
		return nil, err
	}
	p, err := s.providerFor(rec.Kind)
	if err != nil {
		return nil, err
	}
	return p.FetchFile(ctx, s.refOf(rec), path)
}

// ReadMe returns the repository README when present.
func (s *Service) ReadMe(ctx context.Context, id string) (string, bool, error) {
	rec, err := s.repo.Get(id)
	if err != nil {
		return "", false, err
	}
	p, err := s.providerFor(rec.Kind)
	if err != nil {
		return "", false, err
	}
	return p.ReadMe(ctx, s.refOf(rec))
}

// License returns the repository license when present.
func (s *Service) License(ctx context.Context, id string) (string, bool, error) {
	rec, err := s.repo.Get(id)
	if err != nil {
		return "", false, err
	}
	p, err := s.providerFor(rec.Kind)
	if err != nil {
		return "", false, err
	}
	return p.License(ctx, s.refOf(rec))
}

func slugURL(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	prevDash := false
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevDash = false
		} else if !prevDash {
			b.WriteRune('-')
			prevDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > 64 {
		out = out[:64]
	}
	if out == "" {
		out = "index"
	}
	return out
}

// ─────────────────────────────────────────────
// GitHub provider (Contents API, no git clone)
// ─────────────────────────────────────────────

type githubProvider struct {
	client *http.Client
}

func (g *githubProvider) Kind() string { return "github" }

type githubContentEntry struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	Type        string `json:"type"`
	DownloadURL string `json:"download_url"`
	Content     string `json:"content"`
	Encoding    string `json:"encoding"`
}

func (g *githubProvider) apiGet(ctx context.Context, url string, target any) error {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := g.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return fmt.Errorf("github api %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return json.NewDecoder(resp.Body).Decode(target)
}

func (g *githubProvider) ListPluginFiles(ctx context.Context, ref RepoRef) ([]RepoFile, error) {
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/contents/llm-router-plugins", ref.Owner, ref.Repo)
	var entries []githubContentEntry
	if err := g.apiGet(ctx, apiURL, &entries); err != nil {
		return nil, err
	}
	var out []RepoFile
	for _, e := range entries {
		if e.Type != "file" || !strings.HasSuffix(e.Name, ".lua") {
			continue
		}
		out = append(out, RepoFile{Path: "llm-router-plugins/" + e.Name, URL: e.DownloadURL})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

func (g *githubProvider) FetchFile(ctx context.Context, ref RepoRef, path string) ([]byte, error) {
	if !strings.HasPrefix(path, "llm-router-plugins/") || strings.Contains(strings.TrimPrefix(path, "llm-router-plugins/"), "/") {
		return nil, fmt.Errorf("invalid plugin path %q", path)
	}
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/contents/%s", ref.Owner, ref.Repo, path)
	var entry githubContentEntry
	if err := g.apiGet(ctx, apiURL, &entry); err != nil {
		return nil, err
	}
	if entry.DownloadURL == "" {
		return nil, fmt.Errorf("no download url for %q", path)
	}
	req, err := http.NewRequestWithContext(ctx, "GET", entry.DownloadURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := g.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %q: status %d", path, resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 1<<20))
}

func (g *githubProvider) fetchRootFile(ctx context.Context, ref RepoRef, name string) (string, bool, error) {
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/contents/%s", ref.Owner, ref.Repo, name)
	var entry githubContentEntry
	if err := g.apiGet(ctx, apiURL, &entry); err != nil {
		return "", false, nil
	}
	if entry.Encoding == "base64" && entry.Content != "" {
		raw, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(entry.Content, "\n", ""))
		if err != nil {
			return "", false, nil
		}
		return string(raw), true, nil
	}
	if entry.DownloadURL != "" {
		req, err := http.NewRequestWithContext(ctx, "GET", entry.DownloadURL, nil)
		if err != nil {
			return "", false, nil
		}
		resp, err := g.client.Do(req)
		if err != nil {
			return "", false, nil
		}
		defer resp.Body.Close()
		raw, err := io.ReadAll(io.LimitReader(resp.Body, 256<<10))
		if err != nil {
			return "", false, nil
		}
		return string(raw), true, nil
	}
	return "", false, nil
}

func (g *githubProvider) ReadMe(ctx context.Context, ref RepoRef) (string, bool, error) {
	return g.fetchRootFile(ctx, ref, "README.md")
}

func (g *githubProvider) License(ctx context.Context, ref RepoRef) (string, bool, error) {
	for _, name := range []string{"LICENSE.md", "LICENSE", "LICENSE.txt"} {
		if content, ok, _ := g.fetchRootFile(ctx, ref, name); ok {
			return content, true, nil
		}
	}
	return "", false, nil
}

// ─────────────────────────────────────────────
// Generic index provider (self-hosted JSON index)
// ─────────────────────────────────────────────

type genericProvider struct {
	client *http.Client
}

func (g *genericProvider) Kind() string { return "generic-index" }

type genericIndex struct {
	Plugins []struct {
		Path string `json:"path"`
		URL  string `json:"url"`
	} `json:"plugins"`
	ReadmeURL  string `json:"readme_url"`
	LicenseURL string `json:"license_url"`
}

func (g *genericProvider) fetchIndex(ctx context.Context, ref RepoRef) (*genericIndex, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", ref.IndexURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := g.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("index status %d", resp.StatusCode)
	}
	var idx genericIndex
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&idx); err != nil {
		return nil, fmt.Errorf("decode index: %w", err)
	}
	return &idx, nil
}

func (g *genericProvider) ListPluginFiles(ctx context.Context, ref RepoRef) ([]RepoFile, error) {
	idx, err := g.fetchIndex(ctx, ref)
	if err != nil {
		return nil, err
	}
	var out []RepoFile
	for _, p := range idx.Plugins {
		if !strings.HasPrefix(p.Path, "llm-router-plugins/") || p.URL == "" {
			continue
		}
		out = append(out, RepoFile{Path: p.Path, URL: p.URL})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

func (g *genericProvider) FetchFile(ctx context.Context, ref RepoRef, path string) ([]byte, error) {
	files, err := g.ListPluginFiles(ctx, ref)
	if err != nil {
		return nil, err
	}
	for _, f := range files {
		if f.Path == path {
			req, err := http.NewRequestWithContext(ctx, "GET", f.URL, nil)
			if err != nil {
				return nil, err
			}
			resp, err := g.client.Do(req)
			if err != nil {
				return nil, err
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				return nil, fmt.Errorf("download %q: status %d", path, resp.StatusCode)
			}
			return io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		}
	}
	return nil, fmt.Errorf("plugin %q not in index", path)
}

func (g *genericProvider) fetchURL(ctx context.Context, raw string) (string, bool, error) {
	if raw == "" {
		return "", false, nil
	}
	req, err := http.NewRequestWithContext(ctx, "GET", raw, nil)
	if err != nil {
		return "", false, nil
	}
	resp, err := g.client.Do(req)
	if err != nil {
		return "", false, nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", false, nil
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 256<<10))
	if err != nil {
		return "", false, nil
	}
	return string(data), true, nil
}

func (g *genericProvider) ReadMe(ctx context.Context, ref RepoRef) (string, bool, error) {
	idx, err := g.fetchIndex(ctx, ref)
	if err != nil {
		return "", false, err
	}
	return g.fetchURL(ctx, idx.ReadmeURL)
}

func (g *genericProvider) License(ctx context.Context, ref RepoRef) (string, bool, error) {
	idx, err := g.fetchIndex(ctx, ref)
	if err != nil {
		return "", false, err
	}
	return g.fetchURL(ctx, idx.LicenseURL)
}
