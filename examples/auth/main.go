// Command auth is the todo example with bearer-token authentication. The
// tenant is the "sub" claim of a verified JWT, so each user sees only their
// own todos and nothing about the tenant is trusted from a plain header.
//
// For a self-contained demo the application signs tokens itself with a
// shared secret and prints one for a demo user at startup:
//
//	go run ./examples/auth
//	curl -H "Authorization: Bearer $TOKEN" -d '{"title":"ship it"}' localhost:8080/todos
//	curl -H "Authorization: Bearer $TOKEN" localhost:8080/todos
//
// Set JIG_JWT_SECRET to use your own secret. A production application
// accepting tokens from an identity provider swaps jwt.HMAC for jwt.JWKS and
// pins the issuer and audience; the commented block in main shows how.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"time"

	"github.com/jigframework/jig"
	"github.com/jigframework/jig/auth/jwt"
	"github.com/jigframework/jig/resource"
	"github.com/jigframework/jig/store"
	"github.com/jigframework/jig/tenant"
	"github.com/jigframework/jig/validate"
)

type Todo struct {
	jig.Model
	Title string `json:"title"`
	Done  bool   `json:"done"`
}

var Todos = resource.Of[Todo]("todos").
	Store(store.Memory[Todo]()).
	Validate(func(t Todo) error {
		return validate.All(
			validate.Required("title", t.Title),
			validate.MaxLen("title", t.Title, 200),
		)
	}).
	Build()

const (
	issuer   = "https://auth.example.test"
	audience = "todo-api"
)

func main() {
	secret := []byte(os.Getenv("JIG_JWT_SECRET"))
	if len(secret) == 0 {
		secret = []byte("dev-only-secret-change-me")
	}

	app := jig.New("auth",
		jig.WithAuth(jwt.Bearer(
			jwt.HMAC(secret),
			jwt.Issuer(issuer),
			jwt.Audience(audience),
		)),
		// Against an identity provider, replace jwt.HMAC with:
		//
		//	jwt.JWKS("https://auth.example.com/.well-known/jwks.json"),
		//
		// and keep the Issuer and Audience checks.
		jig.WithTenant(tenant.FromJWTAttribute("sub")),
	)
	app.Register(Todos)

	// Mint a token for a demo user so the example is usable immediately.
	token, err := jwt.Sign(secret, "", map[string]any{
		"sub": "user_demo",
		"iss": issuer,
		"aud": audience,
		"iat": time.Now().Unix(),
		"exp": time.Now().Add(24 * time.Hour).Unix(),
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("export TOKEN=%s\n\n", token)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := app.Run(ctx, ":8080"); err != nil {
		log.Fatal(err)
	}
}
