// Package validate provides field checks for resource validation hooks. The
// checks are ordinary functions over typed values, so a renamed or retyped
// field fails to compile instead of failing at runtime:
//
//	resource.Of[Todo]("todos").
//		Validate(func(t Todo) error {
//			return validate.All(
//				validate.Required("title", t.Title),
//				validate.MaxLen("title", t.Title, 200),
//			)
//		})
//
// All collects the failures into a single jig.Invalid error whose Fields map
// each field name to its messages, so a client sees every problem at once.
package validate

import (
	"cmp"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/jigframework/jig"
)

// Failure is one failed check on one field.
type Failure struct {
	Field   string
	Message string
}

func (f Failure) Error() string { return f.Field + " " + f.Message }

// All combines the results of several checks. Nil results are skipped.
// Failures are gathered into one jig.Invalid error keyed by field, each
// field holding a []string of messages. Any other error is returned as is,
// since it signals a problem with the check itself rather than the input.
func All(checks ...error) error {
	var fields map[string]any
	for _, err := range checks {
		if err == nil {
			continue
		}
		f, ok := err.(Failure)
		if !ok {
			return err
		}
		if fields == nil {
			fields = map[string]any{}
		}
		msgs, _ := fields[f.Field].([]string)
		fields[f.Field] = append(msgs, f.Message)
	}
	if fields == nil {
		return nil
	}
	return jig.Invalid(fields)
}

// Check is the escape hatch: it fails with message when ok is false.
func Check(field string, ok bool, message string) error {
	if ok {
		return nil
	}
	return Failure{field, message}
}

// Required fails when value is empty or only whitespace.
func Required(field, value string) error {
	return Check(field, strings.TrimSpace(value) != "", "is required")
}

// MaxLen fails when value is longer than n characters.
func MaxLen(field, value string, n int) error {
	return Check(field, utf8.RuneCountInString(value) <= n, fmt.Sprintf("must be at most %d characters", n))
}

// MinLen fails when value is shorter than n characters.
func MinLen(field, value string, n int) error {
	return Check(field, utf8.RuneCountInString(value) >= n, fmt.Sprintf("must be at least %d characters", n))
}

// Range fails when value is outside [lo, hi].
func Range[N cmp.Ordered](field string, value, lo, hi N) error {
	return Check(field, value >= lo && value <= hi, fmt.Sprintf("must be between %v and %v", lo, hi))
}

// OneOf fails when value is not one of allowed.
func OneOf[V comparable](field string, value V, allowed ...V) error {
	for _, a := range allowed {
		if value == a {
			return nil
		}
	}
	return Failure{field, fmt.Sprintf("must be one of %v", allowed)}
}
