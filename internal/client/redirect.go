package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
)

// Redirect is the subset of a redirect row the provider uses. Create,
// update and get all wrap it in `{"redirect": ...}`.
type Redirect struct {
	RedirectID      int     `json:"redirectId"`
	NiceID          string  `json:"niceId"`
	Name            string  `json:"name"`
	ResourceID      *int    `json:"resourceId"`
	DomainID        *string `json:"domainId"`
	Subdomain       *string `json:"subdomain"`
	DestinationHost string  `json:"destinationHost"`
	PathMatchType   string  `json:"pathMatchType"`
	MatchPath       *string `json:"matchPath"`
	RewritePath     *string `json:"rewritePath"`
	RewritePathType *string `json:"rewritePathType"`
	Priority        *int    `json:"priority"`
	Permanent       bool    `json:"permanent"`
	SSL             bool    `json:"ssl"`
	Enabled         bool    `json:"enabled"`
}

// RedirectRequest is the body of both PUT /org/{org}/redirect and POST
// /org/{org}/redirects/{id}. The nullable fields carry no omitempty: on
// update an explicit null is what clears them, and on create it means the
// same as leaving them out.
type RedirectRequest struct {
	Name            string  `json:"name"`
	ResourceID      *int    `json:"resourceId"`
	DomainID        *string `json:"domainId"`
	Subdomain       *string `json:"subdomain"`
	DestinationHost string  `json:"destinationHost"`
	PathMatchType   string  `json:"pathMatchType"`
	MatchPath       *string `json:"matchPath"`
	RewritePath     *string `json:"rewritePath"`
	RewritePathType *string `json:"rewritePathType"`
	Priority        int     `json:"priority"`
	Permanent       bool    `json:"permanent"`
	SSL             bool    `json:"ssl"`
	Enabled         bool    `json:"enabled"`
}

func parseRedirect(data json.RawMessage) (*Redirect, error) {
	var out struct {
		Redirect *Redirect `json:"redirect"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("failed to parse redirect: %w", err)
	}
	if out.Redirect == nil || out.Redirect.RedirectID == 0 {
		return nil, fmt.Errorf("redirect response has no redirectId: %s", truncateForDiag(data))
	}
	return out.Redirect, nil
}

// CreateRedirect creates a redirect. Not retried: a repeated PUT would
// create a second redirect.
func (c *Client) CreateRedirect(ctx context.Context, req *RedirectRequest) (*Redirect, error) {
	resp, err := c.doRequestNoRetry(ctx, "PUT", fmt.Sprintf("/org/%s/redirect", c.OrgID), req)
	if err != nil {
		return nil, err
	}
	return parseRedirect(resp.Data)
}

// GetRedirect reads a redirect by its numeric ID.
func (c *Client) GetRedirect(ctx context.Context, redirectID int) (*Redirect, error) {
	resp, err := c.doRequest(ctx, "GET", fmt.Sprintf("/org/%s/redirects/%d", c.OrgID, redirectID), nil)
	if err != nil {
		return nil, err
	}
	return parseRedirect(resp.Data)
}

// UpdateRedirect replaces every setting of a redirect.
func (c *Client) UpdateRedirect(ctx context.Context, redirectID int, req *RedirectRequest) (*Redirect, error) {
	resp, err := c.doRequest(ctx, "POST", fmt.Sprintf("/org/%s/redirects/%d", c.OrgID, redirectID), req)
	if err != nil {
		return nil, err
	}
	return parseRedirect(resp.Data)
}

// DeleteRedirect deletes a redirect.
func (c *Client) DeleteRedirect(ctx context.Context, redirectID int) error {
	_, err := c.doRequest(ctx, "DELETE", fmt.Sprintf("/org/%s/redirects/%d", c.OrgID, redirectID), nil)
	return err
}

// RedirectListItem is a redirect as GET /org/{org}/redirects returns it:
// the redirect plus the attached resource and the base domain.
type RedirectListItem struct {
	Redirect
	ResourceName       *string `json:"resourceName"`
	ResourceNiceID     *string `json:"resourceNiceId"`
	ResourceFullDomain *string `json:"resourceFullDomain"`
	BaseDomain         *string `json:"baseDomain"`
}

// RedirectListQuery filters ListRedirects. Zero values mean no filter.
type RedirectListQuery struct {
	Query string
	// Type is "permanent" (308) or "temporary" (307).
	Type       string
	Enabled    *bool
	ResourceID *int
	DomainID   string
}

// ListRedirects lists the redirects of the org, sorted by name.
func (c *Client) ListRedirects(ctx context.Context, q RedirectListQuery) ([]RedirectListItem, error) {
	v := url.Values{"sort_by": {"name"}}
	if q.Query != "" {
		v.Set("query", q.Query)
	}
	if q.Type != "" {
		v.Set("type", q.Type)
	}
	if q.Enabled != nil {
		v.Set("enabled", strconv.FormatBool(*q.Enabled))
	}
	if q.ResourceID != nil {
		v.Set("resourceId", strconv.Itoa(*q.ResourceID))
	}
	if q.DomainID != "" {
		v.Set("domainId", q.DomainID)
	}
	return listOrgQuery[RedirectListItem](ctx, c, "redirects", v.Encode(), "redirects")
}
