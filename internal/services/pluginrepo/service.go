// Package pluginrepo manages external plugin repositories (the plugin store).
// Repositories are JSON indexes listing plugin file names; the luaplugin
// service owns installation from downloaded bytes.
package pluginrepo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/repository"
)

// ErrBuiltinRepoProtected is returned when removing a built-in repository.
var ErrBuiltinRepoProtected = errors.New("built-in repository cannot be removed")

// BuiltinRepo is one code-defined repository: the source URL shown in the
// UI and the resolved index.json URL used for downloads.
type BuiltinRepo struct {
	Source string
	Index  string
}

// BuiltinRepos lists repositories seeded in code on every startup.
var BuiltinRepos = []BuiltinRepo{
	{
		Source: "https://github.com/TheSlopMachine/llm-router-store",
		Index:  "https://raw.githubusercontent.com/TheSlopMachine/llm-router-store/main/llm-router-plugins/index.json",
	},
}

// RepoRef identifies one repository.
type RepoRef struct {
	ID       string
	IndexURL string
}

// RepoFile is one plugin file inside a repository.
type RepoFile struct {
	Path string
	URL  string
}

// RepoRecord is the stored repository row in BucketPluginRepos.
type RepoRecord struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`
	IndexURL  string    `json:"index_url"`
	SourceURL string    `json:"source_url"`
	Builtin   bool      `json:"builtin"`
	AddedAt   time.Time `json:"added_at"`
}

// indexProvider fetches plugin files from an index.json listing bare file
// names. File URLs resolve against the index directory.
type indexProvider struct {
	client *http.Client
}

// Service owns repository records.
type Service struct {
	repo  *repository.Repository[RepoRecord]
	index *indexProvider
}

// New constructs a pluginrepo Service.
func New(database *db.DB) *Service {
	client := &http.Client{Timeout: 30 * time.Second}
	return &Service{
		repo:  repository.New[RepoRecord](database, db.BucketPluginRepos, "plugin repo"),
		index: &indexProvider{client: client},
	}
}

func (s *Service) refOf(rec *RepoRecord) RepoRef {
	return RepoRef{ID: rec.ID, IndexURL: rec.IndexURL}
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

// repoIDForIndex derives a stable record ID from a resolved index URL.
func repoIDForIndex(indexURL string) string {
	sum := fnv.New32a()
	_, _ = sum.Write([]byte(indexURL))
	return "index/" + slugURL(indexURL) + "-" + strconv.FormatUint(uint64(sum.Sum32()), 16)
}

// resolveIndexCandidates turns user input into candidate index.json URLs:
// either the URL itself when it points at index.json, or the conventional
// index locations for a repository URL on a known git host.
func resolveIndexCandidates(raw string) ([]string, error) {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("invalid repository url")
	}
	if u.Path == "/index.json" || strings.HasSuffix(u.Path, "/index.json") {
		u.Fragment = ""
		return []string{u.String()}, nil
	}
	repoPath := strings.Trim(strings.TrimSuffix(strings.Trim(u.Path, "/"), ".git"), "/")
	if repoPath == "" || strings.Contains(repoPath, "..") {
		return nil, fmt.Errorf("invalid repository url")
	}
	segments := strings.Split(repoPath, "/")
	for _, segment := range segments {
		if segment == "" || segment == "." {
			return nil, fmt.Errorf("invalid repository url")
		}
	}
	branchIndex := func(base string) []string {
		return []string{
			base + "/main/llm-router-plugins/index.json",
			base + "/master/llm-router-plugins/index.json",
		}
	}
	switch strings.ToLower(u.Host) {
	case "github.com":
		if len(segments) != 2 {
			return nil, fmt.Errorf("github url must be owner/repo, or paste the index.json url directly")
		}
		return branchIndex("https://raw.githubusercontent.com/" + repoPath), nil
	case "gitlab.com":
		if len(segments) < 2 {
			return nil, fmt.Errorf("gitlab url must be group/repo, or paste the index.json url directly")
		}
		return branchIndex("https://" + u.Host + "/" + repoPath + "/-/raw"), nil
	case "bitbucket.org":
		if len(segments) != 2 {
			return nil, fmt.Errorf("bitbucket url must be owner/repo, or paste the index.json url directly")
		}
		return branchIndex("https://bitbucket.org/" + repoPath + "/raw"), nil
	case "codeberg.org":
		if len(segments) != 2 {
			return nil, fmt.Errorf("codeberg url must be owner/repo, or paste the index.json url directly")
		}
		return branchIndex("https://codeberg.org/" + repoPath + "/raw/branch"), nil
	default:
		return nil, fmt.Errorf("unsupported host %q: paste the index.json url directly", u.Host)
	}
}

// AddRepo validates a repository or index URL and stores the repo. The first
// candidate index that parses with a non-empty plugin list wins.
func (s *Service) AddRepo(ctx context.Context, rawURL string) (*RepoRecord, error) {
	candidates, err := resolveIndexCandidates(rawURL)
	if err != nil {
		return nil, err
	}
	var lastErr error
	for _, indexURL := range candidates {
		id := repoIDForIndex(indexURL)
		if existing, err := s.repo.Get(id); err == nil && existing != nil {
			return existing, nil
		}
		if _, err := s.index.fetchIndex(ctx, indexURL); err != nil {
			lastErr = err
			continue
		}
		rec := &RepoRecord{ID: id, Kind: "index", IndexURL: indexURL, SourceURL: strings.TrimSpace(rawURL), AddedAt: time.Now()}
		if err := s.repo.Put(id, rec); err != nil {
			return nil, err
		}
		return rec, nil
	}
	if lastErr != nil {
		return nil, fmt.Errorf("no usable index: %w", lastErr)
	}
	return nil, fmt.Errorf("no usable index")
}

// EnsureBuiltinRepos seeds the code-defined repositories without network
// validation, so startup never depends on store availability. Existing rows
// with the same ID are marked built-in.
func (s *Service) EnsureBuiltinRepos() error {
	for _, builtin := range BuiltinRepos {
		id := repoIDForIndex(builtin.Index)
		if existing, err := s.repo.Get(id); err == nil && existing != nil {
			if !existing.Builtin || existing.IndexURL != builtin.Index || existing.SourceURL != builtin.Source {
				existing.Builtin = true
				existing.IndexURL = builtin.Index
				existing.SourceURL = builtin.Source
				if err := s.repo.Put(id, existing); err != nil {
					return err
				}
			}
			continue
		}
		rec := &RepoRecord{
			ID: id, Kind: "index", IndexURL: builtin.Index, SourceURL: builtin.Source,
			Builtin: true, AddedAt: time.Now(),
		}
		if err := s.repo.Put(id, rec); err != nil {
			return err
		}
	}
	return nil
}

// PruneLegacyRepos deletes repository rows from retired schemes. Installed
// plugins keep working; updates resume after reinstall from the catalog.
func (s *Service) PruneLegacyRepos() error {
	items, err := s.repo.List()
	if err != nil {
		return err
	}
	for _, rec := range items {
		if rec.Kind == "index" {
			continue
		}
		if err := s.repo.Delete(rec.ID); err != nil {
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
	return s.index.ListPluginFiles(ctx, s.refOf(rec))
}

// FetchFile downloads one plugin file.
func (s *Service) FetchFile(ctx context.Context, id, path string) ([]byte, error) {
	rec, err := s.repo.Get(id)
	if err != nil {
		return nil, err
	}
	return s.index.FetchFile(ctx, s.refOf(rec), path)
}

// ReadMe returns the repository README when present.
func (s *Service) ReadMe(ctx context.Context, id string) (string, bool, error) {
	rec, err := s.repo.Get(id)
	if err != nil {
		return "", false, err
	}
	return s.index.ReadMe(ctx, s.refOf(rec))
}

// License returns the repository license when present.
func (s *Service) License(ctx context.Context, id string) (string, bool, error) {
	rec, err := s.repo.Get(id)
	if err != nil {
		return "", false, err
	}
	return s.index.License(ctx, s.refOf(rec))
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
// Index provider (index.json listing bare file names)
// ─────────────────────────────────────────────

var errRawNotFound = errors.New("raw file not found")

// repoIndex is index.json: plugin file names only. Versions and
// descriptions come from manifests at runtime.
type repoIndex struct {
	Plugins []string `json:"plugins"`
}

func (p *indexProvider) download(ctx context.Context, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := p.client.Do(req)
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

func (p *indexProvider) fetchIndex(ctx context.Context, indexURL string) (*repoIndex, error) {
	body, err := p.download(ctx, indexURL, 64<<10)
	if err != nil {
		return nil, err
	}
	var idx repoIndex
	if err := json.Unmarshal(body, &idx); err != nil {
		return nil, fmt.Errorf("decode index: %w", err)
	}
	if len(idx.Plugins) == 0 {
		return nil, fmt.Errorf("index lists no plugins")
	}
	return &idx, nil
}

// indexDir returns the directory holding index.json.
func indexDir(indexURL string) string {
	return strings.TrimSuffix(indexURL, "/index.json")
}

func (p *indexProvider) ListPluginFiles(ctx context.Context, ref RepoRef) ([]RepoFile, error) {
	idx, err := p.fetchIndex(ctx, ref.IndexURL)
	if err != nil {
		return nil, err
	}
	dir := indexDir(ref.IndexURL)
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
		out = append(out, RepoFile{Path: "llm-router-plugins/" + name, URL: dir + "/" + name})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

func (p *indexProvider) FetchFile(ctx context.Context, ref RepoRef, path string) ([]byte, error) {
	if !strings.HasPrefix(path, "llm-router-plugins/") || strings.Contains(strings.TrimPrefix(path, "llm-router-plugins/"), "/") {
		return nil, fmt.Errorf("invalid plugin path %q", path)
	}
	files, err := p.ListPluginFiles(ctx, ref)
	if err != nil {
		return nil, err
	}
	for _, f := range files {
		if f.Path == path {
			body, err := p.download(ctx, f.URL, 1<<20)
			if err != nil {
				if errors.Is(err, errRawNotFound) {
					return nil, fmt.Errorf("plugin %q not found", path)
				}
				return nil, err
			}
			return body, nil
		}
	}
	return nil, fmt.Errorf("plugin %q not in index", path)
}

func (p *indexProvider) fetchText(ctx context.Context, url string) (string, bool) {
	body, err := p.download(ctx, url, 256<<10)
	if err != nil {
		return "", false
	}
	return string(body), true
}

// docCandidates probes well-known doc names next to the index, then one
// level up, covering flat layouts and llm-router-plugins/ subdirectories.
func (p *indexProvider) docCandidates(ctx context.Context, ref RepoRef, names []string) (string, bool) {
	dir := indexDir(ref.IndexURL)
	parent := dir
	if i := strings.LastIndex(dir, "/"); i > 0 {
		parent = dir[:i]
	}
	dirs := []string{dir}
	if parent != dir {
		dirs = append(dirs, parent)
	}
	for _, d := range dirs {
		for _, name := range names {
			if content, ok := p.fetchText(ctx, d+"/"+name); ok {
				return content, true
			}
		}
	}
	return "", false
}

func (p *indexProvider) ReadMe(ctx context.Context, ref RepoRef) (string, bool, error) {
	content, ok := p.docCandidates(ctx, ref, []string{"README.md"})
	return content, ok, nil
}

func (p *indexProvider) License(ctx context.Context, ref RepoRef) (string, bool, error) {
	content, ok := p.docCandidates(ctx, ref, []string{"LICENSE.md", "LICENSE", "LICENSE.txt"})
	return content, ok, nil
}
