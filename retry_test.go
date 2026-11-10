package orbits

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestRetryPolicy(t *testing.T) {
	tests := []struct {
		name      string
		method    string
		responses []canned
		wantCalls int
		wantErr   bool
	}{
		{"GET 503 then ok", "GET", []canned{status(503, ``), ok(`{}`)}, 2, false},
		{"GET 502, 504, ok", "GET", []canned{status(502, ``), status(504, ``), ok(`{}`)}, 3, false},
		{"GET 500 is not retried", "GET", []canned{status(500, ``)}, 1, true},
		{"GET 404 is not retried", "GET", []canned{status(404, `{"status":404}`)}, 1, true},
		{"GET gives up after the retries", "GET", []canned{status(503, ``)}, 1 + DefaultMaxRetries, true},
		{"POST with its key is retried", "POST", []canned{status(503, ``), ok(`{}`)}, 2, false},
		{"PUT is retried", "PUT", []canned{status(502, ``), ok(`{}`)}, 2, false},
		{"DELETE is retried", "DELETE", []canned{status(504, ``), ok(`{}`)}, 2, false},
		{"PATCH 503 is not retried", "PATCH", []canned{status(503, ``)}, 1, true},
		{"PATCH 429 is retried", "PATCH", []canned{status(429, ``), ok(`{}`)}, 2, false},
		{"POST 422 is not retried", "POST", []canned{status(422, `{"status":422}`)}, 1, true},
		{"POST 409 still running is asked again", "POST", []canned{{status: 409, body: `{"status":409}`, header: map[string]string{"Retry-After": "1"}}, ok(`{}`)}, 2, false},
		{"POST 409 conflict is not", "POST", []canned{status(409, `{"status":409}`)}, 1, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := newRecorder(t, tt.responses...)
			c := newTestClient(t, rec.srv.URL)
			err := c.Do(context.Background(), tt.method, "/x", nil, nil, nil)
			if (err != nil) != tt.wantErr {
				t.Errorf("err = %v", err)
			}
			if rec.count() != tt.wantCalls {
				t.Errorf("%d calls, want %d", rec.count(), tt.wantCalls)
			}
		})
	}
}

func TestRetryReusesIdempotencyKey(t *testing.T) {
	rec := newRecorder(t, status(503, ``), status(502, ``), ok(`{}`))
	c := newTestClient(t, rec.srv.URL)
	if err := c.Do(context.Background(), "POST", "/x", nil, map[string]int{"a": 1}, nil); err != nil {
		t.Fatal(err)
	}
	key := rec.request(0).Header.Get("Idempotency-Key")
	for i := 1; i < rec.count(); i++ {
		if got := rec.request(i).Header.Get("Idempotency-Key"); got != key {
			t.Errorf("attempt %d sent key %q, want %q", i, got, key)
		}
		if rec.body(i) != rec.body(0) {
			t.Errorf("attempt %d sent body %q", i, rec.body(i))
		}
	}
}

func TestRetryAfterIsHonored(t *testing.T) {
	rec := newRecorder(t, canned{status: 429, body: `{"status":429}`, header: map[string]string{"Retry-After": "7"}}, ok(`{}`))
	c := newTestClient(t, rec.srv.URL)
	var waits []time.Duration
	c.sleep = func(ctx context.Context, d time.Duration) error { waits = append(waits, d); return nil }
	if err := c.Do(context.Background(), "GET", "/x", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if len(waits) != 1 || waits[0] != 7*time.Second {
		t.Errorf("waited %v, want [7s]", waits)
	}
}

func TestRetryAfterIsCapped(t *testing.T) {
	c, _ := New(testToken)
	if d := c.backoff(0, 10*time.Minute); d != maxRetryAfter {
		t.Errorf("backoff = %v", d)
	}
}

func TestBackoffGrows(t *testing.T) {
	c, _ := New(testToken)
	c.jitter = func(d time.Duration) time.Duration { return d }
	want := []time.Duration{500 * time.Millisecond, time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 8 * time.Second}
	for i, w := range want {
		if d := c.backoff(i, 0); d != w {
			t.Errorf("attempt %d: %v, want %v", i, d, w)
		}
	}
	for i := 0; i < 100; i++ {
		if d := halfJitter(time.Second); d < 500*time.Millisecond || d > time.Second {
			t.Fatalf("halfJitter = %v", d)
		}
	}
}

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)
	tests := map[string]time.Duration{
		"":                              0,
		"3":                             3 * time.Second,
		"0":                             0,
		"-1":                            0,
		"soon":                          0,
		"Tue, 10 Nov 2026 09:00:30 GMT": 30 * time.Second,
		"Tue, 10 Nov 2026 08:00:00 GMT": 0,
	}
	for in, want := range tests {
		if got := parseRetryAfter(in, now); got != want {
			t.Errorf("parseRetryAfter(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestNetworkErrorsAreRetried(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			// Drop the connection without an answer.
			hj, _ := w.(http.Hijacker)
			conn, _, _ := hj.Hijack()
			conn.Close()
			return
		}
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	c := newTestClient(t, srv.URL)
	if err := c.Do(context.Background(), "POST", "/x", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Errorf("%d calls", calls.Load())
	}
	calls.Store(0)
	if err := c.Do(context.Background(), "PATCH", "/x", nil, nil, nil); err == nil {
		t.Error("a PATCH whose answer was lost was retried")
	}
}

func TestWithMaxRetriesAndNoRetry(t *testing.T) {
	rec := newRecorder(t, status(503, ``))
	c := newTestClient(t, rec.srv.URL, WithMaxRetries(1))
	_ = c.Do(context.Background(), "GET", "/x", nil, nil, nil)
	if rec.count() != 2 {
		t.Errorf("%d calls with WithMaxRetries(1)", rec.count())
	}
	_ = c.Do(context.Background(), "GET", "/x", nil, nil, nil, WithNoRetry())
	if rec.count() != 3 {
		t.Errorf("%d calls after WithNoRetry", rec.count())
	}
}
