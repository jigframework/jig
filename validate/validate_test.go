package validate

import (
	"errors"
	"testing"

	"github.com/jigframework/jig"
)

func TestAllCollectsFailuresByField(t *testing.T) {
	err := All(
		Required("title", "  "),
		MaxLen("title", "abcdef", 3),
		Required("owner", "me"),
		Range("priority", 7, 1, 5),
		OneOf("status", "weird", "open", "done"),
		MinLen("code", "ab", 3),
	)
	var e *jig.Error
	if !errors.As(err, &e) || e.Status != 422 {
		t.Fatalf("expected jig.Invalid, got %v", err)
	}
	title, _ := e.Fields["title"].([]string)
	if len(title) != 2 {
		t.Errorf("title should have two messages: %v", e.Fields["title"])
	}
	if _, ok := e.Fields["owner"]; ok {
		t.Error("owner passed and should be absent")
	}
	for _, f := range []string{"priority", "status", "code"} {
		if _, ok := e.Fields[f]; !ok {
			t.Errorf("missing field %q in %v", f, e.Fields)
		}
	}
}

func TestAllNilWhenClean(t *testing.T) {
	if err := All(Required("a", "x"), nil, MaxLen("a", "x", 1)); err != nil {
		t.Fatalf("got %v", err)
	}
}

func TestAllPassesThroughOtherErrors(t *testing.T) {
	boom := errors.New("boom")
	if err := All(Required("a", ""), boom); !errors.Is(err, boom) {
		t.Fatalf("got %v, want boom", err)
	}
}

func TestMaxLenCountsRunes(t *testing.T) {
	if err := MaxLen("s", "héllo", 5); err != nil {
		t.Errorf("5 runes should fit in 5: %v", err)
	}
}
