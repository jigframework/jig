package db

import (
	"context"
	"os"
	"testing"
	"time"
)

// open connects to the database named by JIG_TEST_DATABASE_URL, or skips the
// test when it is unset. Run one locally with:
//
//	docker run --rm -e POSTGRES_PASSWORD=jig -p 5433:5432 postgres:17
//	JIG_TEST_DATABASE_URL=postgres://postgres:jig@localhost:5433/postgres go test ./db
func open(t *testing.T) (context.Context, *testPool) {
	t.Helper()
	dsn := os.Getenv("JIG_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("JIG_TEST_DATABASE_URL not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	pool, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return ctx, &testPool{pool}
}

func TestPostgresRoundTrip(t *testing.T) {
	ctx, pool := open(t)
	table := pool.fresh(t, ctx, "roundtrip")
	s := Postgres[string](pool.Pool, table)

	for _, id := range []string{"a", "b", "c"} {
		if err := s.Put(ctx, "org1", id, "v-"+id); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Put(ctx, "org1", "b", "v-b2"); err != nil {
		t.Fatal(err)
	}

	got, err := s.List(ctx, "org1")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"v-a", "v-b2", "v-c"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("list[%d] = %q, want %q (replace must keep position)", i, got[i], want[i])
		}
	}

	if v, ok, err := s.Get(ctx, "org1", "b"); err != nil || !ok || v != "v-b2" {
		t.Errorf("get b: %q, %v, %v", v, ok, err)
	}
	if _, ok, err := s.Get(ctx, "org1", "zzz"); err != nil || ok {
		t.Errorf("get of missing id: ok=%v err=%v", ok, err)
	}

	if err := s.Delete(ctx, "org1", "b"); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, "org1", "b"); err != nil {
		t.Errorf("second delete should be a no-op, got %v", err)
	}
	got, _ = s.List(ctx, "org1")
	if len(got) != 2 || got[0] != "v-a" || got[1] != "v-c" {
		t.Errorf("after delete: %v", got)
	}
}

func TestPostgresIsolatesTenants(t *testing.T) {
	ctx, pool := open(t)
	table := pool.fresh(t, ctx, "tenants")
	s := Postgres[int](pool.Pool, table)
	if err := s.Put(ctx, "org1", "x", 1); err != nil {
		t.Fatal(err)
	}
	if err := s.Put(ctx, "org2", "x", 2); err != nil {
		t.Fatal(err)
	}

	if v, ok, _ := s.Get(ctx, "org1", "x"); !ok || v != 1 {
		t.Errorf("org1: %d %v", v, ok)
	}
	if v, ok, _ := s.Get(ctx, "org2", "x"); !ok || v != 2 {
		t.Errorf("org2: %d %v", v, ok)
	}
	if got, _ := s.List(ctx, "org3"); len(got) != 0 {
		t.Errorf("unknown org should list nothing, got %v", got)
	}
}

func TestPostgresStoresStructs(t *testing.T) {
	ctx, pool := open(t)
	table := pool.fresh(t, ctx, "structs")
	type rec struct {
		Title string     `json:"title"`
		Done  bool       `json:"done"`
		DueAt *time.Time `json:"dueAt,omitempty"`
	}
	s := Postgres[rec](pool.Pool, table)
	due := time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)
	if err := s.Put(ctx, "org1", "r", rec{Title: "ship it", Done: true, DueAt: &due}); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.Get(ctx, "org1", "r")
	if err != nil || !ok {
		t.Fatalf("get: ok=%v err=%v", ok, err)
	}
	if got.Title != "ship it" || !got.Done || got.DueAt == nil || !got.DueAt.Equal(due) {
		t.Errorf("round trip changed the record: %+v", got)
	}
}

func TestMigrateQuotesTableNames(t *testing.T) {
	ctx, pool := open(t)
	// A name that needs quoting must still work, and must not be injectable.
	table := pool.fresh(t, ctx, `odd"name`)
	s := Postgres[int](pool.Pool, table)
	if err := s.Put(ctx, "org1", "x", 1); err != nil {
		t.Fatal(err)
	}
}
