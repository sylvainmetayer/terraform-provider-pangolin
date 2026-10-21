package resources

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stackopshq/terraform-provider-pangolin/internal/client"
)

// -----------------------------------------------------------------------------
// Schemas: defaults inside nested attributes are only valid in some
// positions, so make the framework check every new schema.
// -----------------------------------------------------------------------------

func TestResourcePolicySchemas_ValidateImplementation(t *testing.T) {
	ctx := context.Background()
	for name, r := range map[string]resource.Resource{
		"resource_policy":             NewResourcePolicyResource(),
		"resource_policy_password":    NewResourcePolicyPasswordResource(),
		"resource_policy_pincode":     NewResourcePolicyPincodeResource(),
		"resource_policy_header_auth": NewResourcePolicyHeaderAuthResource(),
		"resource":                    NewHTTPResource(),
	} {
		var resp resource.SchemaResponse
		r.Schema(ctx, resource.SchemaRequest{}, &resp)
		if resp.Diagnostics.HasError() {
			t.Fatalf("%s: schema diags: %v", name, resp.Diagnostics)
		}
		if diags := resp.Schema.ValidateImplementation(ctx); diags.HasError() {
			t.Errorf("%s: invalid schema: %v", name, diags)
		}
	}
}

// -----------------------------------------------------------------------------
// applyResourcePolicyResponse: wire -> state
// -----------------------------------------------------------------------------

func rulesList(t *testing.T, rules ...ResourcePolicyRuleModel) types.List {
	t.Helper()
	l, diags := types.ListValueFrom(context.Background(), types.ObjectType{AttrTypes: resourcePolicyRuleAttrTypes}, rules)
	if diags.HasError() {
		t.Fatalf("rules list: %v", diags)
	}
	return l
}

func rule(action, match, value string, priority int64, enabled bool) ResourcePolicyRuleModel {
	return ResourcePolicyRuleModel{
		Action:   types.StringValue(action),
		Match:    types.StringValue(match),
		Value:    types.StringValue(value),
		Priority: types.Int64Value(priority),
		Enabled:  types.BoolValue(enabled),
	}
}

func TestApplyResourcePolicyResponse_Nominal(t *testing.T) {
	ctx := context.Background()
	p := &client.ResourcePolicy{
		ResourcePolicyID:      7,
		NiceID:                "brave-otter",
		Name:                  "corp",
		SSO:                   true,
		ApplyRules:            true,
		EmailWhitelistEnabled: true,
		IDPID:                 intPtr(2),
		Roles:                 []client.ResourcePolicyRole{{RoleID: 3}, {RoleID: 4}},
		Users:                 []client.ResourcePolicyUser{{UserID: "u1"}},
		EmailWhitelist:        []client.ResourcePolicyWhitelistEntry{{Email: "a@example.com"}},
		Rules: []client.ResourcePolicyRule{
			{RuleID: 1, Action: "ACCEPT", Match: "CIDR", Value: "10.0.0.0/8", Priority: 2, Enabled: boolPtr(true)},
			{RuleID: 2, Action: "DROP", Match: "COUNTRY", Value: "FR", Priority: 1},
		},
	}
	m, diags := applyResourcePolicyResponse(ctx, ResourcePolicyModel{Rules: types.ListNull(types.ObjectType{AttrTypes: resourcePolicyRuleAttrTypes})}, p)
	if diags.HasError() {
		t.Fatalf("diags: %v", diags)
	}
	if m.ID.ValueInt64() != 7 || m.NiceID.ValueString() != "brave-otter" || m.Name.ValueString() != "corp" {
		t.Errorf("identity = %v / %v / %v", m.ID, m.NiceID, m.Name)
	}
	if !m.SSO.ValueBool() || !m.ApplyRules.ValueBool() || !m.EmailWhitelistEnabled.ValueBool() {
		t.Errorf("flags = %v / %v / %v", m.SSO, m.ApplyRules, m.EmailWhitelistEnabled)
	}
	if m.SkipToIdpID.ValueInt64() != 2 {
		t.Errorf("SkipToIdpID = %v", m.SkipToIdpID)
	}
	wantRoles := types.SetValueMust(types.Int64Type, []attr.Value{types.Int64Value(4), types.Int64Value(3)})
	if !m.RoleIDs.Equal(wantRoles) {
		t.Errorf("RoleIDs = %v", m.RoleIDs)
	}
	if !m.UserIDs.Equal(types.SetValueMust(types.StringType, []attr.Value{types.StringValue("u1")})) {
		t.Errorf("UserIDs = %v", m.UserIDs)
	}
	if !m.WhitelistEmails.Equal(types.SetValueMust(types.StringType, []attr.Value{types.StringValue("a@example.com")})) {
		t.Errorf("WhitelistEmails = %v", m.WhitelistEmails)
	}
	// No prior rules: sorted by priority, missing enabled means true.
	want := rulesList(t, rule("DROP", "COUNTRY", "FR", 1, true), rule("ACCEPT", "CIDR", "10.0.0.0/8", 2, true))
	if !m.Rules.Equal(want) {
		t.Errorf("Rules = %v, want %v", m.Rules, want)
	}
}

func TestApplyResourcePolicyResponse_EmptyCollectionsAreEmptyNotNull(t *testing.T) {
	m, diags := applyResourcePolicyResponse(context.Background(), ResourcePolicyModel{}, &client.ResourcePolicy{ResourcePolicyID: 1})
	if diags.HasError() {
		t.Fatalf("diags: %v", diags)
	}
	for name, v := range map[string]interface {
		IsNull() bool
	}{"role_ids": m.RoleIDs, "user_ids": m.UserIDs, "whitelist_emails": m.WhitelistEmails, "rules": m.Rules} {
		if v.IsNull() {
			t.Errorf("%s is null, want empty (matches the schema default)", name)
		}
	}
	if !m.SkipToIdpID.IsNull() {
		t.Errorf("SkipToIdpID = %v, want null", m.SkipToIdpID)
	}
}

func TestOrderResourcePolicyRules_KeepsPriorOrder(t *testing.T) {
	a := rule("ACCEPT", "PATH", "/a", 5, true)
	b := rule("DROP", "IP", "1.2.3.4", 1, true)
	c := rule("PASS", "ASN", "AS13335", 3, false)
	// Server order differs from the configured order; c is new.
	got := orderResourcePolicyRules([]ResourcePolicyRuleModel{a, b}, []ResourcePolicyRuleModel{c, b, a})
	if len(got) != 3 || !resourcePolicyRuleEqual(got[0], a) || !resourcePolicyRuleEqual(got[1], b) || !resourcePolicyRuleEqual(got[2], c) {
		t.Errorf("got %v", got)
	}
}

func TestOrderResourcePolicyRules_DuplicatesAndRemovals(t *testing.T) {
	a := rule("ACCEPT", "PATH", "/a", 1, true)
	b := rule("DROP", "IP", "1.2.3.4", 2, true)
	// Prior lists a twice and b, but the server only has one a.
	got := orderResourcePolicyRules([]ResourcePolicyRuleModel{a, a, b}, []ResourcePolicyRuleModel{a})
	if len(got) != 1 || !resourcePolicyRuleEqual(got[0], a) {
		t.Errorf("got %v", got)
	}
}

// -----------------------------------------------------------------------------
// Request builders: state -> wire
// -----------------------------------------------------------------------------

func TestBuildResourcePolicyCreateRequest(t *testing.T) {
	ctx := context.Background()
	plan := ResourcePolicyModel{
		Name:                  types.StringValue("corp"),
		SSO:                   types.BoolValue(false),
		SkipToIdpID:           types.Int64Null(),
		RoleIDs:               types.SetValueMust(types.Int64Type, []attr.Value{types.Int64Value(12), types.Int64Value(3)}),
		UserIDs:               types.SetValueMust(types.StringType, []attr.Value{}),
		EmailWhitelistEnabled: types.BoolValue(true),
		WhitelistEmails:       types.SetValueMust(types.StringType, []attr.Value{types.StringValue("*@example.com")}),
		ApplyRules:            types.BoolValue(true),
		Rules:                 rulesList(t, rule("DROP", "METHOD", "DELETE", 1, false)),
	}
	req, diags := buildResourcePolicyCreateRequest(ctx, plan)
	if diags.HasError() {
		t.Fatalf("diags: %v", diags)
	}
	if req.Name != "corp" || req.SSO || !req.EmailWhitelistEnabled || !req.ApplyRules || req.SkipToIdpID != nil {
		t.Errorf("scalars = %+v", req)
	}
	if len(req.RoleIDs) != 2 || req.RoleIDs[0] != "3" || req.RoleIDs[1] != "12" {
		t.Errorf("RoleIDs = %v, want sorted string IDs", req.RoleIDs)
	}
	if req.UserIDs == nil || len(req.UserIDs) != 0 {
		t.Errorf("UserIDs = %#v, want empty non-nil", req.UserIDs)
	}
	if len(req.Emails) != 1 || req.Emails[0] != "*@example.com" {
		t.Errorf("Emails = %v", req.Emails)
	}
	if len(req.Rules) != 1 || req.Rules[0].RuleID != 0 || req.Rules[0].Enabled == nil || *req.Rules[0].Enabled {
		t.Errorf("Rules = %+v", req.Rules)
	}
}

func TestBuildResourcePolicyAccessControlRequest(t *testing.T) {
	plan := ResourcePolicyModel{
		SSO:         types.BoolValue(true),
		SkipToIdpID: types.Int64Value(9),
		RoleIDs:     types.SetValueMust(types.Int64Type, []attr.Value{types.Int64Value(3)}),
		UserIDs:     types.SetValueMust(types.StringType, []attr.Value{types.StringValue("u1")}),
	}
	req, diags := buildResourcePolicyAccessControlRequest(context.Background(), plan)
	if diags.HasError() {
		t.Fatalf("diags: %v", diags)
	}
	if !req.SSO || req.SkipToIdpID == nil || *req.SkipToIdpID != 9 {
		t.Errorf("scalars = %+v", req)
	}
	if len(req.RoleIDs) != 1 || req.RoleIDs[0] != 3 || len(req.UserIDs) != 1 || req.UserIDs[0] != "u1" {
		t.Errorf("grants = %+v", req)
	}
}

// -----------------------------------------------------------------------------
// pangolin_resource.resource_policy_id
// -----------------------------------------------------------------------------

func TestResourcePolicyIDUpdate(t *testing.T) {
	cases := []struct {
		name        string
		plan, prior types.Int64
		want        string // "" = key omitted
	}{
		{"unset keeps attachment", types.Int64Unknown(), types.Int64Value(5), ""},
		{"null keeps attachment", types.Int64Null(), types.Int64Value(5), ""},
		{"unchanged", types.Int64Value(5), types.Int64Value(5), ""},
		{"attach", types.Int64Value(5), types.Int64Null(), "5"},
		{"switch", types.Int64Value(6), types.Int64Value(5), "6"},
		{"detach", types.Int64Value(0), types.Int64Value(5), "null"},
		{"already detached", types.Int64Value(0), types.Int64Null(), ""},
		{"zero at create", types.Int64Value(0), types.Int64Null(), ""},
	}
	for _, tc := range cases {
		got := resourcePolicyIDUpdate(tc.plan, tc.prior)
		switch {
		case tc.want == "" && got != nil:
			t.Errorf("%s: got %s, want omitted", tc.name, *got)
		case tc.want != "" && (got == nil || string(*got) != tc.want):
			t.Errorf("%s: got %v, want %s", tc.name, got, tc.want)
		}
	}
}

func TestApplyHTTPResourceResponse_ResourcePolicyID(t *testing.T) {
	// A detached resource keeps the configured 0.
	m := applyHTTPResourceResponse(HTTPResourceModel{ResourcePolicyID: types.Int64Value(0)}, &client.Resource{ResourceID: 1})
	if m.ResourcePolicyID.IsNull() || m.ResourcePolicyID.ValueInt64() != 0 {
		t.Errorf("detached: ResourcePolicyID = %v, want 0", m.ResourcePolicyID)
	}
	// An attachment made elsewhere is surfaced.
	m = applyHTTPResourceResponse(HTTPResourceModel{ResourcePolicyID: types.Int64Value(0)}, &client.Resource{ResourceID: 1, ResourcePolicyID: intPtr(8)})
	if m.ResourcePolicyID.ValueInt64() != 8 {
		t.Errorf("attached elsewhere: ResourcePolicyID = %v, want 8", m.ResourcePolicyID)
	}
	// A policy detached elsewhere goes back to null.
	m = applyHTTPResourceResponse(HTTPResourceModel{ResourcePolicyID: types.Int64Value(8)}, &client.Resource{ResourceID: 1})
	if !m.ResourcePolicyID.IsNull() {
		t.Errorf("detached elsewhere: ResourcePolicyID = %v, want null", m.ResourcePolicyID)
	}
}
