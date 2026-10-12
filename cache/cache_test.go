package cache

import (
	"testing"
	"time"
)

func TestApplyIsOrderedAndCopies(t *testing.T) {
	base := Policy{}
	p := base.Apply(Enabled(), TTL(time.Minute), TTL(15*time.Minute))
	if !p.Enabled || p.TTL != 15*time.Minute {
		t.Errorf("got %+v", p)
	}
	if base != (Policy{}) {
		t.Errorf("base mutated: %+v", base)
	}
	if q := p.Apply(Disabled()); q.Enabled || q.TTL != 15*time.Minute {
		t.Errorf("Disabled should only flip Enabled: %+v", q)
	}
}

func TestValidate(t *testing.T) {
	cases := map[string]struct {
		p    Policy
		want bool // valid?
	}{
		"zero value":          {Policy{}, true},
		"enabled with ttl":    {Policy{Enabled: true, TTL: time.Minute}, true},
		"ttl while disabled":  {Policy{TTL: time.Minute}, true},
		"enabled without ttl": {Policy{Enabled: true}, false},
		"negative ttl":        {Policy{TTL: -time.Second}, false},
	}
	for name, c := range cases {
		if err := c.p.Validate(); (err == nil) != c.want {
			t.Errorf("%s: valid=%v, err=%v", name, err == nil, err)
		}
	}
}

func TestCacheNilIsSafeAndNeverHits(t *testing.T) {
	var c *Cache[int] = New[int](Policy{})
	if c != nil {
		t.Fatal("disabled policy should produce a nil cache")
	}
	c.Set("k", 1)
	c.Delete("k")
	if _, ok := c.Get("k"); ok {
		t.Fatal("nil cache hit")
	}
}

func TestCacheExpires(t *testing.T) {
	now := time.Unix(1000, 0)
	c := New[string](Policy{Enabled: true, TTL: time.Minute})
	c.now = func() time.Time { return now }

	c.Set("k", "v")
	if v, ok := c.Get("k"); !ok || v != "v" {
		t.Fatalf("miss right after set: %q %v", v, ok)
	}
	now = now.Add(59 * time.Second)
	if _, ok := c.Get("k"); !ok {
		t.Fatal("expired early")
	}
	now = now.Add(time.Second)
	if _, ok := c.Get("k"); ok {
		t.Fatal("served after ttl")
	}
	c.Set("k", "v2")
	c.Delete("k")
	if _, ok := c.Get("k"); ok {
		t.Fatal("served after delete")
	}
}
