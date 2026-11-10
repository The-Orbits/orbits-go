package orbits

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
)

// writeTree creates files (path -> content) under a new temporary folder.
func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func zipNames(t *testing.T, data []byte) []string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	sort.Strings(names)
	return names
}

func TestZipDir(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"package.json":                 `{"name":"web"}`,
		"src/index.js":                 "export {}",
		"node_modules/dep/index.js":    "x",
		"packages/a/node_modules/b.js": "x",
		".git/HEAD":                    "ref",
		".next/cache":                  "x",
		"__MACOSX/._file":              "x",
		".DS_Store":                    "x",
		".orbits/project.json":         "{}",
		"dist/out.js":                  "x",
		"logs/a.log":                   "x",
		"keep.log":                     "x",
		".orbitsignore":                "# build output\ndist/\n*.log\n!keep.log\n",
	})
	var buf bytes.Buffer
	var seen []string
	stats, err := ZipDir(&buf, dir, &ZipOptions{Ignore: []string{"packages"}, OnFile: func(p string, _ int64) { seen = append(seen, p) }})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{".orbitsignore", "keep.log", "package.json", "src/index.js"}
	if got := zipNames(t, buf.Bytes()); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("zip has %v, want %v", got, want)
	}
	if stats.Files != 4 || len(seen) != 4 {
		t.Errorf("stats %+v, OnFile %v", stats, seen)
	}
}

func TestZipDirRefuses(t *testing.T) {
	t.Run("link", func(t *testing.T) {
		dir := writeTree(t, map[string]string{"a.txt": "a"})
		if err := os.Symlink("a.txt", filepath.Join(dir, "link.txt")); err != nil {
			t.Skip("no symlinks here:", err)
		}
		_, err := ZipDir(io.Discard, dir, nil)
		if !errors.Is(err, ErrUnsupportedFile) || !strings.Contains(err.Error(), "link.txt") {
			t.Errorf("err = %v", err)
		}
		// Ignoring it is the way out.
		if _, err := ZipDir(io.Discard, dir, &ZipOptions{Ignore: []string{"link.txt"}}); err != nil {
			t.Errorf("ignored link: %v", err)
		}
	})
	t.Run("empty", func(t *testing.T) {
		dir := writeTree(t, map[string]string{"node_modules/x.js": "x"})
		if _, err := ZipDir(io.Discard, dir, nil); !errors.Is(err, ErrNothingToUpload) {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("not a folder", func(t *testing.T) {
		dir := writeTree(t, map[string]string{"a.txt": "a"})
		if _, err := ZipDir(io.Discard, filepath.Join(dir, "a.txt"), nil); !errors.Is(err, ErrNotAFolder) {
			t.Errorf("err = %v", err)
		}
	})
}

// uploadFake is the API's upload routes and a storage server.
type uploadFake struct {
	mu          sync.Mutex
	api         *httptest.Server
	storage     *httptest.Server
	paths       []string
	putHeaders  http.Header
	putBody     []byte
	putStatus   []int
	incomplete  int // how many completes answer 409 upload_incomplete first
	cancelled   bool
	startBody   string
	putAttempts int
	keys        []string // each API call's Idempotency-Key, in order
}

func newUploadFake(t *testing.T) *uploadFake {
	t.Helper()
	f := &uploadFake{}
	f.storage = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		defer f.mu.Unlock()
		f.putAttempts++
		f.putHeaders = r.Header.Clone()
		f.putBody = body
		if len(f.putStatus) > 0 {
			code := f.putStatus[0]
			f.putStatus = f.putStatus[1:]
			w.WriteHeader(code)
			if code >= 400 {
				io.WriteString(w, "<Error><Code>X</Code><Message>Signature does not match</Message></Error>")
			}
			return
		}
		if r.Method != http.MethodPut || r.ContentLength != int64(len(body)) {
			w.WriteHeader(400)
		}
	}))
	t.Cleanup(f.storage.Close)
	f.api = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		defer f.mu.Unlock()
		f.paths = append(f.paths, r.Method+" "+r.URL.Path)
		f.keys = append(f.keys, r.Header.Get("Idempotency-Key"))
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/uploads"):
			f.startBody = string(body)
			w.WriteHeader(201)
			io.WriteString(w, strings.Replace(fixture(t, "startSourceUpload"), "https://storage.example.com", f.storage.URL, 1))
		case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/complete"):
			if f.incomplete > 0 {
				f.incomplete--
				w.WriteHeader(409)
				io.WriteString(w, fixture(t, "problem.upload-incomplete"))
				return
			}
			w.WriteHeader(202)
			io.WriteString(w, fixture(t, "completeSourceUpload"))
		case r.Method == "DELETE":
			f.cancelled = true
			w.WriteHeader(204)
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(f.api.Close)
	return f
}

func TestUploadDir(t *testing.T) {
	f := newUploadFake(t)
	f.incomplete = 2
	c := newTestClient(t, f.api.URL)
	dir := writeTree(t, map[string]string{"index.html": "<h1>hello</h1>", "node_modules/x.js": "x"})
	var sent int64
	d, err := c.Deployments.UploadDir(context.Background(), "acme", "web", dir, &UploadOptions{
		Filename:   "site.zip",
		OnProgress: func(s, total int64) { sent = s },
	})
	if err != nil {
		t.Fatal(err)
	}
	if d.ID != "dpl_test1" || d.State != DeploymentStateQueued {
		t.Errorf("deployment %+v", d)
	}
	want := []string{
		"POST /v1/teams/acme/projects/web/uploads",
		"POST /v1/teams/acme/projects/web/uploads/upl_test1/complete",
		"POST /v1/teams/acme/projects/web/uploads/upl_test1/complete",
		"POST /v1/teams/acme/projects/web/uploads/upl_test1/complete",
	}
	if strings.Join(f.paths, "\n") != strings.Join(want, "\n") {
		t.Errorf("API calls:\n%s", strings.Join(f.paths, "\n"))
	}
	if !strings.Contains(f.startBody, `"filename":"site.zip"`) || !strings.Contains(f.startBody, `"size":`) {
		t.Errorf("start body %s", f.startBody)
	}
	// Storage gets exactly the signed headers: no token, no User-Agent.
	if f.putHeaders.Get("Authorization") != "" || f.putHeaders.Get("Content-Type") != "application/zip" || f.putHeaders.Get("User-Agent") != "" {
		t.Errorf("PUT headers %v", f.putHeaders)
	}
	if got := zipNames(t, f.putBody); strings.Join(got, ",") != "index.html" {
		t.Errorf("zip has %v", got)
	}
	if sent != int64(len(f.putBody)) {
		t.Errorf("progress reported %d of %d bytes", sent, len(f.putBody))
	}
}

func TestUploadIdempotencyKey(t *testing.T) {
	f := newUploadFake(t)
	c := newTestClient(t, f.api.URL)
	dir := writeTree(t, map[string]string{"index.html": "x"})
	if _, err := c.Deployments.UploadDir(context.Background(), "acme", "web", dir, &UploadOptions{IdempotencyKey: "deploy-key-1"}); err != nil {
		t.Fatal(err)
	}
	if len(f.keys) != 2 || f.keys[0] != "deploy-key-1" {
		t.Fatalf("keys %q", f.keys)
	}
	// Completing gets a key of its own, so asking again after upload_incomplete is not a replay of the first answer.
	if f.keys[1] == "" || f.keys[1] == "deploy-key-1" {
		t.Errorf("complete key %q", f.keys[1])
	}
}

func TestUploadRetriesStorage(t *testing.T) {
	f := newUploadFake(t)
	f.putStatus = []int{503}
	c := newTestClient(t, f.api.URL)
	dir := writeTree(t, map[string]string{"index.html": "x"})
	if _, err := c.Deployments.UploadDir(context.Background(), "acme", "web", dir, nil); err != nil {
		t.Fatal(err)
	}
	if f.putAttempts != 2 || f.cancelled {
		t.Errorf("%d PUTs, cancelled %v", f.putAttempts, f.cancelled)
	}
}

func TestUploadCancelsWhenStorageRefuses(t *testing.T) {
	f := newUploadFake(t)
	f.putStatus = []int{403}
	c := newTestClient(t, f.api.URL)
	dir := writeTree(t, map[string]string{"index.html": "x"})
	_, err := c.Deployments.UploadDir(context.Background(), "acme", "web", dir, nil)
	if err == nil || !strings.Contains(err.Error(), "Signature does not match") {
		t.Fatalf("err = %v", err)
	}
	if !f.cancelled || f.putAttempts != 1 {
		t.Errorf("cancelled %v after %d PUTs", f.cancelled, f.putAttempts)
	}
	if strings.Contains(err.Error(), "X-Amz-Signature") {
		t.Error("the presigned URL is in the error")
	}
}

func TestUploadGitProject(t *testing.T) {
	rec := newRecorder(t, status(409, `{"status":409,"title":"Conflict","code":"not_an_upload_project","detail":"This project deploys from Git."}`))
	c := newTestClient(t, rec.srv.URL)
	dir := writeTree(t, map[string]string{"index.html": "x"})
	_, err := c.Deployments.UploadDir(context.Background(), "acme", "web", dir, nil)
	if !IsCode(err, "not_an_upload_project") {
		t.Errorf("err = %v", err)
	}
}

func TestUploadZipLimits(t *testing.T) {
	c, _ := New(testToken)
	if _, err := c.Deployments.UploadZip(context.Background(), "acme", "web", bytes.NewReader(nil), MaxUploadBytes+1, nil); !errors.Is(err, ErrUploadTooLarge) {
		t.Errorf("err = %v", err)
	}
	if _, err := c.Deployments.UploadZip(context.Background(), "acme", "web", bytes.NewReader(nil), 0, nil); !errors.Is(err, ErrNothingToUpload) {
		t.Errorf("err = %v", err)
	}
}
