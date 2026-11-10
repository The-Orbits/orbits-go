package orbits

import "context"

// IntegrationsService lists a team's integrations. Installing and changing them takes a signed-in person in the
// web app; API tokens can't.
type IntegrationsService struct{ c *Client }

// List lists the integrations and whether each is installed: GET /teams/{team}/integrations.
func (s *IntegrationsService) List(ctx context.Context, team string, opts ...RequestOption) (*IntegrationList, error) {
	return get[IntegrationList](ctx, s.c, teamPath(team, "integrations"), nil, opts)
}
