package jwt

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rsa"
	"fmt"

	jwtlib "github.com/golang-jwt/jwt/v5"
)

// Sign mints a token, for tests and for applications that issue their own.
// The key's type selects the algorithm, mirroring HMAC and PublicKey: []byte
// signs HS256, *rsa.PrivateKey RS256, *ecdsa.PrivateKey ES256, ES384 or
// ES512 by curve, and ed25519.PrivateKey EdDSA. A non-empty kid is written
// to the header so a JWKS verifier can pick the matching key.
//
// Claims are written as given. Sign adds nothing, so callers set exp
// themselves; a Verifier rejects a token without one.
func Sign(key any, kid string, claims map[string]any) (string, error) {
	var method jwtlib.SigningMethod
	switch k := key.(type) {
	case []byte:
		method = jwtlib.SigningMethodHS256
	case *rsa.PrivateKey:
		method = jwtlib.SigningMethodRS256
	case *ecdsa.PrivateKey:
		alg, err := ecdsaAlg(k.Curve)
		if err != nil {
			return "", fmt.Errorf("jwt: %w", err)
		}
		method = jwtlib.GetSigningMethod(alg)
	case ed25519.PrivateKey:
		method = jwtlib.SigningMethodEdDSA
	default:
		return "", fmt.Errorf("jwt: unsupported signing key type %T", key)
	}
	t := jwtlib.NewWithClaims(method, jwtlib.MapClaims(claims))
	if kid != "" {
		t.Header["kid"] = kid
	}
	s, err := t.SignedString(key)
	if err != nil {
		return "", fmt.Errorf("jwt: %w", err)
	}
	return s, nil
}
