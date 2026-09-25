// Package provider owns the unified provider registry.
//
// Every provider is an explicit ProviderInstance database row, regardless of
// whether its TypeKey is served by a Lua plugin or by a built-in Go adapter
// ("custom", "virtual").
package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/db"
	apierrors "github.com/TheSlopMachine/llm-router/internal/errors"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/repository"
	"github.com/TheSlopMachine/llm-router/internal/services/luaplugin"
	bolt "go.etcd.io/bbolt"
)

// GoAdapter is implemented by built-in Go backends ("custom", "virtual").
// Lua plugin types are served by luaplugin.Service instead.
//
// Request methods take the whole sorted credential pool: the backend tries
// keys in order, at most once each, and returns a single result. Key-level
// failures never leave the backend; there are no repeat passes or backoff
// pauses in the request path.
type GoAdapter interface {
	TypeKey() string
	ValidateCredentials(data map[string]any) error
	Complete(ctx context.Context, creds []*models.Credential, req *models.ChatCompletionRequest, providerConfig map[string]any) (*models.ChatCompletionResponse, error)
	CompleteStream(ctx context.Context, creds []*models.Credential, req *models.ChatCompletionRequest, w io.Writer, providerConfig map[string]any) error
	NeedsRefresh(cred *models.Credential) bool
	RefreshCredential(ctx context.Context, cred *models.Credential) (map[string]any, error)
	GetModelInfos(ctx context.Context, cred *models.Credential, providerConfig map[string]any) ([]models.ModelInfo, error)
}

// Transcriber is an optional GoAdapter capability serving
// POST /v1/audio/transcriptions. Lua plugin types implement the equivalent
// via the transcribe handler.
type Transcriber interface {
	Transcribe(ctx context.Context, creds []*models.Credential, req *models.TranscriptionRequest, providerConfig map[string]any) (*models.TranscriptionResponse, error)
}

// Speaker is an optional GoAdapter capability serving
// POST /v1/audio/speech. Lua plugin types implement the equivalent
// via the speech handler.
type Speaker interface {
	Speech(ctx context.Context, creds []*models.Credential, req *models.SpeechRequest, providerConfig map[string]any) (*models.SpeechResponse, error)
}

// ImageGenerator is an optional GoAdapter capability serving
// POST /v1/images/generations. Lua plugin types implement the equivalent
// via the generate_image handler.
type ImageGenerator interface {
	GenerateImage(ctx context.Context, creds []*models.Credential, req *models.ImageGenerationRequest, providerConfig map[string]any) (*models.ImageGenerationResponse, error)
}

// Embedder is an optional GoAdapter capability serving
// POST /v1/embeddings. Lua plugin types implement the equivalent
// via the embed handler.
type Embedder interface {
	Embed(ctx context.Context, creds []*models.Credential, req *models.EmbeddingsRequest, providerConfig map[string]any) (*models.EmbeddingsResponse, error)
}

// Built-in type keys served by Go code.
const (
	TypeCustom  = "custom"
	TypeVirtual = "virtual"
)

// ErrNotRefreshable is returned by Go adapters that never refresh credentials.
var ErrNotRefreshable = errors.New("credential type does not support refresh")

// Service exposes unified ProviderInstance CRUD.
type Service struct {
	db        *db.DB
	providers *repository.Repository[models.ProviderInstance]
	luaSvc    *luaplugin.Service

	mu         sync.RWMutex
	goAdapters map[string]GoAdapter

	onChanged func(providerID string)
	logger    *slog.Logger
}

// NewService constructs a provider Service. Construction performs no I/O:
// legacy migration runs in EnsureSeeded, where failures surface as errors.
func NewService(database *db.DB) *Service {
	s := &Service{
		db:         database,
		providers:  repository.New[models.ProviderInstance](database, db.BucketProviderInstances, "provider"),
		goAdapters: map[string]GoAdapter{},
	}
	return s
}

// SetLogger wires structured logging for CRUD.
func (s *Service) SetLogger(l *slog.Logger) { s.logger = l }

// SetLuaService wires the Lua plugin service for plugin-backed types.
func (s *Service) SetLuaService(svc *luaplugin.Service) { s.luaSvc = svc }

// LuaService returns the wired Lua plugin service, if any.
func (s *Service) LuaService() *luaplugin.Service { return s.luaSvc }

// SetOnChanged registers a callback fired after provider create/update/delete.
func (s *Service) SetOnChanged(fn func(providerID string)) { s.onChanged = fn }

func (s *Service) notifyChanged(providerID string) {
	if s.onChanged != nil {
		s.onChanged(providerID)
	}
}

// ─────────────────────────────────────────────
// CRUD
// ─────────────────────────────────────────────

// CreateOptions holds parameters for creating a provider instance.
type CreateOptions struct {
	Name      string
	TypeKey   string
	Qualifier string
	Config    map[string]any
	IconURL   string
}

// configBaseURL reads the raw base_url from a provider config map.
func configBaseURL(config map[string]any) string {
	if config == nil {
		return ""
	}
	s, _ := config["base_url"].(string)
	return s
}

// Create persists a new provider instance with a generated unique ID.
func (s *Service) Create(opts CreateOptions) (*models.ProviderInstance, error) {
	name := strings.TrimSpace(opts.Name)
	typeKey := strings.TrimSpace(opts.TypeKey)
	qualifier := strings.TrimSpace(opts.Qualifier)
	iconURL := strings.TrimSpace(opts.IconURL)
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}
	if len(name) > 100 {
		return nil, fmt.Errorf("name must be 100 characters or less")
	}
	if typeKey == "" {
		return nil, fmt.Errorf("type_key is required")
	}
	if strings.Contains(typeKey, "/") || strings.Contains(typeKey, ":") {
		return nil, fmt.Errorf("type_key must not contain '/' or ':'")
	}
	config := opts.Config
	if config == nil {
		config = map[string]any{}
	}
	if typeKey == TypeCustom {
		normalized, err := models.NormalizeBaseURL(configBaseURL(config))
		if err != nil {
			return nil, err
		}
		config["base_url"] = normalized
	}

	// Singleton plugin types: the core seeds one row per type key. Reuse it
	// instead of creating a duplicate unqualified instance.
	if typeKey != TypeCustom && qualifier == "" {
		if existing, err := s.providers.Get(typeKey); err == nil && existing != nil {
			return existing, nil
		}
	}

	id := s.uniqueID(typeKey, qualifier, name)
	now := time.Now()
	inst := &models.ProviderInstance{
		ID: id, Name: name, TypeKey: typeKey, Qualifier: qualifier,
		Config: config, IconURL: iconURL, CreatedAt: now, UpdatedAt: now,
	}
	if err := s.providers.Put(id, inst); err != nil {
		return nil, fmt.Errorf("save provider: %w", err)
	}
	if s.logger != nil {
		s.logger.Info("provider created", "provider_id", id, "type", typeKey)
	}
	s.notifyChanged(id)
	return inst, nil
}

func (s *Service) uniqueID(typeKey, qualifier, name string) string {
	var base string
	switch {
	case qualifier != "":
		base = typeKey + ":" + slugify(qualifier)
	case typeKey == TypeCustom:
		base = "custom:" + slugify(name)
	default:
		base = typeKey
	}
	id := base
	for counter := 2; ; counter++ {
		exists, err := s.providers.Exists(id)
		if err != nil || !exists {
			return id
		}
		id = fmt.Sprintf("%s-%d", base, counter)
	}
}

// Get returns a provider instance by ID.
func (s *Service) Get(id string) (*models.ProviderInstance, error) {
	inst, err := s.providers.Get(strings.TrimSpace(id))
	if err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			return nil, apierrors.NewNotFoundError("provider", id)
		}
		return nil, err
	}
	return inst, nil
}

// List returns all provider instances sorted by ID.
func (s *Service) List() ([]*models.ProviderInstance, error) {
	items, err := s.providers.List()
	if err != nil {
		return nil, err
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

// GetByType returns all providers served by a type key.
func (s *Service) GetByType(typeKey string) ([]*models.ProviderInstance, error) {
	return s.providers.ListFiltered(func(p *models.ProviderInstance) bool {
		return p.TypeKey == typeKey
	})
}

// GetByTypeAndQualifier returns a specific provider by type and qualifier.
func (s *Service) GetByTypeAndQualifier(typeKey, qualifier string) (*models.ProviderInstance, error) {
	id := typeKey
	if qualifier != "" {
		id = typeKey + ":" + qualifier
	}
	return s.Get(id)
}

// UpdateOptions holds mutable provider fields.
type UpdateOptions struct {
	Name     string
	Config   map[string]any
	IconURL  string
	Disabled *bool
}

// Update replaces a provider's mutable fields.
func (s *Service) Update(id string, opts UpdateOptions) (*models.ProviderInstance, error) {
	name := strings.TrimSpace(opts.Name)
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}
	if len(name) > 100 {
		return nil, fmt.Errorf("name must be 100 characters or less")
	}
	var updated *models.ProviderInstance
	err := s.providers.Update(strings.TrimSpace(id), func(p *models.ProviderInstance) error {
		if p.TypeKey == TypeCustom && opts.Config != nil {
			normalized, err := models.NormalizeBaseURL(configBaseURL(opts.Config))
			if err != nil {
				return err
			}
			opts.Config["base_url"] = normalized
		}
		p.Name = name
		if opts.Config != nil {
			p.Config = opts.Config
		}
		if opts.Disabled != nil {
			p.Disabled = *opts.Disabled
		}
		p.IconURL = strings.TrimSpace(opts.IconURL)
		p.UpdatedAt = time.Now()
		updated = p
		return nil
	})
	if err != nil {
		return nil, err
	}
	if s.logger != nil {
		s.logger.Info("provider updated", "provider_id", id)
	}
	s.notifyChanged(strings.TrimSpace(id))
	return updated, nil
}

// Delete removes a provider and cascades its credentials in one transaction.
func (s *Service) Delete(id string) error {
	id = strings.TrimSpace(id)
	if _, err := s.providers.Get(id); err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			return fmt.Errorf("provider %q not found", id)
		}
		return err
	}
	n := 0
	if err := s.db.Update(func(tx *bolt.Tx) error {
		pb := tx.Bucket(db.BucketProviderInstances)
		if pb == nil || pb.Get([]byte(id)) == nil {
			return fmt.Errorf("provider %q not found", id)
		}
		if err := pb.Delete([]byte(id)); err != nil {
			return err
		}
		b := tx.Bucket(db.BucketCredentials)
		if b == nil {
			return nil
		}
		var toDelete [][]byte
		if err := b.ForEach(func(k, v []byte) error {
			var c models.Credential
			if err := json.Unmarshal(v, &c); err != nil {
				return fmt.Errorf("unmarshal credential: %w", err)
			}
			if strings.TrimSpace(c.ProviderID) == id {
				toDelete = append(toDelete, append([]byte(nil), k...))
			}
			return nil
		}); err != nil {
			return err
		}
		for _, k := range toDelete {
			if err := b.Delete(k); err != nil {
				return err
			}
			n++
		}
		return nil
	}); err != nil {
		return err
	}
	if s.logger != nil {
		s.logger.Info("provider deleted", "provider_id", id, "cascaded_credentials", n)
	}
	s.notifyChanged(id)
	return nil
}
