package orbits

import "context"

// UsageService reads a team's usage.
type UsageService struct{ c *Client }

// Get returns a month's requests, bandwidth, build minutes, function runs and deployments, by day and by
// project (this month unless params.Month is set, as YYYY-MM): GET /teams/{team}/usage.
func (s *UsageService) Get(ctx context.Context, team string, params *UsageParams, opts ...RequestOption) (*Usage, error) {
	return get[Usage](ctx, s.c, teamPath(team, "usage"), params.values(), opts)
}
