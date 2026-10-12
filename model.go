package jig

import (
	"time"

	"github.com/jigframework/jig/tenant"
)

// Model is the set of columns every resource carries. Embed it in a resource
// struct:
//
//	type Todo struct {
//		jig.Model
//		Title string `json:"title"`
//	}
//
// The framework owns every field on Model. Values a client sends for them are
// ignored, except Version, which a client may send to request an optimistic
// concurrency check.
type Model struct {
	ID        string     `json:"id"`
	OrgID     tenant.ID  `json:"org_id,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	DeletedAt *time.Time `json:"deleted_at,omitempty"`
	Version   int        `json:"version"`
}

// Meta returns the embedded Model. It is what lets the resource package reach
// the framework-owned fields of any struct that embeds Model.
func (m *Model) Meta() *Model { return m }
