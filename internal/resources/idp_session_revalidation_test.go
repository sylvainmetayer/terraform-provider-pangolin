package resources

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/stackopshq/terraform-provider-pangolin/internal/client"
)

func TestValidateSessionRevalidationScopes(t *testing.T) {
	cases := []struct {
		on      types.Bool
		scopes  string
		variant types.String
		wantErr bool
	}{
		{types.BoolValue(true), "openid email", types.StringNull(), true},
		{types.BoolValue(true), "openid offline_access email", types.StringNull(), false},
		{types.BoolValue(true), "openid offline_access", types.StringValue("azure"), false},
		{types.BoolValue(true), "openid offline_access", types.StringValue("google"), true},
		{types.BoolValue(true), "openid email", types.StringValue("google"), false},
		{types.BoolValue(false), "openid", types.StringNull(), false},
		{types.BoolNull(), "openid", types.StringNull(), false},
		{types.BoolUnknown(), "openid", types.StringNull(), false},
	}
	for i, c := range cases {
		got := validateSessionRevalidationScopes(c.on, types.StringValue(c.scopes), c.variant).HasError()
		if got != c.wantErr {
			t.Errorf("case %d: error=%v, want %v", i, got, c.wantErr)
		}
	}
}

// Unset, the field stays out of the body: Pangolin 1.25 refuses unknown keys.
func TestSessionRevalidationOmittedWhenUnset(t *testing.T) {
	for _, req := range []any{&client.CreateIDPRequest{}, &client.UpdateIDPRequest{}, &client.UpdateOrgIDPRequest{}} {
		b, _ := json.Marshal(req)
		if strings.Contains(string(b), "sessionRevalidation") {
			t.Fatalf("sent while unset: %s", b)
		}
	}
	off := false
	b, _ := json.Marshal(&client.UpdateIDPRequest{SessionRevalidation: &off})
	if !strings.Contains(string(b), `"sessionRevalidation":false`) {
		t.Fatalf("false not sent: %s", b)
	}
}
