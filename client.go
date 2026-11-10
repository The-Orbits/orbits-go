// Package orbits is the official Go SDK for the Orbits API (https://orbits.africa): projects, deployments and
// their logs, environment variables, domains and DNS, feature flags, workflows, sandboxes, alerts, observability,
// usage, activity and billing.
//
// Create a client with an API token (Team › Settings › API tokens), then call the typed services on it:
//
//	c, err := orbits.NewFromEnv() // reads ORBITS_TOKEN and ORBITS_API_URL
//	if err != nil { ... }
//	me, err := c.Me.Get(ctx)
//
// Every POST carries an Idempotency-Key that is reused when the request is retried. Errors from the API are
// *Error values built from its problem document; branch on them with IsNotFound, IsValidation, IsForbidden and
// the other helpers.
package orbits

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// Version is this SDK's version. It goes into the User-Agent.
const Version = "0.1.0"

// DefaultBaseURL is the production API. Every path is relative to it.
const DefaultBaseURL = "https://api.theorbits.app/v1"

// DefaultMaxRetries is how many times a request is retried after the first try, unless WithMaxRetries says
// otherwise.
const DefaultMaxRetries = 3

// Environment variables NewFromEnv reads.
const (
	EnvToken  = "ORBITS_TOKEN"
	EnvAPIURL = "ORBITS_API_URL"
)

const (
	// defaultTimeout bounds each attempt of an API call.
	defaultTimeout = 60 * time.Second

	// maxResponseBytes bounds how much of a response is read. Deployment screenshots are the largest bodies.
	maxResponseBytes = 32 << 20

	defaultPollInterval = 2 * time.Second
)

// Client talks to the Orbits API. It is safe for concurrent use. Create it with New or NewFromEnv.
type Client struct {
	Me            *MeService
	Teams         *TeamsService
	Projects      *ProjectsService
	Deployments   *DeploymentsService
	Env           *EnvService
	Domains       *DomainsService
	Flags         *FlagsService
	Workflows     *WorkflowsService
	Sandboxes     *SandboxesService
	Alerts        *AlertsService
	Observability *ObservabilityService
	Usage         *UsageService
	Activity      *ActivityService
	Billing       *BillingService
	Templates     *TemplatesService
	Operations    *OperationsService
	Integrations  *IntegrationsService

	baseURL    string
	userAgent  string
	maxRetries int
	headers    http.Header
	http       *http.Client

	// token is a function, not a string, so printing the Client with %+v shows a function address instead of
	// the token.
	token func() string

	// sleep and jitter are fields so tests can run retries and waits without waiting.
	sleep        func(ctx context.Context, d time.Duration) error
	jitter       func(d time.Duration) time.Duration
	pollInterval time.Duration
}

// Option configures a Client.
type Option func(*settings)

type settings struct {
	baseURL    string
	products   []string
	maxRetries int
	headers    http.Header
	http       *http.Client
}

// WithBaseURL sets the API's base URL, including the /v1 prefix, such as https://stagingapi.theorbits.app/v1.
// The default is DefaultBaseURL (NewFromEnv reads ORBITS_API_URL first).
func WithBaseURL(u string) Option {
	return func(s *settings) { s.baseURL = u }
}

// WithHTTPClient sets the HTTP client requests go through. Its Timeout bounds each attempt; the default client
// allows 60 seconds. Log streams and uploads use a copy of it without the timeout.
func WithHTTPClient(h *http.Client) Option {
	return func(s *settings) { s.http = h }
}

// WithUserAgent puts a product token before the SDK's own in the User-Agent, so the API can tell the tool apart:
// WithUserAgent("orbits-cli/0.1.0") sends "orbits-cli/0.1.0 orbits-go/0.1.0". It is what Settings › API tokens
// shows as a token's "last used by". It may be given more than once; the tokens are sent in that order.
func WithUserAgent(product string) Option {
	return func(s *settings) {
		if p := strings.TrimSpace(product); p != "" {
			s.products = append(s.products, p)
		}
	}
}

// WithMaxRetries sets how many times a failed request is retried after the first try. 0 turns retries off. The
// default is DefaultMaxRetries.
func WithMaxRetries(n int) Option {
	return func(s *settings) { s.maxRetries = max(n, 0) }
}

// WithHeader adds a header to every request. It is set after the SDK's own headers, so it can replace them
// (except Authorization, which is always the token).
func WithHeader(key, value string) Option {
	return func(s *settings) { s.headers.Set(key, value) }
}

// New returns a client that signs in with token (an API token, orb_live_…). It fails when token is empty or the
// base URL is not an http(s) URL. It does not read the environment; NewFromEnv does.
func New(token string, opts ...Option) (*Client, error) {
	s := settings{maxRetries: DefaultMaxRetries, headers: http.Header{}}
	for _, opt := range opts {
		opt(&s)
	}
	if s.baseURL == "" {
		s.baseURL = DefaultBaseURL
	}
	u, err := url.Parse(s.baseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("orbits: invalid base URL %q: use an http(s) URL such as %s", s.baseURL, DefaultBaseURL)
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, errors.New("orbits: no API token: create one under Team › Settings › API tokens and pass it to New, or set " + EnvToken)
	}
	if strings.ContainsAny(token, " \r\n\t") {
		return nil, errors.New("orbits: the API token contains spaces or line breaks")
	}
	if s.http == nil {
		s.http = &http.Client{Timeout: defaultTimeout}
	}
	c := &Client{
		baseURL:      strings.TrimRight(s.baseURL, "/"),
		userAgent:    strings.Join(append(s.products, "orbits-go/"+Version), " "),
		maxRetries:   s.maxRetries,
		headers:      s.headers,
		http:         s.http,
		token:        func() string { return token },
		sleep:        sleepContext,
		jitter:       halfJitter,
		pollInterval: defaultPollInterval,
	}
	c.Me = &MeService{c}
	c.Teams = &TeamsService{c}
	c.Projects = &ProjectsService{c}
	c.Deployments = &DeploymentsService{c}
	c.Env = &EnvService{c}
	c.Domains = &DomainsService{c}
	c.Flags = &FlagsService{c}
	c.Workflows = &WorkflowsService{c}
	c.Sandboxes = &SandboxesService{c}
	c.Alerts = &AlertsService{c}
	c.Observability = &ObservabilityService{c}
	c.Usage = &UsageService{c}
	c.Activity = &ActivityService{c}
	c.Billing = &BillingService{c}
	c.Templates = &TemplatesService{c}
	c.Operations = &OperationsService{c}
	c.Integrations = &IntegrationsService{c}
	return c, nil
}

// NewFromEnv returns a client configured from ORBITS_TOKEN and ORBITS_API_URL. Options apply after the
// environment, so WithBaseURL overrides ORBITS_API_URL.
func NewFromEnv(opts ...Option) (*Client, error) {
	if u := strings.TrimSpace(os.Getenv(EnvAPIURL)); u != "" {
		opts = append([]Option{WithBaseURL(u)}, opts...)
	}
	return New(os.Getenv(EnvToken), opts...)
}

// BaseURL is the base URL the client sends requests to.
func (c *Client) BaseURL() string { return c.baseURL }

// UserAgent is the User-Agent the client sends.
func (c *Client) UserAgent() string { return c.userAgent }

// RequestOption changes one call.
type RequestOption func(*requestOptions)

type requestOptions struct {
	idempotencyKey string
	noRetry        bool
	headers        http.Header
}

// WithIdempotencyKey sends key as the Idempotency-Key of a POST instead of a new random one, to make a change
// that is safe to repeat across processes. The API returns the first result for a key it has seen with the same
// body.
func WithIdempotencyKey(key string) RequestOption {
	return func(o *requestOptions) { o.idempotencyKey = key }
}

// WithRequestHeader sets a header on this call only. It is applied last, so it replaces any header the SDK or
// WithHeader set (except Authorization).
func WithRequestHeader(key, value string) RequestOption {
	return func(o *requestOptions) {
		if o.headers == nil {
			o.headers = http.Header{}
		}
		o.headers.Set(key, value)
	}
}

// WithNoRetry sends the call once, whatever the answer.
func WithNoRetry() RequestOption {
	return func(o *requestOptions) { o.noRetry = true }
}

// Do sends one API call and decodes the answer. Every typed method uses it, and it is exported for calls the
// typed services do not cover.
//
// path is relative to the base URL, such as "/teams/acme/projects"; query may be nil. body is nil or a value that
// marshals to JSON (a json.RawMessage is sent as is). out is nil or a pointer: a 2xx JSON body is decoded into it,
// and a *[]byte receives the raw body.
//
// A POST carries an Idempotency-Key, the same one on every retry. Connection errors and 429, 502, 503 and 504 are
// retried with jittered backoff, waiting for Retry-After when the API sends it. GET, HEAD, PUT, DELETE and POST
// (which has its key) are retried; a PATCH is retried only on 429, which the API answers before doing anything.
// A POST answered 409 with Retry-After (a request with its key is still running) is asked again after the wait.
// A non-2xx answer is returned as an *Error.
func (c *Client) Do(ctx context.Context, method, path string, query url.Values, body, out any, opts ...RequestOption) error {
	var ro requestOptions
	for _, opt := range opts {
		opt(&ro)
	}
	method = strings.ToUpper(method)
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	payload, err := encodeBody(body)
	if err != nil {
		return err
	}
	key := ro.idempotencyKey
	if key == "" && method == http.MethodPost {
		if key, err = newIdempotencyKey(); err != nil {
			return err
		}
	}
	target := c.baseURL + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	retryable := retryableMethod(method, key != "")

	for attempt := 0; ; attempt++ {
		res, err := c.send(ctx, method, target, payload, key, ro.headers)
		if err != nil && ctx.Err() != nil {
			// Cancelled or out of time: say so, not "connection failed".
			return fmt.Errorf("orbits: %s %s: %w", method, path, ctx.Err())
		}
		if err == nil && res.status >= 200 && res.status < 300 {
			return decodeBody(res.body, out)
		}
		var status int
		var retryAfter time.Duration
		if err == nil {
			status = res.status
			retryAfter = parseRetryAfter(res.header.Get("Retry-After"), time.Now())
		}
		// A 409 with Retry-After on a POST with a key is the API saying a request with that key is still running
		// (often this call's own lost first attempt): wait for it and ask again, to get its result.
		stillRunning := status == http.StatusConflict && key != "" && retryAfter > 0
		if !ro.noRetry && attempt < c.maxRetries && (shouldRetry(retryable, status, err) || stillRunning) {
			if err := c.sleep(ctx, c.backoff(attempt, retryAfter)); err != nil {
				return fmt.Errorf("orbits: %s %s: %w", method, path, err)
			}
			continue
		}
		if err != nil {
			return fmt.Errorf("orbits: %s %s: %w", method, path, err)
		}
		return newError(res.status, res.header, res.body)
	}
}

type response struct {
	status int
	header http.Header
	body   []byte
}

// send makes one attempt and reads the whole body, so the connection is free before the retry loop decides what
// to do.
func (c *Client) send(ctx context.Context, method, target string, payload []byte, idempotencyKey string, extra http.Header) (*response, error) {
	req, err := c.newRequest(ctx, method, target, payload, idempotencyKey, extra)
	if err != nil {
		return nil, err
	}
	res, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	data, err := readCapped(res.Body)
	if err != nil {
		return nil, err
	}
	return &response{status: res.StatusCode, header: res.Header, body: data}, nil
}

// newRequest builds a fresh request per attempt, since a body is read once.
func (c *Client) newRequest(ctx context.Context, method, target string, payload []byte, idempotencyKey string, extra http.Header) (*http.Request, error) {
	var body io.Reader
	if payload != nil {
		body = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return nil, fmt.Errorf("orbits: building the request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}
	for k, v := range c.headers {
		req.Header[k] = v
	}
	for k, v := range extra {
		req.Header[k] = v
	}
	req.Header.Set("Authorization", "Bearer "+c.token())
	return req, nil
}

var errTooLarge = fmt.Errorf("orbits: the response is larger than %d bytes", maxResponseBytes)

func readCapped(r io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxResponseBytes {
		return nil, errTooLarge
	}
	return data, nil
}

func encodeBody(body any) ([]byte, error) {
	switch b := body.(type) {
	case nil:
		return nil, nil
	case json.RawMessage:
		return b, nil
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("orbits: encoding the request body: %w", err)
	}
	return payload, nil
}

func decodeBody(body []byte, out any) error {
	if out == nil {
		return nil
	}
	if raw, ok := out.(*[]byte); ok {
		*raw = body
		return nil
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("orbits: decoding the response: %w", err)
	}
	return nil
}

// streamClient is the HTTP client for long requests (log streams and uploads): the caller's, without its
// per-attempt timeout, which would cut them off.
func (c *Client) streamClient() *http.Client {
	hc := *c.http
	hc.Timeout = 0
	return &hc
}

// esc escapes one path segment.
func esc(s string) string { return url.PathEscape(s) }

// teamPath is /teams/{team} followed by more segments, each escaped.
func teamPath(team string, segments ...string) string {
	var b strings.Builder
	b.WriteString("/teams/")
	b.WriteString(esc(team))
	for _, s := range segments {
		b.WriteByte('/')
		b.WriteString(s)
	}
	return b.String()
}

// projectPath is /teams/{team}/projects/{project} followed by more segments.
func projectPath(team, project string, segments ...string) string {
	return teamPath(team, append([]string{"projects", esc(project)}, segments...)...)
}

// get, post, put, patch and del are Do with a typed result.
func get[T any](ctx context.Context, c *Client, path string, query url.Values, opts []RequestOption) (*T, error) {
	var out T
	if err := c.Do(ctx, http.MethodGet, path, query, nil, &out, opts...); err != nil {
		return nil, err
	}
	return &out, nil
}

func send[T any](ctx context.Context, c *Client, method, path string, body any, opts []RequestOption) (*T, error) {
	var out T
	if err := c.Do(ctx, method, path, nil, body, &out, opts...); err != nil {
		return nil, err
	}
	return &out, nil
}
