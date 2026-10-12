package auth

import (
	"context"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	if _, ok := From(context.Background()); ok {
		t.Fatal("empty context should carry no caller")
	}
	in := Caller{Subject: "u_1", Issuer: "iss", Attributes: map[string]any{"sub": "u_1", "org": "acme"}}
	got, ok := From(With(context.Background(), in))
	if !ok || got.Subject != "u_1" || got.Issuer != "iss" {
		t.Fatalf("got %+v, %v", got, ok)
	}
	if v, ok := got.Attribute("org"); !ok || v != "acme" {
		t.Fatalf("attribute: got %v, %v", v, ok)
	}
	if _, ok := got.Attribute("missing"); ok {
		t.Fatal("missing attribute reported present")
	}
	if _, ok := (Caller{}).Attribute("sub"); ok {
		t.Fatal("nil attributes should report absent, not panic")
	}
}
