# 0004. Authorization compiles into query filters

Status: accepted

## Context

Middleware-style authorization checks a record after loading it. List
endpoints then need a second, separately written filter, and the two drift.

## Decision

A policy rule returns a SQL filter. Reads splice it into the query. Writes
evaluate the same rule against the target row. There is one definition of
"what can this caller see".

## Consequences

Rules must be expressible as SQL. Rules that need application logic are
possible as hand-written handlers alongside the resource, but they lose the
list/get guarantee.
