package orbits

import "context"

// OperationsService follows asynchronous changes, such as deleting a project.
type OperationsService struct{ c *Client }

// Get returns an operation: GET /operations/{operation}. Only its starter, or an owner or admin of its team,
// can read it.
func (s *OperationsService) Get(ctx context.Context, operationID string, opts ...RequestOption) (*Operation, error) {
	return get[Operation](ctx, s.c, "/operations/"+esc(operationID), nil, opts)
}
