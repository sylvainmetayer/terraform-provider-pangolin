package client

import (
	"context"
	"encoding/json"
	"fmt"
)

// SiteProvisioningKey lets Newt register new sites by itself (Pangolin
// 1.17+, Enterprise): `newt --provisioning-key <key>` creates the site.
type SiteProvisioningKey struct {
	ID              string  `json:"siteProvisioningKeyId"`
	Name            string  `json:"name"`
	Key             string  `json:"siteProvisioningKey"` // Only returned on creation.
	LastChars       string  `json:"lastChars"`
	CreatedAt       string  `json:"createdAt"`
	LastUsed        *string `json:"lastUsed"`
	MaxBatchSize    *int    `json:"maxBatchSize"`
	NumUsed         int     `json:"numUsed"`
	ValidUntil      *string `json:"validUntil"`
	ApproveNewSites bool    `json:"approveNewSites"`
}

// CreateSiteProvisioningKeyRequest is the body of PUT
// /org/{org}/site-provisioning-key. MaxBatchSize is always sent: the
// schema requires it, null means no limit.
type CreateSiteProvisioningKeyRequest struct {
	Name            string `json:"name"`
	MaxBatchSize    *int   `json:"maxBatchSize"`
	ValidUntil      string `json:"validUntil,omitempty"`
	ApproveNewSites bool   `json:"approveNewSites"`
}

// UpdateSiteProvisioningKeyRequest is the body of PATCH
// /org/{org}/site-provisioning-key/{id}. An empty ValidUntil clears the
// expiry, a null MaxBatchSize removes the limit.
type UpdateSiteProvisioningKeyRequest struct {
	MaxBatchSize    *int   `json:"maxBatchSize"`
	ValidUntil      string `json:"validUntil"`
	ApproveNewSites bool   `json:"approveNewSites"`
}

// CreateSiteProvisioningKey creates a key. Not retried: a repeated PUT
// after a lost response would mint a second, unknown key.
func (c *Client) CreateSiteProvisioningKey(ctx context.Context, req *CreateSiteProvisioningKeyRequest) (*SiteProvisioningKey, error) {
	resp, err := c.doRequestNoRetry(ctx, "PUT", fmt.Sprintf("/org/%s/site-provisioning-key", c.OrgID), req)
	if err != nil {
		return nil, err
	}
	var out SiteProvisioningKey
	if err := json.Unmarshal(resp.Data, &out); err != nil {
		return nil, fmt.Errorf("failed to parse site provisioning key: %w", err)
	}
	if out.ID == "" || out.Key == "" {
		return nil, fmt.Errorf("site provisioning key response has no id or key: %s", truncateForDiag(resp.Data))
	}
	return &out, nil
}

// GetSiteProvisioningKey reads a key. The server has no single GET: it
// pages through the org's list. Its pagination total is the page length,
// so the walk advances by rows and stops on a short or empty page.
func (c *Client) GetSiteProvisioningKey(ctx context.Context, id string) (*SiteProvisioningKey, error) {
	const limit = 1000
	for offset := 0; ; {
		resp, err := c.doRequest(ctx, "GET", fmt.Sprintf("/org/%s/site-provisioning-keys?limit=%d&offset=%d", c.OrgID, limit, offset), nil)
		if err != nil {
			return nil, err
		}
		var out struct {
			Keys []SiteProvisioningKey `json:"siteProvisioningKeys"`
		}
		if err := json.Unmarshal(resp.Data, &out); err != nil {
			return nil, fmt.Errorf("failed to parse site provisioning keys: %w", err)
		}
		for i := range out.Keys {
			if out.Keys[i].ID == id {
				return &out.Keys[i], nil
			}
		}
		if len(out.Keys) < limit {
			return nil, ErrNotFound
		}
		offset += len(out.Keys)
	}
}

// UpdateSiteProvisioningKey updates the limits of a key.
func (c *Client) UpdateSiteProvisioningKey(ctx context.Context, id string, req *UpdateSiteProvisioningKeyRequest) error {
	_, err := c.doRequest(ctx, "PATCH", fmt.Sprintf("/org/%s/site-provisioning-key/%s", c.OrgID, id), req)
	return err
}

// DeleteSiteProvisioningKey deletes a key.
func (c *Client) DeleteSiteProvisioningKey(ctx context.Context, id string) error {
	_, err := c.doRequest(ctx, "DELETE", fmt.Sprintf("/org/%s/site-provisioning-key/%s", c.OrgID, id), nil)
	return err
}
