# Contributing to orbits-go

Thanks for helping improve the Go SDK for the [Orbits](https://orbits.africa) API. Bug reports, fixes and new methods
are all welcome. Please read the [Code of Conduct](CODE_OF_CONDUCT.md) first, and report security issues privately as
described in [SECURITY.md](SECURITY.md), never in a public issue.

## Development setup

You need Go 1.23 or newer. The module has no dependencies outside the standard library.

```sh
git clone https://github.com/The-Orbits/orbits-go
cd orbits-go
go build ./...
```

## Checks

Run these before opening a pull request; CI runs the same ones on the oldest supported Go and the newest.

```sh
gofmt -l .                        # must print nothing
go vet ./...
go test -race ./...
go run ./scripts/gentypes -check  # types.go and params.go match the spec
```

The live suite runs against a real team (staging by default) only when a token is set. It creates its own project
and deletes it at the end:

```sh
ORBITS_E2E_TOKEN=orb_live_… ORBITS_E2E_TEAM=<team> go test -run TestLive -v ./
```

## Layout

```
client.go            New, NewFromEnv, options, Do, request options
retry.go             retry policy, backoff, Retry-After, Idempotency-Key
errors.go            *Error (problem+json), FieldError, the Is… helpers
pagination.go        the iterator behind every All…, Ptr
wait.go              Operations.Wait, Deployments.Wait
stream.go            Deployments.StreamLogs (server-sent events, reconnecting)
upload.go, ignore.go ZipDir, Deployments.UploadDir and UploadZip, .orbitsignore
types.go, params.go  generated from testdata/openapi.yaml: do not edit
<service>.go         one file per service: projects.go, deployments.go, ...
spec_test.go         operationId -> method map, route, type and generated-file checks
internal/yaml        a small YAML reader for the spec
internal/schema      the spec's operations, a schema validator and an example builder
internal/gen         the type generator and its configuration (skipped operations, names)
scripts/             gentypes (the generator) and sync-spec.sh (vendoring the spec)
testdata/            the vendored spec, and fixtures checked against it
examples/            runnable programs; they must build
```

## Keeping in step with the API spec

The SDK follows the API's OpenAPI contract, `libs/contracts/openapi.yaml` in orbits-backend. A copy lives in
`testdata/openapi.yaml`.

1. Refresh the copy, and regenerate the types:

   ```sh
   scripts/sync-spec.sh path/or/url/to/openapi.yaml
   go run ./scripts/gentypes
   ```

   `scripts/sync-spec.sh --check <source>` only reports whether the copy is current.

2. Run `go test ./...`. `TestSpecCoverage` names every operation that has no method and is not skipped;
   `TestTypesMatchSpec` fails when a type cannot hold what the spec describes; `TestSpecRoutes` checks each method
   sends the spec's method and path.
3. For each new operation, add a method to its service file in the shape of its neighbours (path parameters as
   strings, query parameters as a `*…Params` struct, the body as a value, `opts ...RequestOption` last; a paginated
   list gets `List` and an `All…` iterator) and list it in `operationMethods` in `spec_test.go`. An operation the SDK
   leaves out goes in `Skipped` in `internal/gen/config.go` with the reason. A generated name that reads badly gets an
   entry in `TypeNames` or `FieldNames` there.
4. Never edit `types.go` or `params.go` by hand.

## Guidelines

- Every request goes through `Client.Do`, which owns auth, the Idempotency-Key, retries and error mapping. The log
  stream and the upload's PUT to storage are the exceptions.
- `Do`'s signature, the option names, method signatures and `*Error`'s fields are public API: do not change them in a
  minor release.
- Keep the module dependency-free and on `go 1.23`.
- Never log or print the token or environment variable values, and never put them in a URL or an error.
- Add tests for every change in behaviour; fixtures go in `testdata/fixtures`, named for the operation they answer.
- A change users will notice gets a line under `## Unreleased` in `CHANGELOG.md`, in the same pull request.

## Releasing

Rename `## Unreleased` in `CHANGELOG.md` to `## vX.Y.Z — YYYY-MM-DD` and set `Version` in `client.go` to match (a
test keeps the two in step), merge, then push the tag: `git tag vX.Y.Z && git push origin vX.Y.Z`. The release
workflow checks the tag and publishes a GitHub release with that section as its notes. A Go module needs no build
artifacts: the tag is the release.
