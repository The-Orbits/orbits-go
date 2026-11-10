package orbits

import (
	"context"
	"iter"
	"net/http"
)

// DeploymentsService builds, lists and follows deployments: uploads, redeploy, promote, rollback, cancel, and the
// build log.
type DeploymentsService struct{ c *Client }

// ListTeam lists the deployments of every project in a team, newest first, one page at a time: GET
// /teams/{team}/deployments.
func (s *DeploymentsService) ListTeam(ctx context.Context, team string, params *ListTeamDeploymentsParams, opts ...RequestOption) (*TeamDeploymentPage, error) {
	return get[TeamDeploymentPage](ctx, s.c, teamPath(team, "deployments"), params.values(), opts)
}

// AllTeam iterates over every deployment of ListTeam, fetching pages as needed. It stops at the first error,
// which it yields.
func (s *DeploymentsService) AllTeam(ctx context.Context, team string, params *ListTeamDeploymentsParams, opts ...RequestOption) iter.Seq2[Deployment, error] {
	p := deref(params)
	return paginate(p.Cursor, func(cursor string) ([]Deployment, *string, error) {
		p.Cursor = cursor
		page, err := s.ListTeam(ctx, team, &p, opts...)
		if err != nil {
			return nil, nil, err
		}
		return page.Items, page.NextCursor, nil
	})
}

// List lists a project's deployments, newest first, one page at a time: GET
// /teams/{team}/projects/{project}/deployments.
func (s *DeploymentsService) List(ctx context.Context, team, project string, params *ListDeploymentsParams, opts ...RequestOption) (*ProjectDeploymentPage, error) {
	return get[ProjectDeploymentPage](ctx, s.c, projectPath(team, project, "deployments"), params.values(), opts)
}

// All iterates over every deployment of List, fetching pages as needed. It stops at the first error, which it
// yields.
func (s *DeploymentsService) All(ctx context.Context, team, project string, params *ListDeploymentsParams, opts ...RequestOption) iter.Seq2[Deployment, error] {
	p := deref(params)
	return paginate(p.Cursor, func(cursor string) ([]Deployment, *string, error) {
		p.Cursor = cursor
		page, err := s.List(ctx, team, project, &p, opts...)
		if err != nil {
			return nil, nil, err
		}
		return page.Items, page.NextCursor, nil
	})
}

// Create queues a build of a branch's newest commit (the production branch when Branch is nil) for a Git
// project: POST /teams/{team}/projects/{project}/deployments. It returns the queued deployment; follow it with
// Wait or StreamLogs.
func (s *DeploymentsService) Create(ctx context.Context, team, project string, req CreateDeploymentRequest, opts ...RequestOption) (*Deployment, error) {
	return send[Deployment](ctx, s.c, http.MethodPost, projectPath(team, project, "deployments"), req, opts)
}

// Get returns a deployment with its steps, failure and domains: GET
// /teams/{team}/projects/{project}/deployments/{deployment}.
func (s *DeploymentsService) Get(ctx context.Context, team, project, deployment string, opts ...RequestOption) (*DeploymentDetail, error) {
	return get[DeploymentDetail](ctx, s.c, projectPath(team, project, "deployments", esc(deployment)), nil, opts)
}

// ListLogs returns build log lines after params.After, oldest first: GET
// /teams/{team}/projects/{project}/deployments/{deployment}/logs. Call again with After set to the answer's
// NextAfter until Done; StreamLogs follows a running build without polling.
func (s *DeploymentsService) ListLogs(ctx context.Context, team, project, deployment string, params *ListDeploymentLogsParams, opts ...RequestOption) (*DeploymentLogPage, error) {
	return get[DeploymentLogPage](ctx, s.c, projectPath(team, project, "deployments", esc(deployment), "logs"), params.values(), opts)
}

// GetScreenshot returns the deployment's first screen as a 1280x800 JPEG: GET
// /teams/{team}/projects/{project}/deployments/{deployment}/screenshot.
func (s *DeploymentsService) GetScreenshot(ctx context.Context, team, project, deployment string, opts ...RequestOption) ([]byte, error) {
	var out []byte
	opts = append([]RequestOption{WithRequestHeader("Accept", "image/jpeg")}, opts...)
	if err := s.c.Do(ctx, http.MethodGet, projectPath(team, project, "deployments", esc(deployment), "screenshot"), nil, nil, &out, opts...); err != nil {
		return nil, err
	}
	return out, nil
}

// Redeploy builds a deployment's commit (or upload) again, as a new deployment: POST
// /teams/{team}/projects/{project}/deployments/{deployment}/redeploy.
func (s *DeploymentsService) Redeploy(ctx context.Context, team, project, deployment string, req RedeployRequest, opts ...RequestOption) (*Deployment, error) {
	return send[Deployment](ctx, s.c, http.MethodPost, projectPath(team, project, "deployments", esc(deployment), "redeploy"), req, opts)
}

// Promote builds a preview deployment for production: POST
// /teams/{team}/projects/{project}/deployments/{deployment}/promote.
func (s *DeploymentsService) Promote(ctx context.Context, team, project, deployment string, opts ...RequestOption) (*Deployment, error) {
	return send[Deployment](ctx, s.c, http.MethodPost, projectPath(team, project, "deployments", esc(deployment), "promote"), nil, opts)
}

// Rollback makes production serve an earlier deployment at once, without a build, and pauses automatic
// production deploys: POST /teams/{team}/projects/{project}/deployments/{deployment}/rollback. The answer's
// Undo is the SetProduction request that reverses it.
func (s *DeploymentsService) Rollback(ctx context.Context, team, project, deployment string, opts ...RequestOption) (*Rollback, error) {
	return send[Rollback](ctx, s.c, http.MethodPost, projectPath(team, project, "deployments", esc(deployment), "rollback"), nil, opts)
}

// Cancel stops a queued or building deployment: POST
// /teams/{team}/projects/{project}/deployments/{deployment}/cancel.
func (s *DeploymentsService) Cancel(ctx context.Context, team, project, deployment string, opts ...RequestOption) (*Deployment, error) {
	return send[Deployment](ctx, s.c, http.MethodPost, projectPath(team, project, "deployments", esc(deployment), "cancel"), nil, opts)
}

// SetProduction makes production serve a deployment, and pauses or resumes automatic production deploys: POST
// /teams/{team}/projects/{project}/production.
func (s *DeploymentsService) SetProduction(ctx context.Context, team, project string, req SetProductionRequest, opts ...RequestOption) (*ProductionChange, error) {
	return send[ProductionChange](ctx, s.c, http.MethodPost, projectPath(team, project, "production"), req, opts)
}

// StartUpload signs a URL to PUT a zip of an upload project's source to: POST
// /teams/{team}/projects/{project}/uploads. Most callers want UploadDir, which does the whole upload.
func (s *DeploymentsService) StartUpload(ctx context.Context, team, project string, req StartUploadRequest, opts ...RequestOption) (*SourceUpload, error) {
	return send[SourceUpload](ctx, s.c, http.MethodPost, projectPath(team, project, "uploads"), req, opts)
}

// CompleteUpload says the zip has been sent, and queues the production deployment that builds it: POST
// /teams/{team}/projects/{project}/uploads/{upload}/complete. A 409 upload_incomplete means storage has not seen
// the whole file yet; try again a second later.
func (s *DeploymentsService) CompleteUpload(ctx context.Context, team, project, upload string, opts ...RequestOption) (*Deployment, error) {
	return send[Deployment](ctx, s.c, http.MethodPost, projectPath(team, project, "uploads", esc(upload), "complete"), nil, opts)
}

// CancelUpload abandons an upload that was started and not completed: DELETE
// /teams/{team}/projects/{project}/uploads/{upload}.
func (s *DeploymentsService) CancelUpload(ctx context.Context, team, project, upload string, opts ...RequestOption) error {
	return s.c.Do(ctx, http.MethodDelete, projectPath(team, project, "uploads", esc(upload)), nil, nil, nil, opts...)
}
