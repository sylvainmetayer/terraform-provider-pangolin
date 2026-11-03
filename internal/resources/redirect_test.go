package resources

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stackopshq/terraform-provider-pangolin/internal/client"
)

func TestValidateRedirect(t *testing.T) {
	base := func() RedirectResourceModel {
		return RedirectResourceModel{
			ResourceID:      types.Int64Null(),
			DomainID:        types.StringValue("dom"),
			Subdomain:       types.StringNull(),
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
		{"ssl off on a resource", func(m *RedirectResourceModel) {
			m.ResourceID, m.DomainID = types.Int64Value(1), types.StringNull()
			m.SSL = types.BoolValue(false)
		}, true},
		{"ssl off on a domain", func(m *RedirectResourceModel) { m.SSL = types.BoolValue(false) }, false},
		{"rewrite type without path", func(m *RedirectResourceModel) { m.RewritePathType = types.StringValue("prefix") }, true},
		{"stripPrefix without path", func(m *RedirectResourceModel) { m.RewritePathType = types.StringValue(redirectRewriteStripPrefix) }, false},
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
