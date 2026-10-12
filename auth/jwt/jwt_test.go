package jwt

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	jwtlib "github.com/golang-jwt/jwt/v5"
)

var secret = []byte("test-secret-that-is-long-enough")

// claims returns a valid claim set, with overrides applied.
func claims(over map[string]any) map[string]any {
	c := map[string]any{
		"sub": "user_1",
		"iss": "https://issuer.test",
		"aud": "todo-api",
		"exp": time.Now().Add(time.Hour).Unix(),
		"iat": time.Now().Unix(),
	}
	for k, v := range over {
		c[k] = v
	}
	return c
}

func mint(t *testing.T, key any, kid string, over map[string]any) string {
	t.Helper()
	s, err := Sign(key, kid, claims(over))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func request(token string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	return r
}

func TestBearerHMAC(t *testing.T) {
	v := Bearer(HMAC(secret), Issuer("https://issuer.test"), Audience("todo-api"))
	c, found, err := v.Authenticate(request(mint(t, secret, "", map[string]any{"org": "acme", "n": 7})))
	if err != nil || !found {
		t.Fatalf("got %v, %v", found, err)
	}
	if c.Subject != "user_1" || c.Issuer != "https://issuer.test" {
		t.Fatalf("caller: %+v", c)
	}
	if org, _ := c.Attribute("org"); org != "acme" {
		t.Fatalf("attribute org: %v", org)
	}
	if n, _ := c.Attribute("n"); n != json.Number("7") {
		t.Fatalf("numbers should decode as json.Number, got %T %v", n, n)
	}
}

func TestAuthenticateHeaderShapes(t *testing.T) {
	v := Bearer(HMAC(secret))
	ok := mint(t, secret, "", nil)

	anonymous := []string{"", "Basic dXNlcjpwYXNz", "Token " + ok}
	for _, h := range anonymous {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		if h != "" {
			r.Header.Set("Authorization", h)
		}
		if _, found, err := v.Authenticate(r); found || err != nil {
			t.Errorf("%q: should be anonymous, got %v, %v", h, found, err)
		}
	}

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Authorization", "bearer "+ok)
	if _, found, err := v.Authenticate(r); !found || err != nil {
		t.Errorf("scheme should be case-insensitive: %v, %v", found, err)
	}

	bad := []string{"Bearer", "Bearer ", "Bearer    ", "Bearer not.a.token"}
	for _, h := range bad {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set("Authorization", h)
		if _, _, err := v.Authenticate(r); err == nil {
			t.Errorf("%q: should be an error", h)
		}
	}

	huge := ok + strings.Repeat("A", MaxTokenBytes)
	if _, _, err := v.Authenticate(request(huge)); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Errorf("oversized token: %v", err)
	}
}

func TestClaimValidation(t *testing.T) {
	v := Bearer(HMAC(secret), Issuer("https://issuer.test"), Audience("todo-api"), Audience("other-api"))
	past := time.Now().Add(-time.Hour).Unix()
	future := time.Now().Add(time.Hour).Unix()
	cases := []struct {
		name string
		over map[string]any
		want error
	}{
		{"expired", map[string]any{"exp": past}, jwtlib.ErrTokenExpired},
		{"not yet valid", map[string]any{"nbf": future}, jwtlib.ErrTokenNotValidYet},
		{"issued in future", map[string]any{"iat": future}, jwtlib.ErrTokenUsedBeforeIssued},
		{"wrong issuer", map[string]any{"iss": "https://evil.test"}, jwtlib.ErrTokenInvalidIssuer},
		{"missing issuer", map[string]any{"iss": nil}, jwtlib.ErrTokenRequiredClaimMissing},
		{"wrong audience", map[string]any{"aud": "someone-else"}, jwtlib.ErrTokenInvalidAudience},
		{"missing audience", map[string]any{"aud": nil}, jwtlib.ErrTokenRequiredClaimMissing},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := claims(tc.over)
			for k, val := range tc.over {
				if val == nil {
					delete(c, k)
				}
			}
			tok, err := Sign(secret, "", c)
			if err != nil {
				t.Fatal(err)
			}
			_, err = v.Verify(context.Background(), tok)
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}

	t.Run("second audience", func(t *testing.T) {
		if _, err := v.Verify(context.Background(), mint(t, secret, "", map[string]any{"aud": "other-api"})); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("audience list", func(t *testing.T) {
		if _, err := v.Verify(context.Background(), mint(t, secret, "", map[string]any{"aud": []string{"x", "todo-api"}})); err != nil {
			t.Fatal(err)
		}
	})
}

func TestExpiryIsRequired(t *testing.T) {
	v := Bearer(HMAC(secret))
	c := claims(nil)
	delete(c, "exp")
	tok, _ := Sign(secret, "", c)
	if _, err := v.Verify(context.Background(), tok); !errors.Is(err, jwtlib.ErrTokenRequiredClaimMissing) {
		t.Fatalf("token without exp should be rejected, got %v", err)
	}
}

func TestLeeway(t *testing.T) {
	justExpired := map[string]any{"exp": time.Now().Add(-30 * time.Second).Unix()}
	if _, err := Bearer(HMAC(secret)).Verify(context.Background(), mint(t, secret, "", justExpired)); err != nil {
		t.Fatalf("default leeway should tolerate 30s: %v", err)
	}
	if _, err := Bearer(HMAC(secret), Leeway(0)).Verify(context.Background(), mint(t, secret, "", justExpired)); !errors.Is(err, jwtlib.ErrTokenExpired) {
		t.Fatalf("zero leeway should reject: %v", err)
	}
}

func TestSignatureTampering(t *testing.T) {
	v := Bearer(HMAC(secret))
	tok := mint(t, secret, "", nil)
	parts := strings.Split(tok, ".")

	// Change the subject without re-signing.
	payload, _ := base64.RawURLEncoding.DecodeString(parts[1])
	payload = []byte(strings.Replace(string(payload), "user_1", "user_2", 1))
	parts[1] = base64.RawURLEncoding.EncodeToString(payload)
	if _, err := v.Verify(context.Background(), strings.Join(parts, ".")); !errors.Is(err, jwtlib.ErrTokenSignatureInvalid) {
		t.Fatalf("tampered payload: %v", err)
	}

	// Signed with a different secret.
	if _, err := v.Verify(context.Background(), mint(t, []byte("another-secret-entirely-ok"), "", nil)); !errors.Is(err, jwtlib.ErrTokenSignatureInvalid) {
		t.Fatalf("wrong secret: %v", err)
	}

	// Malformed segments.
	for _, bad := range []string{"a.b", "a.b.c.d", "!!!.!!!.!!!", parts[0] + "." + parts[1]} {
		if _, err := v.Verify(context.Background(), bad); err == nil {
			t.Errorf("%q should fail", bad)
		}
	}
}

// unsigned builds a token with "alg": "none" and an empty signature.
func unsigned(t *testing.T) string {
	t.Helper()
	header, _ := json.Marshal(map[string]any{"alg": "none", "typ": "JWT"})
	payload, _ := json.Marshal(claims(nil))
	return base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload) + "."
}

func TestAlgNoneRejected(t *testing.T) {
	for _, v := range []*Verifier{Bearer(HMAC(secret)), Bearer(JWKS("http://localhost/jwks"))} {
		if _, err := v.Verify(context.Background(), unsigned(t)); err == nil {
			t.Fatal("alg none must be rejected")
		}
	}
}

func TestKeyConfusion(t *testing.T) {
	rsaKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	v := Bearer(PublicKey(&rsaKey.PublicKey))

	// The classic attack: sign HS256 with the bytes of the public key the
	// server verifies RS256 with.
	pem, _ := x509.MarshalPKIXPublicKey(&rsaKey.PublicKey)
	tok := mint(t, pem, "", nil)
	if _, err := v.Verify(context.Background(), tok); err == nil {
		t.Fatal("HS256 token must not verify against an RSA public key")
	}
	if _, err := v.Verify(context.Background(), mint(t, secret, "", nil)); err == nil {
		t.Fatal("HS256 is not an allowed method when only asymmetric keys are configured")
	}
	if _, err := v.Verify(context.Background(), mint(t, rsaKey, "", nil)); err != nil {
		t.Fatalf("RS256 should verify: %v", err)
	}
}

func TestAsymmetricKeys(t *testing.T) {
	rsaKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	p256, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	p384, _ := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	p521, _ := ecdsa.GenerateKey(elliptic.P521(), rand.Reader)
	edPub, edPriv, _ := ed25519.GenerateKey(rand.Reader)

	cases := []struct {
		name    string
		private any
		public  any
		alg     string
	}{
		{"RS256", rsaKey, &rsaKey.PublicKey, "RS256"},
		{"ES256", p256, &p256.PublicKey, "ES256"},
		{"ES384", p384, &p384.PublicKey, "ES384"},
		{"ES512", p521, &p521.PublicKey, "ES512"},
		{"EdDSA", edPriv, edPub, "EdDSA"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := Bearer(PublicKey(tc.public))
			if got := v.methods(); len(got) != 1 || got[0] != tc.alg {
				t.Fatalf("methods = %v, want [%s]", got, tc.alg)
			}
			c, err := v.Verify(context.Background(), mint(t, tc.private, "", nil))
			if err != nil || c.Subject != "user_1" {
				t.Fatalf("got %+v, %v", c, err)
			}
			// Every other key must fail against this verifier.
			for _, other := range cases {
				if other.name == tc.name {
					continue
				}
				if _, err := v.Verify(context.Background(), mint(t, other.private, "", nil)); err == nil {
					t.Errorf("%s token verified against %s key", other.name, tc.name)
				}
			}
		})
	}

	t.Run("several keys", func(t *testing.T) {
		v := Bearer(HMAC(secret), PublicKey(&rsaKey.PublicKey), PublicKey(edPub))
		for _, key := range []any{secret, rsaKey, edPriv} {
			if _, err := v.Verify(context.Background(), mint(t, key, "", nil)); err != nil {
				t.Errorf("%T: %v", key, err)
			}
		}
		if _, err := v.Verify(context.Background(), mint(t, p256, "", nil)); err == nil {
			t.Error("unconfigured key type verified")
		}
	})
}

func TestConfigurationPanics(t *testing.T) {
	expectPanic := func(name string, f func()) {
		t.Helper()
		defer func() {
			if recover() == nil {
				t.Errorf("%s: expected panic", name)
			}
		}()
		f()
	}
	expectPanic("no keys", func() { Bearer(Issuer("x")) })
	expectPanic("empty secret", func() { HMAC(nil) })
	expectPanic("unsupported key", func() { PublicKey("not a key") })
	expectPanic("unsupported curve", func() {
		k, _ := ecdsa.GenerateKey(elliptic.P224(), rand.Reader)
		PublicKey(&k.PublicKey)
	})
	expectPanic("plain http", func() { JWKS("http://issuer.example.com/jwks") })
	expectPanic("no scheme", func() { JWKS("issuer.example.com/jwks") })
	expectPanic("jwks twice", func() { Bearer(JWKS("https://a.test/jwks"), JWKS("https://b.test/jwks")) })

	for _, ok := range []string{"https://issuer.example.com/jwks", "http://localhost:8080/jwks", "http://127.0.0.1/jwks", "http://[::1]/jwks"} {
		if err := checkJWKSURL(ok); err != nil {
			t.Errorf("%s: %v", ok, err)
		}
	}
}

func TestSignRejectsUnknownKey(t *testing.T) {
	if _, err := Sign("string key", "", claims(nil)); err == nil {
		t.Fatal("expected error")
	}
}
