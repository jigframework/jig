# 0003. Multi-tenancy is mandatory

Status: accepted

## Context

Almost every SaaS application is multi-tenant, and the most damaging bug
class is a query that forgets the tenant filter.

## Decision

Every resource is tenant-scoped unless it opts out with `NoTenant`. The
tenant lives on `context.Context`. Store queries read it and fail with
`tenant.ErrMissing` if absent. Migrations install row-level security policies
so Postgres enforces the same boundary.

## Consequences

Single-tenant applications carry an org ID column they do not need. Global
tables must opt out explicitly, which is the point.
