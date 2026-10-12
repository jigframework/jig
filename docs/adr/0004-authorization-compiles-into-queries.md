# 0004. Authorization is attribute-based and compiles into query filters

Status: accepted

## Context

Middleware-style authorization checks a record after loading it. List
endpoints then need a second, separately written filter, and the two drift.

Role checks alone are not enough for SaaS applications. The usual rule is
"a member can see records in their org, an owner can see their own drafts,
an admin can see everything", which depends on attributes of the caller and
of the row, not on a role name alone.

## Decision

Every resource is protected by attribute-based access control (ABAC) by
default. A resource with no policy admits nothing; `policy.AllowAll` is the
explicit opt-out, by analogy with `NoTenant`.

A policy rule compares attributes of the caller on `context.Context` (user,
org, roles, API key scopes) with attributes of the row (columns) and returns a
SQL filter. Reads splice it into the query. Writes evaluate the same rule
against the target row. There is one definition of "what can this caller
see". Roles from the auth package are one attribute among others, so RBAC is
a special case of the same rule, not a separate mechanism.

## Consequences

Rules must be expressible as SQL. Rules that need application logic are
possible as hand-written handlers alongside the resource, but they lose the
list/get guarantee. Every resource declares a policy or opts out, which is
one more line on the builder and the point.
