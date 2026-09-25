package virtual

import (
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/models"
)

func TestLiveMembers_ManualUsesStoredList(t *testing.T) {
	svc := newVirtualTestStack(t)
	agent := &models.VirtualModel{
		Name:   "manual",
		Models: []models.VirtualModelEntry{{ModelID: "demo/a"}, {ModelID: "demo/b"}},
	}
	members, err := svc.LiveMembers(agent)
	if err != nil {
		t.Fatalf("members: %v", err)
	}
	if len(members) != 2 || members[0] != "demo/a" || members[1] != "demo/b" {
		t.Fatalf("members: %v", members)
	}
	if members, err := svc.LiveMembers(nil); err == nil || len(members) != 0 {
		t.Fatalf("nil agent: %v %v", members, err)
	}
}

func TestLiveMembers_BadMarkerResolvesNothing(t *testing.T) {
	svc := newVirtualTestStack(t)
	for _, marker := range []string{"provider:", "provider:no-colon"} {
		agent := &models.VirtualModel{Name: "m", ManagedBy: marker}
		if members, err := svc.LiveMembers(agent); err == nil || len(members) != 0 {
			t.Fatalf("marker %q: %v %v", marker, members, err)
		}
	}
	agent := &models.VirtualModel{Name: "m", ManagedBy: "provider:missing:chat"}
	if members, err := svc.LiveMembers(agent); err != nil || len(members) != 0 {
		t.Fatalf("unknown provider: %v %v", members, err)
	}
}
