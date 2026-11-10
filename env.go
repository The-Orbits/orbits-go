package orbits

import (
	"context"
	"iter"
	"net/http"
)

// EnvService manages a project's environment variables. The SDK never logs their values.
type EnvService struct{ c *Client }

// List lists a project's variables, one page at a time: GET /teams/{team}/projects/{project}/env. A value is
// returned only for a non-sensitive variable meant for the browser; Reveal reads the others that can be read.
func (s *EnvService) List(ctx context.Context, team, project string, params *ListEnvVarsParams, opts ...RequestOption) (*EnvVarPage, error) {
	return get[EnvVarPage](ctx, s.c, projectPath(team, project, "env"), params.values(), opts)
}

// All iterates over every variable of List, fetching pages as needed. It stops at the first error, which it
// yields.
func (s *EnvService) All(ctx context.Context, team, project string, params *ListEnvVarsParams, opts ...RequestOption) iter.Seq2[EnvVar, error] {
	p := deref(params)
	return paginate(p.Cursor, func(cursor string) ([]EnvVar, *string, error) {
		p.Cursor = cursor
		page, err := s.List(ctx, team, project, &p, opts...)
		if err != nil {
			return nil, nil, err
		}
		return page.Items, page.NextCursor, nil
	})
}

// Create saves one or more variables: POST /teams/{team}/projects/{project}/env. With OnConflict fail (the
// default) a key that exists already is a 409 and nothing is saved; with replace, existing keys get the new value
// and environments. New values apply to deployments created afterwards.
func (s *EnvService) Create(ctx context.Context, team, project string, req EnvVarCreate, opts ...RequestOption) (*EnvVarBatchResult, error) {
	return send[EnvVarBatchResult](ctx, s.c, http.MethodPost, projectPath(team, project, "env"), req, opts)
}

// Set saves variables, replacing the value and environments of keys that exist already: Create with OnConflict
// replace.
func (s *EnvService) Set(ctx context.Context, team, project string, vars []EnvVarInput, opts ...RequestOption) (*EnvVarBatchResult, error) {
	replace := EnvVarCreateOnConflictReplace
	return s.Create(ctx, team, project, EnvVarCreate{Variables: vars, OnConflict: &replace}, opts...)
}

// Update changes a variable's value, its environments or both: PATCH
// /teams/{team}/projects/{project}/env/{key}. Whether it is sensitive cannot be changed.
func (s *EnvService) Update(ctx context.Context, team, project, key string, req EnvVarUpdate, opts ...RequestOption) (*EnvVarChange, error) {
	return send[EnvVarChange](ctx, s.c, http.MethodPatch, projectPath(team, project, "env", esc(key)), req, opts)
}

// Delete deletes a variable: DELETE /teams/{team}/projects/{project}/env/{key}. Restore brings it back until the
// answer's RestorableUntil.
func (s *EnvService) Delete(ctx context.Context, team, project, key string, opts ...RequestOption) (*EnvVarDeleted, error) {
	return send[EnvVarDeleted](ctx, s.c, http.MethodDelete, projectPath(team, project, "env", esc(key)), nil, opts)
}

// Restore brings back a variable deleted in the last 10 minutes: POST
// /teams/{team}/projects/{project}/env/{key}/restore.
func (s *EnvService) Restore(ctx context.Context, team, project, key string, opts ...RequestOption) (*EnvVarChange, error) {
	return send[EnvVarChange](ctx, s.c, http.MethodPost, projectPath(team, project, "env", esc(key), "restore"), nil, opts)
}

// Reveal reads a non-sensitive variable's value: POST /teams/{team}/projects/{project}/env/{key}/reveal. Every
// reveal is recorded in the team's activity; a sensitive variable is a 409, as it can never be read back. A
// read-only token cannot reveal values.
func (s *EnvService) Reveal(ctx context.Context, team, project, key string, opts ...RequestOption) (*EnvVarValue, error) {
	return send[EnvVarValue](ctx, s.c, http.MethodPost, projectPath(team, project, "env", esc(key), "reveal"), nil, opts)
}

// Pull returns the values of every variable that applies to environment, except sensitive ones (listed in
// SkippedSensitive): POST /teams/{team}/projects/{project}/env/pull. It is what `orbits env pull` writes to
// .env.local. Every pull is recorded in the team's activity.
func (s *EnvService) Pull(ctx context.Context, team, project string, environment EnvironmentName, opts ...RequestOption) (*EnvVarPull, error) {
	return send[EnvVarPull](ctx, s.c, http.MethodPost, projectPath(team, project, "env", "pull"), PullEnvVarsRequest{Environment: environment}, opts)
}
