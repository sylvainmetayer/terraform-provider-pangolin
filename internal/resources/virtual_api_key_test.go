package resources

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stackopshq/terraform-provider-pangolin/internal/client"
)

func vkModel(valid types.Int64) VirtualAPIKeyResourceModel {
	return VirtualAPIKeyResourceModel{
		Name: types.StringValue("k"), Description: types.StringNull(), UserID: types.StringNull(),
		AllResources: types.BoolValue(true), ResourceIDs: types.SetNull(types.Int64Type), ValidForSeconds: valid,
	}
}

// resourceIds is always an array (null fails the strict schema), and
// validForSeconds only goes out when it changes, as null on removal.
func TestVirtualAPIKeyRequest_Body(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name        string
		plan, prior types.Int64
		hasPrior    bool
		want        string
	}{
		{"create unset", types.Int64Null(), types.Int64Null(), false, `{"name":"k","description":null,"userId":null,"allResources":true,"resourceIds":[]}`},
		{"create set", types.Int64Value(60), types.Int64Null(), false, `{"name":"k","description":null,"userId":null,"allResources":true,"resourceIds":[],"validForSeconds":60}`},
		{"update same", types.Int64Value(60), types.Int64Value(60), true, `{"name":"k","description":null,"userId":null,"allResources":true,"resourceIds":[]}`},
		{"update removed", types.Int64Null(), types.Int64Value(60), true, `{"name":"k","description":null,"userId":null,"allResources":true,"resourceIds":[],"validForSeconds":null}`},
	}
	for _, c := range cases {
		var prior *VirtualAPIKeyResourceModel
		if c.hasPrior {
			p := vkModel(c.prior)
			prior = &p
		}
		req, err := virtualAPIKeyRequest(ctx, vkModel(c.plan), prior)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(req)
		if string(b) != c.want {
			t.Errorf("%s: body = %s, want %s", c.name, b, c.want)
		}
	}
}

// An empty resource list reads as null, and valid_for_seconds is carried over.
func TestVirtualAPIKeyToModel(t *testing.T) {
	name := "k"
	m, err := virtualAPIKeyToModel(context.Background(), vkModel(types.Int64Value(5)), &client.VirtualAPIKey{
		VirtualAPIKeyID: "abc", Name: &name, AllResources: true, Secret: "s",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !m.ResourceIDs.IsNull() || m.ValidForSeconds.ValueInt64() != 5 || m.Secret.ValueString() != "s" || !m.Description.IsNull() {
		t.Errorf("model = %+v", m)
	}
}
