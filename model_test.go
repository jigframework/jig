package jig

import (
	"reflect"
	"strings"
	"testing"
)

// JSON field names are camelCase everywhere in jig. This guards the fields on
// Model, which every resource inherits, so a snake_case name cannot creep back
// in.
func TestModelJSONTagsAreCamelCase(t *testing.T) {
	typ := reflect.TypeOf(Model{})
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name == "" {
			t.Errorf("%s has no json tag", f.Name)
			continue
		}
		if strings.ContainsAny(name, "_-") || name[0] < 'a' || name[0] > 'z' {
			t.Errorf("%s json tag %q is not camelCase", f.Name, name)
		}
	}
}
