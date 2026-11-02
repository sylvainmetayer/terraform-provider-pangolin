package client

import (
	"context"
	"encoding/json"
	"fmt"
)

// ResourceAIProviderAttachment is one entry of the `aiProviders` list
// accepted by PUT /org/{org}/resource for `mode = "inference"`. The
// server attaches every provider in inherit mode at creation, so only
// the provider ID is sent.
type ResourceAIProviderAttachment struct {
	ProviderID int `json:"providerId"`
}

// ResourceAIProvider is one AI provider attached to an inference
// resource, as returned by GET /resource/{id}/ai-providers (1.22+).
type ResourceAIProvider struct {
	ProviderID      int    `json:"providerId"`
	NiceID          string `json:"niceId"`
	Name            string `json:"name"`
	Type            string `json:"type"`
	Enabled         bool   `json:"enabled"`
	ProviderEnabled bool   `json:"providerEnabled"`
	AccessMode      string `json:"accessMode"`
}

// ListResourceAIProviders returns the AI providers attached to an
// inference resource.
func (c *Client) ListResourceAIProviders(ctx context.Context, resourceID int) ([]ResourceAIProvider, error) {
	resp, err := c.doRequest(ctx, "GET", fmt.Sprintf("/resource/%d/ai-providers", resourceID), nil)
	if err != nil {
		return nil, err
	}
	var wrapper struct {
		Providers []ResourceAIProvider `json:"providers"`
	}
	if err := json.Unmarshal(resp.Data, &wrapper); err != nil {
		return nil, fmt.Errorf("failed to parse resource AI providers: %w", err)
	}
	return wrapper.Providers, nil
}

// AddResourceAIProvider attaches one AI provider to an inference
// resource, in inherit mode. Attaching an already attached provider is
// accepted by the server.
func (c *Client) AddResourceAIProvider(ctx context.Context, resourceID, providerID int) error {
	_, err := c.doRequest(ctx, "POST", fmt.Sprintf("/resource/%d/ai-providers/add", resourceID),
		map[string]int{"providerId": providerID})
	return err
}

// RemoveResourceAIProvider detaches one AI provider from an inference
// resource. The server answers 404 when the provider is not attached.
func (c *Client) RemoveResourceAIProvider(ctx context.Context, resourceID, providerID int) error {
	_, err := c.doRequest(ctx, "POST", fmt.Sprintf("/resource/%d/ai-providers/remove", resourceID),
		map[string]int{"providerId": providerID})
	return err
}
