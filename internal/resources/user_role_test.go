package resources

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stackopshq/terraform-provider-pangolin/internal/client"
)

func TestRemoveRoleDiags(t *testing.T) {
	cases := []struct {
		name          string
		err           error
		wantErr, warn bool
	}{
		{"ok", nil, false, false},
		{"gone", fmt.Errorf("x: %w", client.ErrNotFound), false, false},
		{"last role", errors.New("API error (status 403): User must have at least one role in the organization."), false, true},
		{"other", errors.New("boom"), true, false},
	}
	for _, c := range cases {
		var d diag.Diagnostics
		removeRoleDiags(c.err, &d)
		if d.HasError() != c.wantErr || (d.WarningsCount() > 0) != c.warn {
			t.Errorf("%s: diags = %v", c.name, d)
		}
	}
}

func TestCreateOnlyString(t *testing.T) {
	cases := []struct {
		state, plan types.String
		wantErr     bool
	}{
		{types.StringNull(), types.StringValue("a@x.io"), false},          // create
		{types.StringValue("a@x.io"), types.StringValue("A@X.io"), false}, // case only
		{types.StringValue("a@x.io"), types.StringUnknown(), false},
		{types.StringValue("a@x.io"), types.StringValue("b@x.io"), true},
	}
	for _, c := range cases {
		resp := planmodifier.StringResponse{PlanValue: c.plan}
		createOnlyString{}.PlanModifyString(context.Background(), planmodifier.StringRequest{
			Path: path.Root("email"), StateValue: c.state, PlanValue: c.plan,
		}, &resp)
		if resp.Diagnostics.HasError() != c.wantErr {
			t.Errorf("%s -> %s: diags = %v", c.state, c.plan, resp.Diagnostics)
		}
	}
}
