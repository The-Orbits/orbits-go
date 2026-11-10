package orbits

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Error is an error answer from the API: an RFC 9457 application/problem+json document. Detail is plain language
// and safe to show to people; branch on Status (with the Is helpers) or Code, never on Detail.
type Error struct {
	// Status is the HTTP status code.
	Status int
	// Type is the problem type URI.
	Type string
	// Title is the short summary of the problem type.
	Title string
	// Detail says what went wrong, in plain language.
	Detail string
	// Instance identifies this occurrence of the problem, when the API sends it.
	Instance string
	// RequestID identifies the request; quote it to support. It is the problem's request_id, else the
	// X-Request-Id header.
	RequestID string
	// Code is a stable reason where the API defines one, such as plan_required (402), upload_incomplete or
	// not_an_upload_project (409). Often empty.
	Code string
	// RequiredPlan is, on a 402, the plan that includes what was asked for ("pro").
	RequiredPlan string
	// Feature is, on a 402, what the team's plan lacks (functions, sandboxes, analytics, speed_insights).
	Feature string
	// Errors names each invalid field of a 422.
	Errors []FieldError
	// RetryAfter is the wait the API asked for (on 429, and on a 409 for a request still running), or 0.
	RetryAfter time.Duration
	// RateLimitScope is, on a 429, whose budget ran out: user, token, team, address or email.
	RateLimitScope string
}

// FieldError is one invalid field of a validation failure.
type FieldError struct {
	// Field is the property's path in the request, such as "name" or "env[0].key".
	Field   string `json:"field"`
	Message string `json:"message"`
}

// Error reads like "orbits: Project not found (status 404, request req_…)", followed by any field errors.
func (e *Error) Error() string {
	msg := e.Detail
	if msg == "" {
		msg = e.Title
	}
	if msg == "" {
		msg = http.StatusText(e.Status)
	}
	var b strings.Builder
	b.WriteString("orbits: ")
	b.WriteString(msg)
	parts := []string{}
	if e.Code != "" {
		parts = append(parts, e.Code)
	}
	parts = append(parts, fmt.Sprintf("status %d", e.Status))
	if e.RequestID != "" {
		parts = append(parts, "request "+e.RequestID)
	}
	b.WriteString(" (" + strings.Join(parts, ", ") + ")")
	for _, fe := range e.Errors {
		if fe.Field != "" {
			fmt.Fprintf(&b, "; %s: %s", fe.Field, fe.Message)
		} else {
			fmt.Fprintf(&b, "; %s", fe.Message)
		}
	}
	return b.String()
}

// FieldError returns the validation error for field, if the API named it.
func (e *Error) FieldError(field string) (FieldError, bool) {
	for _, fe := range e.Errors {
		if fe.Field == field {
			return fe, true
		}
	}
	return FieldError{}, false
}

// AsError returns the *Error in err's chain, if there is one.
func AsError(err error) (*Error, bool) {
	var e *Error
	ok := errors.As(err, &e)
	return e, ok
}

func hasStatus(err error, status int) bool {
	e, ok := AsError(err)
	return ok && e.Status == status
}

// IsNotFound reports a 404. The API answers 404 both for what does not exist and for what is not in the token's
// team (or project), on purpose.
func IsNotFound(err error) bool { return hasStatus(err, http.StatusNotFound) }

// IsValidation reports a 422: the request was invalid (see Error.Errors), or its Idempotency-Key was reused with
// a different body.
func IsValidation(err error) bool { return hasStatus(err, http.StatusUnprocessableEntity) }

// IsRateLimited reports a 429. Error.RetryAfter says how long to wait; the SDK has already retried it.
func IsRateLimited(err error) bool { return hasStatus(err, http.StatusTooManyRequests) }

// IsForbidden reports a 403: the person the token acts as may not do this (a viewer, say), or the token may not
// (see IsTokenRefused).
func IsForbidden(err error) bool { return hasStatus(err, http.StatusForbidden) }

// IsConflict reports a 409: the request conflicts with the current state, such as a taken name, a Git project
// asked for an upload, or an upload that has not arrived yet (see Error.Code).
func IsConflict(err error) bool { return hasStatus(err, http.StatusConflict) }

// IsUnauthorized reports a 401: the token is wrong, revoked or expired.
func IsUnauthorized(err error) bool { return hasStatus(err, http.StatusUnauthorized) }

// IsPlanRequired reports a 402: the team's plan does not include this. Error.Feature says what is missing and
// Error.RequiredPlan which plan has it.
func IsPlanRequired(err error) bool { return hasStatus(err, http.StatusPaymentRequired) }

// IsServerError reports a 5xx answer, after the SDK's retries.
func IsServerError(err error) bool {
	e, ok := AsError(err)
	return ok && e.Status >= 500
}

// IsTokenRefused reports a 403 the gateway gave because of the kind of credential: a read-only token sending a
// write, or a route API tokens cannot use (sign-in and account routes, token management, members and invites,
// integrations). Signing in to the web app is the way to do these.
func IsTokenRefused(err error) bool {
	e, ok := AsError(err)
	if !ok || e.Status != http.StatusForbidden {
		return false
	}
	d := e.Detail
	return strings.HasPrefix(d, "API tokens can't") || strings.HasPrefix(d, "This API token is read-only")
}

// IsCode reports whether err is an *Error with this problem code, such as "upload_incomplete".
func IsCode(err error, code string) bool {
	e, ok := AsError(err)
	return ok && e.Code == code
}

// newError builds an *Error from a non-2xx answer. A body that is not a problem document still gives a usable
// error from the status; an HTML page or long text from a proxy is never copied into it.
func newError(status int, header http.Header, body []byte) *Error {
	e := &Error{Status: status}
	var doc struct {
		Type         string       `json:"type"`
		Title        string       `json:"title"`
		Detail       string       `json:"detail"`
		Instance     string       `json:"instance"`
		RequestID    string       `json:"request_id"`
		Code         string       `json:"code"`
		RequiredPlan string       `json:"required_plan"`
		Feature      string       `json:"feature"`
		Errors       []FieldError `json:"errors"`
	}
	if err := json.Unmarshal(body, &doc); err == nil {
		e.Type, e.Title, e.Detail, e.Instance = doc.Type, doc.Title, doc.Detail, doc.Instance
		e.RequestID, e.Code, e.RequiredPlan, e.Feature, e.Errors = doc.RequestID, doc.Code, doc.RequiredPlan, doc.Feature, doc.Errors
	} else if text := strings.TrimSpace(string(body)); text != "" && len(text) <= 300 && !strings.HasPrefix(text, "<") {
		e.Detail = text
	}
	if e.Title == "" {
		e.Title = http.StatusText(status)
	}
	if e.Detail == "" && status >= 500 {
		e.Detail = fmt.Sprintf("The Orbits API is temporarily unavailable (%d %s). Try again in a moment.", status, http.StatusText(status))
	}
	if header != nil {
		if e.RequestID == "" {
			e.RequestID = header.Get("X-Request-Id")
		}
		e.RetryAfter = parseRetryAfter(header.Get("Retry-After"), time.Now())
		e.RateLimitScope = header.Get("RateLimit-Scope")
	}
	return e
}
