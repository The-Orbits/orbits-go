package orbits

import (
	"context"
	"net/http"
)

// FlagsService manages a project's feature flags. Deployed apps read them with @useorbits/flags.
type FlagsService struct{ c *Client }

// List lists a project's flags: GET /teams/{team}/projects/{project}/flags.
func (s *FlagsService) List(ctx context.Context, team, project string, params *ListFlagsParams, opts ...RequestOption) (*FlagList, error) {
	return get[FlagList](ctx, s.c, projectPath(team, project, "flags"), params.values(), opts)
}

// Create creates a flag: POST /teams/{team}/projects/{project}/flags.
func (s *FlagsService) Create(ctx context.Context, team, project string, req CreateFlagRequest, opts ...RequestOption) (*Flag, error) {
	return send[Flag](ctx, s.c, http.MethodPost, projectPath(team, project, "flags"), req, opts)
}

// GetKeys returns the project's flag keys, one per environment, for apps that read flags outside Orbits: GET
// /teams/{team}/projects/{project}/flag-keys. Treat them as secrets.
func (s *FlagsService) GetKeys(ctx context.Context, team, project string, opts ...RequestOption) (*FlagKeys, error) {
	return get[FlagKeys](ctx, s.c, projectPath(team, project, "flag-keys"), nil, opts)
}

// Get returns a flag with its history: GET /teams/{team}/projects/{project}/flags/{flag}.
func (s *FlagsService) Get(ctx context.Context, team, project, flag string, opts ...RequestOption) (*FlagDetail, error) {
	return get[FlagDetail](ctx, s.c, projectPath(team, project, "flags", esc(flag)), nil, opts)
}

// Update changes a flag's description: PATCH /teams/{team}/projects/{project}/flags/{flag}.
func (s *FlagsService) Update(ctx context.Context, team, project, flag string, req UpdateFlagRequest, opts ...RequestOption) (*Flag, error) {
	return send[Flag](ctx, s.c, http.MethodPatch, projectPath(team, project, "flags", esc(flag)), req, opts)
}

// Delete deletes a flag: DELETE /teams/{team}/projects/{project}/flags/{flag}.
func (s *FlagsService) Delete(ctx context.Context, team, project, flag string, opts ...RequestOption) error {
	return s.c.Do(ctx, http.MethodDelete, projectPath(team, project, "flags", esc(flag)), nil, nil, nil, opts...)
}

// SetEnvironment changes a flag in one environment: a boolean flag's On and Rollout, a string or number flag's
// Value. Fields left nil stay as they are: PUT /teams/{team}/projects/{project}/flags/{flag}/environments/{environment}.
func (s *FlagsService) SetEnvironment(ctx context.Context, team, project, flag string, environment FlagEnvironmentName, req SetFlagEnvironmentRequest, opts ...RequestOption) (*Flag, error) {
	return send[Flag](ctx, s.c, http.MethodPut, flagEnvPath(team, project, flag, environment), req, opts)
}

// AddRule adds a targeting rule to a flag in one environment: POST
// /teams/{team}/projects/{project}/flags/{flag}/environments/{environment}/rules.
func (s *FlagsService) AddRule(ctx context.Context, team, project, flag string, environment FlagEnvironmentName, req AddFlagRuleRequest, opts ...RequestOption) (*Flag, error) {
	return send[Flag](ctx, s.c, http.MethodPost, flagEnvPath(team, project, flag, environment)+"/rules", req, opts)
}

// UpdateRule changes what a rule serves: PATCH
// /teams/{team}/projects/{project}/flags/{flag}/environments/{environment}/rules/{rule}.
func (s *FlagsService) UpdateRule(ctx context.Context, team, project, flag string, environment FlagEnvironmentName, rule string, req UpdateFlagRuleRequest, opts ...RequestOption) (*Flag, error) {
	return send[Flag](ctx, s.c, http.MethodPatch, flagEnvPath(team, project, flag, environment)+"/rules/"+esc(rule), req, opts)
}

// RemoveRule removes a rule: DELETE
// /teams/{team}/projects/{project}/flags/{flag}/environments/{environment}/rules/{rule}.
func (s *FlagsService) RemoveRule(ctx context.Context, team, project, flag string, environment FlagEnvironmentName, rule string, opts ...RequestOption) error {
	return s.c.Do(ctx, http.MethodDelete, flagEnvPath(team, project, flag, environment)+"/rules/"+esc(rule), nil, nil, nil, opts...)
}

func flagEnvPath(team, project, flag string, environment FlagEnvironmentName) string {
	return projectPath(team, project, "flags", esc(flag), "environments", esc(string(environment)))
}
