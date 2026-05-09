package repository

import (
	"testing"

	bolt "go.etcd.io/bbolt"
	"github.com/TheSlopMachine/llm-router/internal/testutil"
)

type testRecord struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
	Value  int    `json:"value"`
}

func setupTestRepo(t *testing.T) *Repository[testRecord] {
	t.Helper()
	db := testutil.SetupTestDB(t)
	
	// Create the test bucket
	err := db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists([]byte("test"))
		return err
	})
	if err != nil {
		t.Fatalf("failed to create test bucket: %v", err)
	}
	
	return New[testRecord](db, []byte("test"), "test_record")
}

func TestRepository_Get_Exists(t *testing.T) {
	repo := setupTestRepo(t)

	record := &testRecord{ID: "test1", Name: "Test Record", Status: "active", Value: 42}
	if err := repo.Put("test1", record); err != nil {
		t.Fatalf("put failed: %v", err)
	}

	got, err := repo.Get("test1")
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}

	if got.ID != "test1" {
		t.Errorf("id: got %q, want %q", got.ID, "test1")
	}
	if got.Name != "Test Record" {
		t.Errorf("name: got %q, want %q", got.Name, "Test Record")
	}
	if got.Value != 42 {
		t.Errorf("value: got %d, want %d", got.Value, 42)
	}
}

func TestRepository_Get_NotFound(t *testing.T) {
	repo := setupTestRepo(t)

	_, err := repo.Get("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent record, got nil")
	}
}

func TestRepository_Put_New(t *testing.T) {
	repo := setupTestRepo(t)

	record := &testRecord{ID: "new1", Name: "New Record", Status: "pending", Value: 100}
	if err := repo.Put("new1", record); err != nil {
		t.Fatalf("put failed: %v", err)
	}

	got, err := repo.Get("new1")
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}

	if got.Name != "New Record" {
		t.Errorf("name: got %q, want %q", got.Name, "New Record")
	}
}

func TestRepository_Put_Update(t *testing.T) {
	repo := setupTestRepo(t)

	record := &testRecord{ID: "upd1", Name: "Original", Status: "active", Value: 1}
	if err := repo.Put("upd1", record); err != nil {
		t.Fatalf("initial put failed: %v", err)
	}

	updated := &testRecord{ID: "upd1", Name: "Updated", Status: "inactive", Value: 2}
	if err := repo.Put("upd1", updated); err != nil {
		t.Fatalf("update put failed: %v", err)
	}

	got, err := repo.Get("upd1")
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}

	if got.Name != "Updated" {
		t.Errorf("name: got %q, want %q", got.Name, "Updated")
	}
	if got.Value != 2 {
		t.Errorf("value: got %d, want %d", got.Value, 2)
	}
}

func TestRepository_Delete_Exists(t *testing.T) {
	repo := setupTestRepo(t)

	record := &testRecord{ID: "del1", Name: "To Delete", Status: "active", Value: 1}
	if err := repo.Put("del1", record); err != nil {
		t.Fatalf("put failed: %v", err)
	}

	if err := repo.Delete("del1"); err != nil {
		t.Fatalf("delete failed: %v", err)
	}

	_, err := repo.Get("del1")
	if err == nil {
		t.Error("expected error after delete, got nil")
	}
}

func TestRepository_Delete_NotFound(t *testing.T) {
	repo := setupTestRepo(t)

	err := repo.Delete("nonexistent")
	if err == nil {
		t.Error("expected error for deleting nonexistent record, got nil")
	}
}

func TestRepository_DeleteIfExists(t *testing.T) {
	repo := setupTestRepo(t)

	if err := repo.DeleteIfExists("nonexistent"); err != nil {
		t.Errorf("delete if exists should not error on nonexistent: %v", err)
	}

	record := &testRecord{ID: "del2", Name: "To Delete", Status: "active", Value: 1}
	if err := repo.Put("del2", record); err != nil {
		t.Fatalf("put failed: %v", err)
	}

	if err := repo.DeleteIfExists("del2"); err != nil {
		t.Fatalf("delete if exists failed: %v", err)
	}

	_, err := repo.Get("del2")
	if err == nil {
		t.Error("expected error after delete, got nil")
	}
}

func TestRepository_List_Empty(t *testing.T) {
	repo := setupTestRepo(t)

	items, err := repo.List()
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}

	if len(items) != 0 {
		t.Errorf("expected empty list, got %d items", len(items))
	}
}

func TestRepository_List_Multiple(t *testing.T) {
	repo := setupTestRepo(t)

	records := []*testRecord{
		{ID: "r1", Name: "Record 1", Status: "active", Value: 1},
		{ID: "r2", Name: "Record 2", Status: "inactive", Value: 2},
		{ID: "r3", Name: "Record 3", Status: "active", Value: 3},
	}

	for _, r := range records {
		if err := repo.Put(r.ID, r); err != nil {
			t.Fatalf("put failed: %v", err)
		}
	}

	items, err := repo.List()
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}

	if len(items) != 3 {
		t.Errorf("expected 3 items, got %d", len(items))
	}
}

func TestRepository_ListFiltered(t *testing.T) {
	repo := setupTestRepo(t)

	records := []*testRecord{
		{ID: "r1", Name: "Record 1", Status: "active", Value: 1},
		{ID: "r2", Name: "Record 2", Status: "inactive", Value: 2},
		{ID: "r3", Name: "Record 3", Status: "active", Value: 3},
	}

	for _, r := range records {
		if err := repo.Put(r.ID, r); err != nil {
			t.Fatalf("put failed: %v", err)
		}
	}

	items, err := repo.ListFiltered(func(r *testRecord) bool {
		return r.Status == "active"
	})
	if err != nil {
		t.Fatalf("list filtered failed: %v", err)
	}

	if len(items) != 2 {
		t.Errorf("expected 2 active items, got %d", len(items))
	}

	for _, item := range items {
		if item.Status != "active" {
			t.Errorf("expected active status, got %q", item.Status)
		}
	}
}

func TestRepository_FindFirst_Match(t *testing.T) {
	repo := setupTestRepo(t)

	records := []*testRecord{
		{ID: "r1", Name: "Record 1", Status: "active", Value: 1},
		{ID: "r2", Name: "Target", Status: "inactive", Value: 2},
		{ID: "r3", Name: "Record 3", Status: "active", Value: 3},
	}

	for _, r := range records {
		if err := repo.Put(r.ID, r); err != nil {
			t.Fatalf("put failed: %v", err)
		}
	}

	item, err := repo.FindFirst(func(r *testRecord) bool {
		return r.Name == "Target"
	})
	if err != nil {
		t.Fatalf("find first failed: %v", err)
	}

	if item.Name != "Target" {
		t.Errorf("name: got %q, want %q", item.Name, "Target")
	}
	if item.Value != 2 {
		t.Errorf("value: got %d, want %d", item.Value, 2)
	}
}

func TestRepository_FindFirst_NoMatch(t *testing.T) {
	repo := setupTestRepo(t)

	records := []*testRecord{
		{ID: "r1", Name: "Record 1", Status: "active", Value: 1},
		{ID: "r2", Name: "Record 2", Status: "inactive", Value: 2},
	}

	for _, r := range records {
		if err := repo.Put(r.ID, r); err != nil {
			t.Fatalf("put failed: %v", err)
		}
	}

	_, err := repo.FindFirst(func(r *testRecord) bool {
		return r.Name == "Nonexistent"
	})
	if err == nil {
		t.Error("expected error for no match, got nil")
	}
}

func TestRepository_Exists_True(t *testing.T) {
	repo := setupTestRepo(t)

	record := &testRecord{ID: "exists1", Name: "Exists", Status: "active", Value: 1}
	if err := repo.Put("exists1", record); err != nil {
		t.Fatalf("put failed: %v", err)
	}

	exists, err := repo.Exists("exists1")
	if err != nil {
		t.Fatalf("exists failed: %v", err)
	}

	if !exists {
		t.Error("expected record to exist")
	}
}

func TestRepository_Exists_False(t *testing.T) {
	repo := setupTestRepo(t)

	exists, err := repo.Exists("nonexistent")
	if err != nil {
		t.Fatalf("exists failed: %v", err)
	}

	if exists {
		t.Error("expected record to not exist")
	}
}

func TestRepository_Count_Zero(t *testing.T) {
	repo := setupTestRepo(t)

	count, err := repo.Count()
	if err != nil {
		t.Fatalf("count failed: %v", err)
	}

	if count != 0 {
		t.Errorf("expected count 0, got %d", count)
	}
}

func TestRepository_Count_Multiple(t *testing.T) {
	repo := setupTestRepo(t)

	records := []*testRecord{
		{ID: "r1", Name: "Record 1", Status: "active", Value: 1},
		{ID: "r2", Name: "Record 2", Status: "inactive", Value: 2},
		{ID: "r3", Name: "Record 3", Status: "active", Value: 3},
	}

	for _, r := range records {
		if err := repo.Put(r.ID, r); err != nil {
			t.Fatalf("put failed: %v", err)
		}
	}

	count, err := repo.Count()
	if err != nil {
		t.Fatalf("count failed: %v", err)
	}

	if count != 3 {
		t.Errorf("expected count 3, got %d", count)
	}
}

func TestRepository_Update_Exists(t *testing.T) {
	repo := setupTestRepo(t)

	record := &testRecord{ID: "upd1", Name: "Original", Status: "active", Value: 10}
	if err := repo.Put("upd1", record); err != nil {
		t.Fatalf("put failed: %v", err)
	}

	err := repo.Update("upd1", func(r *testRecord) error {
		r.Name = "Modified"
		r.Value = 20
		return nil
	})
	if err != nil {
		t.Fatalf("update failed: %v", err)
	}

	got, err := repo.Get("upd1")
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}

	if got.Name != "Modified" {
		t.Errorf("name: got %q, want %q", got.Name, "Modified")
	}
	if got.Value != 20 {
		t.Errorf("value: got %d, want %d", got.Value, 20)
	}
}

func TestRepository_Update_NotFound(t *testing.T) {
	repo := setupTestRepo(t)

	err := repo.Update("nonexistent", func(r *testRecord) error {
		r.Name = "Modified"
		return nil
	})
	if err == nil {
		t.Error("expected error for updating nonexistent record, got nil")
	}
}

