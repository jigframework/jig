package db

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type testPool struct{ *pgxpool.Pool }

// fresh migrates a uniquely named table and drops it when the test ends.
func (p *testPool) fresh(t *testing.T, ctx context.Context, prefix string) string {
	t.Helper()
	table := fmt.Sprintf("jig_test_%s_%d", prefix, time.Now().UnixNano())
	if err := Migrate(ctx, p.Pool, table); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = p.Exec(context.Background(), `DROP TABLE IF EXISTS `+pgx.Identifier{table}.Sanitize())
	})
	return table
}
