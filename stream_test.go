package orbits

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func lineEvent(n int, text string) string {
	return fmt.Sprintf("id: %d\nevent: line\ndata: {\"n\":%d,\"at\":\"2026-11-10T09:30:0%dZ\",\"step\":\"building\",\"tone\":\"plain\",\"text\":%q}\n\n", n, n, n%10, text)
}

const stepsEvent = "event: steps\ndata: [{\"key\":\"queued\",\"state\":\"done\",\"started_at\":\"2026-11-10T09:30:00Z\",\"finished_at\":\"2026-11-10T09:30:01Z\",\"seconds\":1}]\n\n"

const doneReady = "event: done\ndata: {\"state\":\"ready\",\"failure\":null,\"duration_seconds\":42}\n\n"

// streamServer answers each connection with the next script, recording the Last-Event-ID and after it was sent.
type streamServer struct {
	mu      sync.Mutex
	scripts []string
	last    []string
	after   []string
	srv     *httptest.Server
}

func newStreamServer(t *testing.T, scripts ...string) *streamServer {
	t.Helper()
	s := &streamServer{scripts: scripts}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		n := len(s.last)
		s.last = append(s.last, r.Header.Get("Last-Event-ID"))
		s.after = append(s.after, r.URL.Query().Get("after"))
		script := ""
		if n < len(s.scripts) {
			script = s.scripts[n]
		}
		s.mu.Unlock()
		if strings.HasPrefix(script, "status:") {
			var code int
			fmt.Sscanf(script, "status:%d", &code)
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(code)
			fmt.Fprintf(w, `{"status":%d,"title":"%s","detail":"Deployment not found"}`, code, http.StatusText(code))
			return
		}
		if r.Header.Get("Accept") != "text/event-stream" {
			t.Errorf("Accept = %q", r.Header.Get("Accept"))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		fmt.Fprint(w, script)
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func collect(t *testing.T, c *Client, opts *StreamLogsOptions) ([]LogEvent, error) {
	t.Helper()
	var events []LogEvent
	for ev, err := range c.Deployments.StreamLogs(context.Background(), "acme", "web", "dpl_test1", opts) {
		if err != nil {
			return events, err
		}
		events = append(events, ev)
	}
	return events, nil
}

func TestStreamLogs(t *testing.T) {
	s := newStreamServer(t, ": keep-alive\n\n"+lineEvent(1, "Installing")+stepsEvent+lineEvent(2, "Building")+"event: future\ndata: {}\n\n"+doneReady)
	c := newTestClient(t, s.srv.URL)
	events, err := collect(t, c, nil)
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, ev := range events {
		kinds = append(kinds, string(ev.Type))
	}
	if strings.Join(kinds, ",") != "line,steps,line,done" {
		t.Fatalf("events %v", kinds)
	}
	if events[0].Line.N != 1 || events[0].Line.Text != "Installing" || events[0].Line.Step != DeploymentStepKeyBuilding {
		t.Errorf("line %+v", events[0].Line)
	}
	if len(events[1].Steps) != 1 || events[1].Steps[0].Key != DeploymentStepKeyQueued {
		t.Errorf("steps %+v", events[1].Steps)
	}
	if d := events[3].Done; d.State != DeploymentStateReady || *d.DurationSeconds != 42 {
		t.Errorf("done %+v", d)
	}
}

func TestStreamLogsReconnects(t *testing.T) {
	// The first connection drops after line 2; the second carries on from line 3 (and repeats line 2, which is
	// dropped); the third finishes.
	s := newStreamServer(t,
		lineEvent(1, "a")+lineEvent(2, "b"),
		lineEvent(2, "b")+lineEvent(3, "c"),
		lineEvent(4, "d")+doneReady,
	)
	c := newTestClient(t, s.srv.URL)
	events, err := collect(t, c, &StreamLogsOptions{After: 0})
	if err != nil {
		t.Fatal(err)
	}
	var texts []string
	for _, ev := range events {
		if ev.Line != nil {
			texts = append(texts, ev.Line.Text)
		}
	}
	if strings.Join(texts, "") != "abcd" || events[len(events)-1].Type != LogEventDone {
		t.Errorf("lines %v", texts)
	}
	if strings.Join(s.last, ",") != ",2,3" {
		t.Errorf("Last-Event-ID sent: %q", s.last)
	}
}

func TestStreamLogsStartsAfter(t *testing.T) {
	s := newStreamServer(t, lineEvent(6, "f")+doneReady)
	c := newTestClient(t, s.srv.URL)
	if _, err := collect(t, c, &StreamLogsOptions{After: 5}); err != nil {
		t.Fatal(err)
	}
	if s.after[0] != "5" || s.last[0] != "" {
		t.Errorf("after %q, Last-Event-ID %q", s.after[0], s.last[0])
	}
}

func TestStreamLogsNotFound(t *testing.T) {
	s := newStreamServer(t, "status:404")
	c := newTestClient(t, s.srv.URL)
	_, err := collect(t, c, nil)
	if !IsNotFound(err) {
		t.Errorf("err = %v", err)
	}
	if len(s.last) != 1 {
		t.Errorf("%d connections for a 404", len(s.last))
	}
}

func TestStreamLogsGivesUp(t *testing.T) {
	s := newStreamServer(t, "", "", "", "")
	c := newTestClient(t, s.srv.URL)
	_, err := collect(t, c, &StreamLogsOptions{MaxReconnects: 2})
	if err == nil || !strings.Contains(err.Error(), "ended early") {
		t.Errorf("err = %v", err)
	}
	if len(s.last) != 3 {
		t.Errorf("%d connections, want 3", len(s.last))
	}
}

func TestStreamLogsRetriesUnavailable(t *testing.T) {
	s := newStreamServer(t, "status:503", lineEvent(1, "a")+doneReady)
	c := newTestClient(t, s.srv.URL)
	events, err := collect(t, c, nil)
	if err != nil || len(events) != 2 {
		t.Errorf("events %d, err %v", len(events), err)
	}
}

func TestStreamLogsScreenshot(t *testing.T) {
	script := lineEvent(1, "a") + doneReady + "event: screenshot\ndata: {\"screenshot_url\":\"/v1/teams/acme/projects/web/deployments/dpl_test1/screenshot\"}\n\n"
	s := newStreamServer(t, script, script)
	c := newTestClient(t, s.srv.URL)
	events, _ := collect(t, c, nil)
	if events[len(events)-1].Type != LogEventDone {
		t.Errorf("without WaitForScreenshot the stream should end at done")
	}
	events, err := collect(t, c, &StreamLogsOptions{WaitForScreenshot: true})
	if err != nil {
		t.Fatal(err)
	}
	last := events[len(events)-1]
	if last.Type != LogEventScreenshot || !strings.HasSuffix(last.ScreenshotURL, "/screenshot") {
		t.Errorf("last event %+v", last)
	}
}

func TestStreamLogsStopsWhenAsked(t *testing.T) {
	s := newStreamServer(t, lineEvent(1, "a")+lineEvent(2, "b")+doneReady)
	c := newTestClient(t, s.srv.URL)
	n := 0
	for range c.Deployments.StreamLogs(context.Background(), "acme", "web", "dpl_test1", nil) {
		n++
		break
	}
	if n != 1 || len(s.last) != 1 {
		t.Errorf("n = %d, connections %d", n, len(s.last))
	}
}

func TestStreamLogsIdleTimeout(t *testing.T) {
	old := streamIdleTimeout
	streamIdleTimeout = 50 * time.Millisecond
	defer func() { streamIdleTimeout = old }()
	release := make(chan struct{})
	var mu sync.Mutex
	conns := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		conns++
		n := conns
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		if n == 1 {
			fmt.Fprint(w, lineEvent(1, "a"))
			w.(http.Flusher).Flush()
			select { // then go quiet
			case <-r.Context().Done():
			case <-release:
			}
			return
		}
		fmt.Fprint(w, lineEvent(2, "b")+doneReady)
	}))
	defer srv.Close()
	defer close(release)
	c := newTestClient(t, srv.URL)
	events, err := collect(t, c, nil)
	if err != nil || len(events) != 3 {
		t.Fatalf("events %d, err %v", len(events), err)
	}
}

func TestReadEvents(t *testing.T) {
	in := "retry: 100\r\n: comment\r\nid: 7\r\nevent: line\r\ndata: one\r\ndata: two\r\n\r\ndata: plain\n\n"
	var got []sseEvent
	for ev, err := range readEvents(strings.NewReader(in)) {
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, ev)
	}
	if len(got) != 2 || got[0].name != "line" || got[0].id != "7" || got[0].data != "one\ntwo" || got[1].name != "message" {
		t.Errorf("%+v", got)
	}
}
