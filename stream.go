package orbits

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// LogEventType is the kind of a build log stream event.
type LogEventType string

// Build log stream events.
const (
	// LogEventLine is one build log line, in Line.
	LogEventLine LogEventType = "line"
	// LogEventSteps is the six build steps, in Steps, sent whenever one changes.
	LogEventSteps LogEventType = "steps"
	// LogEventDone is the end of the build, in Done, after every line.
	LogEventDone LogEventType = "done"
	// LogEventScreenshot is the deployment's screenshot being saved, in ScreenshotURL; sent after done only when
	// StreamLogsOptions.WaitForScreenshot is set.
	LogEventScreenshot LogEventType = "screenshot"
)

// LogEvent is one event of a deployment's build log stream. Type says which field is set.
type LogEvent struct {
	Type          LogEventType
	Line          *DeploymentLogLine
	Steps         []DeploymentStep
	Done          *LogStreamDone
	ScreenshotURL string
}

// LogStreamDone is how a build ended.
type LogStreamDone struct {
	State           DeploymentState    `json:"state"`
	Failure         *DeploymentFailure `json:"failure"`
	DurationSeconds *int               `json:"duration_seconds"`
}

// StreamLogsOptions tune StreamLogs. The zero value (or nil) starts at the first line, ends at done, and
// reconnects up to 5 times in a row.
type StreamLogsOptions struct {
	// After starts the stream after this line number (a line's N), for a caller that already has lines.
	After int
	// WaitForScreenshot keeps reading after done, for up to two minutes, until the screenshot event or the
	// stream's end. It is never sent for a failed or canceled build, or for a worker.
	WaitForScreenshot bool
	// MaxReconnects is how many times in a row a dropped stream is reopened before StreamLogs gives up with an
	// error. 0 means 5; a negative value never reconnects.
	MaxReconnects int
}

// streamIdleTimeout ends a connection that has sent nothing, not even its 15-second keep-alive, for this long;
// the stream is then reopened.
var streamIdleTimeout = 45 * time.Second

// StreamLogs follows a deployment's build log over server-sent events: GET
// /teams/{team}/projects/{project}/deployments/{deployment}/logs/stream. It yields lines oldest first, the steps
// whenever they change, and done at the end, then stops. A finished build yields its remaining lines and done at
// once.
//
// A dropped connection (the API closes streams after 30 minutes, and proxies sooner) is reopened from the last
// line received, without gaps or repeats. An error is yielded once, and ends the stream: an *Error for an API
// answer (such as 404), or the connection error after MaxReconnects failed tries. Stop ranging to close it.
func (s *DeploymentsService) StreamLogs(ctx context.Context, team, project, deployment string, opts *StreamLogsOptions) iter.Seq2[LogEvent, error] {
	o := deref(opts)
	maxReconnects := o.MaxReconnects
	if maxReconnects == 0 {
		maxReconnects = 5
	}
	path := projectPath(team, project, "deployments", esc(deployment), "logs", "stream")
	return func(yield func(LogEvent, error) bool) {
		last := o.After
		failures := 0
		doneSeen := false
		for attempt := 0; ; attempt++ {
			if attempt > 0 {
				if maxReconnects < 0 || failures > maxReconnects {
					return
				}
				if err := s.c.sleep(ctx, s.c.backoff(min(failures, 4), 0)); err != nil {
					return
				}
			}
			res, err := s.openStream(ctx, path, last, attempt)
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				var apiErr *Error
				failures++
				if e, ok := AsError(err); ok {
					apiErr = e
				}
				retryable := apiErr == nil || shouldRetry(true, apiErr.Status, nil)
				if !retryable || maxReconnects < 0 || failures > maxReconnects {
					yield(LogEvent{}, err)
					return
				}
				continue
			}
			stop, gotEvent, err := s.readStream(ctx, res, &last, &doneSeen, o.WaitForScreenshot, yield)
			res.Body.Close()
			if stop || ctx.Err() != nil {
				return
			}
			if doneSeen {
				// The stream closed after done without a screenshot: there is nothing more to wait for.
				return
			}
			if gotEvent {
				failures = 0
			}
			failures++
			if maxReconnects < 0 || failures > maxReconnects {
				if err == nil {
					err = io.ErrUnexpectedEOF
				}
				yield(LogEvent{}, fmt.Errorf("orbits: the build log stream ended early: %w", err))
				return
			}
		}
	}
}

// idleReader cancels its request when no bytes arrive for streamIdleTimeout.
type idleReader struct {
	r     io.Reader
	timer *time.Timer
}

func (r *idleReader) Read(p []byte) (int, error) {
	n, err := r.r.Read(p)
	if n > 0 {
		r.timer.Reset(streamIdleTimeout)
	}
	return n, err
}

func (s *DeploymentsService) openStream(ctx context.Context, path string, last, attempt int) (*http.Response, error) {
	extra := http.Header{"Accept": {"text/event-stream"}, "Cache-Control": {"no-cache"}}
	target := s.c.baseURL + path
	if last > 0 {
		if attempt == 0 {
			target += "?" + url.Values{"after": {strconv.Itoa(last)}}.Encode()
		} else {
			extra.Set("Last-Event-ID", strconv.Itoa(last))
		}
	}
	connCtx, cancel := context.WithCancel(ctx)
	req, err := s.c.newRequest(connCtx, http.MethodGet, target, nil, "", extra)
	if err != nil {
		cancel()
		return nil, err
	}
	res, err := s.c.streamClient().Do(req)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("orbits: GET %s: %w", path, err)
	}
	if res.StatusCode < 200 || res.StatusCode > 299 {
		body, _ := readCapped(res.Body)
		res.Body.Close()
		cancel()
		return nil, newError(res.StatusCode, res.Header, body)
	}
	timer := time.AfterFunc(streamIdleTimeout, cancel)
	res.Body = &streamBody{Reader: &idleReader{r: res.Body, timer: timer}, closer: res.Body, stop: func() { timer.Stop(); cancel() }}
	return res, nil
}

type streamBody struct {
	io.Reader
	closer io.Closer
	stop   func()
}

func (b *streamBody) Close() error {
	b.stop()
	return b.closer.Close()
}

// readStream reads one connection's events. stop is true when the caller stopped ranging, an error was yielded,
// or the stream is complete.
func (s *DeploymentsService) readStream(ctx context.Context, res *http.Response, last *int, doneSeen *bool, waitScreenshot bool, yield func(LogEvent, error) bool) (stop, gotEvent bool, err error) {
	for ev, err := range readEvents(res.Body) {
		if err != nil {
			return false, gotEvent, err
		}
		gotEvent = true
		var out LogEvent
		switch ev.name {
		case "line":
			var line DeploymentLogLine
			if err := json.Unmarshal([]byte(ev.data), &line); err != nil {
				yield(LogEvent{}, fmt.Errorf("orbits: decoding a build log line: %w", err))
				return true, gotEvent, nil
			}
			if line.N <= *last && *last > 0 {
				continue // already yielded before a reconnect
			}
			*last = line.N
			out = LogEvent{Type: LogEventLine, Line: &line}
		case "steps":
			var steps []DeploymentStep
			if err := json.Unmarshal([]byte(ev.data), &steps); err != nil {
				yield(LogEvent{}, fmt.Errorf("orbits: decoding the build steps: %w", err))
				return true, gotEvent, nil
			}
			out = LogEvent{Type: LogEventSteps, Steps: steps}
		case "done":
			var done LogStreamDone
			if err := json.Unmarshal([]byte(ev.data), &done); err != nil {
				yield(LogEvent{}, fmt.Errorf("orbits: decoding the end of the build: %w", err))
				return true, gotEvent, nil
			}
			*doneSeen = true
			if !yield(LogEvent{Type: LogEventDone, Done: &done}, nil) {
				return true, gotEvent, nil
			}
			if !waitScreenshot || done.State != DeploymentStateReady {
				return true, gotEvent, nil
			}
			continue
		case "screenshot":
			var shot struct {
				ScreenshotURL string `json:"screenshot_url"`
			}
			if err := json.Unmarshal([]byte(ev.data), &shot); err != nil {
				yield(LogEvent{}, fmt.Errorf("orbits: decoding the screenshot event: %w", err))
				return true, gotEvent, nil
			}
			yield(LogEvent{Type: LogEventScreenshot, ScreenshotURL: shot.ScreenshotURL}, nil)
			return true, gotEvent, nil
		default:
			continue // an event this SDK does not know yet
		}
		if !yield(out, nil) {
			return true, gotEvent, nil
		}
	}
	return false, gotEvent, ctx.Err()
}

type sseEvent struct {
	name string
	id   string
	data string
}

// readEvents parses a text/event-stream body into events. Comments (such as ": keep-alive") are skipped.
func readEvents(r io.Reader) iter.Seq2[sseEvent, error] {
	return func(yield func(sseEvent, error) bool) {
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 64<<10), 4<<20)
		var ev sseEvent
		var data []string
		for sc.Scan() {
			line := strings.TrimSuffix(sc.Text(), "\r")
			switch {
			case line == "":
				if len(data) > 0 || ev.name != "" {
					ev.data = strings.Join(data, "\n")
					if ev.name == "" {
						ev.name = "message"
					}
					if !yield(ev, nil) {
						return
					}
				}
				ev, data = sseEvent{}, nil
			case strings.HasPrefix(line, ":"):
			default:
				field, value, _ := strings.Cut(line, ":")
				value = strings.TrimPrefix(value, " ")
				switch field {
				case "event":
					ev.name = value
				case "data":
					data = append(data, value)
				case "id":
					ev.id = value
				}
			}
		}
		if err := sc.Err(); err != nil {
			yield(sseEvent{}, err)
		}
	}
}
