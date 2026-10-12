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

## Authenticated tenants

`tenant.FromHeader` trusts the request, which suits development and internal
services. A public application identifies the caller first and takes the
tenant from the verified identity:

```go
app := jig.New("todo",
	jig.WithAuth(jwt.Bearer(
		jwt.JWKS("https://issuer.example.com/.well-known/jwks.json"),
		jwt.Issuer("https://issuer.example.com"),
		jwt.Audience("todo-api"),
	)),
	jig.WithTenant(tenant.FromJWTAttribute("sub")),
)
```

`jig.WithAuth` verifies the bearer token on every request and places the
caller, with every claim as an attribute, on the context. `WithTenant` then
reads one attribute. Using `sub` makes each user their own tenant; name the
claim that carries the org ID instead for a SaaS with organizations. Requests
without a token are anonymous and tenant-scoped routes reject them; requests
with an invalid token are rejected everywhere.

The verifier takes keys from `jwt.JWKS`, `jwt.PublicKey` or `jwt.HMAC`. The
algorithms it accepts are derived from those keys, never from the token, so
`alg: none` and key confusion are rejected by construction. `examples/auth`
is the todo example with a self-signed HS256 token.

## The opinions

- **One resource definition is the source of truth.** A Go struct plus a
  builder. Routes, validation, storage and caching derive from it.
- **Compile-time safety over code generation.** Generics check the wiring;
  the only reflection is JSON encoding.
- **Postgres only.** No database abstraction layer.
- **Multi-tenancy is mandatory.** Every record carries an org ID, every
  operation is scoped from the request context, and an application cannot
  register a resource without saying how the tenant is resolved.
- **Authentication is separate from tenancy.** An authenticator verifies who
  is calling and exposes their attributes; the tenant resolver and policy
  rules read those attributes. Invalid credentials are never anonymous.
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
auth/           the caller on context; users, orgs, memberships, sessions, API keys
auth/jwt/       bearer-token authentication with JWKS, public keys or a shared secret
web/            server-rendered UI and admin
examples/todo/  the reference application
examples/auth/  the reference application behind JWT authentication
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
