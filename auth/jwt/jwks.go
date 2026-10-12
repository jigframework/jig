package jwt

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	jwtlib "github.com/golang-jwt/jwt/v5"
)

// Defaults for JWKS fetching.
const (
	DefaultJWKSRefresh  = time.Hour   // keys older than this are fetched again on next use
	DefaultJWKSCooldown = time.Minute // minimum gap between fetches triggered by an unknown kid
	maxJWKSBytes        = 1 << 20
)

// jwksAlgorithms are the algorithms a JWKS key may verify. HS256 is never
// among them: a key set published over HTTP holds public keys only.
var jwksAlgorithms = []string{
	jwtlib.SigningMethodRS256.Alg(),
	jwtlib.SigningMethodES256.Alg(),
	jwtlib.SigningMethodES384.Alg(),
	jwtlib.SigningMethodES512.Alg(),
	jwtlib.SigningMethodEdDSA.Alg(),
}

// JWKSOption configures JWKS fetching.
type JWKSOption func(*keySet)

// HTTPClient sets the client used to fetch the key set. The default has a
// ten second timeout.
func HTTPClient(c *http.Client) JWKSOption { return func(s *keySet) { s.client = c } }

// Refresh sets how old the cached key set may be before it is fetched again
// on the next lookup. The default is DefaultJWKSRefresh.
func Refresh(d time.Duration) JWKSOption { return func(s *keySet) { s.refresh = d } }

// Cooldown sets the minimum gap between fetches triggered by a token whose
// kid is not in the cache, which bounds how often an attacker can make the
// application call the issuer. The default is DefaultJWKSCooldown.
func Cooldown(d time.Duration) JWKSOption { return func(s *keySet) { s.cooldown = d } }

// JWKS fetches verification keys from a JSON Web Key Set URL, as published
// by identity providers under /.well-known. Keys are fetched on first use,
// cached, refreshed when older than Refresh, and refetched once per Cooldown
// when a token names a kid that is not cached, which is how key rotation is
// picked up.
//
// The URL must use https. Plain http is allowed only for loopback hosts, for
// tests and local development. Any other URL is a programming error and
// panics.
func JWKS(rawURL string, opts ...JWKSOption) Option {
	if err := checkJWKSURL(rawURL); err != nil {
		panic("jwt: " + err.Error())
	}
	s := &keySet{
		url:      rawURL,
		client:   &http.Client{Timeout: 10 * time.Second},
		refresh:  DefaultJWKSRefresh,
		cooldown: DefaultJWKSCooldown,
		now:      time.Now,
	}
	for _, o := range opts {
		o(s)
	}
	return func(v *Verifier) {
		if v.jwks != nil {
			panic("jwt: JWKS given twice")
		}
		s.now = v.now
		v.jwks = s
	}
}

func checkJWKSURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("JWKS url: %w", err)
	}
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		host := u.Hostname()
		if host == "localhost" {
			return nil
		}
		if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
			return nil
		}
		return fmt.Errorf("JWKS url %q must use https; http is allowed for loopback hosts only", rawURL)
	default:
		return fmt.Errorf("JWKS url %q must use https", rawURL)
	}
}

// keySet is the cached contents of a JWKS endpoint.
type keySet struct {
	url      string
	client   *http.Client
	refresh  time.Duration
	cooldown time.Duration
	now      func() time.Time

	mu      sync.Mutex
	keys    []jwk
	fetched time.Time // when keys was last replaced
	tried   time.Time // when a fetch was last attempted, successful or not
	lastErr error     // why the last attempt failed, nil after a success
}

type jwk struct {
	kid string
	alg string
	key any
}

// lookup returns the cached keys for alg, narrowed to kid when the token
// names one. A miss on a named kid triggers a fetch, subject to the
// cooldown. The mutex is held across the fetch so concurrent misses share
// one request; fetches are rare enough that serialising them is fine.
func (s *keySet) lookup(ctx context.Context, kid, alg string) ([]any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()

	if s.keys == nil || now.Sub(s.fetched) > s.refresh {
		if err := s.fetchLocked(ctx); err != nil && s.keys == nil {
			return nil, err
		}
		// A failed refresh keeps serving the stale set: an issuer outage
		// should not log every user out before the keys have changed.
	}
	keys := s.matchLocked(kid, alg)
	if len(keys) == 0 && kid != "" && now.Sub(s.tried) >= s.cooldown {
		if err := s.fetchLocked(ctx); err != nil {
			return nil, err
		}
		keys = s.matchLocked(kid, alg)
	}
	if len(keys) == 0 {
		if s.lastErr != nil {
			return nil, fmt.Errorf("no JWKS key for kid %q and alg %s (last fetch failed: %w)", kid, alg, s.lastErr)
		}
		return nil, fmt.Errorf("no JWKS key for kid %q and alg %s", kid, alg)
	}
	return keys, nil
}

func (s *keySet) matchLocked(kid, alg string) []any {
	var out []any
	for _, k := range s.keys {
		if k.alg == alg && (kid == "" || k.kid == kid) {
			out = append(out, k.key)
		}
	}
	return out
}

func (s *keySet) fetchLocked(ctx context.Context) error {
	s.tried = s.now()
	s.lastErr = s.fetch(ctx)
	return s.lastErr
}

func (s *keySet) fetch(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.url, nil)
	if err != nil {
		return fmt.Errorf("jwks: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("jwks: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("jwks: %s returned %s", s.url, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxJWKSBytes+1))
	if err != nil {
		return fmt.Errorf("jwks: %w", err)
	}
	if len(body) > maxJWKSBytes {
		return fmt.Errorf("jwks: %s exceeds %d bytes", s.url, maxJWKSBytes)
	}
	keys, err := parseJWKS(body)
	if err != nil {
		return fmt.Errorf("jwks: %s: %w", s.url, err)
	}
	s.keys = keys
	s.fetched = s.tried
	return nil
}

// rawJWK is the subset of RFC 7517 a verifier needs.
type rawJWK struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Use string `json:"use"`
	Alg string `json:"alg"`
	N   string `json:"n"`
	E   string `json:"e"`
	Crv string `json:"crv"`
	X   string `json:"x"`
	Y   string `json:"y"`
}

// parseJWKS decodes a key set. Keys it cannot use are skipped rather than
// failing the whole set: an identity provider may publish encryption keys or
// algorithms jig does not support alongside the signing keys it needs. A
// set with no usable key is an error. A key that is malformed is also an
// error, since silently dropping it would turn a typo into a login outage.
func parseJWKS(data []byte) ([]jwk, error) {
	var set struct {
		Keys []rawJWK `json:"keys"`
	}
	if err := json.Unmarshal(data, &set); err != nil {
		return nil, fmt.Errorf("malformed key set: %w", err)
	}
	keys := make([]jwk, 0, len(set.Keys))
	for i, raw := range set.Keys {
		if raw.Use != "" && raw.Use != "sig" {
			continue
		}
		key, alg, err := parseJWK(raw)
		if errors.Is(err, errSkip) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("key %d: %w", i, err)
		}
		keys = append(keys, jwk{kid: raw.Kid, alg: alg, key: key})
	}
	if len(keys) == 0 {
		return nil, errors.New("no usable signing key")
	}
	return keys, nil
}

var errSkip = errors.New("skip")

func parseJWK(raw rawJWK) (any, string, error) {
	switch raw.Kty {
	case "RSA":
		if raw.Alg != "" && raw.Alg != jwtlib.SigningMethodRS256.Alg() {
			return nil, "", errSkip
		}
		n, err := decodeBigInt(raw.N)
		if err != nil {
			return nil, "", fmt.Errorf("n: %w", err)
		}
		e, err := decodeBigInt(raw.E)
		if err != nil {
			return nil, "", fmt.Errorf("e: %w", err)
		}
		if !e.IsInt64() || e.Int64() < 3 {
			return nil, "", errors.New("e: invalid exponent")
		}
		return &rsa.PublicKey{N: n, E: int(e.Int64())}, jwtlib.SigningMethodRS256.Alg(), nil
	case "EC":
		var curve elliptic.Curve
		switch raw.Crv {
		case "P-256":
			curve = elliptic.P256()
		case "P-384":
			curve = elliptic.P384()
		case "P-521":
			curve = elliptic.P521()
		default:
			return nil, "", errSkip
		}
		alg, _ := ecdsaAlg(curve)
		if raw.Alg != "" && raw.Alg != alg {
			return nil, "", fmt.Errorf("alg %s does not match curve %s", raw.Alg, raw.Crv)
		}
		x, err := decodeCoordinate(raw.X, curve)
		if err != nil {
			return nil, "", fmt.Errorf("x: %w", err)
		}
		y, err := decodeCoordinate(raw.Y, curve)
		if err != nil {
			return nil, "", fmt.Errorf("y: %w", err)
		}
		point := append(append([]byte{4}, x...), y...)
		key, err := ecdsa.ParseUncompressedPublicKey(curve, point)
		if err != nil {
			return nil, "", err
		}
		return key, alg, nil
	case "OKP":
		if raw.Crv != "Ed25519" {
			return nil, "", errSkip
		}
		if raw.Alg != "" && raw.Alg != jwtlib.SigningMethodEdDSA.Alg() {
			return nil, "", fmt.Errorf("alg %s does not match curve Ed25519", raw.Alg)
		}
		x, err := base64.RawURLEncoding.DecodeString(raw.X)
		if err != nil {
			return nil, "", fmt.Errorf("x: %w", err)
		}
		if len(x) != ed25519.PublicKeySize {
			return nil, "", fmt.Errorf("x: %d bytes, want %d", len(x), ed25519.PublicKeySize)
		}
		return ed25519.PublicKey(x), jwtlib.SigningMethodEdDSA.Alg(), nil
	default:
		return nil, "", errSkip
	}
}

func decodeBigInt(s string) (*big.Int, error) {
	if s == "" {
		return nil, errors.New("missing")
	}
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, err
	}
	return new(big.Int).SetBytes(b), nil
}

// decodeCoordinate decodes a base64url field-element and left-pads it to
// the curve's byte size, which issuers do not always do themselves.
func decodeCoordinate(s string, curve elliptic.Curve) ([]byte, error) {
	size := (curve.Params().BitSize + 7) / 8
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, err
	}
	if len(b) == 0 || len(b) > size {
		return nil, fmt.Errorf("%d bytes, want at most %d", len(b), size)
	}
	out := make([]byte, size)
	copy(out[size-len(b):], b)
	return out, nil
}
