// Package policy expresses authorization as query filters. A Rule decides
// what a caller may see as a WHERE clause fragment, so list and get endpoints
// share one definition and cannot disagree. Write operations evaluate the
// same rule against the row in question.
//
// The tenant filter is always applied by the db package and is not a Rule;
// rules refine visibility within a tenant.
package policy

import "context"

// Action is the operation being authorized.
type Action int

const (
	Read Action = iota + 1
	Create
	Update
	Delete
)

// Filter is a SQL fragment with positional arguments. The db store splices
// it into its queries with argument indices renumbered.
type Filter struct {
	SQL  string
	Args []any
}

// Allow is the filter that admits every row in the tenant.
var Allow = Filter{SQL: "TRUE"}

// Deny is the filter that admits no rows.
var Deny = Filter{SQL: "FALSE"}

// Rule produces the visibility filter for an action given the caller on
// ctx. Rules must be pure and cheap: they run on every request.
type Rule interface {
	Filter(ctx context.Context, action Action) Filter
}

// Func adapts a function to Rule.
type Func func(ctx context.Context, action Action) Filter

// Filter implements Rule.
func (f Func) Filter(ctx context.Context, action Action) Filter { return f(ctx, action) }

// AllowAll is a Rule that admits every row in the tenant for every action.
var AllowAll Rule = Func(func(context.Context, Action) Filter { return Allow })
