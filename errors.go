package jig

import (
	"errors"
	"fmt"
	"net/http"
)

// Error is the single error type that crosses the handler boundary. Handlers
// return one of the constructors below, or wrap a lower-level error with
// Internal, and the framework maps it to an HTTP status and a JSON body.
type Error struct {
	Code    string         // stable machine-readable code, e.g. "not_found"
	Status  int            // HTTP status
	Message string         // human-readable, safe to show to the caller
	Fields  map[string]any // optional per-field detail for validation errors
	cause   error
}

func (e *Error) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.cause)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Unwrap exposes the underlying cause for errors.Is and errors.As.
func (e *Error) Unwrap() error { return e.cause }

// NotFound reports that the requested record does not exist or is not
// visible to the caller. jig deliberately does not distinguish the two.
func NotFound(what string) *Error {
	return &Error{Code: "not_found", Status: http.StatusNotFound, Message: what + " not found"}
}

// Forbidden reports that the caller is authenticated but the policy denied
// the action.
func Forbidden(msg string) *Error {
	return &Error{Code: "forbidden", Status: http.StatusForbidden, Message: msg}
}

// Unauthenticated reports that no valid identity was presented.
func Unauthenticated() *Error {
	return &Error{Code: "unauthenticated", Status: http.StatusUnauthorized, Message: "authentication required"}
}

// Invalid reports a validation failure with per-field detail.
func Invalid(fields map[string]any) *Error {
	return &Error{Code: "invalid", Status: http.StatusUnprocessableEntity, Message: "validation failed", Fields: fields}
}

// Conflict reports an optimistic concurrency or uniqueness failure.
func Conflict(msg string) *Error {
	return &Error{Code: "conflict", Status: http.StatusConflict, Message: msg}
}

// Internal wraps an unexpected error. The cause is logged but never shown to
// the caller.
func Internal(cause error) *Error {
	return &Error{Code: "internal", Status: http.StatusInternalServerError, Message: "internal error", cause: cause}
}

// StatusOf returns the HTTP status for err, defaulting to 500 for errors that
// are not a *Error.
func StatusOf(err error) int {
	var e *Error
	if errors.As(err, &e) {
		return e.Status
	}
	return http.StatusInternalServerError
}
