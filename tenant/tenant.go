// Package tenant carries the current organization through a request. Every
// tenant-scoped resource reads the tenant from context and refuses to run
// without one.
package tenant

import (
	"context"
	"errors"
	"net/http"
)

// ID identifies an organization. It is a string so the storage type can be
// changed without touching call sites.
type ID string

type ctxKey struct{}

// ErrMissing is returned when a tenant-scoped operation runs on a context
// that has no tenant.
var ErrMissing = errors.New("tenant: no tenant on context")

// With returns a context carrying the given tenant.
func With(ctx context.Context, id ID) context.Context {
	return context.WithValue(ctx, ctxKey{}, id)
}

// From returns the tenant on ctx, if any.
func From(ctx context.Context) (ID, bool) {
	id, ok := ctx.Value(ctxKey{}).(ID)
	return id, ok && id != ""
}

// Must returns the tenant on ctx or ErrMissing.
func Must(ctx context.Context) (ID, error) {
	id, ok := From(ctx)
	if !ok {
		return "", ErrMissing
	}
	return id, nil
}

// Resolver reads the current organization from a request. It returns an empty
// ID when the request carries no tenant and an error when it carries an
// invalid one, which the application reports as unauthenticated.
type Resolver func(r *http.Request) (ID, error)

// FromHeader resolves the tenant from a request header. It is the simplest
// possible resolver and suits development and trusted internal services; a
// public application should resolve the tenant from an authenticated session
// or API key instead.
func FromHeader(name string) Resolver {
	return func(r *http.Request) (ID, error) {
		return ID(r.Header.Get(name)), nil
	}
}
