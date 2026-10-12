package jig

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jigframework/jig/tenant"
)

type fakeResource string

func (f fakeResource) Name() string { return string(f) }
func (f fakeResource) Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /"+string(f), func(w http.ResponseWriter, r *http.Request) {
		id, _ := tenant.From(r.Context())
		_, _ = w.Write([]byte(id))
	})
}

// lockedBuffer is a bytes.Buffer safe to write from the server goroutine and
// read from the test goroutine.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestRegisterRejectsDuplicates(t *testing.T) {
	app := New("test")
	app.Register(fakeResource("project"))
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on duplicate resource")
		}
	}()
	app.Register(fakeResource("project"))
}

func TestHandlerRequiresTenantResolver(t *testing.T) {
	app := New("test")
	app.Register(fakeResource("things"))
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic when resources are registered without WithTenant")
		}
	}()
	app.Handler()
}

func TestTenantMiddleware(t *testing.T) {
	app := New("test", WithTenant(tenant.FromHeader("X-Org-ID")))
	app.Register(fakeResource("things"))
	h := app.Handler()

	req := httptest.NewRequest(http.MethodGet, "/things", nil)
	req.Header.Set("X-Org-ID", "acme")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Body.String() != "acme" {
		t.Fatalf("tenant not placed on context: %q", rec.Body.String())
	}

	bad := New("test", WithTenant(func(*http.Request) (tenant.ID, error) { return "", errors.New("bad token") }))
	bad.Register(fakeResource("things"))
	rec = httptest.NewRecorder()
	bad.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/things", nil))
	if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), "unauthenticated") {
		t.Fatalf("resolver error should be 401: %d %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "bad token") {
		t.Fatal("cause leaked to client")
	}
}

func TestServe(t *testing.T) {
	type in struct{ N int }
	type out struct {
		Double int `json:"double"`
	}
	h := Serve(http.StatusOK,
		func(r *http.Request) (in, error) {
			var v in
			return v, DecodeBody(r, &v)
		},
		func(_ context.Context, v in) (out, error) {
			if v.N < 0 {
				return out{}, NotFound("positive number")
			}
			return out{v.N * 2}, nil
		})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"N":21}`)))
	if rec.Code != 200 || strings.TrimSpace(rec.Body.String()) != `{"double":42}` {
		t.Fatalf("ok: %d %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"N":-1}`)))
	if rec.Code != 404 || !strings.Contains(rec.Body.String(), `"code":"not_found"`) {
		t.Fatalf("handler error: %d %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"Nope":1}`)))
	if rec.Code != 422 {
		t.Fatalf("unknown field: %d %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	Serve(http.StatusNoContent, func(*http.Request) (struct{}, error) { return struct{}{}, nil },
		func(context.Context, struct{}) (struct{}, error) { return struct{}{}, nil }).
		ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/", nil))
	if rec.Code != 204 || rec.Body.Len() != 0 {
		t.Fatalf("204 must have no body: %d %q", rec.Code, rec.Body.String())
	}
}

func TestHealthz(t *testing.T) {
	rec := httptest.NewRecorder()
	New("test").Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", rec.Code)
	}
}

func TestStatusOf(t *testing.T) {
	cases := map[error]int{
		NotFound("project"):          404,
		Forbidden("no"):              403,
		Unauthenticated():            401,
		Invalid(nil):                 422,
		Conflict("stale"):            409,
		Internal(errors.New("boom")): 500,
		errors.New("plain"):          500,
	}
	for err, want := range cases {
		if got := StatusOf(err); got != want {
			t.Errorf("%v: got %d, want %d", err, got, want)
		}
	}
}

func TestBanner(t *testing.T) {
	var buf bytes.Buffer
	writeBanner(&buf, "todo", 1)
	out := buf.String()
	for _, want := range []string{"|__/   |___/", ":: jig dev ::", "todo", "1 resource,"} {
		if !strings.Contains(out, want) {
			t.Errorf("banner missing %q:\n%s", want, out)
		}
	}
	if !strings.HasSuffix(out, "\n\n") {
		t.Errorf("banner should end with a blank line:\n%q", out)
	}
}

func TestWithoutBanner(t *testing.T) {
	if New("test", WithoutBanner()).banner != nil {
		t.Fatal("WithoutBanner should clear the banner writer")
	}
	var buf bytes.Buffer
	if New("test", WithBannerWriter(&buf)).banner != &buf {
		t.Fatal("WithBannerWriter should set the banner writer")
	}
}

func TestRunLogsStartupTime(t *testing.T) {
	var logs lockedBuffer
	app := New("test",
		WithoutBanner(),
		WithLogger(slog.New(slog.NewTextHandler(&logs, nil))),
	)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- app.Run(ctx, "127.0.0.1:0") }()

	deadline := time.After(5 * time.Second)
	for !strings.Contains(logs.String(), "jig: listening") {
		select {
		case err := <-done:
			t.Fatalf("Run returned early: %v", err)
		case <-deadline:
			t.Fatalf("no listening log line:\n%s", logs.String())
		case <-time.After(5 * time.Millisecond):
		}
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, want := range []string{"app=test", "addr=127.0.0.1:", "startup="} {
		if !strings.Contains(logs.String(), want) {
			t.Errorf("log missing %q:\n%s", want, logs.String())
		}
	}
}

func TestRoundDuration(t *testing.T) {
	cases := map[time.Duration]time.Duration{
		1234 * time.Nanosecond:                       time.Microsecond,
		1234567 * time.Nanosecond:                    time.Millisecond,
		1500*time.Millisecond + 400*time.Microsecond: 1500 * time.Millisecond,
	}
	for in, want := range cases {
		if got := roundDuration(in); got != want {
			t.Errorf("roundDuration(%v) = %v, want %v", in, got, want)
		}
	}
}
