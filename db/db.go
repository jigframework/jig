// Package db owns the Postgres connection, transactions and migrations.
//
// jig targets Postgres only, via pgx. The package will provide a store.Store
// implementation and enforce the tenant contract: a tenant-scoped query on a
// context without a tenant returns tenant.ErrMissing before it reaches the
// database, and row-level security policies installed by the migrations
// enforce it a second time inside Postgres.
package db
