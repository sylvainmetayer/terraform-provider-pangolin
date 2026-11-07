package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
)

// AIProvider is an AI provider of the organization (Pangolin 1.22+), as
// returned by GET /ai-provider/{id}. The API key comes back decrypted.
type AIProvider struct {
	ProviderID          int              `json:"providerId"`
	NiceID              string           `json:"niceId"`
	Name                string           `json:"name"`
	Type                string           `json:"type"`
	UpstreamURL         *string          `json:"upstreamUrl"`
	APIKey              *string          `json:"apiKey"`
	AuthType            string           `json:"authType"`
	RoutingMode         string           `json:"routingMode"`
	Capabilities        []string         `json:"capabilities"`
	Headers             []ResourceHeader `json:"headers"`
	SkipTLSVerification bool             `json:"skipTlsVerification"`
	Enabled             bool             `json:"enabled"`
}

// AIProviderRequest is the body of PUT /org/{org}/ai-provider and POST
// /ai-provider/{id}. Both schemas are strict, and the update one refuses
// `type`, so it stays empty there.
type AIProviderRequest struct {
	Name         string   `json:"name"`
	Type         string   `json:"type,omitempty"`
	UpstreamURL  *string  `json:"upstreamUrl,omitempty"`
	APIKey       *string  `json:"apiKey,omitempty"`
	AuthType     string   `json:"authType,omitempty"`
	RoutingMode  string   `json:"routingMode,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`
	// Always sent: null clears the headers.
	Headers             []ResourceHeader `json:"headers"`
	SkipTLSVerification *bool            `json:"skipTlsVerification,omitempty"`
	Enabled             *bool            `json:"enabled,omitempty"`
}

func parseAIProvider(data json.RawMessage) (*AIProvider, error) {
	var out struct {
		Provider *AIProvider `json:"provider"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("failed to parse AI provider: %w", err)
	}
	if out.Provider == nil || out.Provider.ProviderID == 0 {
		return nil, fmt.Errorf("AI provider response has no providerId: %s", truncateForDiag(data))
	}
	return out.Provider, nil
}

// CreateAIProvider creates an AI provider. Not retried: a repeated PUT
// after a lost response would create a second provider.
func (c *Client) CreateAIProvider(ctx context.Context, req *AIProviderRequest) (*AIProvider, error) {
	resp, err := c.doRequestNoRetry(ctx, "PUT", fmt.Sprintf("/org/%s/ai-provider", c.OrgID), req)
	if err != nil {
		return nil, err
	}
	return parseAIProvider(resp.Data)
}

// GetAIProvider reads an AI provider by ID.
func (c *Client) GetAIProvider(ctx context.Context, providerID int) (*AIProvider, error) {
	resp, err := c.doRequest(ctx, "GET", fmt.Sprintf("/ai-provider/%d", providerID), nil)
	if err != nil {
		return nil, err
	}
	return parseAIProvider(resp.Data)
}

// UpdateAIProvider updates an AI provider.
func (c *Client) UpdateAIProvider(ctx context.Context, providerID int, req *AIProviderRequest) (*AIProvider, error) {
	resp, err := c.doRequest(ctx, "POST", fmt.Sprintf("/ai-provider/%d", providerID), req)
	if err != nil {
		return nil, err
	}
	return parseAIProvider(resp.Data)
}

// DeleteAIProvider deletes an AI provider.
func (c *Client) DeleteAIProvider(ctx context.Context, providerID int) error {
	_, err := c.doRequest(ctx, "DELETE", fmt.Sprintf("/ai-provider/%d", providerID), nil)
	return err
}

// ListAIProviders lists the AI providers of the org. The API key is not
// part of the list. query filters on the name (case-insensitive).
func (c *Client) ListAIProviders(ctx context.Context, query string) ([]AIProvider, error) {
	q := url.Values{}
	if query != "" {
		q.Set("query", query)
	}
	return listOrgQuery[AIProvider](ctx, c, "ai-providers", q.Encode(), "providers")
}
