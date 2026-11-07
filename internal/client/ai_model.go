package client

import (
	"context"
	"encoding/json"
	"fmt"
)

// AIModel is a model entry of an AI provider (Pangolin 1.22+): an allow
// or block rule for one upstream model key.
type AIModel struct {
	ModelID    int    `json:"modelId"`
	ProviderID int    `json:"providerId"`
	ModelKey   string `json:"modelKey"`
	Name       string `json:"name"`
	ListType   string `json:"listType"`
	Enabled    bool   `json:"enabled"`
}

// AIModelRequest is the body of PUT /ai-provider/{id}/model and POST
// /ai-model/{id}.
type AIModelRequest struct {
	ModelKey string `json:"modelKey"`
	Name     string `json:"name"`
	Enabled  *bool  `json:"enabled,omitempty"`
	ListType string `json:"listType,omitempty"`
}

func parseAIModel(data json.RawMessage) (*AIModel, error) {
	var out struct {
		Model *AIModel `json:"model"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("failed to parse AI model: %w", err)
	}
	if out.Model == nil || out.Model.ModelID == 0 {
		return nil, fmt.Errorf("AI model response has no modelId: %s", truncateForDiag(data))
	}
	return out.Model, nil
}

// CreateAIModel adds a model to an AI provider. Not retried: a repeated
// PUT after a lost response would hit the (provider, modelKey) unique key.
func (c *Client) CreateAIModel(ctx context.Context, providerID int, req *AIModelRequest) (*AIModel, error) {
	resp, err := c.doRequestNoRetry(ctx, "PUT", fmt.Sprintf("/ai-provider/%d/model", providerID), req)
	if err != nil {
		return nil, err
	}
	return parseAIModel(resp.Data)
}

// GetAIModel reads an AI model by ID.
func (c *Client) GetAIModel(ctx context.Context, modelID int) (*AIModel, error) {
	resp, err := c.doRequest(ctx, "GET", fmt.Sprintf("/ai-model/%d", modelID), nil)
	if err != nil {
		return nil, err
	}
	return parseAIModel(resp.Data)
}

// UpdateAIModel updates an AI model.
func (c *Client) UpdateAIModel(ctx context.Context, modelID int, req *AIModelRequest) (*AIModel, error) {
	resp, err := c.doRequest(ctx, "POST", fmt.Sprintf("/ai-model/%d", modelID), req)
	if err != nil {
		return nil, err
	}
	return parseAIModel(resp.Data)
}

// DeleteAIModel deletes an AI model.
func (c *Client) DeleteAIModel(ctx context.Context, modelID int) error {
	_, err := c.doRequest(ctx, "DELETE", fmt.Sprintf("/ai-model/%d", modelID), nil)
	return err
}
