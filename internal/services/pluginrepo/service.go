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
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/db/record"
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

// RepoRecord is the stored repository row.
type RepoRecord struct {
	ID          string    `json:"id"`
	Kind        string    `json:"kind"`
	IndexURL    string    `json:"index_url"`
	SourceURL   string    `json:"source_url"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Builtin     bool      `json:"builtin"`
	AddedAt     time.Time `json:"added_at"`
}

// indexProvider fetches plugin files from an index.json listing bare file
// names. File URLs resolve against the index directory.
type indexProvider struct {
	client *http.Client
	logger *slog.Logger
}

// Service owns repository records in the plugin_repositories table.
type Service struct {
	index  *indexProvider
	sqlite *repository.GormRepository[record.PluginRepository]
	logger *slog.Logger
}

// New constructs a pluginrepo Service on the SQLite handle.
func New(sqliteDatabase *db.Database) *Service {
	client := &http.Client{Timeout: 30 * time.Second}
	service := &Service{
		index: &indexProvider{client: client},
	}
	if sqliteDatabase != nil && sqliteDatabase.Handle != nil {
		service.sqlite = repository.NewGormRepository[record.PluginRepository](sqliteDatabase.Handle, "id")
	}
	return service
}

// SetLogger wires structured logging.
func (s *Service) SetLogger(logger *slog.Logger) {
	if s == nil {
		return
	}
	s.logger = logger
	if s.index != nil {
		s.index.logger = logger
	}
}

func (s *Service) referenceOf(value *RepoRecord) RepoRef {
	return RepoRef{ID: value.ID, IndexURL: value.IndexURL}
}

// List returns all added repositories sorted by ID.
func (s *Service) List() ([]*RepoRecord, error) {
	return s.listRecords()
}

// Get returns one repository by ID.
func (s *Service) Get(id string) (*RepoRecord, error) {
	return s.getRecord(id)
}

// repositoryIDForIndex derives a stable record ID from a resolved index URL.
func repositoryIDForIndex(indexURL string) string {
	sum := fnv.New32a()
	_, _ = sum.Write([]byte(indexURL))
	return "index/" + slugURL(indexURL) + "-" + strconv.FormatUint(uint64(sum.Sum32()), 16)
}

// resolveIndexCandidates turns user input into candidate index.json URLs:
// either the URL itself when it points at index.json, or the conventional
// index locations for a repository URL on a known git host.
func resolveIndexCandidates(raw string) ([]string, error) {
	raw = strings.TrimSpace(raw)
	repositoryURL, err := url.Parse(raw)
	if err != nil || (repositoryURL.Scheme != "http" && repositoryURL.Scheme != "https") || repositoryURL.Host == "" {
		return nil, fmt.Errorf("invalid repository url")
	}
	if repositoryURL.Path == "/index.json" || strings.HasSuffix(repositoryURL.Path, "/index.json") {
		repositoryURL.Fragment = ""
		return []string{repositoryURL.String()}, nil
	}
	repositoryPath := strings.Trim(strings.TrimSuffix(strings.Trim(repositoryURL.Path, "/"), ".git"), "/")
	if repositoryPath == "" || strings.Contains(repositoryPath, "..") {
		return nil, fmt.Errorf("invalid repository url")
	}
	segments := strings.Split(repositoryPath, "/")
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
	switch strings.ToLower(repositoryURL.Host) {
	case "github.com":
		if len(segments) != 2 {
			return nil, fmt.Errorf("github url must be owner/repo, or paste the index.json url directly")
		}
		return branchIndex("https://raw.githubusercontent.com/" + repositoryPath), nil
	case "gitlab.com":
		if len(segments) < 2 {
			return nil, fmt.Errorf("gitlab url must be group/repo, or paste the index.json url directly")
		}
		return branchIndex("https://" + repositoryURL.Host + "/" + repositoryPath + "/-/raw"), nil
	case "bitbucket.org":
		if len(segments) != 2 {
			return nil, fmt.Errorf("bitbucket url must be owner/repo, or paste the index.json url directly")
		}
		return branchIndex("https://bitbucket.org/" + repositoryPath + "/raw"), nil
	case "codeberg.org":
		if len(segments) != 2 {
			return nil, fmt.Errorf("codeberg url must be owner/repo, or paste the index.json url directly")
		}
		return branchIndex("https://codeberg.org/" + repositoryPath + "/raw/branch"), nil
	default:
		return nil, fmt.Errorf("unsupported host %q: paste the index.json url directly", repositoryURL.Host)
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
	for position, indexURL := range candidates {
		id := repositoryIDForIndex(indexURL)
		if existing, err := s.getRecord(id); err == nil && existing != nil {
			if s != nil && s.logger != nil {
				s.logger.Debug("Repository already stored", "repo_id", id, "index_url", indexURL)
			}
			return existing, nil
		}
		repositoryIndex, err := s.index.fetchIndex(ctx, indexURL)
		if err != nil {
			lastErr = err
			if s != nil && s.logger != nil {
				s.logger.Debug("Index candidate fetch failed", "index_url", indexURL, "source_url", strings.TrimSpace(rawURL), "error", err)
				if position+1 < len(candidates) {
					s.logger.Debug("Falling back to alternate branch index", "index_url", indexURL, "source_url", strings.TrimSpace(rawURL), "reason", "alternate_branch")
				}
			}
			continue
		}
		stored := &RepoRecord{
			ID: id, Kind: "index", IndexURL: indexURL, SourceURL: strings.TrimSpace(rawURL),
			Title: repositoryIndex.Title, Description: repositoryIndex.Description, AddedAt: time.Now(),
		}
		if err := s.putRecord(stored); err != nil {
			return nil, err
		}
		if s != nil && s.logger != nil {
			s.logger.Info("Repository stored", "repo_id", id, "index_url", indexURL, "source_url", strings.TrimSpace(rawURL))
		}
		return stored, nil
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
		id := repositoryIDForIndex(builtin.Index)
		if existing, err := s.getRecord(id); err == nil && existing != nil {
			if !existing.Builtin || existing.IndexURL != builtin.Index || existing.SourceURL != builtin.Source {
				existing.Builtin = true
				existing.IndexURL = builtin.Index
				existing.SourceURL = builtin.Source
				if err := s.putRecord(existing); err != nil {
					return err
				}
				if s != nil && s.logger != nil {
					s.logger.Info("Builtin repository marked built-in", "repo_id", id, "index_url", builtin.Index, "source_url", builtin.Source)
				}
			} else if s != nil && s.logger != nil {
				s.logger.Debug("Builtin repository already present", "repo_id", id, "index_url", builtin.Index)
			}
			continue
		}
		seeded := &RepoRecord{
			ID: id, Kind: "index", IndexURL: builtin.Index, SourceURL: builtin.Source,
			Builtin: true, AddedAt: time.Now(),
		}
		if err := s.putRecord(seeded); err != nil {
			return err
		}
		if s != nil && s.logger != nil {
			s.logger.Info("Builtin repository seeded", "repo_id", id, "index_url", builtin.Index, "source_url", builtin.Source)
		}
	}
	return nil
}

// Remove deletes a repository record.
func (s *Service) Remove(id string) error {
	if existing, err := s.getRecord(id); err == nil && existing != nil && existing.Builtin {
		if s != nil && s.logger != nil {
			s.logger.Warn("Builtin repository removal blocked", "repo_id", id, "reason", "builtin_protected")
		}
		return fmt.Errorf("remove repo %q: %w", id, ErrBuiltinRepoProtected)
	}
	if err := s.deleteRecord(id); err != nil {
		return err
	}
	if s != nil && s.logger != nil {
		s.logger.Info("Repository deleted", "repo_id", id)
	}
	return nil
}

// GetIndex fetches the index document with display info and files.
func (s *Service) GetIndex(ctx context.Context, id string) (IndexDoc, error) {
	record, err := s.getRecord(id)
	if err != nil {
		return IndexDoc{}, err
	}
	files, repositoryIndex, err := s.index.listWithIndex(ctx, s.referenceOf(record))
	if err != nil {
		return IndexDoc{}, err
	}
	return IndexDoc{Title: repositoryIndex.Title, Description: repositoryIndex.Description, Files: files}, nil
}

// ListPluginFiles lists plugin files for one repository.
func (s *Service) ListPluginFiles(ctx context.Context, id string) ([]RepoFile, error) {
	record, err := s.getRecord(id)
	if err != nil {
		return nil, err
	}
	return s.index.ListPluginFiles(ctx, s.referenceOf(record))
}

// FetchFile downloads one plugin file.
func (s *Service) FetchFile(ctx context.Context, id, path string) ([]byte, error) {
	record, err := s.getRecord(id)
	if err != nil {
		return nil, err
	}
	return s.index.FetchFile(ctx, s.referenceOf(record), path)
}

// ReadMe returns the repository README when present.
func (s *Service) ReadMe(ctx context.Context, id string) (string, bool, error) {
	record, err := s.getRecord(id)
	if err != nil {
		return "", false, err
	}
	return s.index.ReadMe(ctx, s.referenceOf(record))
}

// License returns the repository license when present.
func (s *Service) License(ctx context.Context, id string) (string, bool, error) {
	record, err := s.getRecord(id)
	if err != nil {
		return "", false, err
	}
	return s.index.License(ctx, s.referenceOf(record))
}

func slugURL(value string) string {
	value = strings.ToLower(value)
	var builder strings.Builder
	prevDash := false
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			builder.WriteRune(r)
			prevDash = false
		} else if !prevDash {
			builder.WriteRune('-')
			prevDash = true
		}
	}
	output := strings.Trim(builder.String(), "-")
	if len(output) > 64 {
		output = output[:64]
	}
	if output == "" {
		output = "index"
	}
	return output
}

// ─────────────────────────────────────────────
// Index provider (index.json listing bare file names)
// ─────────────────────────────────────────────

var errRawNotFound = errors.New("raw file not found")

// repositoryIndex is index.json: repository display info and plugin file names.
// Versions and descriptions come from manifests at runtime.
type repositoryIndex struct {
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Plugins     []string `json:"plugins"`
}

func (p *indexProvider) download(ctx context.Context, url string, limit int64) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	response, err := p.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		if p != nil && p.logger != nil {
			p.logger.Debug("Download returned not found", "index_url", url, "reason", "not_found")
		}
		return nil, errRawNotFound
	}
	if response.StatusCode != http.StatusOK {
		downloadErr := fmt.Errorf("download %q: status %d", url, response.StatusCode)
		if p != nil && p.logger != nil {
			p.logger.Warn("Download returned unexpected status", "index_url", url, "reason", "unexpected_status", "error", downloadErr)
		}
		return nil, downloadErr
	}
	return io.ReadAll(io.LimitReader(response.Body, limit))
}

func (p *indexProvider) fetchIndex(ctx context.Context, indexURL string) (*repositoryIndex, error) {
	body, err := p.download(ctx, indexURL, 64<<10)
	if err != nil {
		return nil, err
	}
	var index repositoryIndex
	if err := json.Unmarshal(body, &index); err != nil {
		return nil, fmt.Errorf("decode index: %w", err)
	}
	if len(index.Plugins) == 0 {
		return nil, fmt.Errorf("index lists no plugins")
	}
	return &index, nil
}

// indexDirectory returns the directory holding index.json.
func indexDirectory(indexURL string) string {
	return strings.TrimSuffix(indexURL, "/index.json")
}

func (p *indexProvider) ListPluginFiles(ctx context.Context, repositoryRef RepoRef) ([]RepoFile, error) {
	files, _, err := p.listWithIndex(ctx, repositoryRef)
	return files, err
}

// IndexDoc is a fetched index: display info plus resolved plugin files.
type IndexDoc struct {
	Title       string
	Description string
	Files       []RepoFile
}

func (p *indexProvider) listWithIndex(ctx context.Context, repositoryRef RepoRef) ([]RepoFile, *repositoryIndex, error) {
	index, err := p.fetchIndex(ctx, repositoryRef.IndexURL)
	if err != nil {
		return nil, nil, err
	}
	directory := indexDirectory(repositoryRef.IndexURL)
	output := []RepoFile{}
	seen := map[string]bool{}
	for _, name := range index.Plugins {
		name = strings.TrimSpace(name)
		if name == "" || !strings.HasSuffix(name, ".lua") || strings.Contains(name, "/") {
			if p != nil && p.logger != nil {
				p.logger.Debug("Plugin entry skipped", "repo_id", repositoryRef.ID, "index_url", repositoryRef.IndexURL, "reason", name)
			}
			continue
		}
		if seen[name] {
			if p != nil && p.logger != nil {
				p.logger.Debug("Duplicate plugin entry skipped", "repo_id", repositoryRef.ID, "index_url", repositoryRef.IndexURL, "reason", name)
			}
			continue
		}
		seen[name] = true
		output = append(output, RepoFile{Path: "llm-router-plugins/" + name, URL: directory + "/" + name})
	}
	sort.Slice(output, func(i, j int) bool { return output[i].Path < output[j].Path })
	if p != nil && p.logger != nil {
		p.logger.Debug("Plugin list built", "repo_id", repositoryRef.ID, "index_url", repositoryRef.IndexURL, "count", len(output))
	}
	return output, index, nil
}

func (p *indexProvider) FetchFile(ctx context.Context, repositoryRef RepoRef, path string) ([]byte, error) {
	if !strings.HasPrefix(path, "llm-router-plugins/") || strings.Contains(strings.TrimPrefix(path, "llm-router-plugins/"), "/") {
		return nil, fmt.Errorf("invalid plugin path %q", path)
	}
	files, err := p.ListPluginFiles(ctx, repositoryRef)
	if err != nil {
		return nil, err
	}
	for _, file := range files {
		if file.Path == path {
			body, err := p.download(ctx, file.URL, 1<<20)
			if err != nil {
				if errors.Is(err, errRawNotFound) {
					if p != nil && p.logger != nil {
						p.logger.Debug("Plugin file probe missed", "repo_id", repositoryRef.ID, "index_url", repositoryRef.IndexURL, "reason", path)
					}
					return nil, fmt.Errorf("plugin %q not found", path)
				}
				return nil, err
			}
			return body, nil
		}
	}
	if p != nil && p.logger != nil {
		p.logger.Debug("Plugin file probe missed", "repo_id", repositoryRef.ID, "index_url", repositoryRef.IndexURL, "reason", path)
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

// documentCandidates probes well-known doc names next to the index, then one
// level up, covering flat layouts and llm-router-plugins/ subdirectories.
func (p *indexProvider) documentCandidates(ctx context.Context, repositoryRef RepoRef, names []string) (string, bool) {
	directory := indexDirectory(repositoryRef.IndexURL)
	parent := directory
	if i := strings.LastIndex(directory, "/"); i > 0 {
		parent = directory[:i]
	}
	directories := []string{directory}
	if parent != directory {
		directories = append(directories, parent)
	}
	for _, candidateDirectory := range directories {
		for _, name := range names {
			if content, ok := p.fetchText(ctx, candidateDirectory+"/"+name); ok {
				return content, true
			}
		}
	}
	if p != nil && p.logger != nil {
		p.logger.Debug("Document probe missed", "repo_id", repositoryRef.ID, "index_url", repositoryRef.IndexURL, "count", len(directories)*len(names))
	}
	return "", false
}

func (p *indexProvider) ReadMe(ctx context.Context, repositoryRef RepoRef) (string, bool, error) {
	content, ok := p.documentCandidates(ctx, repositoryRef, []string{"README.md"})
	return content, ok, nil
}

func (p *indexProvider) License(ctx context.Context, repositoryRef RepoRef) (string, bool, error) {
	content, ok := p.documentCandidates(ctx, repositoryRef, []string{"LICENSE.md", "LICENSE", "LICENSE.txt"})
	return content, ok, nil
}
