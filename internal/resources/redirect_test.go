package resources

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/stackopshq/terraform-provider-pangolin/internal/client"
)

func TestValidateRedirect(t *testing.T) {
	base := func() RedirectResourceModel {
		return RedirectResourceModel{
			ResourceID:      types.Int64Null(),
			DomainID:        types.StringValue("dom"),
			Subdomain:       types.StringNull(),
			MatchPath:       types.StringNull(),
			RewritePath:     types.StringNull(),
			RewritePathType: types.StringNull(),
			SSL:             types.BoolNull(),
		}
	}
	for _, tc := range []struct {
		name    string
		mutate  func(*RedirectResourceModel)
		wantErr bool
	}{
		{"domain only", func(*RedirectResourceModel) {}, false},
		{"neither target", func(m *RedirectResourceModel) { m.DomainID = types.StringNull() }, true},
		{"both targets", func(m *RedirectResourceModel) { m.ResourceID = types.Int64Value(1) }, true},
		{"unknown target left to the server", func(m *RedirectResourceModel) {
			m.ResourceID = types.Int64Unknown()
			m.DomainID = types.StringNull()
		}, false},
		{"subdomain on a resource", func(m *RedirectResourceModel) {
			m.ResourceID, m.DomainID = types.Int64Value(1), types.StringNull()
			m.Subdomain = types.StringValue("www")
		}, true},
		{"unknown subdomain on a resource left to the server", func(m *RedirectResourceModel) {
			m.ResourceID, m.DomainID = types.Int64Value(1), types.StringNull()
			m.Subdomain = types.StringUnknown()
		}, false},
		{"ssl off on a resource", func(m *RedirectResourceModel) {
			m.ResourceID, m.DomainID = types.Int64Value(1), types.StringNull()
			m.SSL = types.BoolValue(false)
		}, true},
		{"ssl off on a domain", func(m *RedirectResourceModel) { m.SSL = types.BoolValue(false) }, false},
		{"rewrite type without path", func(m *RedirectResourceModel) {
			m.RewritePathType, m.MatchPath = types.StringValue("prefix"), types.StringValue("/a")
		}, true},
		{"stripPrefix without path", func(m *RedirectResourceModel) {
			m.RewritePathType, m.MatchPath = types.StringValue(redirectRewriteStripPrefix), types.StringValue("/a")
		}, false},
		{"rewrite path without type", func(m *RedirectResourceModel) { m.RewritePath = types.StringValue("/b") }, true},
		{"unknown rewrite path without type left to the server", func(m *RedirectResourceModel) { m.RewritePath = types.StringUnknown() }, false},
		{"exact rewrite without match path", func(m *RedirectResourceModel) {
			m.RewritePathType, m.RewritePath = types.StringValue(redirectRewriteExact), types.StringValue("/b")
		}, false},
		{"prefix rewrite without match path", func(m *RedirectResourceModel) {
			m.RewritePathType, m.RewritePath = types.StringValue("prefix"), types.StringValue("/b")
		}, true},
		{"stripPrefix without match path", func(m *RedirectResourceModel) { m.RewritePathType = types.StringValue(redirectRewriteStripPrefix) }, true},
		{"prefix rewrite with an unknown match path", func(m *RedirectResourceModel) {
			m.RewritePathType, m.RewritePath, m.MatchPath = types.StringValue("prefix"), types.StringValue("/b"), types.StringUnknown()
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := base()
			tc.mutate(&m)
			if got := validateRedirect(m).HasError(); got != tc.wantErr {
				t.Errorf("error = %v, want %v", got, tc.wantErr)
			}
		})
	}
}

func TestRedirectDestinationHost(t *testing.T) {
	for value, want := range map[string]bool{
		"https://example.com":     true,
		"http://example.com:8080": true,
		"https://[::1]:8443":      true,
		"https://example.com/":    false,
		"https://example.com/a":   false,
		"https://example.com?a=1": false,
		"https://example.com#a":   false,
		"https://u@example.com":   false,
		"https://Example.com":     false,
		"HTTPS://example.com":     false,
		"example.com":             false,
		"mailto://example.com":    false,
	} {
		if got := redirectDestinationHostRegexp.MatchString(value); got != want {
			t.Errorf("%q: match = %v, want %v", value, got, want)
		}
	}
}

// A redirect goes away with its parent, so only a change between two parents
// of the same kind replaces it; attaching or detaching stays in place.
func TestRedirectParentChangeRequiresReplace(t *testing.T) {
	ctx := context.Background()
	var resp resource.SchemaResponse
	NewRedirectResource().Schema(ctx, resource.SchemaRequest{}, &resp)
	resourceID := resp.Schema.Attributes["resource_id"].(schema.Int64Attribute).PlanModifiers[0]
	domainID := resp.Schema.Attributes["domain_id"].(schema.StringAttribute).PlanModifiers[0]
	raw := tftypes.NewValue(tftypes.String, "not null")

	for _, tc := range []struct {
		name         string
		state, plan  types.Int64
		wantReplaced bool
	}{
		{"same parent", types.Int64Value(1), types.Int64Value(1), false},
		{"other parent", types.Int64Value(1), types.Int64Value(2), true},
		{"parent being replaced", types.Int64Value(1), types.Int64Unknown(), true},
		{"detached", types.Int64Value(1), types.Int64Null(), false},
		{"attached", types.Int64Null(), types.Int64Value(1), false},
		{"attached to a new parent", types.Int64Null(), types.Int64Unknown(), false},
	} {
		t.Run("resource_id/"+tc.name, func(t *testing.T) {
			var got planmodifier.Int64Response
			resourceID.PlanModifyInt64(ctx, planmodifier.Int64Request{
				State: tfsdk.State{Raw: raw}, Plan: tfsdk.Plan{Raw: raw},
				StateValue: tc.state, PlanValue: tc.plan,
			}, &got)
			if got.RequiresReplace != tc.wantReplaced {
				t.Errorf("RequiresReplace = %v, want %v", got.RequiresReplace, tc.wantReplaced)
			}
		})
	}

	for _, tc := range []struct {
		name         string
		state, plan  types.String
		wantReplaced bool
	}{
		{"other parent", types.StringValue("a"), types.StringValue("b"), true},
		{"parent being replaced", types.StringValue("a"), types.StringUnknown(), true},
		{"detached", types.StringValue("a"), types.StringNull(), false},
		{"attached", types.StringNull(), types.StringValue("a"), false},
	} {
		t.Run("domain_id/"+tc.name, func(t *testing.T) {
			var got planmodifier.StringResponse
			domainID.PlanModifyString(ctx, planmodifier.StringRequest{
				State: tfsdk.State{Raw: raw}, Plan: tfsdk.Plan{Raw: raw},
				StateValue: tc.state, PlanValue: tc.plan,
			}, &got)
			if got.RequiresReplace != tc.wantReplaced {
				t.Errorf("RequiresReplace = %v, want %v", got.RequiresReplace, tc.wantReplaced)
			}
		})
	}
}

func TestRedirectRoundTrip(t *testing.T) {
	sub := "www"
	domain := "dom"
	m := redirectToModel(&client.Redirect{RedirectID: 7, NiceID: "n", Name: "r", DomainID: &domain, Subdomain: &sub, DestinationHost: "https://example.com", PathMatchType: "regex", SSL: true, Enabled: true})
	if m.Priority.ValueInt64() != 100 {
		t.Errorf("a null priority reads as %d, want 100", m.Priority.ValueInt64())
	}
	req := redirectRequest(m)
	if req.ResourceID != nil || *req.DomainID != "dom" || *req.Subdomain != "www" || req.MatchPath != nil {
		t.Errorf("request = %+v", req)
	}
}
