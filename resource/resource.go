// Package resource turns a Go struct into a set of HTTP routes.
//
// A resource is any struct that embeds jig.Model. Of starts a builder for it,
// and Build returns a Resource that an application registers:
//
//	type Todo struct {
//		jig.Model
//		Title string `json:"title"`
//		Done  bool   `json:"done"`
//	}
//
//	var Todos = resource.Of[Todo]("todos").
//		Store(store.Memory[Todo]()).
//		Validate(func(t Todo) error {
//			return validate.Required("title", t.Title)
//		}).
//		Build()
//
// Registering Todos serves the standard CRUD routes:
//
//	GET    /todos         list, with ?limit= and ?cursor= pagination
//	POST   /todos         create, 201 with the record
//	GET    /todos/{id}    get
//	PUT    /todos/{id}    replace
//	PATCH  /todos/{id}    merge the fields present in the body
//	DELETE /todos/{id}    delete, 204
//
// Every operation is tenant-scoped, records are soft-deleted and updates are
// guarded by optimistic concurrency unless the builder opts out. The same
// operations are available as typed methods on Resource for use outside HTTP.
package resource

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/jigframework/jig"
	"github.com/jigframework/jig/cache"
	"github.com/jigframework/jig/store"
	"github.com/jigframework/jig/tenant"
)

// Entity is satisfied by a pointer to any struct that embeds jig.Model.
type Entity interface {
	Meta() *jig.Model
}

// Options are the defaults a resource can opt out of.
type Options struct {
	NoTenant     bool // records are global, not scoped by org. Rarely correct.
	NoSoftDelete bool // DELETE removes the record instead of setting deleted_at
	NoVersion    bool // disable optimistic concurrency
}

// Builder accumulates a Resource via chained calls. It is a value type so each
// call returns a modified copy and a partial builder can be shared.
//
// The second type parameter is inferred: write resource.Of[Todo]("todos").
type Builder[T any, PT interface {
	*T
	Entity
}] struct {
	r Resource[T, PT]
}

// Of starts building a resource for T served under /name. T must embed
// jig.Model, which the compiler checks.
func Of[T any, PT interface {
	*T
	Entity
}](name string) Builder[T, PT] {
	return Builder[T, PT]{Resource[T, PT]{name: name}}
}

// Store sets where records are persisted. It is required.
func (b Builder[T, PT]) Store(s store.Store[T]) Builder[T, PT] { b.r.store = s; return b }

// Validate sets the check run on every create and update, after the body is
// decoded or merged. Return a jig.Invalid error, typically built with the
// validate package, to report field problems; any other error is reported as
// a validation failure with its message.
func (b Builder[T, PT]) Validate(fn func(T) error) Builder[T, PT] { b.r.validate = fn; return b }

// Caching applies cache options to the resource in order. It may be called
// more than once; later options override earlier ones. Caching is disabled
// until a policy enables it.
func (b Builder[T, PT]) Caching(opts ...cache.Option) Builder[T, PT] {
	b.r.policy = b.r.policy.Apply(opts...)
	return b
}

// NoTenant makes the records global instead of scoped by org. Rarely correct.
func (b Builder[T, PT]) NoTenant() Builder[T, PT] { b.r.opts.NoTenant = true; return b }

// NoSoftDelete makes DELETE remove the record instead of setting deleted_at.
func (b Builder[T, PT]) NoSoftDelete() Builder[T, PT] { b.r.opts.NoSoftDelete = true; return b }

// NoVersion disables optimistic concurrency for the resource.
func (b Builder[T, PT]) NoVersion() Builder[T, PT] { b.r.opts.NoVersion = true; return b }

// Build returns the finished Resource. Resources are normally built into
// package-level variables, so a misconfigured one is a programming error and
// Build panics with the reason rather than returning an error nobody checks.
func (b Builder[T, PT]) Build() *Resource[T, PT] {
	r := b.r
	if r.name == "" {
		panic("resource: resource has no name")
	}
	if r.store == nil {
		panic(fmt.Sprintf("resource: %s has no store; call Store on the builder", r.name))
	}
	if err := r.policy.Validate(); err != nil {
		panic(fmt.Sprintf("resource: %s: %v", r.name, err))
	}
	r.cache = cache.New[T](r.policy)
	r.now = time.Now
	r.newID = newID
	return &r
}

// Resource serves CRUD operations for T. Build one with Of.
type Resource[T any, PT interface {
	*T
	Entity
}] struct {
	name     string
	store    store.Store[T]
	validate func(T) error
	policy   cache.Policy
	cache    *cache.Cache[T]
	opts     Options
	now      func() time.Time
	newID    func() string
}

// Name returns the path segment the resource is served under.
func (r *Resource[T, PT]) Name() string { return r.name }

// Options returns the opt-outs applied to the resource.
func (r *Resource[T, PT]) Options() Options { return r.opts }

// CachePolicy returns the resource's caching policy.
func (r *Resource[T, PT]) CachePolicy() cache.Policy { return r.policy }

// ListParams control pagination. Limit defaults to DefaultLimit and is capped
// at MaxLimit. Cursor is the NextCursor of the previous page.
type ListParams struct {
	Limit  int
	Cursor string
}

const (
	DefaultLimit = 50
	MaxLimit     = 200
)

// Page is one page of a list.
type Page[T any] struct {
	Items      []T    `json:"items"`
	NextCursor string `json:"next_cursor,omitempty"`
}

func meta[T any, PT interface {
	*T
	Entity
}](v *T) *jig.Model {
	return PT(v).Meta()
}

func (r *Resource[T, PT]) org(ctx context.Context) (tenant.ID, error) {
	if r.opts.NoTenant {
		return "", nil
	}
	id, err := tenant.Must(ctx)
	if err != nil {
		return "", jig.Unauthenticated()
	}
	return id, nil
}

func (r *Resource[T, PT]) key(org tenant.ID, id string) string {
	return string(org) + "/" + id
}

func (r *Resource[T, PT]) notFound() error { return jig.NotFound(r.name) }

func (r *Resource[T, PT]) check(v T) error {
	if r.validate == nil {
		return nil
	}
	err := r.validate(v)
	if err == nil {
		return nil
	}
	var e *jig.Error
	if errors.As(err, &e) {
		return err
	}
	e = jig.Invalid(nil)
	e.Message = err.Error()
	return e
}

// load fetches a live record, skipping soft-deleted ones. The cache is only
// consulted here, and only for reads that are not about to write.
func (r *Resource[T, PT]) load(ctx context.Context, org tenant.ID, id string, useCache bool) (T, error) {
	var zero T
	if useCache {
		if v, ok := r.cache.Get(r.key(org, id)); ok {
			return v, nil
		}
	}
	v, ok, err := r.store.Get(ctx, org, id)
	if err != nil {
		return zero, jig.Internal(err)
	}
	if !ok || (!r.opts.NoSoftDelete && meta[T, PT](&v).DeletedAt != nil) {
		return zero, r.notFound()
	}
	if useCache {
		r.cache.Set(r.key(org, id), v)
	}
	return v, nil
}

func (r *Resource[T, PT]) save(ctx context.Context, org tenant.ID, v T) error {
	id := meta[T, PT](&v).ID
	if err := r.store.Put(ctx, org, id, v); err != nil {
		return jig.Internal(err)
	}
	r.cache.Delete(r.key(org, id))
	return nil
}

// List returns one page of live records in creation order.
func (r *Resource[T, PT]) List(ctx context.Context, p ListParams) (Page[T], error) {
	org, err := r.org(ctx)
	if err != nil {
		return Page[T]{}, err
	}
	limit := p.Limit
	if limit <= 0 {
		limit = DefaultLimit
	}
	if limit > MaxLimit {
		limit = MaxLimit
	}
	all, err := r.store.List(ctx, org)
	if err != nil {
		return Page[T]{}, jig.Internal(err)
	}
	page := Page[T]{Items: []T{}}
	skipping := p.Cursor != ""
	for i := range all {
		m := meta[T, PT](&all[i])
		if skipping {
			if m.ID == p.Cursor {
				skipping = false
			}
			continue
		}
		if !r.opts.NoSoftDelete && m.DeletedAt != nil {
			continue
		}
		if len(page.Items) == limit {
			page.NextCursor = meta[T, PT](&page.Items[limit-1]).ID
			break
		}
		page.Items = append(page.Items, all[i])
	}
	return page, nil
}

// Get returns the record with id.
func (r *Resource[T, PT]) Get(ctx context.Context, id string) (T, error) {
	org, err := r.org(ctx)
	if err != nil {
		var zero T
		return zero, err
	}
	return r.load(ctx, org, id, true)
}

// Create validates and stores a new record. Framework-owned fields on the
// input are overwritten.
func (r *Resource[T, PT]) Create(ctx context.Context, in T) (T, error) {
	var zero T
	org, err := r.org(ctx)
	if err != nil {
		return zero, err
	}
	if err := r.check(in); err != nil {
		return zero, err
	}
	now := r.now()
	*meta[T, PT](&in) = jig.Model{ID: r.newID(), OrgID: org, CreatedAt: now, UpdatedAt: now, Version: 1}
	if err := r.save(ctx, org, in); err != nil {
		return zero, err
	}
	return in, nil
}

// Update replaces the record with id. If in carries a non-zero Version it
// must match the stored one, or Update returns jig.Conflict.
func (r *Resource[T, PT]) Update(ctx context.Context, id string, in T) (T, error) {
	return r.modify(ctx, id, func(cur *T) error {
		want := meta[T, PT](&in).Version
		have := meta[T, PT](cur).Version
		if !r.opts.NoVersion && want != 0 && want != have {
			return jig.Conflict(fmt.Sprintf("%s has version %d, not %d", r.name, have, want))
		}
		m := *meta[T, PT](cur)
		*cur = in
		*meta[T, PT](cur) = m
		return nil
	})
}

// Patch applies fn to a copy of the stored record and saves the result. fn
// may change framework-owned fields freely: they are restored afterwards,
// except that a changed Version is treated as a concurrency check and
// Patch returns jig.Conflict when it does not match.
func (r *Resource[T, PT]) Patch(ctx context.Context, id string, fn func(*T) error) (T, error) {
	return r.modify(ctx, id, func(cur *T) error {
		m := *meta[T, PT](cur)
		if err := fn(cur); err != nil {
			return err
		}
		got := meta[T, PT](cur).Version
		if !r.opts.NoVersion && got != 0 && got != m.Version {
			return jig.Conflict(fmt.Sprintf("%s has version %d, not %d", r.name, m.Version, got))
		}
		*meta[T, PT](cur) = m
		return nil
	})
}

func (r *Resource[T, PT]) modify(ctx context.Context, id string, apply func(*T) error) (T, error) {
	var zero T
	org, err := r.org(ctx)
	if err != nil {
		return zero, err
	}
	cur, err := r.load(ctx, org, id, false)
	if err != nil {
		return zero, err
	}
	if err := apply(&cur); err != nil {
		return zero, err
	}
	if err := r.check(cur); err != nil {
		return zero, err
	}
	m := meta[T, PT](&cur)
	m.UpdatedAt = r.now()
	m.Version++
	if err := r.save(ctx, org, cur); err != nil {
		return zero, err
	}
	return cur, nil
}

// Delete removes the record with id, or marks it deleted when soft deletes
// are on. Deleting a record that is already gone returns jig.NotFound.
func (r *Resource[T, PT]) Delete(ctx context.Context, id string) error {
	org, err := r.org(ctx)
	if err != nil {
		return err
	}
	cur, err := r.load(ctx, org, id, false)
	if err != nil {
		return err
	}
	if r.opts.NoSoftDelete {
		if err := r.store.Delete(ctx, org, id); err != nil {
			return jig.Internal(err)
		}
		r.cache.Delete(r.key(org, id))
		return nil
	}
	now := r.now()
	m := meta[T, PT](&cur)
	m.DeletedAt = &now
	m.UpdatedAt = now
	m.Version++
	return r.save(ctx, org, cur)
}

// Routes registers the CRUD routes on mux.
func (r *Resource[T, PT]) Routes(mux *http.ServeMux) {
	col := "/" + r.name
	one := col + "/{id}"

	mux.Handle("GET "+col, jig.Serve(http.StatusOK, decodeList, r.List))
	mux.Handle("POST "+col, jig.Serve(http.StatusCreated, decodeBody[T], r.Create))
	mux.Handle("GET "+one, jig.Serve(http.StatusOK, decodeID,
		func(ctx context.Context, id string) (T, error) { return r.Get(ctx, id) }))
	mux.Handle("PUT "+one, jig.Serve(http.StatusOK, decodeIDBody[T],
		func(ctx context.Context, in idBody[T]) (T, error) { return r.Update(ctx, in.id, in.body) }))
	mux.Handle("PATCH "+one, jig.Serve(http.StatusOK, decodeIDRaw,
		func(ctx context.Context, in idRaw) (T, error) {
			return r.Patch(ctx, in.id, func(v *T) error { return jig.Unmarshal(in.raw, v) })
		}))
	mux.Handle("DELETE "+one, jig.Serve(http.StatusNoContent, decodeID,
		func(ctx context.Context, id string) (struct{}, error) { return struct{}{}, r.Delete(ctx, id) }))
}

type idBody[T any] struct {
	id   string
	body T
}

type idRaw struct {
	id  string
	raw []byte
}

func decodeList(req *http.Request) (ListParams, error) {
	p := ListParams{Cursor: req.URL.Query().Get("cursor")}
	if s := req.URL.Query().Get("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 {
			return p, jig.Invalid(map[string]any{"limit": []string{"must be a positive integer"}})
		}
		p.Limit = n
	}
	return p, nil
}

func decodeID(req *http.Request) (string, error) { return req.PathValue("id"), nil }

func decodeBody[T any](req *http.Request) (T, error) {
	var v T
	err := jig.DecodeBody(req, &v)
	return v, err
}

func decodeIDBody[T any](req *http.Request) (idBody[T], error) {
	in := idBody[T]{id: req.PathValue("id")}
	err := jig.DecodeBody(req, &in.body)
	return in, err
}

func decodeIDRaw(req *http.Request) (idRaw, error) {
	raw, err := jig.ReadBody(req)
	return idRaw{id: req.PathValue("id"), raw: raw}, err
}

// newID returns 32 hex characters of randomness. IDs are opaque to clients.
func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("resource: crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(b[:])
}
