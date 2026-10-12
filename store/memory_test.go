package store

import (
	"context"
	"testing"
)

func TestMemoryRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := Memory[string]()

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

	if v, ok, _ := s.Get(ctx, "org1", "b"); !ok || v != "v-b2" {
		t.Errorf("get b: %q, %v", v, ok)
	}
	if _, ok, _ := s.Get(ctx, "org1", "zzz"); ok {
		t.Error("get of missing id reported ok")
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

func TestMemoryIsolatesTenants(t *testing.T) {
	ctx := context.Background()
	s := Memory[int]()
	_ = s.Put(ctx, "org1", "x", 1)
	_ = s.Put(ctx, "org2", "x", 2)

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
