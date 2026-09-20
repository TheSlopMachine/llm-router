package virtual

import (
	"context"
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
// member list is rewritten.
func (s *Service) SyncProviderModels(ctx context.Context, providerID string) ([]ProviderVMGroup, error) {
	_ = ctx
	inst, err := s.providerSvc.Get(providerID)
	if err != nil {
		return nil, err
	}
	groups, err := s.GroupsForProvider(providerID)
	if err != nil {
		return nil, err
	}
	live := map[string]bool{}
	for i := range groups {
		g := &groups[i]
		live[g.Endpoint] = true
		entries := make([]models.VirtualModelEntry, 0, len(g.Models))
		for _, name := range g.Models {
			entries = append(entries, models.VirtualModelEntry{ModelID: models.ModelId(providerID + "/" + name)})
		}
		marker := ProviderMarker(providerID, g.Endpoint)
		if g.Virtual == nil {
			vm := &models.VirtualModel{
				Name:        fmt.Sprintf("%s %s", inst.Name, g.Label),
				Description: fmt.Sprintf("Automatic fall-through across %s %s models. Members refresh on import.", inst.Name, strings.ToLower(g.Label)),
				Models:      entries,
				ManagedBy:   marker,
			}
			if err := s.Create(vm); err != nil {
				return nil, err
			}
			g.Virtual = vm
			continue
		}
		if sameMembers(g.Virtual.Models, entries) {
			continue
		}
		updated := *g.Virtual
		updated.Models = entries
		if err := s.Update(updated.ID, &updated); err != nil {
			return nil, err
		}
		if fresh, err := s.Get(updated.ID); err == nil {
			g.Virtual = fresh
		} else {
			g.Virtual = &updated
		}
	}
	all, err := s.List()
	if err != nil {
		return nil, err
	}
	prefix := "provider:" + providerID + ":"
	for _, vm := range all {
		if strings.HasPrefix(vm.ManagedBy, prefix) && !live[strings.TrimPrefix(vm.ManagedBy, prefix)] {
			if err := s.Delete(vm.ID); err != nil {
				return nil, err
			}
		}
	}
	return groups, nil
}
