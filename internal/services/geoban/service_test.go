package geoban

import (
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/testutil"
)

func TestMarkAndIsBanned(t *testing.T) {
	svc := New(testutil.SetupTestDB(t))
	if err := svc.Mark("plug", "prov", "px1", "geo blocked"); err != nil {
		t.Fatalf("mark: %v", err)
	}
	banned, err := svc.IsBanned("plug", "prov", "px1")
	if err != nil || !banned {
		t.Fatalf("must be banned: %v %v", banned, err)
	}
	banned, err = svc.IsBanned("plug", "prov", "px2")
	if err != nil || banned {
		t.Fatalf("other proxy must pass: %v %v", banned, err)
	}
	banned, err = svc.IsBanned("plug", "other", "px1")
	if err != nil || banned {
		t.Fatalf("other provider must pass: %v %v", banned, err)
	}
}

func TestMarkFirstWins(t *testing.T) {
	svc := New(testutil.SetupTestDB(t))
	if err := svc.Mark("plug", "prov", "px1", "first"); err != nil {
		t.Fatalf("mark: %v", err)
	}
	if err := svc.Mark("plug", "prov", "px1", "second"); err != nil {
		t.Fatalf("remake: %v", err)
	}
	bans, err := svc.ListProvider("plug", "prov")
	if err != nil || len(bans) != 1 {
		t.Fatalf("one ban: %v %v", len(bans), err)
	}
	if bans[0].Reason != "first" {
		t.Fatalf("first reason wins, got %q", bans[0].Reason)
	}
}

func TestClearFlows(t *testing.T) {
	svc := New(testutil.SetupTestDB(t))
	if err := svc.Mark("plug", "prov", "px1", "a"); err != nil {
		t.Fatal(err)
	}
	if err := svc.Mark("plug", "prov", "px2", "b"); err != nil {
		t.Fatal(err)
	}
	if err := svc.Mark("plug", "other", "px1", "c"); err != nil {
		t.Fatal(err)
	}
	if err := svc.Clear("plug", "prov", "px1"); err != nil {
		t.Fatal(err)
	}
	if banned, _ := svc.IsBanned("plug", "prov", "px1"); banned {
		t.Fatal("cleared ban must pass")
	}
	n, err := svc.ClearProxy("px1")
	if err != nil || n != 1 {
		t.Fatalf("clear proxy: %d %v", n, err)
	}
	n, err = svc.ClearProvider("plug", "prov")
	if err != nil || n != 1 {
		t.Fatalf("clear provider: %d %v", n, err)
	}
	if banned, _ := svc.IsBanned("plug", "prov", "px2"); banned {
		t.Fatal("provider clear must pass")
	}
}

func TestBannedRegions(t *testing.T) {
	svc := New(testutil.SetupTestDB(t))
	if err := svc.Mark("plug", "prov", "px1", "a"); err != nil {
		t.Fatal(err)
	}
	regions, err := svc.BannedRegions("plug", "prov", func(id string) string {
		if id == "px1" {
			return "US"
		}
		return ""
	})
	if err != nil || !regions["US"] {
		t.Fatalf("US must be banned: %v %v", regions, err)
	}
}
