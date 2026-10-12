# 0002. Postgres only

Status: accepted

## Context

Database abstraction layers cost the features that make Postgres worth
choosing: row-level security, advisory locks, LISTEN/NOTIFY, jsonb, SKIP
LOCKED.

## Decision

jig supports Postgres and nothing else. The db package uses pgx directly.

## Consequences

Users on other databases cannot use jig. In exchange, tenancy, jobs and
concurrency control get real database guarantees instead of emulation.
