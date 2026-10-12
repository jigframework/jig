// Package auth identifies the caller of a request.
//
// An Authenticator reads credentials from a request and produces a Caller: a
// subject plus the attributes the credentials vouch for. The application
// places the Caller on the request context, where tenant resolvers and policy
// rules read it. Authentication says who is calling; tenancy and policy decide
// what they may touch.
//
// The auth/jwt package authenticates bearer tokens. This package will also
// grow the SaaS primitives every application rebuilds: users, organizations,
// memberships, invites, roles, sessions and API keys.
package auth

import (
	"context"
	"net/http"
)

// Caller is an authenticated identity. Attributes holds every claim the
// credential carried, keyed by claim name, so a tenant resolver or policy
// rule can read any of them. Subject and Issuer are copied out of Attributes
// for convenience.
type Caller struct {
	Subject    string
	Issuer     string
	Attributes map[string]any
}

// Attribute returns the named attribute and whether it was present.
func (c Caller) Attribute(name string) (any, bool) {
	v, ok := c.Attributes[name]
	return v, ok
}

// Authenticator reads credentials from a request. It returns found=false
// when the request carries no credentials it recognises, in which case the
// request proceeds anonymously. It returns an error when credentials are
// present but invalid; the application reports that as unauthenticated and
// never lets the request through.
type Authenticator interface {
	Authenticate(r *http.Request) (c Caller, found bool, err error)
}

// Func adapts a function to Authenticator.
type Func func(r *http.Request) (Caller, bool, error)

// Authenticate implements Authenticator.
func (f Func) Authenticate(r *http.Request) (Caller, bool, error) { return f(r) }

type ctxKey struct{}

// With returns a context carrying the given caller.
func With(ctx context.Context, c Caller) context.Context {
	return context.WithValue(ctx, ctxKey{}, c)
}

// From returns the caller on ctx, if any.
func From(ctx context.Context) (Caller, bool) {
	c, ok := ctx.Value(ctxKey{}).(Caller)
	return c, ok
}
