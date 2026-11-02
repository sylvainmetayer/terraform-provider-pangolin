package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
)

// --- Resource policies (Pangolin Enterprise) ---
//
// A resource policy bundles the access settings of an HTTP resource
// (SSO, role / user grants, email whitelist, rules and the password,
// PIN code and header-auth secrets) so several resources can share
// them. Create, list and delete live on the licensed router
// (`tierMatrix.resourcePolicies`); get, update and the set* endpoints
// live on the public integration router.

// ResourcePolicyRule is one access rule of a resource policy, as sent
// to the create / set-rules endpoints and returned by GET.
type ResourcePolicyRule struct {
	RuleID   int    `json:"ruleId,omitempty"`
	Action   string `json:"action"`
	Match    string `json:"match"`
	Value    string `json:"value"`
	Priority int    `json:"priority"`
	Enabled  *bool  `json:"enabled,omitempty"`
}

// IsEnabled reports whether the rule is enabled; the server's default
// for a missing value is true.
func (r ResourcePolicyRule) IsEnabled() bool {
	return r.Enabled == nil || *r.Enabled
}

// ResourcePolicyRole is a role granted access by a policy. GET leaves
// out the org admin role, which the server always keeps bound.
type ResourcePolicyRole struct {
	RoleID int    `json:"roleId"`
	Name   string `json:"name"`
}

// ResourcePolicyUser is a user granted access by a policy.
type ResourcePolicyUser struct {
	UserID   string  `json:"userId"`
	Email    *string `json:"email"`
	Name     *string `json:"name"`
	Username string  `json:"username"`
	Type     string  `json:"type"`
	IDPName  *string `json:"idpName"`
}

// ResourcePolicyWhitelistEntry is one email of the policy whitelist.
type ResourcePolicyWhitelistEntry struct {
	WhitelistID int    `json:"whiteListId"`
	Email       string `json:"email"`
}

// ResourcePolicyHeaderAuth reports whether header auth is configured.
// Both fields are null when it is not. The server spells the second
// key `extendedCompability`.
type ResourcePolicyHeaderAuth struct {
	ID                    *int  `json:"id"`
	ExtendedCompatibility *bool `json:"extendedCompability"`
}

// ResourcePolicy is the GET /resource-policy/{id} payload. Secrets are
// never returned: PasswordID, PincodeID and HeaderAuth.ID only tell
// whether one is set.
type ResourcePolicy struct {
	ResourcePolicyID      int                            `json:"resourcePolicyId"`
	NiceID                string                         `json:"niceId"`
	Name                  string                         `json:"name"`
	SSO                   bool                           `json:"sso"`
	ApplyRules            bool                           `json:"applyRules"`
	EmailWhitelistEnabled bool                           `json:"emailWhitelistEnabled"`
	IDPID                 *int                           `json:"idpId"`
	PasswordID            *int                           `json:"passwordId"`
	PincodeID             *int                           `json:"pincodeId"`
	HeaderAuth            *ResourcePolicyHeaderAuth      `json:"headerAuth"`
	Roles                 []ResourcePolicyRole           `json:"roles"`
	Users                 []ResourcePolicyUser           `json:"users"`
	EmailWhitelist        []ResourcePolicyWhitelistEntry `json:"emailWhiteList"`
	Rules                 []ResourcePolicyRule           `json:"rules"`
}

// HasHeaderAuth reports whether header auth is set on the policy.
func (p *ResourcePolicy) HasHeaderAuth() bool {
	return p.HeaderAuth != nil && p.HeaderAuth.ID != nil
}

// CreateResourcePolicyRequest is the payload of
// POST /org/{org}/resource-policy. The create schema parses role IDs
// as strings (`z.string().transform(Number)`), unlike the
// access-control endpoint which takes integers. Secrets are left to
// the dedicated set* endpoints so this payload never carries them.
type CreateResourcePolicyRequest struct {
	Name                  string               `json:"name"`
	SSO                   bool                 `json:"sso"`
	SkipToIdpID           *int                 `json:"skipToIdpId"`
	RoleIDs               []string             `json:"roleIds"`
	UserIDs               []string             `json:"userIds"`
	EmailWhitelistEnabled bool                 `json:"emailWhitelistEnabled"`
	Emails                []string             `json:"emails"`
	ApplyRules            bool                 `json:"applyRules"`
	Rules                 []ResourcePolicyRule `json:"rules"`
}

// CreatedResourcePolicy is the raw row returned by create / update.
type CreatedResourcePolicy struct {
	ResourcePolicyID int    `json:"resourcePolicyId"`
	NiceID           string `json:"niceId"`
	Name             string `json:"name"`
}

// CreateResourcePolicy creates a shared resource policy in the
// client's org.
func (c *Client) CreateResourcePolicy(ctx context.Context, req *CreateResourcePolicyRequest) (*CreatedResourcePolicy, error) {
	// The array keys must be JSON arrays, never null.
	body := *req
	if body.RoleIDs == nil {
		body.RoleIDs = []string{}
	}
	if body.UserIDs == nil {
		body.UserIDs = []string{}
	}
	if body.Emails == nil {
		body.Emails = []string{}
	}
	if body.Rules == nil {
		body.Rules = []ResourcePolicyRule{}
	}
	resp, err := c.doRequest(ctx, "POST", fmt.Sprintf("/org/%s/resource-policy", c.OrgID), &body)
	if err != nil {
		return nil, err
	}
	var out CreatedResourcePolicy
	if err := json.Unmarshal(resp.Data, &out); err != nil {
		return nil, fmt.Errorf("failed to parse resource policy: %w", err)
	}
	return &out, nil
}

// GetResourcePolicy returns a policy with its grants, whitelist and
// rules.
func (c *Client) GetResourcePolicy(ctx context.Context, policyID int) (*ResourcePolicy, error) {
	resp, err := c.doRequest(ctx, "GET", fmt.Sprintf("/resource-policy/%d", policyID), nil)
	if err != nil {
		return nil, err
	}
	var out ResourcePolicy
	if err := json.Unmarshal(resp.Data, &out); err != nil {
		return nil, fmt.Errorf("failed to parse resource policy: %w", err)
	}
	return &out, nil
}

// UpdateResourcePolicyRequest renames a policy. Both keys are
// optional server-side.
type UpdateResourcePolicyRequest struct {
	Name   string `json:"name,omitempty"`
	NiceID string `json:"niceId,omitempty"`
}

// UpdateResourcePolicy updates the name and / or nice ID of a policy.
func (c *Client) UpdateResourcePolicy(ctx context.Context, policyID int, req *UpdateResourcePolicyRequest) error {
	_, err := c.doRequest(ctx, "POST", fmt.Sprintf("/resource-policy/%d", policyID), req)
	return err
}

// DeleteResourcePolicy deletes a policy. The server refuses (400) while
// a resource is still attached to it.
func (c *Client) DeleteResourcePolicy(ctx context.Context, policyID int) error {
	_, err := c.doRequest(ctx, "DELETE", fmt.Sprintf("/resource-policy/%d", policyID), nil)
	return err
}

// SetResourcePolicyAccessControlRequest replaces SSO, the IdP shortcut
// and the role / user grants. Role IDs are integers here. A null
// skipToIdpId clears the shortcut; the admin role stays bound.
type SetResourcePolicyAccessControlRequest struct {
	SSO         bool     `json:"sso"`
	UserIDs     []string `json:"userIds"`
	RoleIDs     []int    `json:"roleIds"`
	SkipToIdpID *int     `json:"skipToIdpId"`
}

// SetResourcePolicyAccessControl replaces the access-control settings.
func (c *Client) SetResourcePolicyAccessControl(ctx context.Context, policyID int, req *SetResourcePolicyAccessControlRequest) error {
	body := *req
	if body.UserIDs == nil {
		body.UserIDs = []string{}
	}
	if body.RoleIDs == nil {
		body.RoleIDs = []int{}
	}
	_, err := c.doRequest(ctx, "POST", fmt.Sprintf("/resource-policy/%d/access-control", policyID), &body)
	return err
}

// SetResourcePolicyWhitelist replaces the email whitelist. The server
// only stores the emails while the whitelist is enabled.
func (c *Client) SetResourcePolicyWhitelist(ctx context.Context, policyID int, enabled bool, emails []string) error {
	if emails == nil {
		emails = []string{}
	}
	body := map[string]interface{}{"emailWhitelistEnabled": enabled, "emails": emails}
	_, err := c.doRequest(ctx, "POST", fmt.Sprintf("/resource-policy/%d/whitelist", policyID), body)
	return err
}

// SetResourcePolicyRules replaces every rule of the policy. Rules sent
// without a ruleId are created; existing ones not listed are deleted.
func (c *Client) SetResourcePolicyRules(ctx context.Context, policyID int, applyRules bool, rules []ResourcePolicyRule) error {
	if rules == nil {
		rules = []ResourcePolicyRule{}
	}
	body := map[string]interface{}{"applyRules": applyRules, "rules": rules}
	_, err := c.doRequest(ctx, "POST", fmt.Sprintf("/resource-policy/%d/rules", policyID), body)
	return err
}

// SetResourcePolicyPassword sets or clears (nil) the policy password.
func (c *Client) SetResourcePolicyPassword(ctx context.Context, policyID int, password *string) error {
	body := map[string]interface{}{"password": password}
	_, err := c.doRequest(ctx, "POST", fmt.Sprintf("/resource-policy/%d/password", policyID), body)
	return err
}

// SetResourcePolicyPincode sets or clears (nil) the policy PIN code.
func (c *Client) SetResourcePolicyPincode(ctx context.Context, policyID int, pincode *string) error {
	body := map[string]interface{}{"pincode": pincode}
	_, err := c.doRequest(ctx, "POST", fmt.Sprintf("/resource-policy/%d/pincode", policyID), body)
	return err
}

// ResourcePolicyHeaderAuthCredentials is the nested headerAuth object
// of POST /resource-policy/{id}/header-auth.
type ResourcePolicyHeaderAuthCredentials struct {
	User                  string `json:"user"`
	Password              string `json:"password"`
	ExtendedCompatibility bool   `json:"extendedCompatibility"`
}

// SetResourcePolicyHeaderAuth sets or clears (nil) the policy header
// authentication.
func (c *Client) SetResourcePolicyHeaderAuth(ctx context.Context, policyID int, creds *ResourcePolicyHeaderAuthCredentials) error {
	body := map[string]interface{}{"headerAuth": creds}
	_, err := c.doRequest(ctx, "POST", fmt.Sprintf("/resource-policy/%d/header-auth", policyID), body)
	return err
}

// ResourcePolicyAttachedResource is a resource attached to a policy,
// as listed by GET /org/{org}/resource-policies.
type ResourcePolicyAttachedResource struct {
	ResourceID int     `json:"resourceId"`
	NiceID     string  `json:"niceId"`
	Name       string  `json:"name"`
	FullDomain *string `json:"fullDomain"`
}

// ResourcePolicySummary is one entry of the policy list.
type ResourcePolicySummary struct {
	ResourcePolicyID int                              `json:"resourcePolicyId"`
	NiceID           string                           `json:"niceId"`
	Name             string                           `json:"name"`
	OrgID            string                           `json:"orgId"`
	Resources        []ResourcePolicyAttachedResource `json:"resources"`
}

// resourcePolicyListPageSize is the page size used to walk the list.
const resourcePolicyListPageSize = 100

// ListResourcePolicies returns every shared policy of the client's org,
// walking the paginated endpoint. Per-resource inline policies are not
// listed by the server. query filters on name / nice ID when non-empty.
func (c *Client) ListResourcePolicies(ctx context.Context, query string) ([]ResourcePolicySummary, error) {
	var all []ResourcePolicySummary
	for page := 1; ; page++ {
		params := url.Values{}
		params.Set("pageSize", strconv.Itoa(resourcePolicyListPageSize))
		params.Set("page", strconv.Itoa(page))
		if query != "" {
			params.Set("query", query)
		}
		resp, err := c.doRequest(ctx, "GET", fmt.Sprintf("/org/%s/resource-policies?%s", c.OrgID, params.Encode()), nil)
		if err != nil {
			return nil, err
		}
		var out struct {
			Policies   []ResourcePolicySummary `json:"policies"`
			Pagination struct {
				Total    *int `json:"total"`
				PageSize *int `json:"pageSize"`
			} `json:"pagination"`
		}
		if err := json.Unmarshal(resp.Data, &out); err != nil {
			return nil, fmt.Errorf("failed to parse resource policies: %w", err)
		}
		all = append(all, out.Policies...)
		// The server echoes the page size it applied; trust it over the
		// requested one so a server-side cap does not end the walk early.
		pageSize := resourcePolicyListPageSize
		if out.Pagination.PageSize != nil && *out.Pagination.PageSize > 0 {
			pageSize = *out.Pagination.PageSize
		}
		if len(out.Policies) == 0 || len(out.Policies) < pageSize ||
			(out.Pagination.Total != nil && len(all) >= *out.Pagination.Total) {
			return all, nil
		}
	}
}

// IsSharedResourcePolicy reports whether policyID is a shared (global)
// policy of the client's org. GET /resource-policy/{id} also answers for
// the inline default policy of a resource without saying which kind it
// is, so the list, which only holds shared policies, is the authority.
func (c *Client) IsSharedResourcePolicy(ctx context.Context, policyID int) (bool, error) {
	policies, err := c.ListResourcePolicies(ctx, "")
	if err != nil {
		return false, err
	}
	for _, p := range policies {
		if p.ResourcePolicyID == policyID {
			return true, nil
		}
	}
	return false, nil
}
