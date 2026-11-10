# orbits-go

[![CI](https://github.com/The-Orbits/orbits-go/actions/workflows/ci.yml/badge.svg)](https://github.com/The-Orbits/orbits-go/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/The-Orbits/orbits-go.svg)](https://pkg.go.dev/github.com/The-Orbits/orbits-go)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

The official Go SDK for the [Orbits](https://orbits.africa) API: projects, deployments (Git, uploads, promote,
rollback, redeploy), build logs and the live log stream, environment variables, domains and DNS, feature flags,
workflows, sandboxes, alerts, runtime logs, Analytics and Speed Insights, usage, activity and billing.

- Every operation an API token can call has a typed method. Types are generated from the API's OpenAPI contract.
- `UploadDir` zips a folder and deploys it; `StreamLogs` follows a build as a Go iterator.
- Every POST carries an `Idempotency-Key` that its retries reuse, so a retry never makes a change twice.
- Errors are `*orbits.Error`, from the API's problem documents, with `IsNotFound`, `IsValidation` and friends.
- Lists are Go 1.23 iterators; deployments and operations have waiting helpers.
- No dependencies outside the standard library.

## Install

```sh
go get github.com/The-Orbits/orbits-go
```

Go 1.23 or newer.

## Quickstart

Make an API token under **Team › Settings › API tokens**, then:

```go
package main

import (
	"context"
	"fmt"
	"log"

	orbits "github.com/The-Orbits/orbits-go"
)

func main() {
	c, err := orbits.NewFromEnv() // ORBITS_TOKEN, and ORBITS_API_URL if set
	if err != nil {
		log.Fatal(err)
	}
	for p, err := range c.Projects.All(context.Background(), "my-team", nil) {
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(p.Slug, p.Status)
	}
}
```

`{team}` is a team's slug or `team_` id, and `{project}` a project's slug or `prj_` id. More in
[`examples/`](examples): deploying a folder, listing projects, following a build log, and pulling variables to
`.env.local`.

## Configuration

```go
c, err := orbits.New(token,
	orbits.WithBaseURL("https://stagingapi.theorbits.app/v1"),
	orbits.WithUserAgent("orbits-cli/0.1.0"),
)
```

| Option | What it does |
|---|---|
| `WithBaseURL(url)` | The API's base URL, with `/v1`. Default `https://api.theorbits.app/v1`; `NewFromEnv` reads `ORBITS_API_URL`. |
| `WithUserAgent(product)` | Puts your tool's token before the SDK's: `orbits-cli/0.1.0 orbits-go/0.1.0`. Settings shows it as the token's "last used by". |
| `WithHTTPClient(h)` | Your `*http.Client`. Its `Timeout` bounds each attempt (default 60 s). |
| `WithMaxRetries(n)` | Retries after the first try. Default 3; 0 turns them off. |
| `WithHeader(k, v)` | A header on every request. |

The token is sent only in the `Authorization: Bearer` header. The SDK never logs it or variable values, and printing
a `Client` does not show it. A `read` token can only make GET calls; a token limited to one project reaches only
that project. `c.Me.Get(ctx)` says who the token acts as.

Per call, `orbits.WithIdempotencyKey(key)`, `orbits.WithRequestHeader(k, v)` and `orbits.WithNoRetry()` go after the
other arguments.

## Services

`c.Me`, `c.Teams`, `c.Projects`, `c.Deployments`, `c.Env`, `c.Domains` (with DNS records, checks, zone imports, email
setup and branch addresses), `c.Flags`, `c.Workflows`, `c.Sandboxes`, `c.Alerts`, `c.Observability` (runtime logs,
metrics, Analytics, Speed Insights), `c.Usage`, `c.Activity`, `c.Billing`, `c.Templates`, `c.Operations` and
`c.Integrations`.

Request types follow the API's field names. Optional fields are pointers, and a nil one is not sent; `orbits.Ptr`
sets one inline. Enums are typed string constants:

```go
kind := orbits.ProjectSourceKindUpload
p, err := c.Projects.Create(ctx, "my-team", orbits.ProjectCreate{
	Name:       "storefront",
	SourceKind: &kind,
	Workload:   &orbits.Workload{Type: orbits.WorkloadTypeStatic},
})
```

To send `null` (to clear a value), name the field in the request's `NullFields`:

```go
_, err := c.Domains.Update(ctx, "my-team", "shop.example.com", orbits.UpdateDomainRequest{NullFields: []string{"to"}})
```

For anything the typed methods do not cover, `Do` sends a raw call with the same auth, retries and errors:

```go
var out map[string]any
err := c.Do(ctx, "GET", "/teams/my-team/projects", url.Values{"q": {"shop"}}, nil, &out)
```

## Deploying a folder

```go
d, err := c.Deployments.UploadDir(ctx, "my-team", "storefront", "./site", nil)
if err != nil {
	return err
}
for ev, err := range c.Deployments.StreamLogs(ctx, "my-team", "storefront", d.ID, nil) {
	if err != nil {
		return err
	}
	if ev.Type == orbits.LogEventLine {
		fmt.Println(ev.Line.Text)
	}
}
final, err := c.Deployments.Wait(ctx, "my-team", "storefront", d.ID, nil) // *orbits.DeploymentError if it failed
```

`UploadDir` zips the folder's contents, leaving out `node_modules`, `.git`, `.next`, `__MACOSX`, `.DS_Store`,
`.orbits` and the patterns of the folder's `.orbitsignore` (`.gitignore` syntax). It refuses links and special files,
and checks the API's limits (a zip of up to 100 MiB, 500 MiB and 20,000 files unpacked) before sending anything. Then
it starts the upload, sends the zip straight to storage on the presigned URL (without your token), completes it and
returns the queued production deployment. `UploadZip` sends a zip you already have; `ZipDir` only builds one. Set
`UploadOptions.IdempotencyKey` when you may retry the whole deploy, so a retry gets the same upload instead of a second.

Only projects created with `SourceKind: upload` take uploads. A Git project deploys with `Deployments.Create`
(a branch's newest commit), `Redeploy`, or a push.

## Build logs

`StreamLogs` yields `line`, `steps` and `done` events and stops after `done`; a finished build answers at once. When
the connection drops (the API closes streams after 30 minutes, proxies sooner), it reconnects from the last line it
received, without gaps or repeats. `ListLogs` pages through a finished build's log instead.

## Errors

Every API error is an `*orbits.Error` with `Status`, `Title`, `Detail`, `RequestID`, `Code`, `Errors` (the fields of
a `422`), `RequiredPlan` and `Feature` (on a `402`), `RetryAfter` and `RateLimitScope`. `Detail` is plain language and
safe to show.

```go
p, err := c.Projects.Get(ctx, "my-team", "storefront")
switch {
case orbits.IsNotFound(err): // not there, or not in the token's team
case orbits.IsTokenRefused(err): // a read-only token, or a route tokens can't use
case err != nil:
	if e, ok := orbits.AsError(err); ok {
		log.Printf("%s (request %s)", e.Detail, e.RequestID)
	}
}
```

Also `IsValidation`, `IsRateLimited`, `IsForbidden`, `IsConflict`, `IsUnauthorized`, `IsPlanRequired`,
`IsServerError` and `IsCode(err, "upload_incomplete")`.

## Retries

Connection errors and `429`, `502`, `503` and `504` are retried up to three times, with exponential backoff and
jitter, or after `Retry-After` when the API sends one. GET, PUT, DELETE and POST (which carries its key) are retried;
a PATCH only on `429`, which the API answers before doing anything.

## Pagination and waiting

Paginated lists have a `List` that returns one page (with its totals and `NextCursor`) and an `All…` iterator over
every item. `Operations.Wait` polls an operation (such as a project's deletion) until it succeeds, and
`Deployments.Wait` a deployment until it is ready, both with `*orbits.WaitOptions{Interval, Timeout}`.

## Versioning

The SDK follows semantic versioning. Its types follow the API's contract, a copy of which is in
[`testdata/openapi.yaml`](testdata/openapi.yaml); see [CONTRIBUTING.md](CONTRIBUTING.md) for how it is kept in step.

## License

[MIT](LICENSE)
