package orbits

import (
	"context"
	"net/http"
)

// DomainsService manages a team's domains, their DNS records on Orbits DNS, and the branch addresses of
// projects.
type DomainsService struct{ c *Client }

// List lists a team's domains: GET /teams/{team}/domains.
func (s *DomainsService) List(ctx context.Context, team string, params *ListDomainsParams, opts ...RequestOption) (*DomainList, error) {
	return get[DomainList](ctx, s.c, teamPath(team, "domains"), params.values(), opts)
}

// Add adds a domain, to serve a project (or a branch's preview) or to redirect: POST /teams/{team}/domains. It
// starts pending: show RequiredRecord (external DNS) or Nameservers (Orbits DNS) to set, then Check.
func (s *DomainsService) Add(ctx context.Context, team string, req AddDomainRequest, opts ...RequestOption) (*Domain, error) {
	return send[Domain](ctx, s.c, http.MethodPost, teamPath(team, "domains"), req, opts)
}

// Preview says what adding a name would do, without adding it: GET /teams/{team}/domains/preview.
func (s *DomainsService) Preview(ctx context.Context, team string, params PreviewDomainParams, opts ...RequestOption) (*DomainPreview, error) {
	return get[DomainPreview](ctx, s.c, teamPath(team, "domains", "preview"), params.values(), opts)
}

// Get returns a domain, by name or dom_ id: GET /teams/{team}/domains/{domain}.
func (s *DomainsService) Get(ctx context.Context, team, domain string, opts ...RequestOption) (*Domain, error) {
	return get[Domain](ctx, s.c, teamPath(team, "domains", esc(domain)), nil, opts)
}

// Update changes what a domain serves or redirects to: PATCH /teams/{team}/domains/{domain}. Name a field in
// NullFields to clear it (project to stop serving, to to stop redirecting).
func (s *DomainsService) Update(ctx context.Context, team, domain string, req UpdateDomainRequest, opts ...RequestOption) (*Domain, error) {
	return send[Domain](ctx, s.c, http.MethodPatch, teamPath(team, "domains", esc(domain)), req, opts)
}

// Remove removes a domain; its project stops answering on it within a minute: DELETE
// /teams/{team}/domains/{domain}.
func (s *DomainsService) Remove(ctx context.Context, team, domain string, opts ...RequestOption) error {
	return s.c.Do(ctx, http.MethodDelete, teamPath(team, "domains", esc(domain)), nil, nil, nil, opts...)
}

// Check looks up the domain's DNS now and returns the domain as it stands: POST
// /teams/{team}/domains/{domain}/checks.
func (s *DomainsService) Check(ctx context.Context, team, domain string, opts ...RequestOption) (*Domain, error) {
	return send[Domain](ctx, s.c, http.MethodPost, teamPath(team, "domains", esc(domain), "checks"), nil, opts)
}

// ListRecords lists the DNS records of a domain on Orbits DNS: GET /teams/{team}/domains/{domain}/records.
func (s *DomainsService) ListRecords(ctx context.Context, team, domain string, opts ...RequestOption) (*DNSRecordList, error) {
	return get[DNSRecordList](ctx, s.c, teamPath(team, "domains", esc(domain), "records"), nil, opts)
}

// AddRecord adds a DNS record: POST /teams/{team}/domains/{domain}/records.
func (s *DomainsService) AddRecord(ctx context.Context, team, domain string, record DNSRecordInput, opts ...RequestOption) (*DNSRecord, error) {
	return send[DNSRecord](ctx, s.c, http.MethodPost, teamPath(team, "domains", esc(domain), "records"), record, opts)
}

// ReplaceRecord replaces a DNS record: PUT /teams/{team}/domains/{domain}/records/{record}. Records Orbits
// manages for a project (Managed) cannot be changed.
func (s *DomainsService) ReplaceRecord(ctx context.Context, team, domain, recordID string, record DNSRecordInput, opts ...RequestOption) (*DNSRecord, error) {
	return send[DNSRecord](ctx, s.c, http.MethodPut, teamPath(team, "domains", esc(domain), "records", esc(recordID)), record, opts)
}

// DeleteRecord deletes a DNS record: DELETE /teams/{team}/domains/{domain}/records/{record}.
func (s *DomainsService) DeleteRecord(ctx context.Context, team, domain, recordID string, opts ...RequestOption) error {
	return s.c.Do(ctx, http.MethodDelete, teamPath(team, "domains", esc(domain), "records", esc(recordID)), nil, nil, nil, opts...)
}

// ImportZone adds the records of a zone file (with DryRun, only says what it would add): POST
// /teams/{team}/domains/{domain}/zone-imports.
func (s *DomainsService) ImportZone(ctx context.Context, team, domain string, req ImportZoneRequest, opts ...RequestOption) (*ZoneImport, error) {
	return send[ZoneImport](ctx, s.c, http.MethodPost, teamPath(team, "domains", esc(domain), "zone-imports"), req, opts)
}

// GetEmailSetup returns a zone's mail setup and the records each provider needs: GET
// /teams/{team}/domains/{domain}/email.
func (s *DomainsService) GetEmailSetup(ctx context.Context, team, domain string, opts ...RequestOption) (*EmailSetup, error) {
	return get[EmailSetup](ctx, s.c, teamPath(team, "domains", esc(domain), "email"), nil, opts)
}

// SetEmailProvider adds a mail provider's records to a zone: PUT /teams/{team}/domains/{domain}/email. It
// returns the zone's records.
func (s *DomainsService) SetEmailProvider(ctx context.Context, team, domain string, req SetEmailProviderRequest, opts ...RequestOption) (*DNSRecordList, error) {
	return send[DNSRecordList](ctx, s.c, http.MethodPut, teamPath(team, "domains", esc(domain), "email"), req, opts)
}

// ListBranchAddresses lists the addresses a project's branches are served on: GET
// /teams/{team}/projects/{project}/branch-addresses.
func (s *DomainsService) ListBranchAddresses(ctx context.Context, team, project string, opts ...RequestOption) (*BranchAddressList, error) {
	return get[BranchAddressList](ctx, s.c, projectPath(team, project, "branch-addresses"), nil, opts)
}
