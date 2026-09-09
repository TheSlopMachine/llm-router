// Package pluginrepo manages external plugin repositories (the plugin store).
// It knows about GitHub and generic index endpoints; the luaplugin service
// owns installation from downloaded bytes.
package pluginrepo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/repository"
)

// ErrBuiltinRepoProtected is returned when removing a built-in repository.
var ErrBuiltinRepoProtected = errors.New("built-in repository cannot be removed")

// BuiltinRepos lists repositories seeded in code on every startup.
// Entries use the same IDs as user-added ones: "github/<owner>/<repo>".
var BuiltinRepos = []RepoRef{
	{ID: "github/TheSlopMachine/llm-router-store", Kind: "github", Owner: "TheSlopMachine", Repo: "llm-router-store"},
}

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
	Builtin  bool      `json:"builtin"`
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
		repo:      repository.New[RepoRecord](database, db.BucketPluginRepos, "plugin repo"),
		client:    &http.Client{Timeout: 30 * time.Second},
		providers: map[string]RepoProvider{},
	}
	gh := &githubProvider{client: s.client, branches: map[string]string{}}
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

// AddGitHub validates owner/repo via index.json and stores the repo.
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

// EnsureBuiltinRepos seeds the code-defined repositories without network
// validation, so startup never depends on store availability. Existing rows
// with the same ID are marked built-in.
func (s *Service) EnsureBuiltinRepos() error {
	for _, ref := range BuiltinRepos {
		if _, err := s.providerFor(ref.Kind); err != nil {
			return err
		}
		if existing, err := s.repo.Get(ref.ID); err == nil && existing != nil {
			if !existing.Builtin {
				existing.Builtin = true
				if err := s.repo.Put(ref.ID, existing); err != nil {
					return err
				}
			}
			continue
		}
		rec := &RepoRecord{
			ID: ref.ID, Kind: ref.Kind, Owner: ref.Owner, Repo: ref.Repo,
			IndexURL: ref.IndexURL, Builtin: true, AddedAt: time.Now(),
		}
		if err := s.repo.Put(ref.ID, rec); err != nil {
			return err
		}
	}
	return nil
}

// Remove deletes a repository record.
func (s *Service) Remove(id string) error {
	if existing, err := s.repo.Get(id); err == nil && existing != nil && existing.Builtin {
		return fmt.Errorf("remove repo %q: %w", id, ErrBuiltinRepoProtected)
	}
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
// GitHub provider (raw files + index.json, no API calls)
// ─────────────────────────────────────────────

var errRawNotFound = errors.New("raw file not found")

type githubProvider struct {
	client  *http.Client
	rawBase string // override in tests; defaults to raw.githubusercontent.com

	mu       sync.Mutex
	branches map[string]string // repo ID -> resolved default branch
}

func (g *githubProvider) Kind() string { return "github" }

// githubIndex is llm-router-plugins/index.json: file names only.
// Versions and descriptions come from manifests at runtime.
type githubIndex struct {
	Plugins []string `json:"plugins"`
}

func (g *githubProvider) rawBaseURL() string {
	if g.rawBase != "" {
		return g.rawBase
	}
	return "https://raw.githubusercontent.com"
}

func (g *githubProvider) rawURL(ref RepoRef, branch string, parts ...string) string {
	return g.rawBaseURL() + "/" + ref.Owner + "/" + ref.Repo + "/" + branch + "/" + strings.Join(parts, "/")
}

func (g *githubProvider) rawHead(ctx context.Context, url string) (int, error) {
	req, err := http.NewRequestWithContext(ctx, "HEAD", url, nil)
	if err != nil {
		return 0, err
	}
	resp, err := g.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	return resp.StatusCode, nil
}

func (g *githubProvider) download(ctx context.Context, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := g.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, errRawNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %q: status %d", url, resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, limit))
}

// resolveBranch finds the default branch by probing index.json on main,
// then master. The result is cached in memory.
func (g *githubProvider) resolveBranch(ctx context.Context, ref RepoRef) (string, error) {
	g.mu.Lock()
	branch, ok := g.branches[ref.ID]
	g.mu.Unlock()
	if ok {
		return branch, nil
	}
	for _, candidate := range []string{"main", "master"} {
		code, err := g.rawHead(ctx, g.rawURL(ref, candidate, "llm-router-plugins", "index.json"))
		if err != nil {
			return "", err
		}
		if code == http.StatusOK {
			g.mu.Lock()
			g.branches[ref.ID] = candidate
			g.mu.Unlock()
			return candidate, nil
		}
		if code != http.StatusNotFound {
			return "", fmt.Errorf("resolve branch for %q: status %d", ref.ID, code)
		}
	}
	return "", fmt.Errorf("index.json not found on main or master for %q", ref.ID)
}

func (g *githubProvider) fetchIndex(ctx context.Context, ref RepoRef) (*githubIndex, string, error) {
	branch, err := g.resolveBranch(ctx, ref)
	if err != nil {
		return nil, "", err
	}
	body, err := g.download(ctx, g.rawURL(ref, branch, "llm-router-plugins", "index.json"), 64<<10)
	if err != nil {
		return nil, "", err
	}
	var idx githubIndex
	if err := json.Unmarshal(body, &idx); err != nil {
		return nil, "", fmt.Errorf("decode index: %w", err)
	}
	if len(idx.Plugins) == 0 {
		return nil, "", fmt.Errorf("index lists no plugins for %q", ref.ID)
	}
	return &idx, branch, nil
}

func (g *githubProvider) ListPluginFiles(ctx context.Context, ref RepoRef) ([]RepoFile, error) {
	idx, _, err := g.fetchIndex(ctx, ref)
	if err != nil {
		return nil, err
	}
	out := []RepoFile{}
	seen := map[string]bool{}
	for _, name := range idx.Plugins {
		name = strings.TrimSpace(name)
		if name == "" || !strings.HasSuffix(name, ".lua") || strings.Contains(name, "/") {
			continue
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, RepoFile{Path: "llm-router-plugins/" + name})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

func (g *githubProvider) FetchFile(ctx context.Context, ref RepoRef, path string) ([]byte, error) {
	if !strings.HasPrefix(path, "llm-router-plugins/") || strings.Contains(strings.TrimPrefix(path, "llm-router-plugins/"), "/") {
		return nil, fmt.Errorf("invalid plugin path %q", path)
	}
	branch, err := g.resolveBranch(ctx, ref)
	if err != nil {
		return nil, err
	}
	body, err := g.download(ctx, g.rawURL(ref, branch, path), 1<<20)
	if err != nil {
		if errors.Is(err, errRawNotFound) {
			return nil, fmt.Errorf("plugin %q not found in %q", path, ref.ID)
		}
		return nil, err
	}
	return body, nil
}

func (g *githubProvider) fetchRootFile(ctx context.Context, ref RepoRef, name string) (string, bool, error) {
	branch, err := g.resolveBranch(ctx, ref)
	if err != nil {
		return "", false, nil
	}
	body, err := g.download(ctx, g.rawURL(ref, branch, name), 256<<10)
	if err != nil {
		return "", false, nil
	}
	return string(body), true, nil
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
