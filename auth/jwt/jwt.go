// Package jwt authenticates requests that carry a JSON Web Token as a bearer
// token. Bearer builds an auth.Authenticator for jig.WithAuth:
//
//	app := jig.New("todo",
//		jig.WithAuth(jwt.Bearer(
//			jwt.JWKS("https://issuer.example.com/.well-known/jwks.json"),
//			jwt.Issuer("https://issuer.example.com"),
//			jwt.Audience("todo-api"),
//		)),
//		jig.WithTenant(tenant.FromJWTAttribute("sub")),
//	)
//
// Every claim on a verified token becomes an attribute of the auth.Caller on
// the request context, so tenant resolvers and policy rules can read them.
//
// The rules that close the usual JWT holes are not configurable. The
// algorithms a token may use are derived from the keys the application
// configured, never from the token header, so "alg": "none" is always
// rejected and an HMAC secret can never verify a token that claims RS256. An
// expiry claim is required. Issued-at and not-before claims are checked when
// present, with a leeway of one minute by default. Tokens larger than
// MaxTokenBytes are rejected unread.
//
// Signature verification and claim validation are delegated to
// github.com/golang-jwt/jwt/v5. Key discovery over JWKS is implemented here.
package jwt

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rsa"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	jwtlib "github.com/golang-jwt/jwt/v5"

	"github.com/jigframework/jig/auth"
)

// MaxTokenBytes is the largest bearer token Verifier accepts.
const MaxTokenBytes = 8 << 10

// DefaultLeeway is the clock skew tolerated when checking time claims.
const DefaultLeeway = time.Minute

// Verifier authenticates bearer tokens. Build one with Bearer.
type Verifier struct {
	static   []staticKey
	jwks     *keySet
	issuer   string
	audience []string
	leeway   time.Duration
	now      func() time.Time
	parser   *jwtlib.Parser
}

type staticKey struct {
	alg string
	key any
}

// Option configures a Verifier.
type Option func(*Verifier)

// HMAC adds a shared secret. Tokens signed with HS256 verify against it. A
// shared secret suits a single service that issues its own tokens; an
// application that accepts tokens from an identity provider should use JWKS
// or PublicKey instead, so that the application cannot mint tokens.
func HMAC(secret []byte) Option {
	if len(secret) == 0 {
		panic("jwt: HMAC secret is empty")
	}
	return func(v *Verifier) {
		v.static = append(v.static, staticKey{alg: jwtlib.SigningMethodHS256.Alg(), key: secret})
	}
}

// PublicKey adds a verification key. The key's type selects the algorithm:
// *rsa.PublicKey verifies RS256, *ecdsa.PublicKey verifies ES256, ES384 or
// ES512 by its curve, and ed25519.PublicKey verifies EdDSA. Any other type
// panics.
func PublicKey(key crypto.PublicKey) Option {
	alg, err := algFor(key)
	if err != nil {
		panic("jwt: " + err.Error())
	}
	return func(v *Verifier) {
		v.static = append(v.static, staticKey{alg: alg, key: key})
	}
}

// Issuer requires the token's iss claim to equal iss.
func Issuer(iss string) Option { return func(v *Verifier) { v.issuer = iss } }

// Audience requires the token's aud claim to contain aud. It may be given
// more than once; a token must then match at least one.
func Audience(aud string) Option {
	return func(v *Verifier) { v.audience = append(v.audience, aud) }
}

// Leeway sets the clock skew tolerated when checking exp, nbf and iat. The
// default is DefaultLeeway.
func Leeway(d time.Duration) Option { return func(v *Verifier) { v.leeway = d } }

// Bearer returns a Verifier that reads the token from the Authorization
// header. At least one of HMAC, PublicKey or JWKS is required; configuring
// none is a programming error and panics.
func Bearer(opts ...Option) *Verifier {
	v := &Verifier{leeway: DefaultLeeway, now: time.Now}
	for _, o := range opts {
		o(v)
	}
	if len(v.static) == 0 && v.jwks == nil {
		panic("jwt: Bearer has no keys; pass HMAC, PublicKey or JWKS")
	}
	v.parser = v.newParser()
	return v
}

func (v *Verifier) newParser() *jwtlib.Parser {
	opts := []jwtlib.ParserOption{
		jwtlib.WithValidMethods(v.methods()),
		jwtlib.WithJSONNumber(),
		jwtlib.WithStrictDecoding(),
		jwtlib.WithExpirationRequired(),
		jwtlib.WithIssuedAt(),
		jwtlib.WithLeeway(v.leeway),
		jwtlib.WithTimeFunc(v.now),
	}
	if v.issuer != "" {
		opts = append(opts, jwtlib.WithIssuer(v.issuer))
	}
	if len(v.audience) > 0 {
		opts = append(opts, jwtlib.WithAudience(v.audience...))
	}
	return jwtlib.NewParser(opts...)
}

// methods lists the algorithms the configured keys can verify. The token
// header is checked against this list before any key is consulted.
func (v *Verifier) methods() []string {
	var out []string
	for _, k := range v.static {
		out = append(out, k.alg)
	}
	if v.jwks != nil {
		out = append(out, jwksAlgorithms...)
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// Authenticate implements auth.Authenticator. A request with no
// Authorization header, or one using a scheme other than Bearer, is
// anonymous. A Bearer token that is empty, too large or fails verification
// is an error.
func (v *Verifier) Authenticate(r *http.Request) (auth.Caller, bool, error) {
	header := r.Header.Get("Authorization")
	if header == "" {
		return auth.Caller{}, false, nil
	}
	scheme, token, _ := strings.Cut(header, " ")
	if !strings.EqualFold(scheme, "Bearer") {
		return auth.Caller{}, false, nil
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return auth.Caller{}, false, errors.New("jwt: empty bearer token")
	}
	c, err := v.Verify(r.Context(), token)
	if err != nil {
		return auth.Caller{}, false, err
	}
	return c, true, nil
}

// Verify checks a compact JWT and returns the caller it identifies. It is
// what Authenticate calls once it has the token; use it directly for tokens
// that arrive some other way, such as in a WebSocket handshake.
func (v *Verifier) Verify(ctx context.Context, token string) (auth.Caller, error) {
	if len(token) > MaxTokenBytes {
		return auth.Caller{}, fmt.Errorf("jwt: token exceeds %d bytes", MaxTokenBytes)
	}
	claims := jwtlib.MapClaims{}
	if _, err := v.parser.ParseWithClaims(token, claims, v.keyfunc(ctx)); err != nil {
		return auth.Caller{}, fmt.Errorf("jwt: %w", err)
	}
	sub, _ := claims.GetSubject()
	iss, _ := claims.GetIssuer()
	return auth.Caller{Subject: sub, Issuer: iss, Attributes: map[string]any(claims)}, nil
}

// keyfunc selects the keys that may verify a token: every static key for the
// token's algorithm, plus the JWKS keys matching its kid. The parser tries
// each until one verifies the signature.
func (v *Verifier) keyfunc(ctx context.Context) jwtlib.Keyfunc {
	return func(t *jwtlib.Token) (any, error) {
		alg := t.Method.Alg()
		var set jwtlib.VerificationKeySet
		for _, k := range v.static {
			if k.alg == alg {
				set.Keys = append(set.Keys, k.key)
			}
		}
		if v.jwks != nil && slices.Contains(jwksAlgorithms, alg) {
			kid, _ := t.Header["kid"].(string)
			keys, err := v.jwks.lookup(ctx, kid, alg)
			if err != nil && len(set.Keys) == 0 {
				return nil, err
			}
			for _, k := range keys {
				set.Keys = append(set.Keys, k)
			}
		}
		if len(set.Keys) == 0 {
			return nil, fmt.Errorf("no key for alg %s", alg)
		}
		return set, nil
	}
}

// algFor returns the algorithm a verification key is used with.
func algFor(key any) (string, error) {
	switch k := key.(type) {
	case *rsa.PublicKey:
		return jwtlib.SigningMethodRS256.Alg(), nil
	case *ecdsa.PublicKey:
		return ecdsaAlg(k.Curve)
	case ed25519.PublicKey:
		return jwtlib.SigningMethodEdDSA.Alg(), nil
	default:
		return "", fmt.Errorf("unsupported key type %T", key)
	}
}

func ecdsaAlg(c elliptic.Curve) (string, error) {
	switch c {
	case elliptic.P256():
		return jwtlib.SigningMethodES256.Alg(), nil
	case elliptic.P384():
		return jwtlib.SigningMethodES384.Alg(), nil
	case elliptic.P521():
		return jwtlib.SigningMethodES512.Alg(), nil
	default:
		return "", fmt.Errorf("unsupported curve %s", c.Params().Name)
	}
}
