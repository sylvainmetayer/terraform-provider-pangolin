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

// walkOrgList walks the paginated list /org/{org}/{route}, whose items sit
// under key, and hands each item to visit until it answers true. query holds
// extra query parameters, if any. A response without key or pagination.total
// is an error: callers read a miss as "gone", which removes the object from
// the state, so an unexpected shape must not look like one.
func (c *Client) walkOrgList(ctx context.Context, route, query, key string, visit func(json.RawMessage) (bool, error)) error {
	seen := 0
	for page := 1; ; page++ {
		path := fmt.Sprintf("/org/%s/%s?pageSize=%d&page=%d", c.OrgID, route, labelListPageSize, page)
		if query != "" {
			path += "&" + query
		}
		resp, err := c.doRequest(ctx, "GET", path, nil)
		if err != nil {
			return err
		}
		var out map[string]json.RawMessage
		if err := json.Unmarshal(resp.Data, &out); err != nil {
			return fmt.Errorf("failed to parse %s: %w", route, err)
		}
		raw, ok := out[key]
		if !ok {
			return fmt.Errorf("%s response has no %q: %s", route, key, truncateForDiag(resp.Data))
		}
		var items []json.RawMessage
		if err := json.Unmarshal(raw, &items); err != nil {
			return fmt.Errorf("failed to parse %s: %w", route, err)
		}
		var pagination struct {
			Total *int `json:"total"`
		}
		if err := json.Unmarshal(out["pagination"], &pagination); err != nil || pagination.Total == nil {
			return fmt.Errorf("%s response has no pagination total: %s", route, truncateForDiag(resp.Data))
		}
		for _, item := range items {
			done, err := visit(item)
			if err != nil || done {
				return err
			}
		}
		seen += len(items)
		if len(items) == 0 || seen >= *pagination.Total {
			return nil
		}
	}
}

// listOrg returns every item of the paginated list /org/{org}/{route}.
func listOrg[T any](ctx context.Context, c *Client, route, key string) ([]T, error) {
	items := []T{}
	err := c.walkOrgList(ctx, route, "", key, func(raw json.RawMessage) (bool, error) {
		var item T
		if err := json.Unmarshal(raw, &item); err != nil {
			return false, fmt.Errorf("failed to parse %s: %w", route, err)
		}
		items = append(items, item)
		return false, nil
	})
	return items, err
}

// GetLabel finds a label by ID. The API has no route for a single label, so
// this walks the list and answers ErrNotFound when it is not there.
func (c *Client) GetLabel(ctx context.Context, labelID int) (*Label, error) {
	var found *Label
	err := c.walkOrgList(ctx, "labels", "", "labels", func(raw json.RawMessage) (bool, error) {
		var l Label
		if err := json.Unmarshal(raw, &l); err != nil {
			return false, fmt.Errorf("failed to parse label: %w", err)
		}
		if l.LabelID != labelID {
			return false, nil
		}
		found = &l
		return true, nil
	})
	if err != nil {
		return nil, err
	}
	if found == nil {
		return nil, fmt.Errorf("label %d: %w", labelID, ErrNotFound)
	}
	return found, nil
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
// clients only, not user devices, and only the active ones unless asked for
// every status: attaching does not care about a client's status.
func (c *Client) LabelAttached(ctx context.Context, labelID int, target LabelTarget) (bool, error) {
	var route, query, key, idKey string
	var id int
	switch {
	case target.SiteID != nil:
		route, key, idKey, id = "sites", "sites", "siteId", *target.SiteID
	case target.ResourceID != nil:
		route, key, idKey, id = "resources", "resources", "resourceId", *target.ResourceID
	case target.SiteResourceID != nil:
		route, key, idKey, id = "site-resources", "siteResources", "siteResourceId", *target.SiteResourceID
	case target.ClientID != nil:
		route, query, key, idKey, id = "clients", "status=active,blocked,archived", "clients", "clientId", *target.ClientID
	default:
		return false, fmt.Errorf("label target names nothing")
	}
	attached := false
	err := c.walkOrgList(ctx, route, query, key, func(raw json.RawMessage) (bool, error) {
		var item map[string]json.RawMessage
		if err := json.Unmarshal(raw, &item); err != nil {
			return false, fmt.Errorf("failed to parse %s: %w", route, err)
		}
		var itemID int
		if err := json.Unmarshal(item[idKey], &itemID); err != nil {
			return false, fmt.Errorf("%s item has no %s: %s", route, idKey, truncateForDiag(raw))
		}
		if itemID != id {
			return false, nil
		}
		var labels []Label
		if rawLabels, ok := item["labels"]; ok {
			if err := json.Unmarshal(rawLabels, &labels); err != nil {
				return false, fmt.Errorf("failed to parse labels of %s %d: %w", route, id, err)
			}
		}
		for _, l := range labels {
			if l.LabelID == labelID {
				attached = true
			}
		}
		return true, nil
	})
	return attached, err
}
