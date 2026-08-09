// Package httpx provides HTTP helpers: routing, middleware, errors and pagination.
package httpx

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// Error is an API error with a machine-readable code.
type Error struct {
	Status  int    `json:"-"`
	Code    string `json:"code"`
	Message string `json:"message"`
	Details any    `json:"details,omitempty"`
}

func (e *Error) Error() string { return fmt.Sprintf("%s: %s", e.Code, e.Message) }

// NewError builds an Error.
func NewError(status int, code, message string) *Error {
	return &Error{Status: status, Code: code, Message: message}
}

// WithDetails attaches extra details.
func (e *Error) WithDetails(d any) *Error { e.Details = d; return e }

// WrapError returns an *Error from any error, defaulting to 500 internal_error.
func WrapError(err error) *Error {
	if err == nil {
		return nil
	}
	var ae *Error
	if errors.As(err, &ae) {
		return ae
	}
	return &Error{Status: http.StatusInternalServerError, Code: "internal_error", Message: "internal server error"}
}

// Common error constructors.
var (
	ErrBadRequest      = func(msg string) *Error { return NewError(http.StatusBadRequest, "bad_request", msg) }
	ErrUnauthorized    = func(msg string) *Error { return NewError(http.StatusUnauthorized, "unauthorized", msg) }
	ErrForbidden       = func(msg string) *Error { return NewError(http.StatusForbidden, "forbidden", msg) }
	ErrNotFound        = func(msg string) *Error { return NewError(http.StatusNotFound, "not_found", msg) }
	ErrConflict        = func(msg string) *Error { return NewError(http.StatusConflict, "conflict", msg) }
	ErrUnprocessable   = func(msg string) *Error { return NewError(http.StatusUnprocessableEntity, "validation_failed", msg) }
	ErrTooManyRequests = func(msg string) *Error { return NewError(http.StatusTooManyRequests, "rate_limited", msg) }
)

// Body is the standard API response envelope.
type Body struct {
	Data  any    `json:"data,omitempty"`
	Error *Error `json:"error,omitempty"`
}

// JSON writes a data envelope.
func JSON(w http.ResponseWriter, status int, data any) {
	writeJSON(w, status, Body{Data: data})
}

// WriteError writes an error envelope and returns the error for logging.
func WriteError(w http.ResponseWriter, err error) *Error {
	ae := WrapError(err)
	writeJSON(w, ae.Status, Body{Error: ae})
	return ae
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

// DecodeJSON reads a JSON request body with a size limit.
func DecodeJSON(r *http.Request, dst any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 8<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return ErrBadRequest("invalid JSON body: " + err.Error())
	}
	return nil
}
