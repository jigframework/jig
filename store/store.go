// Package store defines how a resource persists its records and provides an
// in-memory implementation.
//
// A Store is a keyed collection and nothing more. The resource package owns
// IDs, timestamps, versions and soft deletes, so a Store never inspects T and
// any Go type can be stored.
package store

import (
	"context"

	"github.com/jigframework/jig/tenant"
)

// Store persists records of type T, partitioned by organization. A global
// resource uses the empty tenant ID.
type Store[T any] interface {
	// List returns every record for org in creation order.
	List(ctx context.Context, org tenant.ID) ([]T, error)
	// Get returns the record with id, and false if there is none.
	Get(ctx context.Context, org tenant.ID, id string) (T, bool, error)
	// Put inserts the record with id or replaces an existing one in place.
	Put(ctx context.Context, org tenant.ID, id string, v T) error
	// Delete removes the record with id. Deleting a missing record is not
	// an error.
	Delete(ctx context.Context, org tenant.ID, id string) error
}
