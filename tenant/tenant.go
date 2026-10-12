// Package tenant carries the current organization through a request. Every
// tenant-scoped resource reads the tenant from context and refuses to run
// without one.
package tenant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"

	"github.com/jigframework/jig/auth"
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

// FromJWTAttribute resolves the tenant from an attribute of the authenticated
// caller, such as a JWT claim. The application must also set jig.WithAuth,
// typically with auth/jwt, so that the caller is verified and on the context
// before the resolver runs; without it every request is anonymous.
//
// A request with no caller, or a caller without the attribute, has no tenant
// and tenant-scoped routes reject it as unauthenticated. The attribute must
// be a string or an integer; any other type is an error, so a misconfigured
// claim fails loudly instead of scoping records to a tenant named "map[...]".
//
// Using "sub" makes each user their own tenant. A SaaS application with
// organizations usually names the claim that carries the org ID instead.
func FromJWTAttribute(name string) Resolver {
	return func(r *http.Request) (ID, error) {
		c, ok := auth.From(r.Context())
		if !ok {
			return "", nil
		}
		v, ok := c.Attribute(name)
		if !ok {
			return "", nil
		}
		id, err := attributeID(v)
		if err != nil {
			return "", fmt.Errorf("tenant: attribute %q: %w", name, err)
		}
		return id, nil
	}
}

// attributeID renders a claim value as a tenant ID. JSON decoders produce
// strings, json.Number or float64; integers of either numeric kind are
// accepted so an issuer that emits numeric IDs works unchanged.
func attributeID(v any) (ID, error) {
	switch v := v.(type) {
	case string:
		return ID(v), nil
	case json.Number:
		if _, err := v.Int64(); err != nil {
			return "", fmt.Errorf("number %s is not an integer", v)
		}
		return ID(v.String()), nil
	case float64:
		if v != math.Trunc(v) || math.IsInf(v, 0) || math.IsNaN(v) {
			return "", fmt.Errorf("number %v is not an integer", v)
		}
		return ID(strconv.FormatFloat(v, 'f', 0, 64)), nil
	case int:
		return ID(strconv.Itoa(v)), nil
	case int64:
		return ID(strconv.FormatInt(v, 10)), nil
	default:
		return "", fmt.Errorf("is %T, want string or integer", v)
	}
}
