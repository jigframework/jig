// Package jig is an opinionated framework for building CRUD applications and
// SaaS products in Go.
//
// A jig application is a set of resources registered on an App. A resource is
// an ordinary Go struct that embeds Model, built into HTTP routes with the
// resource package. There is no code generation: the compiler checks the
// wiring, and the docs/adr directory records the reasoning behind each
// opinion.
package jig

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/jigframework/jig/tenant"
)

// App holds the registered resources and the configuration needed to serve
// them. Construct one with New.
type App struct {
	name      string
	log       *slog.Logger
	banner    io.Writer // nil disables the startup banner
	created   time.Time // when New was called; startup time is measured from here
	tenants   tenant.Resolver
	resources []Resource
}

// Option configures an App.
type Option func(*App)

// WithLogger sets the logger used by the application. The default writes
// text to stderr.
func WithLogger(l *slog.Logger) Option {
	return func(a *App) { a.log = l }
}

// WithTenant sets how the current organization is read from each request.
// Every application that registers a resource must set one; tenancy is not
// optional in jig.
func WithTenant(r tenant.Resolver) Option {
	return func(a *App) { a.tenants = r }
}

// WithoutBanner suppresses the ASCII art banner Run prints on startup. Use it
// when stdout is consumed by a log shipper that expects one record per line.
func WithoutBanner() Option {
	return func(a *App) { a.banner = nil }
}

// WithBannerWriter sets where Run prints the startup banner. The default is
// stdout.
func WithBannerWriter(w io.Writer) Option {
	return func(a *App) { a.banner = w }
}

// New creates an application with the given name.
func New(name string, opts ...Option) *App {
	a := &App{name: name, log: slog.Default(), banner: os.Stdout, created: time.Now()}
	for _, o := range opts {
		o(a)
	}
	return a
}

// Register adds resources to the application. Registering two resources with
// the same name is a programming error and panics.
func (a *App) Register(rs ...Resource) {
	for _, r := range rs {
		for _, existing := range a.resources {
			if existing.Name() == r.Name() {
				panic(fmt.Sprintf("jig: resource %q registered twice", r.Name()))
			}
		}
		a.resources = append(a.resources, r)
	}
}

// Resources returns the registered resources in registration order.
func (a *App) Resources() []Resource {
	out := make([]Resource, len(a.resources))
	copy(out, a.resources)
	return out
}

// Handler returns the HTTP handler for the application: a health endpoint at
// GET /healthz plus the routes of every registered resource, behind
// middleware that places the tenant on the request context.
//
// Handler panics if resources are registered but no tenant resolver was set,
// because every resource route would otherwise refuse every request.
func (a *App) Handler() http.Handler {
	if len(a.resources) > 0 && a.tenants == nil {
		panic("jig: resources registered without a tenant resolver; pass jig.WithTenant to New")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok\n"))
	})
	for _, r := range a.resources {
		r.Routes(mux)
	}
	if a.tenants == nil {
		return mux
	}
	return resolveTenant(a.tenants, mux)
}

// resolveTenant runs the resolver on every request and, when it names an
// organization, places it on the context. Requests without a tenant are
// still passed through: tenant-scoped handlers reject them themselves, and
// global resources and /healthz do not need one.
func resolveTenant(resolve tenant.Resolver, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, err := resolve(r)
		if err != nil {
			e := Unauthenticated()
			e.cause = err
			WriteError(w, e)
			return
		}
		if id != "" {
			r = r.WithContext(tenant.With(r.Context(), id))
		}
		next.ServeHTTP(w, r)
	})
}

// Run prints the startup banner and serves the application until ctx is
// cancelled. Once the listener is bound it logs the address and how long
// startup took, measured from the call to New.
func (a *App) Run(ctx context.Context, addr string) error {
	if a.banner != nil {
		writeBanner(a.banner, a.name, len(a.resources))
	}
	handler := a.Handler() // fail before binding if the app is misconfigured
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	a.log.Info("jig: listening",
		"app", a.name,
		"addr", ln.Addr().String(),
		"startup", roundDuration(time.Since(a.created)),
	)

	srv := &http.Server{Handler: handler}
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	select {
	case <-ctx.Done():
		return srv.Shutdown(context.Background())
	case err := <-errc:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

// roundDuration trims a duration to the precision a human reading a startup
// log cares about: whole milliseconds, or microseconds below one millisecond.
func roundDuration(d time.Duration) time.Duration {
	if d < time.Millisecond {
		return d.Round(time.Microsecond)
	}
	return d.Round(time.Millisecond)
}
