package virtual

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/TheSlopMachine/llm-router/internal/models"
)

// syncEndpoint is one managed virtual-model group: one VM per endpoint.
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
	idx := strings.LastIndex(rest, ":")
	if idx == -1 {
		return "", "", fmt.Errorf("invalid managed marker %q: missing endpoint slug", marker)
	}
	providerID, slug = rest[:idx], rest[idx+1:]
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
func (s *Service) GroupsForProvider(providerID string) ([]ProviderVMGroup, error) {
	views, err := s.modelInfoSvc.PeekMergedView(providerID)
	if err != nil {
		return nil, err
	}
	managed, err := s.List()
	if err != nil {
		return nil, err
	}
	byMarker := map[string]*models.VirtualModel{}
	for _, vm := range managed {
		if vm.ManagedBy != "" {
			byMarker[vm.ManagedBy] = vm
		}
	}
	groups := []ProviderVMGroup{}
	for _, se := range syncEndpoints {
		g := ProviderVMGroup{Endpoint: se.Slug, Label: se.Label, Models: []string{}}
		for _, v := range views {
			if v.Disabled {
				continue
			}
			if !v.SupportsEndpoint(se.Endpoint) {
				continue
			}
			g.Models = append(g.Models, v.Name)
		}
		if len(g.Models) == 0 {
			continue
		}
		if vm, ok := byMarker[ProviderMarker(providerID, se.Slug)]; ok {
			cp := *vm
			g.Virtual = &cp
		}
		groups = append(groups, g)
	}
	return groups, nil
}

func sameMembers(a []models.VirtualModelEntry, b []models.VirtualModelEntry) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].ModelID != b[i].ModelID {
			return false
		}
	}
	return true
}

// SyncProviderModels creates/updates/deletes managed virtual models so each
// non-empty endpoint group has exactly one. Members are enabled models
// serving the endpoint, in cache order. Managed VMs whose group emptied are
// deleted. Manual edits to name/description/instruction survive: only the
// member list is rewritten. Per-group failures accumulate into the returned
// error; completed groups still apply.
func (s *Service) SyncProviderModels(ctx context.Context, providerID string) ([]ProviderVMGroup, error) {
	inst, err := s.providerSvc.Get(providerID)
	if err != nil {
		return nil, err
	}
	groups, err := s.GroupsForProvider(providerID)
	if err != nil {
		return nil, err
	}
	// Orphan index: an unmanaged record squatting an auto-generated name is
	// adopted instead of colliding with it.
	orphans := map[string]*models.VirtualModel{}
	if all, err := s.List(); err == nil {
		for _, vm := range all {
			if vm.ManagedBy == "" {
				orphans[strings.ToLower(vm.Name)] = vm
			}
		}
	}
	live := map[string]bool{}
	var syncErrs []error
	fail := func(endpoint, op string, err error) {
		syncErrs = append(syncErrs, fmt.Errorf("endpoint %s %s: %w", endpoint, op, err))
		s.log().Warn("managed virtual model "+op+" failed",
			"provider_id", providerID, "endpoint", endpoint, "error", err)
	}
	for i := range groups {
		if err := ctx.Err(); err != nil {
			return groups, errors.Join(append(syncErrs, fmt.Errorf("sync canceled: %w", err))...)
		}
		g := &groups[i]
		live[g.Endpoint] = true
		entries := make([]models.ModelId, 0, len(g.Models))
		for _, name := range g.Models {
			entries = append(entries, models.ModelId(providerID+"/"+name))
		}
		marker := ProviderMarker(providerID, g.Endpoint)
		if g.Virtual == nil {
			autoName := fmt.Sprintf("%s %s", inst.Name, g.Label)
			if orphan, ok := orphans[strings.ToLower(autoName)]; ok {
				adopted := *orphan
				adopted.ManagedBy = marker
				adopted.Models = toEntries(entries)
				if err := s.Update(adopted.ID, &adopted); err != nil {
					fail(g.Endpoint, "adopt", err)
					continue
				}
				if fresh, err := s.Get(adopted.ID); err == nil {
					g.Virtual = fresh
				} else {
					g.Virtual = &adopted
				}
				continue
			}
			vm := &models.VirtualModel{
				Name:        autoName,
				Description: fmt.Sprintf("Automatic fall-through across %s %s models. Members refresh on import.", inst.Name, strings.ToLower(g.Label)),
				Models:      toEntries(entries),
				ManagedBy:   marker,
			}
			if err := s.Create(vm); err != nil {
				fail(g.Endpoint, "create", err)
				continue
			}
			g.Virtual = vm
			continue
		}
		if sameMembers(g.Virtual.Models, toEntries(entries)) {
			continue
		}
		updated := *g.Virtual
		updated.Models = toEntries(entries)
		if err := s.Update(updated.ID, &updated); err != nil {
			fail(g.Endpoint, "update", err)
			continue
		}
		if fresh, err := s.Get(updated.ID); err == nil {
			g.Virtual = fresh
		} else {
			g.Virtual = &updated
		}
	}
	all, err := s.List()
	if err != nil {
		return groups, errors.Join(append(syncErrs, err)...)
	}
	for _, vm := range all {
		if err := ctx.Err(); err != nil {
			return groups, errors.Join(append(syncErrs, fmt.Errorf("sync canceled: %w", err))...)
		}
		markerProvider, _, merr := ParseMarker(vm.ManagedBy)
		if merr != nil || markerProvider != providerID {
			continue
		}
		if !live[strings.TrimPrefix(vm.ManagedBy, "provider:"+providerID+":")] {
			if err := s.Delete(vm.ID); err != nil {
				fail(vm.ID, "delete", err)
			}
		}
	}
	return groups, errors.Join(syncErrs...)
}

func toEntries(ids []models.ModelId) []models.VirtualModelEntry {
	entries := make([]models.VirtualModelEntry, 0, len(ids))
	for _, id := range ids {
		entries = append(entries, models.VirtualModelEntry{ModelID: id})
	}
	return entries
}
