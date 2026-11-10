package orbits

import (
	"context"
	"iter"
)

// ActivityService reads a team's activity feed.
type ActivityService struct{ c *Client }

// List lists what happened in a team, newest first, one page at a time: GET /teams/{team}/activity. It covers
// the last 7 days unless params.Since says otherwise (at most 90).
func (s *ActivityService) List(ctx context.Context, team string, params *ListActivityParams, opts ...RequestOption) (*ActivityPage, error) {
	return get[ActivityPage](ctx, s.c, teamPath(team, "activity"), params.values(), opts)
}

// All iterates over every line of List, fetching pages as needed. It stops at the first error, which it yields.
func (s *ActivityService) All(ctx context.Context, team string, params *ListActivityParams, opts ...RequestOption) iter.Seq2[Activity, error] {
	p := deref(params)
	return paginate(p.Cursor, func(cursor string) ([]Activity, *string, error) {
		p.Cursor = cursor
		page, err := s.List(ctx, team, &p, opts...)
		if err != nil {
			return nil, nil, err
		}
		return page.Items, page.NextCursor, nil
	})
}
