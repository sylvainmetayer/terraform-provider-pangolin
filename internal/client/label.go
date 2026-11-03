package client

import (
	"context"
	"encoding/json"
	"fmt"
)

// labelListPageSize is the page size of every list this file walks.
const labelListPageSize = 100

// Label is an organization label.
type Label struct {
	LabelID int    `json:"labelId"`
	Name    string `json:"name"`
	Color   string `json:"color"`
}

// LabelRequest is the body of both POST /org/{org}/labels and PATCH
// /org/{org}/label/{id}.
type LabelRequest struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

// LabelTarget names what a label attaches to: exactly one field is set.
type LabelTarget struct {
	SiteID         *int `json:"siteId,omitempty"`
	ResourceID     *int `json:"resourceId,omitempty"`
	SiteResourceID *int `json:"siteResourceId,omitempty"`
	ClientID       *int `json:"clientId,omitempty"`
}

func parseLabel(data json.RawMessage) (*Label, error) {
	var out struct {
		Label *Label `json:"label"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("failed to parse label: %w", err)
	}
	if out.Label == nil || out.Label.LabelID == 0 {
		return nil, fmt.Errorf("label response has no labelId: %s", truncateForDiag(data))
	}
	return out.Label, nil
}

// CreateLabel creates a label. Not retried: a repeated POST after a lost
// response would answer 409 on the label it just created.
func (c *Client) CreateLabel(ctx context.Context, req *LabelRequest) (*Label, error) {
	resp, err := c.doRequestNoRetry(ctx, "POST", fmt.Sprintf("/org/%s/labels", c.OrgID), req)
	if err != nil {
		return nil, err
	}
	return parseLabel(resp.Data)
}

// GetLabel finds a label by ID. The API has no route for a single label, so
// this walks the list and answers ErrNotFound when it is not there.
func (c *Client) GetLabel(ctx context.Context, labelID int) (*Label, error) {
	seen := 0
	for page := 1; ; page++ {
		resp, err := c.doRequest(ctx, "GET", fmt.Sprintf("/org/%s/labels?pageSize=%d&page=%d", c.OrgID, labelListPageSize, page), nil)
		if err != nil {
			return nil, err
		}
		var out struct {
			Labels     []Label `json:"labels"`
			Pagination struct {
				Total int `json:"total"`
			} `json:"pagination"`
		}
		if err := json.Unmarshal(resp.Data, &out); err != nil {
			return nil, fmt.Errorf("failed to parse labels: %w", err)
		}
		for i := range out.Labels {
			if out.Labels[i].LabelID == labelID {
				return &out.Labels[i], nil
			}
		}
		seen += len(out.Labels)
		if len(out.Labels) == 0 || seen >= out.Pagination.Total {
			return nil, fmt.Errorf("label %d: %w", labelID, ErrNotFound)
		}
	}
}

// UpdateLabel changes the name and color of a label.
func (c *Client) UpdateLabel(ctx context.Context, labelID int, req *LabelRequest) (*Label, error) {
	resp, err := c.doRequest(ctx, "PATCH", fmt.Sprintf("/org/%s/label/%d", c.OrgID, labelID), req)
	if err != nil {
		return nil, err
	}
	return parseLabel(resp.Data)
}

// DeleteLabel deletes a label and, through the foreign keys, its attachments.
func (c *Client) DeleteLabel(ctx context.Context, labelID int) error {
	_, err := c.doRequest(ctx, "DELETE", fmt.Sprintf("/org/%s/label/%d", c.OrgID, labelID), nil)
	return err
}

// AttachLabel attaches a label to its target. The route is idempotent.
func (c *Client) AttachLabel(ctx context.Context, labelID int, target LabelTarget) error {
	_, err := c.doRequest(ctx, "PUT", fmt.Sprintf("/org/%s/label/%d/attach", c.OrgID, labelID), target)
	return err
}

// DetachLabel detaches a label from its target.
func (c *Client) DetachLabel(ctx context.Context, labelID int, target LabelTarget) error {
	_, err := c.doRequest(ctx, "PUT", fmt.Sprintf("/org/%s/label/%d/detach", c.OrgID, labelID), target)
	return err
}

// LabelAttached says whether the label is on its target. Only the list routes
// return an item's labels, so this walks the list of the target's kind. A
// target that is gone counts as not attached. The clients list holds machine
// clients only, not user devices.
func (c *Client) LabelAttached(ctx context.Context, labelID int, target LabelTarget) (bool, error) {
	var route, key, idKey string
	var id int
	switch {
	case target.SiteID != nil:
		route, key, idKey, id = "sites", "sites", "siteId", *target.SiteID
	case target.ResourceID != nil:
		route, key, idKey, id = "resources", "resources", "resourceId", *target.ResourceID
	case target.SiteResourceID != nil:
		route, key, idKey, id = "site-resources", "siteResources", "siteResourceId", *target.SiteResourceID
	case target.ClientID != nil:
		route, key, idKey, id = "clients", "clients", "clientId", *target.ClientID
	default:
		return false, fmt.Errorf("label target names nothing")
	}
	seen := 0
	for page := 1; ; page++ {
		resp, err := c.doRequest(ctx, "GET", fmt.Sprintf("/org/%s/%s?pageSize=%d&page=%d", c.OrgID, route, labelListPageSize, page), nil)
		if err != nil {
			return false, err
		}
		var out map[string]json.RawMessage
		if err := json.Unmarshal(resp.Data, &out); err != nil {
			return false, fmt.Errorf("failed to parse %s: %w", route, err)
		}
		var items []map[string]json.RawMessage
		if raw, ok := out[key]; ok {
			if err := json.Unmarshal(raw, &items); err != nil {
				return false, fmt.Errorf("failed to parse %s: %w", route, err)
			}
		}
		for _, item := range items {
			var itemID int
			if err := json.Unmarshal(item[idKey], &itemID); err != nil || itemID != id {
				continue
			}
			var labels []Label
			if raw, ok := item["labels"]; ok {
				if err := json.Unmarshal(raw, &labels); err != nil {
					return false, fmt.Errorf("failed to parse labels of %s %d: %w", route, id, err)
				}
			}
			for _, l := range labels {
				if l.LabelID == labelID {
					return true, nil
				}
			}
			return false, nil
		}
		var pagination struct {
			Total int `json:"total"`
		}
		if raw, ok := out["pagination"]; ok {
			if err := json.Unmarshal(raw, &pagination); err != nil {
				return false, fmt.Errorf("failed to parse %s pagination: %w", route, err)
			}
		}
		seen += len(items)
		if len(items) == 0 || seen >= pagination.Total {
			return false, nil
		}
	}
}
