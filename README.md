# jig

An opinionated Go framework for CRUD applications and SaaS products.

A jig is the workshop fixture that guides the tool so every cut comes out
right. This framework does the same for Go web applications: you describe
each resource once, as a plain struct, and jig gives you the HTTP routes,
validation, tenancy, soft deletes, optimistic concurrency and caching for it.
The compiler checks the wiring. There is no code generation.

> Status: pre-alpha. The package layout and public types are being designed.
> Nothing here is stable.

## A complete application

```go
type Todo struct {
	jig.Model
	Title string     `json:"title"`
	Done  bool       `json:"done"`
	DueAt *time.Time `json:"dueAt,omitempty"`
}

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
	app := jig.New("todo", jig.WithTenant(tenant.FromHeader("X-Org-ID")))
	app.Register(Todos)
	log.Fatal(app.Run(context.Background(), ":8080"))
}
```

That serves:

| Route | Does |
|---|---|
| `GET /todos` | list, paginated with `?limit=` and `?cursor=` |
| `POST /todos` | create, returns 201 with the record |
| `GET /todos/{id}` | get |
| `PUT /todos/{id}` | replace |
| `PATCH /todos/{id}` | merge the fields present in the body |
| `DELETE /todos/{id}` | soft delete, returns 204 |
| `GET /healthz` | health check |

Every route is scoped to the tenant on the request. Errors are JSON with a
stable code: `not_found`, `invalid` (with per-field messages), `conflict`,
`unauthenticated`, `forbidden`, `internal`.

The full example lives in `examples/todo`.

## The opinions

- **One resource definition is the source of truth.** A Go struct plus a
  builder. Routes, validation, storage and caching derive from it.
- **Compile-time safety over code generation.** Generics check the wiring;
  the only reflection is JSON encoding.
- **Postgres only.** No database abstraction layer.
- **Multi-tenancy is mandatory.** Every record carries an org ID, every
  operation is scoped from the request context, and an application cannot
  register a resource without saying how the tenant is resolved.
- **Handlers are typed functions.** `func(ctx, In) (Out, error)`. Decoding,
  encoding and error mapping come from the types.
- **Authorization is attribute-based and compiles into queries.** Every
  resource is protected by default. A policy compares caller and row
  attributes and is a WHERE clause, so list and get endpoints can never
  disagree.
- **SaaS primitives ship in the box.** Users, orgs, memberships, invites, roles,
  API keys, sessions, billing webhooks.
- **Background jobs run on Postgres.** Enqueue and the record commit together.
- **Server-rendered by default.** templ + htmx, with an admin.
- **Boring defaults are enforced.** Soft deletes, timestamps, cursor pagination,
  optimistic concurrency, strict JSON decoding.
- **Single binary.** Migrations, templates and assets are embedded.

## Layout

```
cmd/jig/        the CLI: jig new
*.go            the root package: App, Serve, Error, Model, the startup banner
resource/       the builder that turns a struct into routes
store/          Store[T] and the in-memory implementation
validate/       typed field checks for resource validation
tenant/         tenant identity carried on context, and resolvers
cache/          read-through caching policy and in-process cache
policy/         authorization rules that compile to query filters
db/             Postgres connection, transactions, migrations
jobs/           transactional job queue on Postgres
auth/           users, orgs, memberships, sessions, API keys
web/            server-rendered UI and admin
examples/todo/  the reference application
docs/adr/       architecture decision records
```

## Quick start

```sh
go get github.com/jigframework/jig
```

Copy `examples/todo/main.go`, change the struct, run it.

## Development

```sh
make build   # build the CLI into ./bin/jig
make test    # go test -race ./...
make lint    # go vet + staticcheck if installed
```

## License

MIT
