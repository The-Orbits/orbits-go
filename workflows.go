package orbits

import (
	"context"
	"net/http"
)

// WorkflowsService lists a project's durable workflows (defined in the app with @useorbits/workflows) and runs
// them.
type WorkflowsService struct{ c *Client }

// List lists the workflows of the production deployment, and recent runs: GET
// /teams/{team}/projects/{project}/workflows.
func (s *WorkflowsService) List(ctx context.Context, team, project string, opts ...RequestOption) (*WorkflowsPage, error) {
	return get[WorkflowsPage](ctx, s.c, projectPath(team, project, "workflows"), nil, opts)
}

// Run starts a run of a workflow on the production deployment, with Input as its JSON input: POST
// /teams/{team}/projects/{project}/workflows/{workflow}/runs.
func (s *WorkflowsService) Run(ctx context.Context, team, project, workflow string, req RunWorkflowRequest, opts ...RequestOption) (*WorkflowRun, error) {
	return send[WorkflowRun](ctx, s.c, http.MethodPost, projectPath(team, project, "workflows", esc(workflow), "runs"), req, opts)
}

// GetRun returns a run with its steps: GET /teams/{team}/projects/{project}/workflows/runs/{run}.
func (s *WorkflowsService) GetRun(ctx context.Context, team, project, run string, opts ...RequestOption) (*WorkflowRun, error) {
	return get[WorkflowRun](ctx, s.c, projectPath(team, project, "workflows", "runs", esc(run)), nil, opts)
}

// CancelRun cancels a running run: POST /teams/{team}/projects/{project}/workflows/runs/{run}/cancel.
func (s *WorkflowsService) CancelRun(ctx context.Context, team, project, run string, opts ...RequestOption) (*WorkflowRun, error) {
	return send[WorkflowRun](ctx, s.c, http.MethodPost, projectPath(team, project, "workflows", "runs", esc(run), "cancel"), nil, opts)
}

// RetryRun retries a failed run from its failed step: POST /teams/{team}/projects/{project}/workflows/runs/{run}/retry.
func (s *WorkflowsService) RetryRun(ctx context.Context, team, project, run string, opts ...RequestOption) (*WorkflowRun, error) {
	return send[WorkflowRun](ctx, s.c, http.MethodPost, projectPath(team, project, "workflows", "runs", esc(run), "retry"), nil, opts)
}
