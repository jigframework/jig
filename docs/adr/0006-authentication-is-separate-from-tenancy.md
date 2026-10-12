# 0006. Authentication is separate from tenancy

Status: accepted

## Context

A tenant resolver such as `tenant.FromHeader` trusts the request. A public
application cannot: the organization must come from a credential the server
has verified. The obvious shortcut is a resolver that verifies a JWT and
returns a claim, but then the verification settings live on the tenant
option, a second resolver for a different claim verifies the token again,
and policy rules (0004) have no way to reach the other claims.

## Decision

Identifying the caller and choosing the tenant are two steps with two
options. `jig.WithAuth` takes an `auth.Authenticator`, which verifies the
request's credentials and produces an `auth.Caller`: a subject, an issuer and
every attribute the credential vouched for. `jig.WithTenant` takes a resolver
as before; `tenant.FromJWTAttribute(name)` reads one attribute of the caller.
The authenticator runs first, so the resolver and every policy rule see the
same verified caller.

Requests with no credentials proceed anonymously and are rejected by
tenant-scoped routes, as they are today. Requests with credentials that fail
verification are rejected on every route. Invalid is never downgraded to
anonymous.

The first authenticator is `auth/jwt`, which accepts bearer tokens.
Signature verification and claim checks are delegated to
`github.com/golang-jwt/jwt/v5`; key discovery over JWKS is jig's own, because
the library does not provide it and the cache, cooldown and rotation
behaviour are opinions jig wants to hold. The permitted algorithms are
derived from the configured keys and never from the token, an expiry claim is
required, and JWKS URLs must be https.

## Consequences

Two options instead of one on every public application, which is the point:
the tenant line says which claim, the auth line says who is trusted to issue
it. Attributes are an untyped map, because claim names and shapes are the
issuer's choice; a resolver that wants a nested or namespaced claim writes a
few lines against `auth.From`. The framework takes a dependency on a JWT
library and owns a JWKS client that must be kept correct.
