// Command todo is the smallest useful jig application: a multi-tenant todo
// list. It is the reference for what a jig application should look like, so
// it stays small and uses nothing but the public API.
//
// Run it and try:
//
//	curl -H 'X-Org-ID: acme' -d '{"title":"ship it"}' localhost:8080/todos
//	curl -H 'X-Org-ID: acme' localhost:8080/todos
//
// Records live in memory unless DATABASE_URL is set, in which case they live
// in Postgres.
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"time"

	"github.com/jigframework/jig"
	"github.com/jigframework/jig/cache"
	"github.com/jigframework/jig/db"
	"github.com/jigframework/jig/resource"
	"github.com/jigframework/jig/store"
	"github.com/jigframework/jig/tenant"
	"github.com/jigframework/jig/validate"
)

// Todo is a plain struct. Embedding jig.Model gives it an ID, tenant,
// timestamps, soft delete and version; the compiler checks the rest.
type Todo struct {
	jig.Model
	Title string     `json:"title"`
	Done  bool       `json:"done"`
	DueAt *time.Time `json:"dueAt,omitempty"`
}

// Todos serves GET/POST /todos and GET/PUT/PATCH/DELETE /todos/{id}.
var Todos = resource.Of[Todo]("todos").
	Store(todoStore()).
	Validate(func(t Todo) error {
		return validate.All(
			validate.Required("title", t.Title),
			validate.MaxLen("title", t.Title, 200),
		)
	}).
	Caching(
		cache.Enabled(),
		cache.TTL(15*time.Minute),
	).
	Build()

// todoStore is Postgres when DATABASE_URL is set and memory otherwise, so the
// example runs with no setup and can still be measured against a real
// database.
func todoStore() store.Store[Todo] {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return store.Memory[Todo]()
	}
	ctx := context.Background()
	pool, err := db.Open(ctx, dsn)
	if err != nil {
		log.Fatal(err)
	}
	if err := db.Migrate(ctx, pool, "todos"); err != nil {
		log.Fatal(err)
	}
	return db.Postgres[Todo](pool, "todos")
}

func main() {
	app := jig.New("todo",
		// Read the organization from a header. Fine for a demo; a real
		// application resolves it from the authenticated session.
		jig.WithTenant(tenant.FromHeader("X-Org-ID")),
	)
	app.Register(Todos)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := app.Run(ctx, ":8080"); err != nil {
		log.Fatal(err)
	}
}
