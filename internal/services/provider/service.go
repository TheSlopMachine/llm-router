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
	Complete(ctx context.Context, credentials []*models.Credential, request *models.ChatCompletionRequest, providerConfig map[string]any) (*models.ChatCompletionResponse, error)
	CompleteStream(ctx context.Context, credentials []*models.Credential, request *models.ChatCompletionRequest, w io.Writer, providerConfig map[string]any) error
	NeedsRefresh(credential *models.Credential) bool
	RefreshCredential(ctx context.Context, credential *models.Credential) (map[string]any, error)
	GetModelInfos(ctx context.Context, credential *models.Credential, providerConfig map[string]any) ([]models.ModelInfo, error)
}

// Transcriber is an optional GoAdapter capability serving
// POST /v1/audio/transcriptions. Lua plugin types implement the equivalent
// via the transcribe handler.
type Transcriber interface {
	Transcribe(ctx context.Context, credentials []*models.Credential, request *models.TranscriptionRequest, providerConfig map[string]any) (*models.TranscriptionResponse, error)
}

// Speaker is an optional GoAdapter capability serving
// POST /v1/audio/speech. Lua plugin types implement the equivalent
// via the speech handler.
type Speaker interface {
	Speech(ctx context.Context, credentials []*models.Credential, request *models.SpeechRequest, providerConfig map[string]any) (*models.SpeechResponse, error)
}

// ImageGenerator is an optional GoAdapter capability serving
// POST /v1/images/generations. Lua plugin types implement the equivalent
// via the generate_image handler.
type ImageGenerator interface {
	GenerateImage(ctx context.Context, credentials []*models.Credential, request *models.ImageGenerationRequest, providerConfig map[string]any) (*models.ImageGenerationResponse, error)
}

// Embedder is an optional GoAdapter capability serving
// POST /v1/embeddings. Lua plugin types implement the equivalent
// via the embed handler.
type Embedder interface {
	Embed(ctx context.Context, credentials []*models.Credential, request *models.EmbeddingsRequest, providerConfig map[string]any) (*models.EmbeddingsResponse, error)
}

// Moderator is an optional GoAdapter capability serving
// POST /v1/moderations. Lua plugin types implement the equivalent
// via the moderate handler.
type Moderator interface {
	Moderate(ctx context.Context, credentials []*models.Credential, request *models.ModerationRequest, providerConfig map[string]any) (*models.ModerationResponse, error)
}

// VideoSubmitter is the narrow video capability virtual fan-out needs:
// submit one video job. Full backends implement VideoGenerator.
type VideoSubmitter interface {
	SubmitVideo(ctx context.Context, credentials []*models.Credential, request *models.VideoGenerationRequest, providerConfig map[string]any) (*models.VideoGenerationResponse, error)
}

// VideoGenerator is an optional GoAdapter capability serving
// /v1/videos. Lua plugin types implement the equivalent via the
// generate_video, poll_video and video_content handlers.
type VideoGenerator interface {
	VideoSubmitter
	PollVideo(ctx context.Context, credentials []*models.Credential, model models.ModelId, upstreamJobID string, providerConfig map[string]any) (*models.VideoGenerationResponse, error)
	VideoContent(ctx context.Context, credentials []*models.Credential, model models.ModelId, upstreamJobID string, index int, providerConfig map[string]any) (*models.VideoContentResponse, error)
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
	database   *db.DB
	providers  *repository.Repository[models.ProviderInstance]
	luaService *luaplugin.Service

	mutex      sync.RWMutex
	goAdapters map[string]GoAdapter

	onChanged func(providerID string)
	logger    *slog.Logger
}

// NewService constructs a provider Service. Construction performs no I/O:
// legacy migration runs in EnsureSeeded, where failures surface as errors.
func NewService(database *db.DB) *Service {
	s := &Service{
		database:   database,
		providers:  repository.New[models.ProviderInstance](database, db.BucketProviderInstances, "provider"),
		goAdapters: map[string]GoAdapter{},
	}
	return s
}

// SetLogger wires structured logging for CRUD.
func (s *Service) SetLogger(l *slog.Logger) { s.logger = l }

// SetLuaService wires the Lua plugin service for plugin-backed types.
func (s *Service) SetLuaService(service *luaplugin.Service) { s.luaService = service }

// LuaService returns the wired Lua plugin service, if any.
func (s *Service) LuaService() *luaplugin.Service { return s.luaService }

// SetOnChanged registers a callback fired after provider create/update/delete.
func (s *Service) SetOnChanged(function func(providerID string)) { s.onChanged = function }

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
func (s *Service) Create(options CreateOptions) (*models.ProviderInstance, error) {
	name := strings.TrimSpace(options.Name)
	typeKey := strings.TrimSpace(options.TypeKey)
	qualifier := strings.TrimSpace(options.Qualifier)
	iconURL := strings.TrimSpace(options.IconURL)
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
	config := options.Config
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
	return s.providers.ListFiltered(func(provider *models.ProviderInstance) bool {
		return provider.TypeKey == typeKey
	})
}

// GetByTypeAndQualifier returns a specific provider by type and qualifier.
// The qualifier runs through the same slugify as uniqueID, so lookups use
// the stored form ("My Qual" finds type:my-qual). Dedup-suffixed duplicates
// (type:qual-2) resolve only by exact ID via Get.
func (s *Service) GetByTypeAndQualifier(typeKey, qualifier string) (*models.ProviderInstance, error) {
	id := typeKey
	if qualifier != "" {
		id = typeKey + ":" + slugify(qualifier)
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
func (s *Service) Update(id string, options UpdateOptions) (*models.ProviderInstance, error) {
	name := strings.TrimSpace(options.Name)
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}
	if len(name) > 100 {
		return nil, fmt.Errorf("name must be 100 characters or less")
	}
	var updated *models.ProviderInstance
	err := s.providers.Update(strings.TrimSpace(id), func(provider *models.ProviderInstance) error {
		if provider.TypeKey == TypeCustom && options.Config != nil {
			normalized, err := models.NormalizeBaseURL(configBaseURL(options.Config))
			if err != nil {
				return err
			}
			options.Config["base_url"] = normalized
		}
		provider.Name = name
		if options.Config != nil {
			provider.Config = options.Config
		}
		if options.Disabled != nil {
			provider.Disabled = *options.Disabled
			if *options.Disabled {
				now := time.Now()
				provider.DisabledBy = models.DisabledByAdmin
				provider.DisabledReason = "disabled by admin"
				provider.DisabledAt = &now
			} else {
				provider.DisabledBy = ""
				provider.DisabledReason = ""
				provider.DisabledAt = nil
			}
		}
		provider.IconURL = strings.TrimSpace(options.IconURL)
		provider.UpdatedAt = time.Now()
		updated = provider
		return nil
	})
	if err != nil {
		return nil, err
	}
	if s.logger != nil {
		s.logger.Info("provider updated", "provider_id", id, "type", updated.TypeKey, "disabled", updated.Disabled, "disabled_by", updated.DisabledBy)
	}
	s.notifyChanged(strings.TrimSpace(id))
	return updated, nil
}

// SystemDisable disables a provider on explicit system request (doctor fix
// for backend-less types). Traffic outcomes never call it. First wins: an
// already-disabled provider keeps its original cause, so a late duplicate
// cannot rewrite the admin-visible reason. Manual admin re-enable clears
// the flag.
func (s *Service) SystemDisable(id, reason string) error {
	if reason == "" {
		reason = "disabled by system"
	}
	if len(reason) > 1024 {
		reason = reason[:1024] + "…[truncated]"
	}
	id = strings.TrimSpace(id)
	disabled := false
	err := s.providers.Update(id, func(provider *models.ProviderInstance) error {
		if provider.Disabled {
			return nil
		}
		now := time.Now()
		provider.Disabled = true
		provider.DisabledBy = models.DisabledBySystem
		provider.DisabledReason = reason
		provider.DisabledAt = &now
		provider.UpdatedAt = now
		disabled = true
		return nil
	})
	if err != nil {
		return err
	}
	if disabled && s.logger != nil {
		s.logger.Info("provider disabled by system", "provider_id", id, "reason", reason)
	}
	s.notifyChanged(id)
	return nil
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
	deletedCount := 0
	if err := s.database.Update(func(tx *bolt.Tx) error {
		providerBucket := tx.Bucket(db.BucketProviderInstances)
		if providerBucket == nil || providerBucket.Get([]byte(id)) == nil {
			return fmt.Errorf("provider %q not found", id)
		}
		if err := providerBucket.Delete([]byte(id)); err != nil {
			return err
		}
		bucket := tx.Bucket(db.BucketCredentials)
		if bucket == nil {
			return nil
		}
		var toDelete [][]byte
		if err := bucket.ForEach(func(key, value []byte) error {
			var credential models.Credential
			if err := json.Unmarshal(value, &credential); err != nil {
				return fmt.Errorf("unmarshal credential: %w", err)
			}
			if strings.TrimSpace(credential.ProviderID) == id {
				toDelete = append(toDelete, append([]byte(nil), key...))
			}
			return nil
		}); err != nil {
			return err
		}
		for _, key := range toDelete {
			if err := bucket.Delete(key); err != nil {
				return err
			}
			deletedCount++
		}
		return nil
	}); err != nil {
		return err
	}
	if s.logger != nil {
		s.logger.Info("provider deleted", "provider_id", id, "cascaded_credentials", deletedCount)
	}
	s.notifyChanged(id)
	return nil
}
