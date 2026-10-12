// Command todo is the smallest useful jig application: a multi-tenant todo
// list. It is the reference for what a jig application should look like, so
// it stays small and uses nothing but the public API.
//
// Run it and try:
//
//	curl -H 'X-Org-ID: acme' -d '{"title":"ship it"}' localhost:8080/todos
//	curl -H 'X-Org-ID: acme' localhost:8080/todos
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"time"

	"github.com/jigframework/jig"
	"github.com/jigframework/jig/cache"
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
	DueAt *time.Time `json:"due_at,omitempty"`
}

// Todos serves GET/POST /todos and GET/PUT/PATCH/DELETE /todos/{id}.
var Todos = resource.Of[Todo]("todos").
	Store(store.Memory[Todo]()).
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
