package orbits

import (
	"context"
	"net/http"
)

// SandboxesService manages a project's on-demand dev machines. Starting one needs the Pro plan.
type SandboxesService struct{ c *Client }

// List lists a project's sandboxes: GET /teams/{team}/projects/{project}/sandboxes.
func (s *SandboxesService) List(ctx context.Context, team, project string, opts ...RequestOption) (*SandboxPage, error) {
	return get[SandboxPage](ctx, s.c, projectPath(team, project, "sandboxes"), nil, opts)
}

// Create starts a sandbox: POST /teams/{team}/projects/{project}/sandboxes.
func (s *SandboxesService) Create(ctx context.Context, team, project string, req SandboxCreate, opts ...RequestOption) (*Sandbox, error) {
	return send[Sandbox](ctx, s.c, http.MethodPost, projectPath(team, project, "sandboxes"), req, opts)
}

// Get returns a sandbox with its ports: GET /teams/{team}/projects/{project}/sandboxes/{sandbox}.
func (s *SandboxesService) Get(ctx context.Context, team, project, sandbox string, opts ...RequestOption) (*SandboxDetail, error) {
	return get[SandboxDetail](ctx, s.c, projectPath(team, project, "sandboxes", esc(sandbox)), nil, opts)
}

// Delete deletes a sandbox and its disk: DELETE /teams/{team}/projects/{project}/sandboxes/{sandbox}.
func (s *SandboxesService) Delete(ctx context.Context, team, project, sandbox string, opts ...RequestOption) error {
	return s.c.Do(ctx, http.MethodDelete, projectPath(team, project, "sandboxes", esc(sandbox)), nil, nil, nil, opts...)
}

// Stop suspends a sandbox, keeping its disk: POST /teams/{team}/projects/{project}/sandboxes/{sandbox}/stop.
func (s *SandboxesService) Stop(ctx context.Context, team, project, sandbox string, opts ...RequestOption) (*Sandbox, error) {
	return send[Sandbox](ctx, s.c, http.MethodPost, projectPath(team, project, "sandboxes", esc(sandbox), "stop"), nil, opts)
}

// Start resumes a stopped sandbox: POST /teams/{team}/projects/{project}/sandboxes/{sandbox}/start.
func (s *SandboxesService) Start(ctx context.Context, team, project, sandbox string, opts ...RequestOption) (*Sandbox, error) {
	return send[Sandbox](ctx, s.c, http.MethodPost, projectPath(team, project, "sandboxes", esc(sandbox), "start"), nil, opts)
}

// TerminalTicket mints a ticket for the sandbox's terminal: POST
// /teams/{team}/projects/{project}/sandboxes/{sandbox}/terminal-ticket. The terminal is a WebSocket, which the SDK
// doesn't open: connect to wss://<api>/v1/teams/{team}/projects/{project}/sandboxes/{sandbox}/terminal?ticket=<ticket>
// within 60 seconds. The sandbox must be running.
func (s *SandboxesService) TerminalTicket(ctx context.Context, team, project, sandbox string, opts ...RequestOption) (*SandboxTerminalTicket, error) {
	return send[SandboxTerminalTicket](ctx, s.c, http.MethodPost, projectPath(team, project, "sandboxes", esc(sandbox), "terminal-ticket"), nil, opts)
}
