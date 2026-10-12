// Package db owns the Postgres connection and the Postgres-backed store.
//
// jig targets Postgres only, via pgx. Open connects, Migrate creates the
// tables a resource needs, and Postgres returns a store.Store that keeps one
// jsonb row per record keyed by (org, id). The resource layer owns IDs,
// timestamps, versions and soft deletes, so the store never inspects T.
//
//	pool, err := db.Open(ctx, os.Getenv("DATABASE_URL"))
//	err = db.Migrate(ctx, pool, "todos")
//	Todos := resource.Of[Todo]("todos").Store(db.Postgres[Todo](pool, "todos")).Build()
//
// Row-level security policies, so that Postgres enforces the tenant boundary
// a second time, are not installed yet; every query filters by org
// explicitly.
package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jigframework/jig/store"
	"github.com/jigframework/jig/tenant"
)

// Open connects to Postgres with a connection pool and verifies the
// connection with a ping. dsn is a libpq URL or key=value string.
func Open(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("db: open: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("db: ping: %w", err)
	}
	return pool, nil
}

// Migrate creates the named tables if they do not exist. Each table has the
// shape Postgres expects: org and id text columns forming the primary key, a
// seq column that gives List its creation order, and a jsonb data column.
func Migrate(ctx context.Context, pool *pgxpool.Pool, tables ...string) error {
	for _, table := range tables {
		q := `CREATE TABLE IF NOT EXISTS ` + pgx.Identifier{table}.Sanitize() + ` (
			org  text      NOT NULL,
			id   text      NOT NULL,
			seq  bigserial NOT NULL,
			data jsonb     NOT NULL,
			PRIMARY KEY (org, id)
		)`
		if _, err := pool.Exec(ctx, q); err != nil {
			return fmt.Errorf("db: migrate %s: %w", table, err)
		}
	}
	return nil
}

// Postgres returns a Store backed by table, which Migrate must have created.
// Records are stored as jsonb, so T must round-trip through encoding/json.
func Postgres[T any](pool *pgxpool.Pool, table string) store.Store[T] {
	t := pgx.Identifier{table}.Sanitize()
	return &postgres[T]{
		pool:   pool,
		list:   `SELECT data FROM ` + t + ` WHERE org = $1 ORDER BY seq`,
		get:    `SELECT data FROM ` + t + ` WHERE org = $1 AND id = $2`,
		put:    `INSERT INTO ` + t + ` (org, id, data) VALUES ($1, $2, $3) ON CONFLICT (org, id) DO UPDATE SET data = EXCLUDED.data`,
		delete: `DELETE FROM ` + t + ` WHERE org = $1 AND id = $2`,
	}
}

type postgres[T any] struct {
	pool                   *pgxpool.Pool
	list, get, put, delete string
}

func (s *postgres[T]) List(ctx context.Context, org tenant.ID) ([]T, error) {
	rows, err := s.pool.Query(ctx, s.list, string(org))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []T
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var v T
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *postgres[T]) Get(ctx context.Context, org tenant.ID, id string) (T, bool, error) {
	var zero, v T
	var raw []byte
	err := s.pool.QueryRow(ctx, s.get, string(org), id).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return zero, false, nil
	}
	if err != nil {
		return zero, false, err
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return zero, false, err
	}
	return v, true, nil
}

func (s *postgres[T]) Put(ctx context.Context, org tenant.ID, id string, v T) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, s.put, string(org), id, raw)
	return err
}

func (s *postgres[T]) Delete(ctx context.Context, org tenant.ID, id string) error {
	_, err := s.pool.Exec(ctx, s.delete, string(org), id)
	return err
}
