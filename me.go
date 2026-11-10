package orbits

import "context"

// MeService describes who the token acts as.
type MeService struct{ c *Client }

// Get returns the person the token acts as, with their teams and a preview of each team's projects: GET /me.
func (s *MeService) Get(ctx context.Context, opts ...RequestOption) (*Me, error) {
	return get[Me](ctx, s.c, "/me", nil, opts)
}
