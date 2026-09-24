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
	members := svc.LiveMembers(agent)
	if len(members) != 2 || members[0] != "demo/a" || members[1] != "demo/b" {
		t.Fatalf("members: %v", members)
	}
	if members := svc.LiveMembers(nil); len(members) != 0 {
		t.Fatalf("nil agent: %v", members)
	}
}

func TestLiveMembers_BadMarkerResolvesNothing(t *testing.T) {
	svc := newVirtualTestStack(t)
	for _, marker := range []string{"provider:", "provider:no-colon", "provider:missing:chat"} {
		agent := &models.VirtualModel{Name: "m", ManagedBy: marker}
		if members := svc.LiveMembers(agent); len(members) != 0 {
			t.Fatalf("marker %q: %v", marker, members)
		}
	}
}
