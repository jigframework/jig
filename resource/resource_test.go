package resource

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jigframework/jig"
	"github.com/jigframework/jig/cache"
	"github.com/jigframework/jig/store"
	"github.com/jigframework/jig/tenant"
	"github.com/jigframework/jig/validate"
)

type Note struct {
	jig.Model
	Title string `json:"title"`
	Body  string `json:"body,omitempty"`
	Stars int    `json:"stars"`
}

func notes(opts ...func(Builder[Note, *Note]) Builder[Note, *Note]) *Resource[Note, *Note] {
	b := Of[Note]("notes").
		Store(store.Memory[Note]()).
		Validate(func(n Note) error {
			return validate.All(
				validate.Required("title", n.Title),
				validate.MaxLen("title", n.Title, 20),
			)
		})
	for _, o := range opts {
		b = o(b)
	}
	return b.Build()
}

// server mounts the resource behind a header tenant resolver, exactly as an
// application would.
func server(r jig.Resource) http.Handler {
	app := jig.New("test", jig.WithoutBanner(), jig.WithTenant(tenant.FromHeader("X-Org-ID")))
	app.Register(r)
	return app.Handler()
}

type response struct {
	code int
	body map[string]any
	raw  string
}

func call(t *testing.T, h http.Handler, org, method, path string, body any) response {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		switch b := body.(type) {
		case string:
			buf.WriteString(b)
		default:
			_ = json.NewEncoder(&buf).Encode(b)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	if org != "" {
		req.Header.Set("X-Org-ID", org)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	res := response{code: rec.Code, raw: rec.Body.String()}
	if strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
		if err := json.Unmarshal(rec.Body.Bytes(), &res.body); err != nil {
			t.Fatalf("%s %s: non-JSON body %q", method, path, res.raw)
		}
	}
	return res
}

func (r response) str(key string) string {
	v, _ := r.body[key].(string)
	return v
}

func (r response) num(key string) float64 {
	v, _ := r.body[key].(float64)
	return v
}

func (r response) errCode() string {
	e, _ := r.body["error"].(map[string]any)
	code, _ := e["code"].(string)
	return code
}

func TestCRUDOverHTTP(t *testing.T) {
	h := server(notes())

	created := call(t, h, "org1", "POST", "/notes", map[string]any{"title": "first", "stars": 3})
	if created.code != 201 {
		t.Fatalf("create: %d %s", created.code, created.raw)
	}
	id := created.str("id")
	if id == "" || created.str("orgId") != "org1" || created.num("version") != 1 {
		t.Fatalf("create body: %s", created.raw)
	}

	got := call(t, h, "org1", "GET", "/notes/"+id, nil)
	if got.code != 200 || got.str("title") != "first" {
		t.Fatalf("get: %d %s", got.code, got.raw)
	}

	list := call(t, h, "org1", "GET", "/notes", nil)
	items, _ := list.body["items"].([]any)
	if list.code != 200 || len(items) != 1 {
		t.Fatalf("list: %d %s", list.code, list.raw)
	}

	put := call(t, h, "org1", "PUT", "/notes/"+id, map[string]any{"title": "replaced", "version": 1})
	if put.code != 200 || put.str("title") != "replaced" || put.num("stars") != 0 || put.num("version") != 2 {
		t.Fatalf("put: %d %s", put.code, put.raw)
	}
	if put.str("id") != id || put.str("orgId") != "org1" {
		t.Fatalf("put must keep framework fields: %s", put.raw)
	}

	patch := call(t, h, "org1", "PATCH", "/notes/"+id, map[string]any{"stars": 5})
	if patch.code != 200 || patch.str("title") != "replaced" || patch.num("stars") != 5 || patch.num("version") != 3 {
		t.Fatalf("patch: %d %s", patch.code, patch.raw)
	}

	del := call(t, h, "org1", "DELETE", "/notes/"+id, nil)
	if del.code != 204 || del.raw != "" {
		t.Fatalf("delete: %d %q", del.code, del.raw)
	}
	if again := call(t, h, "org1", "GET", "/notes/"+id, nil); again.code != 404 || again.errCode() != "not_found" {
		t.Fatalf("get after delete: %d %s", again.code, again.raw)
	}
	if again := call(t, h, "org1", "DELETE", "/notes/"+id, nil); again.code != 404 {
		t.Fatalf("second delete: %d %s", again.code, again.raw)
	}
	if list := call(t, h, "org1", "GET", "/notes", nil); len(list.body["items"].([]any)) != 0 {
		t.Fatalf("list after delete: %s", list.raw)
	}
}

func TestValidationErrors(t *testing.T) {
	h := server(notes())

	res := call(t, h, "org1", "POST", "/notes", map[string]any{"title": strings.Repeat("x", 21)})
	if res.code != 422 || res.errCode() != "invalid" {
		t.Fatalf("too long: %d %s", res.code, res.raw)
	}
	e := res.body["error"].(map[string]any)
	fields := e["fields"].(map[string]any)
	if _, ok := fields["title"]; !ok {
		t.Fatalf("missing title in fields: %s", res.raw)
	}

	res = call(t, h, "org1", "POST", "/notes", map[string]any{"title": "ok", "nope": 1})
	if res.code != 422 || !strings.Contains(res.raw, "nope") {
		t.Fatalf("unknown field: %d %s", res.code, res.raw)
	}

	res = call(t, h, "org1", "POST", "/notes", "{not json")
	if res.code != 422 {
		t.Fatalf("malformed: %d %s", res.code, res.raw)
	}

	res = call(t, h, "org1", "GET", "/notes?limit=abc", nil)
	if res.code != 422 {
		t.Fatalf("bad limit: %d %s", res.code, res.raw)
	}

	created := call(t, h, "org1", "POST", "/notes", map[string]any{"title": "ok"})
	res = call(t, h, "org1", "PATCH", "/notes/"+created.str("id"), map[string]any{"title": ""})
	if res.code != 422 {
		t.Fatalf("patch must re-validate the merged record: %d %s", res.code, res.raw)
	}
}

func TestOptimisticConcurrency(t *testing.T) {
	h := server(notes())
	created := call(t, h, "org1", "POST", "/notes", map[string]any{"title": "v1"})
	id := created.str("id")

	stale := call(t, h, "org1", "PUT", "/notes/"+id, map[string]any{"title": "v2", "version": 7})
	if stale.code != 409 || stale.errCode() != "conflict" {
		t.Fatalf("stale put: %d %s", stale.code, stale.raw)
	}
	stale = call(t, h, "org1", "PATCH", "/notes/"+id, map[string]any{"title": "v2", "version": 7})
	if stale.code != 409 {
		t.Fatalf("stale patch: %d %s", stale.code, stale.raw)
	}
	ok := call(t, h, "org1", "PUT", "/notes/"+id, map[string]any{"title": "v2"})
	if ok.code != 200 || ok.num("version") != 2 {
		t.Fatalf("put without version should skip the check: %d %s", ok.code, ok.raw)
	}

	loose := server(notes(func(b Builder[Note, *Note]) Builder[Note, *Note] { return b.NoVersion() }))
	created = call(t, loose, "org1", "POST", "/notes", map[string]any{"title": "v1"})
	if res := call(t, loose, "org1", "PUT", "/notes/"+created.str("id"), map[string]any{"title": "v2", "version": 7}); res.code != 200 {
		t.Fatalf("NoVersion put: %d %s", res.code, res.raw)
	}
}

func TestTenantIsolation(t *testing.T) {
	h := server(notes())
	created := call(t, h, "org1", "POST", "/notes", map[string]any{"title": "mine"})
	id := created.str("id")

	if res := call(t, h, "org2", "GET", "/notes/"+id, nil); res.code != 404 {
		t.Fatalf("other org must see 404, got %d %s", res.code, res.raw)
	}
	if res := call(t, h, "org2", "GET", "/notes", nil); len(res.body["items"].([]any)) != 0 {
		t.Fatalf("other org must list nothing: %s", res.raw)
	}
	if res := call(t, h, "org2", "DELETE", "/notes/"+id, nil); res.code != 404 {
		t.Fatalf("other org must not delete: %d", res.code)
	}
	if res := call(t, h, "", "GET", "/notes", nil); res.code != 401 || res.errCode() != "unauthenticated" {
		t.Fatalf("no tenant: %d %s", res.code, res.raw)
	}
	if res := call(t, h, "", "GET", "/healthz", nil); res.code != 200 {
		t.Fatalf("healthz needs no tenant: %d", res.code)
	}
}

func TestNoTenant(t *testing.T) {
	h := server(notes(func(b Builder[Note, *Note]) Builder[Note, *Note] { return b.NoTenant() }))
	created := call(t, h, "", "POST", "/notes", map[string]any{"title": "global"})
	if created.code != 201 || created.str("orgId") != "" {
		t.Fatalf("create: %d %s", created.code, created.raw)
	}
	if res := call(t, h, "org9", "GET", "/notes/"+created.str("id"), nil); res.code != 200 {
		t.Fatalf("global record visible to all: %d %s", res.code, res.raw)
	}
}

func TestSoftDeleteVersusHardDelete(t *testing.T) {
	ctx := tenant.With(context.Background(), "org1")

	soft := notes()
	n, err := soft.Create(ctx, Note{Title: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if err := soft.Delete(ctx, n.ID); err != nil {
		t.Fatal(err)
	}
	raw, ok, _ := soft.store.Get(ctx, "org1", n.ID)
	if !ok || raw.DeletedAt == nil {
		t.Fatalf("soft delete should keep the row with deletedAt set: %+v %v", raw, ok)
	}

	hard := notes(func(b Builder[Note, *Note]) Builder[Note, *Note] { return b.NoSoftDelete() })
	n, _ = hard.Create(ctx, Note{Title: "x"})
	if err := hard.Delete(ctx, n.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := hard.store.Get(ctx, "org1", n.ID); ok {
		t.Fatal("hard delete should remove the row")
	}
}

func TestPagination(t *testing.T) {
	ctx := tenant.With(context.Background(), "org1")
	r := notes()
	for i := 0; i < 5; i++ {
		if _, err := r.Create(ctx, Note{Title: fmt.Sprint("n", i)}); err != nil {
			t.Fatal(err)
		}
	}
	var seen []string
	cursor := ""
	for pages := 0; ; pages++ {
		p, err := r.List(ctx, ListParams{Limit: 2, Cursor: cursor})
		if err != nil {
			t.Fatal(err)
		}
		for _, n := range p.Items {
			seen = append(seen, n.Title)
		}
		if p.NextCursor == "" {
			if pages != 2 || len(p.Items) != 1 {
				t.Fatalf("expected 3 pages of 2,2,1; last page had %d on page %d", len(p.Items), pages)
			}
			break
		}
		cursor = p.NextCursor
	}
	if strings.Join(seen, ",") != "n0,n1,n2,n3,n4" {
		t.Fatalf("order: %v", seen)
	}

	if p, _ := r.List(ctx, ListParams{Limit: 10, Cursor: "nope"}); len(p.Items) != 0 || p.NextCursor != "" {
		t.Fatalf("unknown cursor should yield an empty page: %+v", p)
	}
	if p, _ := r.List(ctx, ListParams{Limit: MaxLimit + 1}); len(p.Items) != 5 {
		t.Fatalf("limit above max should be capped, not rejected: %d", len(p.Items))
	}
}

func TestCachingServesAndInvalidates(t *testing.T) {
	ctx := tenant.With(context.Background(), "org1")
	r := notes(func(b Builder[Note, *Note]) Builder[Note, *Note] {
		return b.Caching(cache.Enabled(), cache.TTL(time.Hour))
	})
	n, _ := r.Create(ctx, Note{Title: "cached"})

	if _, err := r.Get(ctx, n.ID); err != nil {
		t.Fatal(err)
	}
	// Change the store behind the resource's back: a cached read must not see it.
	behind := n
	behind.Title = "changed behind cache"
	_ = r.store.Put(ctx, "org1", n.ID, behind)
	if got, _ := r.Get(ctx, n.ID); got.Title != "cached" {
		t.Fatalf("expected cache hit, got %q", got.Title)
	}

	// A write through the resource invalidates.
	if _, err := r.Patch(ctx, n.ID, func(v *Note) error { v.Title = "patched"; return nil }); err != nil {
		t.Fatal(err)
	}
	if got, _ := r.Get(ctx, n.ID); got.Title != "patched" {
		t.Fatalf("expected fresh read after write, got %q", got.Title)
	}
	if err := r.Delete(ctx, n.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Get(ctx, n.ID); jig.StatusOf(err) != 404 {
		t.Fatalf("deleted record served from cache: %v", err)
	}
}

func TestPlainErrorFromValidateIsInvalid(t *testing.T) {
	ctx := tenant.With(context.Background(), "org1")
	r := Of[Note]("notes").
		Store(store.Memory[Note]()).
		Validate(func(Note) error { return fmt.Errorf("nope") }).
		Build()
	_, err := r.Create(ctx, Note{})
	if jig.StatusOf(err) != 422 || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("got %v", err)
	}
}

func TestBuilderIsValue(t *testing.T) {
	base := Of[Note]("notes").Store(store.Memory[Note]())
	a := base.NoTenant().Build()
	b := base.Build()
	if !a.Options().NoTenant || b.Options().NoTenant {
		t.Fatalf("builder shared state: a=%+v b=%+v", a.Options(), b.Options())
	}
}

func TestBuildPanics(t *testing.T) {
	cases := map[string]func(){
		"no name":      func() { Of[Note]("").Store(store.Memory[Note]()).Build() },
		"no store":     func() { Of[Note]("notes").Build() },
		"cache no ttl": func() { Of[Note]("notes").Store(store.Memory[Note]()).Caching(cache.Enabled()).Build() },
	}
	for name, fn := range cases {
		t.Run(name, func(t *testing.T) {
			defer func() {
				msg, _ := recover().(string)
				if !strings.HasPrefix(msg, "resource: ") {
					t.Fatalf("expected resource panic, got %q", msg)
				}
			}()
			fn()
		})
	}
}
