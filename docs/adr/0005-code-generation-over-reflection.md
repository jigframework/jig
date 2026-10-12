# 0005. Code generation over reflection

Status: superseded by 0006

## Context

Go frameworks that rely on reflection trade compile-time safety and
readability for convenience, and the result is hard to step through.

## Decision

`jig gen` emits ordinary Go that a user can read, grep and debug. Runtime
reflection is limited to decoding request bodies.

## Consequences

Generated files are committed. A change to a definition requires re-running
the generator, and CI checks that the output is up to date.
