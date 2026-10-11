package dashboard

import (
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/models"
)

func managedVirtualModel() *models.VirtualModel {
	return &models.VirtualModel{
		Name:        "P Chat",
		Description: "auto",
		Instruction: "be nice",
		ManagedBy:   "provider:p:chat",
		Models:      []models.VirtualModelEntry{{ModelID: "p/a"}, {ModelID: "p/b"}},
	}
}

func TestManagedUpdateIsToggleOnly_AllowsDisabledFlip(t *testing.T) {
	existing := managedVirtualModel()
	incoming := managedVirtualModel()
	incoming.Disabled = !existing.Disabled
	if !managedUpdateIsToggleOnly(existing, incoming) {
		t.Fatal("disabled-only flip must be allowed")
	}
}

func TestManagedUpdateIsToggleOnly_RejectsEdits(t *testing.T) {
	cases := map[string]func(virtualModel *models.VirtualModel){
		"name":        func(virtualModel *models.VirtualModel) { virtualModel.Name = "other" },
		"description": func(virtualModel *models.VirtualModel) { virtualModel.Description = "other" },
		"instruction": func(virtualModel *models.VirtualModel) { virtualModel.Instruction = "other" },
		"marker":      func(virtualModel *models.VirtualModel) { virtualModel.ManagedBy = "provider:p:tts" },
		"members":     func(virtualModel *models.VirtualModel) { virtualModel.Models = virtualModel.Models[:1] },
	}
	for label, mutate := range cases {
		incoming := managedVirtualModel()
		mutate(incoming)
		if managedUpdateIsToggleOnly(managedVirtualModel(), incoming) {
			t.Fatalf("%s edit must be rejected", label)
		}
	}
}
