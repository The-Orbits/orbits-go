package orbits

import "context"

// TemplatesService lists the templates a project can start from.
type TemplatesService struct{ c *Client }

// List lists the templates: GET /templates. Send a template's Key as ProjectCreate.Template.
func (s *TemplatesService) List(ctx context.Context, opts ...RequestOption) (*TemplateList, error) {
	return get[TemplateList](ctx, s.c, "/templates", nil, opts)
}
