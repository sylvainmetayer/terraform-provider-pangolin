package client

import (
	"context"
	"encoding/json"
	"fmt"
)

// CreateDomainRequest is the body for PUT /org/{org}/domain. The
// server schema is strict: only these four keys are accepted.
// `certResolver` and `preferWildcardCert` only matter for wildcard
// domains (the update endpoint refuses them on ns / cname domains).
type CreateDomainRequest struct {
	Type               string  `json:"type"`
	BaseDomain         string  `json:"baseDomain"`
	CertResolver       *string `json:"certResolver,omitempty"`
	PreferWildcardCert *bool   `json:"preferWildcardCert,omitempty"`
}

// CreateDomainResponse is the subset of the create response the
// provider uses. The server also echoes the DNS records it generated
// (nsRecords / cnameRecords / aRecords), which the provider reads
// back through [Client.ListDomainDNSRecords] instead so create and
// refresh share one shape.
type CreateDomainResponse struct {
	DomainID string `json:"domainId"`
}

// CreateDomain adds a domain to orgID and returns its ID. On
// Community Edition only `type = "wildcard"` is accepted (ns and
// cname answer 501).
func (c *Client) CreateDomain(ctx context.Context, orgID string, req *CreateDomainRequest) (string, error) {
	resp, err := c.doRequest(ctx, "PUT", fmt.Sprintf("/org/%s/domain", orgID), req)
	if err != nil {
		return "", err
	}
	var out CreateDomainResponse
	if err := json.Unmarshal(resp.Data, &out); err != nil {
		return "", fmt.Errorf("failed to parse created domain: %w", err)
	}
	if out.DomainID == "" {
		return "", fmt.Errorf("create domain response has no domainId: %s", truncateForDiag(resp.Data))
	}
	return out.DomainID, nil
}

// UpdateDomainRequest is the body for POST /org/{org}/domain/{id}
// (the PATCH twin of this route is deprecated). `certResolver` is
// always sent: an explicit null clears the resolver, which the server
// applies whenever the key is present. `preferWildcardCert` is
// ignored by the server when null, so it is omitted instead. The
// server refuses both fields on non-wildcard domains (400).
type UpdateDomainRequest struct {
	CertResolver       *string `json:"certResolver"`
	PreferWildcardCert *bool   `json:"preferWildcardCert,omitempty"`
}

// UpdateDomain updates the cert-resolver settings of a wildcard domain.
func (c *Client) UpdateDomain(ctx context.Context, orgID, domainID string, req *UpdateDomainRequest) error {
	_, err := c.doRequest(ctx, "POST", fmt.Sprintf("/org/%s/domain/%s", orgID, domainID), req)
	return err
}

// DeleteDomain removes a domain from orgID. The server refuses (400)
// to delete a domain managed by its config file.
func (c *Client) DeleteDomain(ctx context.Context, orgID, domainID string) error {
	_, err := c.doRequest(ctx, "DELETE", fmt.Sprintf("/org/%s/domain/%s", orgID, domainID), nil)
	return err
}
