package jig

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
)

// Handler is the shape of every request handler in jig. A handler never
// touches http.Request or http.ResponseWriter; Serve adapts it to net/http.
type Handler[In, Out any] func(ctx context.Context, in In) (Out, error)

// Resource is anything that can register routes on an application. The
// resource package builds these; applications rarely implement it directly.
type Resource interface {
	// Name is the URL path segment the resource lives under, e.g. "todos".
	Name() string
	// Routes registers the resource's handlers on mux using Go 1.22 method
	// patterns such as "GET /todos/{id}".
	Routes(mux *http.ServeMux)
}

// MaxBodyBytes is the largest request body Serve and DecodeBody accept.
const MaxBodyBytes = 1 << 20

// Serve adapts a typed handler to net/http. decode builds In from the
// request, the handler runs, and Out is written as JSON with the given
// status. Errors from either step are written by WriteError. A status of 204
// writes no body.
func Serve[In, Out any](status int, decode func(*http.Request) (In, error), h Handler[In, Out]) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		in, err := decode(r)
		if err != nil {
			WriteError(w, err)
			return
		}
		out, err := h(r.Context(), in)
		if err != nil {
			WriteError(w, err)
			return
		}
		if status == http.StatusNoContent {
			w.WriteHeader(status)
			return
		}
		WriteJSON(w, status, out)
	})
}

// WriteJSON writes v as a JSON response with the given status.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// WriteError writes err as a JSON error response. A *Error is written with
// its own status and code; any other error is treated as Internal. Server
// errors are logged with the cause, which is never sent to the client.
func WriteError(w http.ResponseWriter, err error) {
	var e *Error
	if !errors.As(err, &e) {
		e = Internal(err)
	}
	if e.Status >= http.StatusInternalServerError {
		slog.Error("jig: handler failed", "code", e.Code, "err", err)
	}
	WriteJSON(w, e.Status, errorBody{Error: e})
}

type errorBody struct {
	Error *Error `json:"error"`
}

// MarshalJSON renders the public parts of an Error: the cause is omitted.
func (e *Error) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Code    string         `json:"code"`
		Message string         `json:"message"`
		Fields  map[string]any `json:"fields,omitempty"`
	}{e.Code, e.Message, e.Fields})
}

// ReadBody reads a request body up to MaxBodyBytes.
func ReadBody(r *http.Request) ([]byte, error) {
	data, err := io.ReadAll(http.MaxBytesReader(nil, r.Body, MaxBodyBytes))
	if err != nil {
		return nil, Invalid(map[string]any{"body": []string{"could not be read: " + err.Error()}})
	}
	return data, nil
}

// Unmarshal decodes JSON into v. Unknown fields are an error, so a typo in a
// request is reported instead of silently ignored.
func Unmarshal(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return Invalid(map[string]any{"body": []string{"malformed JSON: " + err.Error()}})
	}
	if dec.More() {
		return Invalid(map[string]any{"body": []string{"trailing data after JSON value"}})
	}
	return nil
}

// DecodeBody reads and decodes a JSON request body into v.
func DecodeBody(r *http.Request, v any) error {
	data, err := ReadBody(r)
	if err != nil {
		return err
	}
	return Unmarshal(data, v)
}
