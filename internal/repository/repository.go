// Package repository provides generic type-safe database operations using Go generics.
package repository

import (
	"encoding/json"
	"errors"
	"fmt"

	bolt "go.etcd.io/bbolt"
	"github.com/TheSlopMachine/llm-router/internal/db"
)

var errFound = errors.New("found")

// Repository provides type-safe CRUD operations for a bbolt bucket.
// T is the type of records stored in this repository.
type Repository[T any] struct {
	db     *db.DB
	bucket []byte
	name   string // human-readable name for error messages
}

// New creates a new typed repository for the given bucket.
//
// Parameters:
//   - database: the bbolt database wrapper
//   - bucket: the bucket name (e.g., db.BucketProviders)
//   - name: human-readable name for error messages (e.g., "provider")
func New[T any](database *db.DB, bucket []byte, name string) *Repository[T] {
	return &Repository[T]{
		db:     database,
		bucket: bucket,
		name:   name,
	}
}

// Get retrieves a single record by ID.
// Returns an error if the record does not exist.
func (r *Repository[T]) Get(id string) (*T, error) {
	var obj T
	err := r.db.View(func(tx *bolt.Tx) error {
		data := tx.Bucket(r.bucket).Get([]byte(id))
		if data == nil {
			return fmt.Errorf("%s %q not found", r.name, id)
		}
		return json.Unmarshal(data, &obj)
	})
	if err != nil {
		return nil, err
	}
	return &obj, nil
}

// Put stores a record with the given ID.
// Overwrites any existing record with the same ID.
func (r *Repository[T]) Put(id string, obj *T) error {
	enc, err := json.Marshal(obj)
	if err != nil {
		return fmt.Errorf("marshal %s: %w", r.name, err)
	}
	return r.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(r.bucket).Put([]byte(id), enc)
	})
}

// List returns all records in the bucket.
func (r *Repository[T]) List() ([]*T, error) {
	var items []*T
	err := r.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(r.bucket).ForEach(func(_, v []byte) error {
			var item T
			if err := json.Unmarshal(v, &item); err != nil {
				return fmt.Errorf("unmarshal %s: %w", r.name, err)
			}
			items = append(items, &item)
			return nil
		})
	})
	return items, err
}

// Delete removes a record by ID.
// Returns an error if the record does not exist.
func (r *Repository[T]) Delete(id string) error {
	return r.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(r.bucket)
		if b.Get([]byte(id)) == nil {
			return fmt.Errorf("%s %q not found", r.name, id)
		}
		return b.Delete([]byte(id))
	})
}

// DeleteIfExists removes a record by ID if it exists.
// Does not return an error if the record doesn't exist.
func (r *Repository[T]) DeleteIfExists(id string) error {
	return r.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(r.bucket).Delete([]byte(id))
	})
}

// Update performs an atomic get-modify-put operation.
// The provided function receives a pointer to the record and can modify it.
// The modified record is then saved back to the database.
//
// Example:
//
//	err := repo.Update(id, func(obj *MyType) error {
//	    obj.Field = "new value"
//	    return nil
//	})
func (r *Repository[T]) Update(id string, fn func(*T) error) error {
	return r.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(r.bucket)
		raw := b.Get([]byte(id))
		if raw == nil {
			return fmt.Errorf("%s %q not found", r.name, id)
		}

		var obj T
		if err := json.Unmarshal(raw, &obj); err != nil {
			return fmt.Errorf("unmarshal %s: %w", r.name, err)
		}

		if err := fn(&obj); err != nil {
			return err
		}

		enc, err := json.Marshal(&obj)
		if err != nil {
			return fmt.Errorf("marshal %s: %w", r.name, err)
		}
		return b.Put([]byte(id), enc)
	})
}

// ListFiltered returns all records matching the given predicate.
//
// Example:
//
//	items, err := repo.ListFiltered(func(obj *MyType) bool {
//	    return obj.Status == "active"
//	})
func (r *Repository[T]) ListFiltered(predicate func(*T) bool) ([]*T, error) {
	var items []*T
	err := r.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(r.bucket).ForEach(func(_, v []byte) error {
			var item T
			if err := json.Unmarshal(v, &item); err != nil {
				return fmt.Errorf("unmarshal %s: %w", r.name, err)
			}
			if predicate(&item) {
				items = append(items, &item)
			}
			return nil
		})
	})
	return items, err
}

// FindFirst returns the first record matching the given predicate.
// Returns an error if no matching record is found.
//
// Example:
//
//	item, err := repo.FindFirst(func(obj *MyType) bool {
//	    return obj.Name == "target"
//	})
func (r *Repository[T]) FindFirst(predicate func(*T) bool) (*T, error) {
	var result *T
	err := r.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(r.bucket).ForEach(func(_, v []byte) error {
			var item T
			if err := json.Unmarshal(v, &item); err != nil {
				return fmt.Errorf("unmarshal %s: %w", r.name, err)
			}
			if predicate(&item) {
				result = &item
				return errFound
			}
			return nil
		})
	})
	if errors.Is(err, errFound) {
		return result, nil
	}
	if err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("%s not found", r.name)
}

// Exists checks if a record with the given ID exists.
func (r *Repository[T]) Exists(id string) (bool, error) {
	var exists bool
	err := r.db.View(func(tx *bolt.Tx) error {
		exists = tx.Bucket(r.bucket).Get([]byte(id)) != nil
		return nil
	})
	return exists, err
}

// Count returns the total number of records in the bucket.
func (r *Repository[T]) Count() (int, error) {
	var count int
	err := r.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(r.bucket).ForEach(func(_, _ []byte) error {
			count++
			return nil
		})
	})
	return count, err
}

