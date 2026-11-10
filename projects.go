package orbits

import (
	"context"
	"iter"
	"net/http"
)

// ProjectsService manages a team's projects.
type ProjectsService struct{ c *Client }

// List lists a team's projects, one page at a time: GET /teams/{team}/projects.
func (s *ProjectsService) List(ctx context.Context, team string, params *ListProjectsParams, opts ...RequestOption) (*ProjectPage, error) {
	return get[ProjectPage](ctx, s.c, teamPath(team, "projects"), params.values(), opts)
}

// All iterates over every project of List, fetching pages as needed. It stops at the first error, which it
// yields.
func (s *ProjectsService) All(ctx context.Context, team string, params *ListProjectsParams, opts ...RequestOption) iter.Seq2[Project, error] {
	p := deref(params)
	return paginate(p.Cursor, func(cursor string) ([]Project, *string, error) {
		p.Cursor = cursor
		page, err := s.List(ctx, team, &p, opts...)
		if err != nil {
			return nil, nil, err
		}
		return page.Items, page.NextCursor, nil
	})
}

// Create creates a project: POST /teams/{team}/projects. With a Git source, its first production deployment is
// queued too (see the returned project's LatestDeployment). With SourceKind upload, the project has no
// repository and deploys with Deployments.UploadDir. The answer is 202: the project exists, its first build may
// still be running.
func (s *ProjectsService) Create(ctx context.Context, team string, req ProjectCreate, opts ...RequestOption) (*Project, error) {
	return send[Project](ctx, s.c, http.MethodPost, teamPath(team, "projects"), req, opts)
}

// Get returns a project, by slug or prj_ id: GET /teams/{team}/projects/{project}.
func (s *ProjectsService) Get(ctx context.Context, team, project string, opts ...RequestOption) (*Project, error) {
	return get[Project](ctx, s.c, projectPath(team, project), nil, opts)
}

// Update changes a project's settings: PATCH /teams/{team}/projects/{project}. Fields left nil are kept; list a
// field in NullFields of BuildConfig to go back to the detected default.
func (s *ProjectsService) Update(ctx context.Context, team, project string, req ProjectUpdate, opts ...RequestOption) (*Project, error) {
	return send[Project](ctx, s.c, http.MethodPatch, projectPath(team, project), req, opts)
}

// Delete deletes a project with its deployments, variables and flags: DELETE /teams/{team}/projects/{project}.
// It returns the Operation doing it; wait for it with Operations.Wait.
func (s *ProjectsService) Delete(ctx context.Context, team, project string, opts ...RequestOption) (*Operation, error) {
	return send[Operation](ctx, s.c, http.MethodDelete, projectPath(team, project), nil, opts)
}

// GetDeletionImpact counts what deleting the project removes: GET
// /teams/{team}/projects/{project}/deletion-impact.
func (s *ProjectsService) GetDeletionImpact(ctx context.Context, team, project string, opts ...RequestOption) (*ProjectDeletionImpact, error) {
	return get[ProjectDeletionImpact](ctx, s.c, projectPath(team, project, "deletion-impact"), nil, opts)
}

// SetPaused pauses a project (paused true: its deployments stop answering) or resumes it: POST
// /teams/{team}/projects/{project}/pause.
func (s *ProjectsService) SetPaused(ctx context.Context, team, project string, paused bool, opts ...RequestOption) (*Project, error) {
	return send[Project](ctx, s.c, http.MethodPost, projectPath(team, project, "pause"), PauseProjectRequest{Paused: paused}, opts)
}

// Pause is SetPaused with true.
func (s *ProjectsService) Pause(ctx context.Context, team, project string, opts ...RequestOption) (*Project, error) {
	return s.SetPaused(ctx, team, project, true, opts...)
}

// Resume is SetPaused with false.
func (s *ProjectsService) Resume(ctx context.Context, team, project string, opts ...RequestOption) (*Project, error) {
	return s.SetPaused(ctx, team, project, false, opts...)
}
