package tenant

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/jigframework/jig/auth"
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

func TestFromJWTAttribute(t *testing.T) {
	resolve := FromJWTAttribute("org")
	cases := []struct {
		name  string
		attrs map[string]any // nil means no caller on the context
		want  ID
		err   bool
	}{
		{"no caller", nil, "", false},
		{"missing attribute", map[string]any{"sub": "u"}, "", false},
		{"string", map[string]any{"org": "acme"}, "acme", false},
		{"json number", map[string]any{"org": json.Number("42")}, "42", false},
		{"float", map[string]any{"org": float64(42)}, "42", false},
		{"int", map[string]any{"org": 7}, "7", false},
		{"fraction", map[string]any{"org": 4.5}, "", true},
		{"fraction number", map[string]any{"org": json.Number("4.5")}, "", true},
		{"object", map[string]any{"org": map[string]any{"id": "x"}}, "", true},
		{"bool", map[string]any{"org": true}, "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/", nil)
			if tc.attrs != nil {
				r = r.WithContext(auth.With(r.Context(), auth.Caller{Attributes: tc.attrs}))
			}
			got, err := resolve(r)
			if (err != nil) != tc.err {
				t.Fatalf("err = %v, want error %v", err, tc.err)
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}
