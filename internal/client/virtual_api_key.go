package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
)

// VirtualAPIKey is a manual virtual API key (Pangolin 1.26+), as returned
// by GET /virtual-api-key/{id}. The secret comes back decrypted.
type VirtualAPIKey struct {
	VirtualAPIKeyID string  `json:"virtualApiKeyId"`
	Name            *string `json:"name"`
	Description     *string `json:"description"`
	UserID          *string `json:"userId"`
	Secret          string  `json:"secret"`
	AllResources    bool    `json:"allResources"`
	ResourceIDs     []int   `json:"resourceIds"`
	// ExpiresAt is a Unix time in milliseconds.
	ExpiresAt *int64 `json:"expiresAt"`
}

// VirtualAPIKeyRequest is the body of PUT /org/{org}/virtual-api-key and
// POST /virtual-api-key/{id}. Both schemas are strict. ValidForSeconds is
// relative to the request time, so it is sent on create and on change only.
type VirtualAPIKeyRequest struct {
	Name         string `json:"name"`
	Description  any    `json:"description"` // null clears it
	UserID       any    `json:"userId"`      // null clears it
	AllResources bool   `json:"allResources"`
	ResourceIDs  []int  `json:"resourceIds"`
	// Omitted when nil; the JSON literal null clears the expiry on update.
	ValidForSeconds json.RawMessage `json:"validForSeconds,omitempty"`
}

func parseVirtualAPIKey(data json.RawMessage) (*VirtualAPIKey, error) {
	var out struct {
		Key *VirtualAPIKey `json:"virtualApiKey"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("failed to parse virtual API key: %w", err)
	}
	if out.Key == nil || out.Key.VirtualAPIKeyID == "" {
		return nil, fmt.Errorf("virtual API key response has no virtualApiKeyId: %s", truncateForDiag(data))
	}
	return out.Key, nil
}

// CreateVirtualAPIKey creates a virtual API key. Not retried: a repeated
// PUT after a lost response would mint a second key.
func (c *Client) CreateVirtualAPIKey(ctx context.Context, req *VirtualAPIKeyRequest) (*VirtualAPIKey, error) {
	resp, err := c.doRequestNoRetry(ctx, "PUT", fmt.Sprintf("/org/%s/virtual-api-key", c.OrgID), req)
	if err != nil {
		return nil, err
	}
	return parseVirtualAPIKey(resp.Data)
}

// GetVirtualAPIKey reads a virtual API key by ID.
func (c *Client) GetVirtualAPIKey(ctx context.Context, id string) (*VirtualAPIKey, error) {
	resp, err := c.doRequest(ctx, "GET", "/virtual-api-key/"+url.PathEscape(id), nil)
	if err != nil {
		return nil, err
	}
	return parseVirtualAPIKey(resp.Data)
}

// UpdateVirtualAPIKey updates a virtual API key.
func (c *Client) UpdateVirtualAPIKey(ctx context.Context, id string, req *VirtualAPIKeyRequest) (*VirtualAPIKey, error) {
	resp, err := c.doRequest(ctx, "POST", "/virtual-api-key/"+url.PathEscape(id), req)
	if err != nil {
		return nil, err
	}
	return parseVirtualAPIKey(resp.Data)
}

// DeleteVirtualAPIKey deletes a virtual API key.
func (c *Client) DeleteVirtualAPIKey(ctx context.Context, id string) error {
	_, err := c.doRequest(ctx, "DELETE", "/virtual-api-key/"+url.PathEscape(id), nil)
	return err
}
