package orbits

import (
	"context"
	"iter"
	"net/http"
)

// TeamsService reads and changes teams. An API token belongs to one team and reaches only that team's routes.
type TeamsService struct{ c *Client }

// List lists the teams the token's person belongs to, one page at a time: GET /teams.
func (s *TeamsService) List(ctx context.Context, params *ListTeamsParams, opts ...RequestOption) (*TeamPage, error) {
	return get[TeamPage](ctx, s.c, "/teams", params.values(), opts)
}

// All iterates over every team of List, fetching pages as needed. It stops at the first error, which it yields.
func (s *TeamsService) All(ctx context.Context, params *ListTeamsParams, opts ...RequestOption) iter.Seq2[Team, error] {
	p := deref(params)
	return paginate(p.Cursor, func(cursor string) ([]Team, *string, error) {
		p.Cursor = cursor
		page, err := s.List(ctx, &p, opts...)
		if err != nil {
			return nil, nil, err
		}
		return page.Items, page.NextCursor, nil
	})
}

// Get returns a team, by slug or team_ id, with its members and notification rules: GET /teams/{team}.
func (s *TeamsService) Get(ctx context.Context, team string, opts ...RequestOption) (*TeamDetail, error) {
	return get[TeamDetail](ctx, s.c, teamPath(team), nil, opts)
}

// Update changes a team's name, slug or other settings: PATCH /teams/{team}. API tokens can't change the team's
// security rules (two-factor, sign-in methods, session length, who can invite).
func (s *TeamsService) Update(ctx context.Context, team string, req TeamUpdate, opts ...RequestOption) (*Team, error) {
	return send[Team](ctx, s.c, http.MethodPatch, teamPath(team), req, opts)
}

// GetOverview returns the team's dashboard numbers over a range: GET /teams/{team}/overview.
func (s *TeamsService) GetOverview(ctx context.Context, team string, params *TeamOverviewParams, opts ...RequestOption) (*TeamOverview, error) {
	return get[TeamOverview](ctx, s.c, teamPath(team, "overview"), params.values(), opts)
}

// ListNotifications lists the bell's notifications for the person the token acts as, newest first, one page at
// a time: GET /teams/{team}/notifications.
func (s *TeamsService) ListNotifications(ctx context.Context, team string, params *ListNotificationsParams, opts ...RequestOption) (*NotificationPage, error) {
	return get[NotificationPage](ctx, s.c, teamPath(team, "notifications"), params.values(), opts)
}

// AllNotifications iterates over every notification of ListNotifications, fetching pages as needed. It stops at
// the first error, which it yields.
func (s *TeamsService) AllNotifications(ctx context.Context, team string, params *ListNotificationsParams, opts ...RequestOption) iter.Seq2[Notification, error] {
	p := deref(params)
	return paginate(p.Cursor, func(cursor string) ([]Notification, *string, error) {
		p.Cursor = cursor
		page, err := s.ListNotifications(ctx, team, &p, opts...)
		if err != nil {
			return nil, nil, err
		}
		return page.Items, page.NextCursor, nil
	})
}

// MarkNotificationsRead marks some notifications (IDs) or all of them (All) read, for the person the token acts
// as only: POST /teams/{team}/notifications.
func (s *TeamsService) MarkNotificationsRead(ctx context.Context, team string, req NotificationsReadRequest, opts ...RequestOption) error {
	return s.c.Do(ctx, http.MethodPost, teamPath(team, "notifications"), nil, req, nil, opts...)
}

// deref copies *p, or returns the zero value for nil.
func deref[T any](p *T) T {
	var v T
	if p != nil {
		v = *p
	}
	return v
}
