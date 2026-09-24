package dashboard

import (
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/models"
)

func managedVM() *models.VirtualModel {
	return &models.VirtualModel{
		Name:        "P Chat",
		Description: "auto",
		Instruction: "be nice",
		ManagedBy:   "provider:p:chat",
		Models:      []models.VirtualModelEntry{{ModelID: "p/a"}, {ModelID: "p/b"}},
	}
}

func TestManagedUpdateIsToggleOnly_AllowsDisabledFlip(t *testing.T) {
	existing := managedVM()
	incoming := managedVM()
	incoming.Disabled = !existing.Disabled
	if !managedUpdateIsToggleOnly(existing, incoming) {
		t.Fatal("disabled-only flip must be allowed")
	}
}

func TestManagedUpdateIsToggleOnly_RejectsEdits(t *testing.T) {
	cases := map[string]func(vm *models.VirtualModel){
		"name":        func(vm *models.VirtualModel) { vm.Name = "other" },
		"description": func(vm *models.VirtualModel) { vm.Description = "other" },
		"instruction": func(vm *models.VirtualModel) { vm.Instruction = "other" },
		"marker":      func(vm *models.VirtualModel) { vm.ManagedBy = "provider:p:tts" },
		"members":     func(vm *models.VirtualModel) { vm.Models = vm.Models[:1] },
	}
	for label, mutate := range cases {
		incoming := managedVM()
		mutate(incoming)
		if managedUpdateIsToggleOnly(managedVM(), incoming) {
			t.Fatalf("%s edit must be rejected", label)
		}
	}
}
