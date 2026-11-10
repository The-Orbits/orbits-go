package orbits

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestErrorFromProblem(t *testing.T) {
	rec := newRecorder(t, canned{status: 422, body: fixture(t, "problem.validation"), header: map[string]string{"Content-Type": "application/problem+json"}})
	c := newTestClient(t, rec.srv.URL)
	_, err := c.Projects.Create(context.Background(), "acme", ProjectCreate{})
	e, ok := AsError(err)
	if !ok {
		t.Fatalf("err = %v", err)
	}
	if e.Status != 422 || e.RequestID != "req_test1" || len(e.Errors) != 1 || e.Errors[0].Field != "name" {
		t.Errorf("%+v", e)
	}
	if fe, ok := e.FieldError("name"); !ok || fe.Message == "" {
		t.Error("FieldError(name) not found")
	}
	want := "orbits: Validation failed (status 422, request req_test1); name: must be 1 to 100 characters"
	if e.Error() != want {
		t.Errorf("Error() = %q\nwant      %q", e.Error(), want)
	}
}

func TestErrorHelpers(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		is     func(error) bool
	}{
		{"not found", 404, fixture(t, "problem.not-found"), IsNotFound},
		{"validation", 422, fixture(t, "problem.validation"), IsValidation},
		{"rate limited", 429, `{"status":429,"title":"Too Many Requests"}`, IsRateLimited},
		{"forbidden", 403, `{"status":403,"detail":"Viewers can't do this"}`, IsForbidden},
		{"conflict", 409, fixture(t, "problem.upload-incomplete"), IsConflict},
		{"unauthorized", 401, `{"status":401}`, IsUnauthorized},
		{"plan required", 402, fixture(t, "problem.plan-required"), IsPlanRequired},
		{"server", 500, ``, IsServerError},
		{"read-only token", 403, fixture(t, "problem.read-only-token"), IsTokenRefused},
		{"route refused to tokens", 403, `{"status":403,"detail":"API tokens can't use this endpoint"}`, IsTokenRefused},
	}
	all := []func(error) bool{IsNotFound, IsValidation, IsRateLimited, IsForbidden, IsConflict, IsUnauthorized, IsPlanRequired, IsServerError}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := fmt.Errorf("wrapped: %w", newError(tt.status, nil, []byte(tt.body)))
			if !tt.is(err) {
				t.Errorf("helper is false for %v", err)
			}
			matches := 0
			for _, f := range all {
				if f(err) {
					matches++
				}
			}
			if matches != 1 {
				t.Errorf("%d status helpers match, want 1", matches)
			}
		})
	}
	if IsNotFound(errors.New("plain")) || IsNotFound(nil) {
		t.Error("IsNotFound matched a non-API error")
	}
	if IsTokenRefused(newError(403, nil, []byte(`{"detail":"Viewers can't deploy"}`))) {
		t.Error("IsTokenRefused matched a person's 403")
	}
}

func TestPlanRequired(t *testing.T) {
	e := newError(402, nil, []byte(fixture(t, "problem.plan-required")))
	if e.Code != "plan_required" || e.RequiredPlan != "pro" || e.Feature != "functions" || !IsCode(e, "plan_required") {
		t.Errorf("%+v", e)
	}
}

func TestErrorWithoutProblemDocument(t *testing.T) {
	tests := []struct {
		status int
		body   string
		want   string
	}{
		{502, `<html><body>Bad Gateway</body></html>`, "temporarily unavailable"},
		{503, ``, "temporarily unavailable"},
		{400, `plain text from a proxy`, "plain text from a proxy"},
		{404, strings.Repeat("x", 400), "Not Found"},
	}
	for _, tt := range tests {
		e := newError(tt.status, nil, []byte(tt.body))
		if !strings.Contains(e.Error(), tt.want) || strings.Contains(e.Error(), "<html>") {
			t.Errorf("%d %q: %q", tt.status, tt.body, e.Error())
		}
	}
}

func TestErrorHeaders(t *testing.T) {
	rec := newRecorder(t, canned{status: 429, body: `{"status":429}`, header: map[string]string{
		"Retry-After": "12", "RateLimit-Scope": "team", "X-Request-Id": "req_hdr",
	}})
	c := newTestClient(t, rec.srv.URL, WithMaxRetries(0))
	err := c.Do(context.Background(), "GET", "/x", nil, nil, nil)
	e, _ := AsError(err)
	if e == nil || e.RetryAfter != 12*time.Second || e.RateLimitScope != "team" || e.RequestID != "req_hdr" {
		t.Errorf("%+v", e)
	}
}
