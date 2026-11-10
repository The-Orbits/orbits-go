# Changelog

What changed in each version of orbits-go. Every pull request that changes what the SDK does adds a line under
**Unreleased**. Releasing renames that heading to the version and its date (`## v0.2.0 — 2026-12-01`) and pushes the
tag `v0.2.0`.

## Unreleased

## v0.1.0 — 2026-11-10

### Added

- `Teams.ListNotifications`, `Teams.AllNotifications` and `Teams.MarkNotificationsRead`: the bell's notifications for
  the person the token acts as.
- `Alerts.SetWebhook`, `Alerts.DeleteWebhook` and `Alerts.TestWebhook`: the team's alert webhook.
- `Sandboxes.TerminalTicket`: a 60-second ticket for the sandbox terminal's WebSocket, which takes it as `?ticket=`.
- `orbits.New(token, ...Option)` and `orbits.NewFromEnv()` (reads `ORBITS_TOKEN` and `ORBITS_API_URL`), with
  `WithBaseURL`, `WithHTTPClient`, `WithUserAgent` (puts a product token such as `orbits-cli/0.1.0` before
  `orbits-go/0.1.0`), `WithMaxRetries` and `WithHeader`. The default API is `https://api.theorbits.app/v1`.
- A method for every operation an API token can call: `Me`, `Teams`, `Projects`, `Deployments` (create, redeploy,
  promote, rollback, cancel, set production, build logs, screenshot, uploads), `Env`, `Domains` (DNS records,
  checks, zone imports, email setup, branch addresses), `Flags` (with rules), `Workflows`, `Sandboxes`, `Alerts`
  (with events and channels), `Observability` (runtime logs, metrics, Analytics, Speed Insights), `Usage`,
  `Activity`, `Billing` (plans, billing, invoices), `Templates`, `Operations` and `Integrations` (the list).
- `Deployments.UploadDir` and `UploadZip`: zip a folder (leaving out `node_modules`, `.git`, `.next`, `__MACOSX`,
  `.DS_Store`, `.orbits` and `.orbitsignore` patterns), check the API's limits, upload it and return the deployment.
  `ZipDir` builds the same zip on its own. `UploadOptions.IdempotencyKey` makes retrying a whole deploy return the
  same upload.
- `Deployments.StreamLogs`: the build log's server-sent events as a Go iterator, reconnecting with `Last-Event-ID`.
- `Operations.Wait` and `Deployments.Wait`, with `*OperationError` and `*DeploymentError`.
- `All…` iterators over every cursor-paginated list.
- An `Idempotency-Key` (UUIDv4) on every POST, reused by its retries; retries with backoff and `Retry-After` on
  connection errors, `429`, `502`, `503` and `504`.
- `*orbits.Error` from the API's problem documents, with `IsNotFound`, `IsValidation`, `IsRateLimited`,
  `IsForbidden`, `IsConflict`, `IsUnauthorized`, `IsPlanRequired`, `IsServerError`, `IsTokenRefused` and `IsCode`.
- Request and response types generated from the vendored API spec (`testdata/openapi.yaml`).
