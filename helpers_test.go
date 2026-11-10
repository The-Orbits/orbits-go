package orbits

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/The-Orbits/orbits-go/internal/schema"
)

const testToken = "orb_live_0123456789abcdef0123456789abcdef"

// newTestClient returns a client for a test server whose waits and retry backoffs return at once.
func newTestClient(t *testing.T, serverURL string, opts ...Option) *Client {
	t.Helper()
	opts = append([]Option{WithBaseURL(serverURL + "/v1")}, opts...)
	c, err := New(testToken, opts...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c.sleep = func(ctx context.Context, d time.Duration) error { return ctx.Err() }
	c.jitter = func(d time.Duration) time.Duration { return d }
	return c
}

// recorder is a test server that answers each request with the next canned response and records what it was
// sent. After the last response it repeats the last one.
type recorder struct {
	mu        sync.Mutex
	requests  []*http.Request
	bodies    []string
	responses []canned
	srv       *httptest.Server
}

type canned struct {
	status int
	body   string
	header map[string]string
}

func newRecorder(t *testing.T, responses ...canned) *recorder {
	t.Helper()
	r := &recorder{responses: responses}
	r.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		buf, _ := io.ReadAll(req.Body)
		r.mu.Lock()
		r.requests = append(r.requests, req)
		r.bodies = append(r.bodies, string(buf))
		resp := canned{status: 200, body: `{}`}
		if n := len(r.requests) - 1; n < len(r.responses) {
			resp = r.responses[n]
		} else if len(r.responses) > 0 {
			resp = r.responses[len(r.responses)-1]
		}
		r.mu.Unlock()
		for k, v := range resp.header {
			w.Header().Set(k, v)
		}
		if w.Header().Get("Content-Type") == "" {
			w.Header().Set("Content-Type", "application/json")
		}
		w.WriteHeader(resp.status)
		_, _ = w.Write([]byte(resp.body))
	}))
	t.Cleanup(r.srv.Close)
	return r
}

func (r *recorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.requests)
}

func (r *recorder) request(i int) *http.Request {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.requests[i]
}

func (r *recorder) body(i int) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.bodies[i]
}

func ok(body string) canned { return canned{status: 200, body: body} }

func status(code int, body string) canned { return canned{status: code, body: body} }

var (
	specOnce sync.Once
	specDoc  *schema.Spec
	specErr  error
)

// testSpec is the vendored spec, parsed once.
func testSpec(t *testing.T) *schema.Spec {
	t.Helper()
	specOnce.Do(func() { specDoc, specErr = schema.Load("testdata/openapi.yaml") })
	if specErr != nil {
		t.Fatal(specErr)
	}
	return specDoc
}

// fixture reads testdata/fixtures/<name>.json. Every fixture is checked against the spec by TestFixturesMatchSpec.
func fixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "fixtures", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// decodeAny decodes JSON as encoding/json does into interface values.
func decodeAny(t *testing.T, data string) any {
	t.Helper()
	var v any
	dec := json.NewDecoder(strings.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		t.Fatalf("decoding %q: %v", data, err)
	}
	return v
}
