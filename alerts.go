package orbits

import (
	"context"
	"iter"
	"net/http"
)

// AlertsService manages a project's alert rules and reads where alerts go.
type AlertsService struct{ c *Client }

// List lists a project's alert rules: GET /teams/{team}/projects/{project}/alerts.
func (s *AlertsService) List(ctx context.Context, team, project string, opts ...RequestOption) (*AlertPage, error) {
	return get[AlertPage](ctx, s.c, projectPath(team, project, "alerts"), nil, opts)
}

// Create creates an alert rule: POST /teams/{team}/projects/{project}/alerts.
func (s *AlertsService) Create(ctx context.Context, team, project string, req AlertCreate, opts ...RequestOption) (*Alert, error) {
	return send[Alert](ctx, s.c, http.MethodPost, projectPath(team, project, "alerts"), req, opts)
}

// Preview says what a rule would see now, before it is saved: POST
// /teams/{team}/projects/{project}/alerts/preview.
func (s *AlertsService) Preview(ctx context.Context, team, project string, req AlertPreviewRequest, opts ...RequestOption) (*AlertPreview, error) {
	return send[AlertPreview](ctx, s.c, http.MethodPost, projectPath(team, project, "alerts", "preview"), req, opts)
}

// Update changes an alert rule: PATCH /teams/{team}/projects/{project}/alerts/{alert}.
func (s *AlertsService) Update(ctx context.Context, team, project, alert string, req AlertUpdate, opts ...RequestOption) (*Alert, error) {
	return send[Alert](ctx, s.c, http.MethodPatch, projectPath(team, project, "alerts", esc(alert)), req, opts)
}

// Delete deletes an alert rule: DELETE /teams/{team}/projects/{project}/alerts/{alert}.
func (s *AlertsService) Delete(ctx context.Context, team, project, alert string, opts ...RequestOption) error {
	return s.c.Do(ctx, http.MethodDelete, projectPath(team, project, "alerts", esc(alert)), nil, nil, nil, opts...)
}

// ListEvents lists when a rule fired and resolved, newest first, one page at a time: GET
// /teams/{team}/projects/{project}/alerts/{alert}/events.
func (s *AlertsService) ListEvents(ctx context.Context, team, project, alert string, params *ListAlertEventsParams, opts ...RequestOption) (*AlertEventPage, error) {
	return get[AlertEventPage](ctx, s.c, projectPath(team, project, "alerts", esc(alert), "events"), params.values(), opts)
}

// AllEvents iterates over every event of ListEvents, fetching pages as needed. It stops at the first error,
// which it yields.
func (s *AlertsService) AllEvents(ctx context.Context, team, project, alert string, params *ListAlertEventsParams, opts ...RequestOption) iter.Seq2[AlertEvent, error] {
	p := deref(params)
	return paginate(p.Cursor, func(cursor string) ([]AlertEvent, *string, error) {
		p.Cursor = cursor
		page, err := s.ListEvents(ctx, team, project, alert, &p, opts...)
		if err != nil {
			return nil, nil, err
		}
		return page.Items, page.NextCursor, nil
	})
}

// GetChannels returns where the team's alerts go (its webhook, if it has one): GET /teams/{team}/alert-channels.
func (s *AlertsService) GetChannels(ctx context.Context, team string, opts ...RequestOption) (*AlertChannels, error) {
	return get[AlertChannels](ctx, s.c, teamPath(team, "alert-channels"), nil, opts)
}

// SetWebhook sets the team's alert webhook URL (https only): PUT /teams/{team}/alert-channels/webhook. The first
// time, or with RotateSecret, the answer carries the new signing secret, this once; otherwise its Secret is nil.
// Owners and admins only.
func (s *AlertsService) SetWebhook(ctx context.Context, team string, req AlertWebhookSet, opts ...RequestOption) (*AlertWebhookSaved, error) {
	return send[AlertWebhookSaved](ctx, s.c, http.MethodPut, teamPath(team, "alert-channels", "webhook"), req, opts)
}

// DeleteWebhook removes the team's alert webhook: DELETE /teams/{team}/alert-channels/webhook. Owners and admins
// only.
func (s *AlertsService) DeleteWebhook(ctx context.Context, team string, opts ...RequestOption) error {
	return s.c.Do(ctx, http.MethodDelete, teamPath(team, "alert-channels", "webhook"), nil, nil, nil, opts...)
}

// TestWebhook sends a signed alert.test to the team's webhook now and says how it went: POST
// /teams/{team}/alert-channels/webhook/test. Owners and admins only.
func (s *AlertsService) TestWebhook(ctx context.Context, team string, opts ...RequestOption) (*AlertWebhookTest, error) {
	return send[AlertWebhookTest](ctx, s.c, http.MethodPost, teamPath(team, "alert-channels", "webhook", "test"), nil, opts)
}
