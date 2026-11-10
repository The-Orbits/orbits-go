package orbits

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"testing"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name    string
		token   string
		opts    []Option
		wantErr string
		wantURL string
	}{
		{"defaults", testToken, nil, "", DefaultBaseURL},
		{"base URL", testToken, []Option{WithBaseURL("https://stagingapi.theorbits.app/v1/")}, "", "https://stagingapi.theorbits.app/v1"},
		{"no token", "", nil, "no API token", ""},
		{"blank token", "   ", nil, "no API token", ""},
		{"token with a line break", "orb_live_abc\ndef", nil, "spaces or line breaks", ""},
		{"bad scheme", testToken, []Option{WithBaseURL("ftp://example.com")}, "invalid base URL", ""},
		{"no host", testToken, []Option{WithBaseURL("https://")}, "invalid base URL", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := New(tt.token, tt.opts...)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if c.BaseURL() != tt.wantURL {
				t.Errorf("BaseURL = %q, want %q", c.BaseURL(), tt.wantURL)
			}
		})
	}
}

func TestNewFromEnv(t *testing.T) {
	t.Setenv(EnvToken, testToken)
	t.Setenv(EnvAPIURL, "https://stagingapi.theorbits.app/v1")
	c, err := NewFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if c.BaseURL() != "https://stagingapi.theorbits.app/v1" {
		t.Errorf("BaseURL = %q", c.BaseURL())
	}
	c, err = NewFromEnv(WithBaseURL("http://localhost:8080/v1"))
	if err != nil || c.BaseURL() != "http://localhost:8080/v1" {
		t.Errorf("WithBaseURL should win over the environment: %v %v", c, err)
	}
	t.Setenv(EnvToken, "")
	if _, err := NewFromEnv(); err == nil || !strings.Contains(err.Error(), EnvToken) {
		t.Errorf("no token: err = %v", err)
	}
	t.Setenv(EnvAPIURL, "")
	t.Setenv(EnvToken, testToken)
	if c, _ := NewFromEnv(); c.BaseURL() != DefaultBaseURL {
		t.Errorf("BaseURL = %q, want the default", c.BaseURL())
	}
}

func TestHeaders(t *testing.T) {
	rec := newRecorder(t, ok(`{}`))
	c := newTestClient(t, rec.srv.URL, WithUserAgent("orbits-cli/0.1.0"), WithHeader("X-Extra", "1"))
	if err := c.Do(context.Background(), "GET", "/me", nil, nil, nil, WithRequestHeader("X-Call", "2")); err != nil {
		t.Fatal(err)
	}
	r := rec.request(0)
	for header, want := range map[string]string{
		"Authorization": "Bearer " + testToken,
		"User-Agent":    "orbits-cli/0.1.0 orbits-go/" + Version,
		"Accept":        "application/json",
		"X-Extra":       "1",
		"X-Call":        "2",
	} {
		if got := r.Header.Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}
	if r.Header.Get("Idempotency-Key") != "" {
		t.Error("a GET carried an Idempotency-Key")
	}
	if r.URL.Path != "/v1/me" {
		t.Errorf("path %q", r.URL.Path)
	}
}

func TestDefaultUserAgent(t *testing.T) {
	c, _ := New(testToken)
	if c.UserAgent() != "orbits-go/"+Version {
		t.Errorf("UserAgent = %q", c.UserAgent())
	}
	c, _ = New(testToken, WithUserAgent("orbits-mcp/1.0.0"), WithUserAgent(" "), WithUserAgent("host/2"))
	if c.UserAgent() != "orbits-mcp/1.0.0 host/2 orbits-go/"+Version {
		t.Errorf("UserAgent = %q", c.UserAgent())
	}
}

func TestAuthorizationCannotBeReplaced(t *testing.T) {
	rec := newRecorder(t, ok(`{}`))
	c := newTestClient(t, rec.srv.URL, WithHeader("Authorization", "Bearer other"))
	_ = c.Do(context.Background(), "GET", "/me", nil, nil, nil, WithRequestHeader("Authorization", "Bearer x"))
	if got := rec.request(0).Header.Get("Authorization"); got != "Bearer "+testToken {
		t.Errorf("Authorization = %q", got)
	}
}

var uuidV4 = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func TestIdempotencyKey(t *testing.T) {
	tests := []struct {
		method string
		opts   []RequestOption
		want   string // "uuid", "" or a literal key
	}{
		{"POST", nil, "uuid"},
		{"POST", []RequestOption{WithIdempotencyKey("deploy-1234")}, "deploy-1234"},
		{"GET", nil, ""},
		{"PATCH", nil, ""},
		{"PUT", nil, ""},
		{"DELETE", nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			rec := newRecorder(t, ok(`{}`))
			c := newTestClient(t, rec.srv.URL)
			if err := c.Do(context.Background(), tt.method, "/x", nil, nil, nil, tt.opts...); err != nil {
				t.Fatal(err)
			}
			got := rec.request(0).Header.Get("Idempotency-Key")
			switch tt.want {
			case "uuid":
				if !uuidV4.MatchString(got) {
					t.Errorf("Idempotency-Key %q is not a UUIDv4", got)
				}
			default:
				if got != tt.want {
					t.Errorf("Idempotency-Key = %q, want %q", got, tt.want)
				}
			}
		})
	}
}

func TestDoBodyAndQuery(t *testing.T) {
	rec := newRecorder(t, ok(`{"id":"prj_1","name":"web"}`))
	c := newTestClient(t, rec.srv.URL)
	var out struct{ ID, Name string }
	err := c.Do(context.Background(), "post", "teams/acme/projects", map[string][]string{"q": {"a b"}}, map[string]string{"name": "web"}, &out)
	if err != nil {
		t.Fatal(err)
	}
	r := rec.request(0)
	if r.Method != "POST" || r.URL.Path != "/v1/teams/acme/projects" || r.URL.Query().Get("q") != "a b" {
		t.Errorf("sent %s %s", r.Method, r.URL)
	}
	if r.Header.Get("Content-Type") != "application/json" || rec.body(0) != `{"name":"web"}` {
		t.Errorf("body %q (%s)", rec.body(0), r.Header.Get("Content-Type"))
	}
	if out.ID != "prj_1" {
		t.Errorf("decoded %+v", out)
	}
}

func TestDoRawBodies(t *testing.T) {
	rec := newRecorder(t, ok(`raw bytes`))
	c := newTestClient(t, rec.srv.URL)
	var raw []byte
	if err := c.Do(context.Background(), "POST", "/x", nil, json.RawMessage(`{"a":1}`), &raw); err != nil {
		t.Fatal(err)
	}
	if string(raw) != "raw bytes" || rec.body(0) != `{"a":1}` {
		t.Errorf("raw = %q, sent %q", raw, rec.body(0))
	}
}

func TestPathEscaping(t *testing.T) {
	rec := newRecorder(t, ok(`{}`))
	c := newTestClient(t, rec.srv.URL)
	if _, err := c.Env.Delete(context.Background(), "my team", "web/app", "A?B"); err != nil {
		t.Fatal(err)
	}
	if got := rec.request(0).URL.EscapedPath(); got != "/v1/teams/my%20team/projects/web%2Fapp/env/A%3FB" {
		t.Errorf("path %q", got)
	}
}

func TestTokenNotPrinted(t *testing.T) {
	c, _ := New(testToken)
	for _, format := range []string{"%v", "%+v", "%#v"} {
		if s := fmt.Sprintf(format, c); strings.Contains(s, testToken) {
			t.Errorf("%s prints the token", format)
		}
	}
	rec := newRecorder(t, status(http.StatusUnauthorized, `{"title":"Unauthorized","status":401}`))
	tc := newTestClient(t, rec.srv.URL)
	err := tc.Do(context.Background(), "GET", "/me", nil, nil, nil)
	if err == nil || strings.Contains(err.Error(), testToken) {
		t.Errorf("err = %v", err)
	}
}

func TestContextCancelled(t *testing.T) {
	rec := newRecorder(t, ok(`{}`))
	c := newTestClient(t, rec.srv.URL)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := c.Do(ctx, "GET", "/me", nil, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "context canceled") {
		t.Errorf("err = %v", err)
	}
}

func TestResponseTooLarge(t *testing.T) {
	big := `"` + strings.Repeat("a", maxResponseBytes) + `"`
	rec := newRecorder(t, ok(big))
	c := newTestClient(t, rec.srv.URL)
	var out string
	if err := c.Do(context.Background(), "GET", "/x", nil, nil, &out); err == nil || !strings.Contains(err.Error(), "larger than") {
		t.Errorf("err = %v", err)
	}
}
