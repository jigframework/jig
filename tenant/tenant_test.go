package tenant

import (
	"context"
	"errors"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	ctx := With(context.Background(), "org_123")
	id, err := Must(ctx)
	if err != nil || id != "org_123" {
		t.Fatalf("got %q, %v", id, err)
	}
}

func TestMissing(t *testing.T) {
	if _, err := Must(context.Background()); !errors.Is(err, ErrMissing) {
		t.Fatalf("got %v, want ErrMissing", err)
	}
	if _, err := Must(With(context.Background(), "")); !errors.Is(err, ErrMissing) {
		t.Fatalf("empty id should count as missing, got %v", err)
	}
}
