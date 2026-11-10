package orbits

import (
	"context"
	"iter"
	"net/http"
)

// ObservabilityService reads a project's runtime logs, request metrics, Analytics and Speed Insights.
type ObservabilityService struct{ c *Client }

// ListLogs lists runtime log entries (requests and function output), newest first, one page at a time: GET
// /teams/{team}/projects/{project}/logs.
func (s *ObservabilityService) ListLogs(ctx context.Context, team, project string, params *ListRuntimeLogsParams, opts ...RequestOption) (*RuntimeLogPage, error) {
	return get[RuntimeLogPage](ctx, s.c, projectPath(team, project, "logs"), params.values(), opts)
}

// AllLogs iterates over every entry of ListLogs, fetching pages as needed. It stops at the first error, which it
// yields.
func (s *ObservabilityService) AllLogs(ctx context.Context, team, project string, params *ListRuntimeLogsParams, opts ...RequestOption) iter.Seq2[RuntimeLog, error] {
	p := deref(params)
	return paginate(p.Cursor, func(cursor string) ([]RuntimeLog, *string, error) {
		p.Cursor = cursor
		page, err := s.ListLogs(ctx, team, project, &p, opts...)
		if err != nil {
			return nil, nil, err
		}
		return page.Items, page.NextCursor, nil
	})
}

// GetLog returns one runtime log entry: GET /teams/{team}/projects/{project}/logs/{log}.
func (s *ObservabilityService) GetLog(ctx context.Context, team, project, log string, opts ...RequestOption) (*RuntimeLog, error) {
	return get[RuntimeLog](ctx, s.c, projectPath(team, project, "logs", esc(log)), nil, opts)
}

// Get returns request counts, errors and latency over a range, by route, status and region: GET
// /teams/{team}/projects/{project}/observability.
func (s *ObservabilityService) Get(ctx context.Context, team, project string, params *ObservabilityParams, opts ...RequestOption) (*Observability, error) {
	return get[Observability](ctx, s.c, projectPath(team, project, "observability"), params.values(), opts)
}

// GetAnalytics returns visitors and page views: GET /teams/{team}/projects/{project}/analytics. Enabled is false
// (and Locked true on the Free plan) when there is nothing to read.
func (s *ObservabilityService) GetAnalytics(ctx context.Context, team, project string, params *AnalyticsParams, opts ...RequestOption) (*Analytics, error) {
	return get[Analytics](ctx, s.c, projectPath(team, project, "analytics"), params.values(), opts)
}

// SetAnalytics turns Analytics on or off: PUT /teams/{team}/projects/{project}/analytics.
func (s *ObservabilityService) SetAnalytics(ctx context.Context, team, project string, enabled bool, opts ...RequestOption) (*InsightsSwitch, error) {
	return send[InsightsSwitch](ctx, s.c, http.MethodPut, projectPath(team, project, "analytics"), InsightsSwitch{Enabled: enabled}, opts)
}

// GetAnalyticsEvent returns one custom event's counts and properties: GET
// /teams/{team}/projects/{project}/analytics/event. params.Name is required.
func (s *ObservabilityService) GetAnalyticsEvent(ctx context.Context, team, project string, params AnalyticsEventParams, opts ...RequestOption) (*AnalyticsEvent, error) {
	return get[AnalyticsEvent](ctx, s.c, projectPath(team, project, "analytics", "event"), params.values(), opts)
}

// GetSpeedInsights returns Core Web Vitals from real visits: GET /teams/{team}/projects/{project}/speed-insights.
func (s *ObservabilityService) GetSpeedInsights(ctx context.Context, team, project string, params *SpeedInsightsParams, opts ...RequestOption) (*SpeedInsights, error) {
	return get[SpeedInsights](ctx, s.c, projectPath(team, project, "speed-insights"), params.values(), opts)
}

// SetSpeedInsights turns Speed Insights on or off: PUT /teams/{team}/projects/{project}/speed-insights.
func (s *ObservabilityService) SetSpeedInsights(ctx context.Context, team, project string, enabled bool, opts ...RequestOption) (*InsightsSwitch, error) {
	return send[InsightsSwitch](ctx, s.c, http.MethodPut, projectPath(team, project, "speed-insights"), InsightsSwitch{Enabled: enabled}, opts)
}
