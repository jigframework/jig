# 0001. A resource definition is the single source of truth

Status: accepted

## Context

CRUD applications restate the same shape in five places: the migration, the
query layer, the HTTP handler, the API document and the UI. They drift.

## Decision

A resource is defined once, as a Go struct built into routes with the
`resource` package. Storage, validation, HTTP handlers and caching are all
derived from that one definition at compile time (see 0006). Hand-written
code extends a resource through hooks on the builder and never duplicates its
shape.

## Consequences

The builder must be expressive enough for real applications, which means it
will grow. Anything it cannot express is written by hand as an ordinary
handler alongside the resource, not by bypassing the resource.
