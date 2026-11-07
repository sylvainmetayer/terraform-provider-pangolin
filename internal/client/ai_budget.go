package client

import (
	"context"
	"encoding/json"
	"fmt"
)

// AIBudget is a spending cap of the AI gateway (Pangolin 1.26+). At most
// one of the scope IDs is set; none means the whole organization.
type AIBudget struct {
	BudgetID        int     `json:"budgetId"`
	ProviderID      *int    `json:"providerId"`
	ModelID         *int    `json:"modelId"`
	ResourceID      *int    `json:"resourceId"`
	SiteResourceID  *int    `json:"siteResourceId"`
	RoleID          *int    `json:"roleId"`
	VirtualAPIKeyID *string `json:"virtualApiKeyId"`
	Amount          float64 `json:"amount"`
	Unit            string  `json:"unit"`
	Period          string  `json:"period"`
	Enforcement     string  `json:"enforcement"`
	Enabled         bool    `json:"enabled"`
}

// AIBudgetRequest is the body of PUT /org/{org}/ai-budget. The schema is
// strict and refuses null scopes, so unset ones are omitted.
type AIBudgetRequest struct {
	ProviderID      *int    `json:"providerId,omitempty"`
	ModelID         *int    `json:"modelId,omitempty"`
	ResourceID      *int    `json:"resourceId,omitempty"`
	SiteResourceID  *int    `json:"siteResourceId,omitempty"`
	RoleID          *int    `json:"roleId,omitempty"`
	VirtualAPIKeyID *string `json:"virtualApiKeyId,omitempty"`
	Amount          float64 `json:"amount"`
	Unit            string  `json:"unit"`
	Period          string  `json:"period"`
	Enforcement     string  `json:"enforcement"`
	Enabled         *bool   `json:"enabled,omitempty"`
}

// AIBudgetUpdateRequest is the body of POST /ai-budget/{id}. The scopes are
// always sent: null clears the one that was set before.
type AIBudgetUpdateRequest struct {
	ProviderID      *int    `json:"providerId"`
	ModelID         *int    `json:"modelId"`
	ResourceID      *int    `json:"resourceId"`
	SiteResourceID  *int    `json:"siteResourceId"`
	RoleID          *int    `json:"roleId"`
	VirtualAPIKeyID *string `json:"virtualApiKeyId"`
	Amount          float64 `json:"amount"`
	Unit            string  `json:"unit"`
	Period          string  `json:"period"`
	Enforcement     string  `json:"enforcement"`
	Enabled         *bool   `json:"enabled,omitempty"`
}

func parseAIBudget(data json.RawMessage) (*AIBudget, error) {
	var out struct {
		Budget *AIBudget `json:"budget"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("failed to parse AI budget: %w", err)
	}
	if out.Budget == nil || out.Budget.BudgetID == 0 {
		return nil, fmt.Errorf("AI budget response has no budgetId: %s", truncateForDiag(data))
	}
	return out.Budget, nil
}

// CreateAIBudget creates an AI budget. Not retried: a repeated PUT after a
// lost response would hit the per-scope uniqueness check and fail.
func (c *Client) CreateAIBudget(ctx context.Context, req *AIBudgetRequest) (*AIBudget, error) {
	resp, err := c.doRequestNoRetry(ctx, "PUT", fmt.Sprintf("/org/%s/ai-budget", c.OrgID), req)
	if err != nil {
		return nil, err
	}
	return parseAIBudget(resp.Data)
}

// GetAIBudget reads an AI budget by ID.
func (c *Client) GetAIBudget(ctx context.Context, budgetID int) (*AIBudget, error) {
	resp, err := c.doRequest(ctx, "GET", fmt.Sprintf("/ai-budget/%d", budgetID), nil)
	if err != nil {
		return nil, err
	}
	return parseAIBudget(resp.Data)
}

// UpdateAIBudget updates an AI budget.
func (c *Client) UpdateAIBudget(ctx context.Context, budgetID int, req *AIBudgetUpdateRequest) (*AIBudget, error) {
	resp, err := c.doRequest(ctx, "POST", fmt.Sprintf("/ai-budget/%d", budgetID), req)
	if err != nil {
		return nil, err
	}
	return parseAIBudget(resp.Data)
}

// DeleteAIBudget deletes an AI budget.
func (c *Client) DeleteAIBudget(ctx context.Context, budgetID int) error {
	_, err := c.doRequest(ctx, "DELETE", fmt.Sprintf("/ai-budget/%d", budgetID), nil)
	return err
}

// ListAIBudgets lists every AI budget of the org.
func (c *Client) ListAIBudgets(ctx context.Context) ([]AIBudget, error) {
	return listOrg[AIBudget](ctx, c, "ai-budgets", "budgets")
}
