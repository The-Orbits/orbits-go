package orbits

import "context"

// BillingService reads plans, a team's billing and its invoices. Paying and changing the plan happen in the web
// app; API tokens can't.
type BillingService struct{ c *Client }

// ListPlans lists the plans and their prices: GET /plans.
func (s *BillingService) ListPlans(ctx context.Context, opts ...RequestOption) (*PlanList, error) {
	return get[PlanList](ctx, s.c, "/plans", nil, opts)
}

// Get returns a team's plan, what it may do, and its next charge: GET /teams/{team}/billing.
func (s *BillingService) Get(ctx context.Context, team string, opts ...RequestOption) (*Billing, error) {
	return get[Billing](ctx, s.c, teamPath(team, "billing"), nil, opts)
}

// ListInvoices lists a team's invoices, newest first: GET /teams/{team}/billing/invoices.
func (s *BillingService) ListInvoices(ctx context.Context, team string, params *ListInvoicesParams, opts ...RequestOption) (*InvoiceList, error) {
	return get[InvoiceList](ctx, s.c, teamPath(team, "billing", "invoices"), params.values(), opts)
}

// GetInvoice returns an invoice as a receipt: GET /teams/{team}/billing/invoices/{invoice}.
func (s *BillingService) GetInvoice(ctx context.Context, team, invoice string, opts ...RequestOption) (*Receipt, error) {
	return get[Receipt](ctx, s.c, teamPath(team, "billing", "invoices", esc(invoice)), nil, opts)
}
