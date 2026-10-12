# 0006. Typed builders over code generation

Status: accepted. Supersedes 0005.

## Context

ADR 0005 chose code generation so that users could read and debug ordinary
Go. In practice a generator adds a build step, a CLI, a check that output is
up to date, and a second definition language whose field names and types are
strings checked only when the generator runs. The example application could
not run without it.

Go generics now make the alternative viable: a resource is a struct, and a
generic builder derives the routes from the type at compile time.

## Decision

There is no code generation. A resource is a Go struct embedding `jig.Model`,
built into HTTP routes with `resource.Of[T]`. Storage is a typed
`store.Store[T]`, validation is a typed function over `T`, and the cache
policy is declared on the same builder. A misconfigured builder panics at
program start; a mistyped field fails to compile.

Runtime reflection is limited to encoding and decoding JSON bodies, which is
the standard library's job.

## Consequences

The CLI shrinks to project scaffolding. Postgres storage has to map structs to
columns without a generator; the db package will do so through a typed
mapping the user writes once per resource, not through reflection over tags.
Generic type signatures appear in error messages and godoc, which is the cost
of the compiler doing the checking.
