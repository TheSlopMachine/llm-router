package virtual

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/TheSlopMachine/llm-router/internal/models"
)

// syncEndpoint is one managed virtual-model group: one virtual model per endpoint.
type syncEndpoint struct {
	Slug     string
	Label    string
	Endpoint string
}

var syncEndpoints = []syncEndpoint{
	{"chat", "Chat", models.EndpointChatCompletions},
	{"transcription", "STT", models.EndpointAudioTranscription},
	{"speech", "TTS", models.EndpointAudioSpeech},
	{"images", "Images", models.EndpointImagesGenerations},
	{"embeddings", "Embeddings", models.EndpointEmbeddings},
	{"videos", "Videos", models.EndpointVideos},
}

// ProviderMarker builds the ManagedBy marker for a provider endpoint group.
func ProviderMarker(providerID, slug string) string {
	return "provider:" + providerID + ":" + slug
}

// ParseMarker splits a ManagedBy marker into provider ID and endpoint slug.
// The split runs at the last colon, so provider IDs containing colons
// (qualified "type:qualifier" rows) survive the round trip.
func ParseMarker(marker string) (providerID, slug string, err error) {
	rest, ok := strings.CutPrefix(marker, "provider:")
	if !ok {
		return "", "", fmt.Errorf("invalid managed marker %q: missing provider: prefix", marker)
	}
	index := strings.LastIndex(rest, ":")
	if index == -1 {
		return "", "", fmt.Errorf("invalid managed marker %q: missing endpoint slug", marker)
	}
	providerID, slug = rest[:index], rest[index+1:]
	if providerID == "" || slug == "" {
		return "", "", fmt.Errorf("invalid managed marker %q: empty provider or slug", marker)
	}
	return providerID, slug, nil
}

// ProviderVMGroup is one endpoint group with its managed virtual model.
type ProviderVMGroup struct {
	Endpoint string               `json:"endpoint"`
	Label    string               `json:"label"`
	Models   []string             `json:"models"`
	Virtual  *models.VirtualModel `json:"virtual,omitempty"`
}

// GroupsForProvider computes endpoint groups from the cached merged view
// (no upstream fetch) with managed markers resolved. Only non-empty groups
// are returned; disabled models never become members.
func (s *GormService) GroupsForProvider(ctx context.Context, providerID string) ([]ProviderVMGroup, error) {
	views, err := s.modelInfoService.PeekMergedView(ctx, providerID)
	if err != nil {
		s.log().Error("Virtual model group listing fails", "provider_id", providerID, "operation", "peek_view", "error", err)
		return nil, err
	}
	managed, err := s.List(ctx)
	if err != nil {
		s.log().Error("Virtual model group listing fails", "provider_id", providerID, "operation", "list", "error", err)
		return nil, err
	}
	byMarker := map[string]*models.VirtualModel{}
	for _, virtualModel := range managed {
		if virtualModel.ManagedBy != "" {
			byMarker[virtualModel.ManagedBy] = virtualModel
		}
	}
	disabled := 0
	for _, view := range views {
		if view.Disabled {
			disabled++
		}
	}
	if disabled > 0 {
		s.log().Debug("Virtual model group listing filters disabled models", "provider_id", providerID, "count", disabled, "total", len(views))
	}
	groups := []ProviderVMGroup{}
	for _, endpoint := range syncEndpoints {
		group := ProviderVMGroup{Endpoint: endpoint.Slug, Label: endpoint.Label, Models: []string{}}
		for _, view := range views {
			if view.Disabled {
				continue
			}
			if !view.SupportsEndpoint(endpoint.Endpoint) {
				continue
			}
			group.Models = append(group.Models, view.Name)
		}
		if len(group.Models) == 0 {
			continue
		}
		if virtualModel, ok := byMarker[ProviderMarker(providerID, endpoint.Slug)]; ok {
			cp := *virtualModel
			group.Virtual = &cp
		}
		groups = append(groups, group)
	}
	s.log().Debug("Virtual model group listing succeeds", "provider_id", providerID, "count", len(groups))
	return groups, nil
}

func sameMembers(current []models.VirtualModelEntry, desired []models.VirtualModelEntry) bool {
	if len(current) != len(desired) {
		return false
	}
	for i := range current {
		if current[i].ModelID != desired[i].ModelID {
			return false
		}
	}
	return true
}

// SyncProviderModels creates/updates/deletes managed virtual models so each
// non-empty endpoint group has exactly one. Members are enabled models
// serving the endpoint, in cache order. Managed VMs whose group emptied are
// deleted. Disabled providers hold no groups: their managed rows delete so
// a disabled provider never leaves routable-looking virtual models behind;
// re-enabling recreates them from the cache. Manual edits to
// name/description/instruction survive: only the member list is rewritten.
// Per-group failures accumulate into the returned error; completed groups
// still apply.
func (s *GormService) SyncProviderModels(ctx context.Context, providerID string) ([]ProviderVMGroup, error) {
	instance, err := s.providerService.Get(ctx, providerID)
	if err != nil {
		s.log().Error("Managed virtual model sync fails", "provider_id", providerID, "operation", "load_provider", "error", err)
		return nil, err
	}
	var groups []ProviderVMGroup
	if !instance.Disabled {
		groups, err = s.GroupsForProvider(ctx, providerID)
		if err != nil {
			s.log().Error("Managed virtual model sync fails", "provider_id", providerID, "operation", "groups", "error", err)
			return nil, err
		}
	} else {
		s.log().Debug("Managed virtual model sync purges disabled provider", "provider_id", providerID)
	}
	// Orphan index: an unmanaged record squatting an auto-generated name is
	// adopted instead of colliding with it.
	orphans := map[string]*models.VirtualModel{}
	if all, err := s.List(ctx); err == nil {
		for _, virtualModel := range all {
			if virtualModel.ManagedBy == "" {
				orphans[strings.ToLower(virtualModel.Name)] = virtualModel
			}
		}
	} else {
		s.log().Debug("Managed virtual model sync misses orphan index", "provider_id", providerID, "operation", "list_orphans", "error", err)
	}
	live := map[string]bool{}
	var syncErrors []error
	fail := func(endpoint, operation string, err error, attrs ...any) {
		syncErrors = append(syncErrors, fmt.Errorf("endpoint %s %s: %w", endpoint, operation, err))
		args := append([]any{"provider_id", providerID, "endpoint", endpoint, "operation", operation, "error", err}, attrs...)
		s.log().Warn("Managed virtual model sync fails", args...)
	}
	for i := range groups {
		if err := ctx.Err(); err != nil {
			s.log().Debug("Managed virtual model sync cancels", "provider_id", providerID, "operation", "sync_groups", "error", err)
			return groups, errors.Join(append(syncErrors, fmt.Errorf("sync canceled: %w", err))...)
		}
		group := &groups[i]
		live[group.Endpoint] = true
		entries := make([]models.ModelId, 0, len(group.Models))
		for _, name := range group.Models {
			entries = append(entries, models.ModelId(providerID+"/"+name))
		}
		marker := ProviderMarker(providerID, group.Endpoint)
		if group.Virtual == nil {
			autoName := fmt.Sprintf("%s %s", instance.Name, group.Label)
			if orphan, ok := orphans[strings.ToLower(autoName)]; ok {
				adopted := *orphan
				adopted.ManagedBy = marker
				adopted.Models = toEntries(entries)
				if err := s.Update(ctx, adopted.ID, &adopted); err != nil {
					fail(group.Endpoint, "adopt", err, "model", adopted.ID)
					continue
				}
				if fresh, err := s.Get(ctx, adopted.ID); err == nil {
					group.Virtual = fresh
				} else {
					s.log().Debug("Managed virtual model sync misses refreshed model", "provider_id", providerID, "endpoint", group.Endpoint, "model", adopted.ID, "error", err)
					group.Virtual = &adopted
				}
				continue
			}
			virtualModel := &models.VirtualModel{
				Name:        autoName,
				Description: fmt.Sprintf("Automatic fall-through across %s %s models. Members refresh on import.", instance.Name, strings.ToLower(group.Label)),
				Models:      toEntries(entries),
				ManagedBy:   marker,
			}
			if err := s.Create(ctx, virtualModel); err != nil {
				fail(group.Endpoint, "create", err)
				continue
			}
			group.Virtual = virtualModel
			continue
		}
		if sameMembers(group.Virtual.Models, toEntries(entries)) {
			s.log().Debug("Managed virtual model sync skips unchanged members", "provider_id", providerID, "endpoint", group.Endpoint, "model", group.Virtual.ID)
			continue
		}
		updated := *group.Virtual
		updated.Models = toEntries(entries)
		if err := s.Update(ctx, updated.ID, &updated); err != nil {
			fail(group.Endpoint, "update", err, "model", updated.ID)
			continue
		}
		if fresh, err := s.Get(ctx, updated.ID); err == nil {
			group.Virtual = fresh
		} else {
			s.log().Debug("Managed virtual model sync misses refreshed model", "provider_id", providerID, "endpoint", group.Endpoint, "model", updated.ID, "error", err)
			group.Virtual = &updated
		}
	}
	all, err := s.List(ctx)
	if err != nil {
		s.log().Error("Managed virtual model sync fails", "provider_id", providerID, "operation", "list", "error", err)
		return groups, errors.Join(append(syncErrors, err)...)
	}
	purged := 0
	for _, virtualModel := range all {
		if err := ctx.Err(); err != nil {
			s.log().Debug("Managed virtual model sync cancels", "provider_id", providerID, "operation", "purge_stale", "error", err)
			return groups, errors.Join(append(syncErrors, fmt.Errorf("sync canceled: %w", err))...)
		}
		markerProvider, slug, markerError := ParseMarker(virtualModel.ManagedBy)
		if markerError != nil || markerProvider != providerID {
			continue
		}
		if !live[slug] {
			if err := s.Delete(ctx, virtualModel.ID); err != nil {
				fail(slug, "delete", err, "model", virtualModel.ID)
			} else {
				purged++
			}
		}
	}
	if purged > 0 {
		s.log().Debug("Managed virtual model sync purges stale models", "provider_id", providerID, "count", purged)
	}
	s.log().Debug("Managed virtual model sync succeeds", "provider_id", providerID, "count", len(groups))
	return groups, errors.Join(syncErrors...)
}

func toEntries(ids []models.ModelId) []models.VirtualModelEntry {
	entries := make([]models.VirtualModelEntry, 0, len(ids))
	for _, id := range ids {
		entries = append(entries, models.VirtualModelEntry{ModelID: id})
	}
	return entries
}
