package jwt

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// jwksServer serves a mutable key set and counts fetches.
type jwksServer struct {
	*httptest.Server
	mu      sync.Mutex
	keys    []rawJWK
	status  int
	fetches atomic.Int64
}

func newJWKSServer(t *testing.T, keys ...rawJWK) *jwksServer {
	t.Helper()
	s := &jwksServer{keys: keys, status: http.StatusOK}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.fetches.Add(1)
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.status != http.StatusOK {
			w.WriteHeader(s.status)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": s.keys})
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *jwksServer) set(keys ...rawJWK) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.keys = keys
}

func (s *jwksServer) setStatus(code int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = code
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

// toJWK renders a public key as its JWK, the way an issuer publishes it.
func toJWK(kid string, pub any) rawJWK {
	switch k := pub.(type) {
	case *rsa.PublicKey:
		e := []byte{byte(k.E >> 16), byte(k.E >> 8), byte(k.E)}
		return rawJWK{Kty: "RSA", Kid: kid, Use: "sig", Alg: "RS256", N: b64(k.N.Bytes()), E: b64(e)}
	case *ecdsa.PublicKey:
		size := (k.Curve.Params().BitSize + 7) / 8
		point, _ := k.Bytes() // 0x04 || X || Y
		return rawJWK{Kty: "EC", Kid: kid, Crv: k.Curve.Params().Name,
			X: b64(point[1 : 1+size]), Y: b64(point[1+size:])}
	case ed25519.PublicKey:
		return rawJWK{Kty: "OKP", Kid: kid, Crv: "Ed25519", X: b64(k)}
	}
	panic("toJWK: unsupported key")
}

// withClock pins the verifier and its key set to a controllable clock.
func withClock(v *Verifier, now *time.Time) {
	f := func() time.Time { return *now }
	v.now = f
	v.jwks.now = f
	v.parser = v.newParser()
}

func TestJWKSKeyKinds(t *testing.T) {
	rsaKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	p256, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	p384, _ := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	edPub, edPriv, _ := ed25519.GenerateKey(rand.Reader)
	srv := newJWKSServer(t,
		toJWK("rsa", &rsaKey.PublicKey),
		toJWK("p256", &p256.PublicKey),
		toJWK("p384", &p384.PublicKey),
		toJWK("ed", edPub),
		// Keys an issuer may publish that jig must ignore.
		rawJWK{Kty: "RSA", Kid: "enc", Use: "enc", N: "AQAB", E: "AQAB"},
		rawJWK{Kty: "RSA", Kid: "ps", Alg: "PS256", N: "AQAB", E: "AQAB"},
		rawJWK{Kty: "EC", Kid: "secp", Crv: "secp256k1", X: "AA", Y: "AA"},
		rawJWK{Kty: "OKP", Kid: "x25519", Crv: "X25519", X: "AA"},
		rawJWK{Kty: "oct", Kid: "sym"},
	)
	v := Bearer(JWKS(srv.URL))
	for kid, priv := range map[string]any{"rsa": rsaKey, "p256": p256, "p384": p384, "ed": edPriv} {
		c, err := v.Verify(context.Background(), mint(t, priv, kid, nil))
		if err != nil || c.Subject != "user_1" {
			t.Errorf("%s: %+v, %v", kid, c, err)
		}
	}
	if n := srv.fetches.Load(); n != 1 {
		t.Fatalf("keys should be cached after one fetch, got %d fetches", n)
	}
	// A token without a kid still verifies: all keys for its algorithm are tried.
	if _, err := v.Verify(context.Background(), mint(t, p256, "", nil)); err != nil {
		t.Fatalf("no kid: %v", err)
	}
	// HS256 is never accepted from a JWKS verifier, even if the secret were known.
	if _, err := v.Verify(context.Background(), mint(t, secret, "", nil)); err == nil {
		t.Fatal("HS256 verified against JWKS")
	}
	// A kid that exists but holds a different algorithm's key does not match.
	if _, err := v.Verify(context.Background(), mint(t, rsaKey, "p256", nil)); err == nil {
		t.Fatal("RS256 token verified with an EC kid")
	}
}

func TestJWKSRotation(t *testing.T) {
	old, _ := rsa.GenerateKey(rand.Reader, 2048)
	next, _ := rsa.GenerateKey(rand.Reader, 2048)
	srv := newJWKSServer(t, toJWK("old", &old.PublicKey))
	now := time.Now()
	v := Bearer(JWKS(srv.URL))
	withClock(v, &now)

	if _, err := v.Verify(context.Background(), mint(t, old, "old", nil)); err != nil {
		t.Fatal(err)
	}

	// The issuer rotates. The first token with the new kid triggers a refetch.
	srv.set(toJWK("old", &old.PublicKey), toJWK("next", &next.PublicKey))
	now = now.Add(2 * DefaultJWKSCooldown)
	if _, err := v.Verify(context.Background(), mint(t, next, "next", nil)); err != nil {
		t.Fatalf("after rotation: %v", err)
	}
	if n := srv.fetches.Load(); n != 2 {
		t.Fatalf("unknown kid should trigger exactly one refetch, got %d", n)
	}

	// Unknown kids inside the cooldown do not hit the issuer again.
	for range 5 {
		if _, err := v.Verify(context.Background(), mint(t, next, "unknown", nil)); err == nil {
			t.Fatal("unknown kid verified")
		}
	}
	if n := srv.fetches.Load(); n != 2 {
		t.Fatalf("cooldown should suppress refetches, got %d", n)
	}
	now = now.Add(DefaultJWKSCooldown)
	_, _ = v.Verify(context.Background(), mint(t, next, "unknown", nil))
	if n := srv.fetches.Load(); n != 3 {
		t.Fatalf("after cooldown the next unknown kid should refetch, got %d", n)
	}

	// Once the old key is withdrawn and the cache has aged out, old tokens fail.
	srv.set(toJWK("next", &next.PublicKey))
	if _, err := v.Verify(context.Background(), mint(t, old, "old", nil)); err != nil {
		t.Fatalf("withdrawn key should still be served from cache until refresh: %v", err)
	}
	now = now.Add(DefaultJWKSRefresh + time.Second)
	if _, err := v.Verify(context.Background(), mint(t, old, "old", nil)); err == nil {
		t.Fatal("withdrawn key verified after refresh")
	}
}

func TestJWKSOutage(t *testing.T) {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	srv := newJWKSServer(t, toJWK("k1", &key.PublicKey))
	now := time.Now()
	v := Bearer(JWKS(srv.URL, Refresh(time.Minute), Cooldown(time.Second)))
	withClock(v, &now)

	if _, err := v.Verify(context.Background(), mint(t, key, "k1", nil)); err != nil {
		t.Fatal(err)
	}
	srv.setStatus(http.StatusBadGateway)
	now = now.Add(2 * time.Minute)
	if _, err := v.Verify(context.Background(), mint(t, key, "k1", nil)); err != nil {
		t.Fatalf("a failed refresh should keep serving cached keys: %v", err)
	}
	if _, err := v.Verify(context.Background(), mint(t, key, "k2", nil)); err == nil || !strings.Contains(err.Error(), "502") {
		t.Fatalf("a miss during an outage should report the fetch error: %v", err)
	}

	// With nothing cached, the outage is fatal to the request.
	cold := Bearer(JWKS(srv.URL))
	if _, err := cold.Verify(context.Background(), mint(t, key, "k1", nil)); err == nil {
		t.Fatal("cold cache with failing issuer should error")
	}
}

func TestJWKSWithStaticKeys(t *testing.T) {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	srv := newJWKSServer(t, toJWK("k1", &key.PublicKey))
	srv.setStatus(http.StatusInternalServerError)
	v := Bearer(HMAC(secret), JWKS(srv.URL))
	// A static key verifies without the issuer being consulted at all.
	if _, err := v.Verify(context.Background(), mint(t, secret, "", nil)); err != nil {
		t.Fatal(err)
	}
	if srv.fetches.Load() != 0 {
		t.Fatal("HS256 token should not trigger a JWKS fetch")
	}
}

func TestParseJWKSErrors(t *testing.T) {
	cases := map[string]string{
		"not json":        `{"keys": [`,
		"no keys":         `{"keys": []}`,
		"only unusable":   `{"keys": [{"kty":"oct"}]}`,
		"bad rsa n":       `{"keys": [{"kty":"RSA","n":"!!","e":"AQAB"}]}`,
		"bad rsa e":       `{"keys": [{"kty":"RSA","n":"AQAB","e":"AQ"}]}`,
		"ec alg mismatch": `{"keys": [{"kty":"EC","crv":"P-256","alg":"ES384","x":"AA","y":"AA"}]}`,
		"ec off curve":    `{"keys": [{"kty":"EC","crv":"P-256","x":"AQ","y":"AQ"}]}`,
		"ec too long":     `{"keys": [{"kty":"EC","crv":"P-256","x":"` + b64(make([]byte, 33)) + `","y":"AQ"}]}`,
		"ed wrong size":   `{"keys": [{"kty":"OKP","crv":"Ed25519","x":"AQ"}]}`,
		"ed alg mismatch": `{"keys": [{"kty":"OKP","crv":"Ed25519","alg":"ES256","x":"` + b64(make([]byte, 32)) + `"}]}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := parseJWKS([]byte(body)); err == nil {
				t.Fatalf("expected error for %s", body)
			}
		})
	}
}

func TestJWKSBodyLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"keys":[{"kty":"RSA","kid":"` + strings.Repeat("x", maxJWKSBytes) + `"}]}`))
	}))
	defer srv.Close()
	v := Bearer(JWKS(srv.URL))
	_, err := v.Verify(context.Background(), mint(t, secret, "", nil))
	if err == nil {
		t.Fatal("expected error")
	}
	// The HS256 token is rejected before any fetch; use an RSA one to reach it.
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	_, err = v.Verify(context.Background(), mint(t, key, "k", nil))
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized key set: %v", err)
	}
}
