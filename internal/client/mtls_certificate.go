package client

import (
	"context"
	"encoding/json"
	"fmt"
)

// MTLSCertificate is a trusted CA certificate of a resource or resource
// policy (Pangolin 1.25.3+, Enterprise). The PEM is never returned.
type MTLSCertificate struct {
	ID          int     `json:"mtlsCertificateId"`
	Name        *string `json:"name"`
	Subject     *string `json:"subject"`
	Issuer      *string `json:"issuer"`
	Fingerprint *string `json:"fingerprint"`
}

// MTLSOwner addresses the object that holds the certificates: a resource
// or a resource policy.
type MTLSOwner struct {
	ResourceID       int
	ResourcePolicyID int
}

func (o MTLSOwner) base() string {
	if o.ResourcePolicyID != 0 {
		return fmt.Sprintf("/resource-policy/%d", o.ResourcePolicyID)
	}
	return fmt.Sprintf("/resource/%d", o.ResourceID)
}

// CreateMTLSCertificate adds a CA certificate. The server splits a PEM
// bundle and answers with one entry per stored certificate, so callers
// send a single certificate. Not retried: a repeated PUT after a lost
// response would be refused as a duplicate.
func (c *Client) CreateMTLSCertificate(ctx context.Context, o MTLSOwner, name *string, pem string) (*MTLSCertificate, error) {
	body := map[string]interface{}{"certificate": pem}
	if name != nil {
		body["name"] = *name
	}
	resp, err := c.doRequestNoRetry(ctx, "PUT", o.base()+"/mtls/certificate", body)
	if err != nil {
		return nil, err
	}
	var out struct {
		Certificates []MTLSCertificate `json:"certificates"`
	}
	if err := json.Unmarshal(resp.Data, &out); err != nil {
		return nil, fmt.Errorf("failed to parse mTLS certificate: %w", err)
	}
	if len(out.Certificates) != 1 {
		return nil, fmt.Errorf("expected one stored certificate, got %d: %s", len(out.Certificates), truncateForDiag(resp.Data))
	}
	return &out.Certificates[0], nil
}

// GetMTLSCertificate reads a certificate. The server has no single GET:
// it lists the owner's certificates (at most 20).
func (c *Client) GetMTLSCertificate(ctx context.Context, o MTLSOwner, id int) (*MTLSCertificate, error) {
	resp, err := c.doRequest(ctx, "GET", o.base()+"/mtls/certificates", nil)
	if err != nil {
		return nil, err
	}
	var out struct {
		Certificates []MTLSCertificate `json:"certificates"`
	}
	if err := json.Unmarshal(resp.Data, &out); err != nil {
		return nil, fmt.Errorf("failed to parse mTLS certificates: %w", err)
	}
	for i := range out.Certificates {
		if out.Certificates[i].ID == id {
			return &out.Certificates[i], nil
		}
	}
	return nil, ErrNotFound
}

// DeleteMTLSCertificate removes a certificate. Removing the last one
// makes the server switch mTLS off on the owner.
func (c *Client) DeleteMTLSCertificate(ctx context.Context, o MTLSOwner, id int) error {
	_, err := c.doRequest(ctx, "DELETE", fmt.Sprintf("%s/mtls/certificate/%d", o.base(), id), nil)
	return err
}

// GetMTLSEnabled reports the owner's mtlsEnabled flag. For a resource
// with no shared policy the server reports its inline policy's flag.
func (c *Client) GetMTLSEnabled(ctx context.Context, o MTLSOwner) (bool, error) {
	if o.ResourcePolicyID != 0 {
		p, err := c.GetResourcePolicy(ctx, o.ResourcePolicyID)
		if err != nil {
			return false, err
		}
		return p.MTLSEnabled, nil
	}
	r, err := c.GetResource(ctx, o.ResourceID)
	if err != nil {
		return false, err
	}
	return r.MTLSEnabled != nil && *r.MTLSEnabled, nil
}

// SetMTLSEnabled switches mTLS on or off. Switching on is refused by the
// server until the owner has a CA certificate (and, for a resource, SSL).
func (c *Client) SetMTLSEnabled(ctx context.Context, o MTLSOwner, enabled bool) error {
	_, err := c.doRequest(ctx, "POST", o.base(), map[string]interface{}{"mtlsEnabled": enabled})
	return err
}
