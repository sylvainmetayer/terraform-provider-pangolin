package resources

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// The write-only value only exists in the configuration: Create and Update
// must take it from there and send it, while plan and state keep null.
func TestResourcePasswordWriteOnly_SentFromConfig(t *testing.T) {
	ctx := context.Background()
	fake := &fakePangolin{}
	r := &ResourcePasswordResource{client: fake.client(t, func(string, string) any { return map[string]any{} })}
	schemaResp, typ := resourceSchema(t, r)

	set := func(raw *tftypes.Value, m ResourcePasswordModel) {
		st := tfsdk.State{Schema: schemaResp.Schema, Raw: tftypes.NewValue(typ, nil)}
		if diags := st.Set(ctx, &m); diags.HasError() {
			t.Fatalf("set: %v", diags)
		}
		*raw = st.Raw
	}
	var cfgRaw, planRaw tftypes.Value
	set(&cfgRaw, ResourcePasswordModel{ResourceID: types.Int64Value(3), PasswordWO: types.StringValue("first"), PasswordWOVersion: types.Int64Value(1)})
	set(&planRaw, ResourcePasswordModel{ResourceID: types.Int64Value(3), PasswordWOVersion: types.Int64Value(1)})

	createResp := resource.CreateResponse{State: tfsdk.State{Schema: schemaResp.Schema, Raw: tftypes.NewValue(typ, nil)}}
	r.Create(ctx, resource.CreateRequest{
		Config: tfsdk.Config{Schema: schemaResp.Schema, Raw: cfgRaw},
		Plan:   tfsdk.Plan{Schema: schemaResp.Schema, Raw: planRaw},
	}, &createResp)
	if createResp.Diagnostics.HasError() {
		t.Fatalf("create: %v", createResp.Diagnostics)
	}

	set(&cfgRaw, ResourcePasswordModel{ResourceID: types.Int64Value(3), PasswordWO: types.StringValue("second"), PasswordWOVersion: types.Int64Value(2)})
	set(&planRaw, ResourcePasswordModel{ResourceID: types.Int64Value(3), PasswordWOVersion: types.Int64Value(2)})
	updateResp := resource.UpdateResponse{State: tfsdk.State{Schema: schemaResp.Schema, Raw: tftypes.NewValue(typ, nil)}}
	r.Update(ctx, resource.UpdateRequest{
		Config: tfsdk.Config{Schema: schemaResp.Schema, Raw: cfgRaw},
		Plan:   tfsdk.Plan{Schema: schemaResp.Schema, Raw: planRaw},
		State:  createResp.State,
	}, &updateResp)
	if updateResp.Diagnostics.HasError() {
		t.Fatalf("update: %v", updateResp.Diagnostics)
	}

	want := []string{"POST /resource/3/password", "POST /resource/3/password"}
	if got := fake.paths(); !samePaths(got, want) {
		t.Fatalf("calls = %v, want %v", got, want)
	}
	for i, pw := range []string{"first", "second"} {
		if got := fake.calls[i].Body["password"]; got != pw {
			t.Errorf("call %d sent password %v, want %q", i, got, pw)
		}
	}
	var state ResourcePasswordModel
	updateResp.State.Get(ctx, &state)
	if !state.Password.IsNull() || !state.PasswordWO.IsNull() || state.PasswordWOVersion.ValueInt64() != 2 {
		t.Errorf("state = %+v, want null password and password_wo, version 2", state)
	}
}

// The historical attribute keeps working as before: its value comes from the
// plan and stays in the state.
func TestResourcePincode_HistoricalAttributeUnchanged(t *testing.T) {
	ctx := context.Background()
	fake := &fakePangolin{}
	r := &ResourcePincodeResource{client: fake.client(t, func(string, string) any { return map[string]any{} })}
	schemaResp, typ := resourceSchema(t, r)

	plan := tfsdk.Plan{Schema: schemaResp.Schema, Raw: tftypes.NewValue(typ, nil)}
	if diags := plan.Set(ctx, &ResourcePincodeModel{ResourceID: types.Int64Value(3), Pincode: types.StringValue("123456")}); diags.HasError() {
		t.Fatalf("plan: %v", diags)
	}
	resp := resource.CreateResponse{State: tfsdk.State{Schema: schemaResp.Schema, Raw: tftypes.NewValue(typ, nil)}}
	r.Create(ctx, resource.CreateRequest{Config: tfsdk.Config{Schema: schemaResp.Schema, Raw: plan.Raw}, Plan: plan}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("create: %v", resp.Diagnostics)
	}
	if got := fake.calls[0].Body["pincode"]; got != "123456" {
		t.Errorf("sent pincode %v, want 123456", got)
	}
	var state ResourcePincodeModel
	resp.State.Get(ctx, &state)
	if state.Pincode.ValueString() != "123456" {
		t.Errorf("state pincode = %v", state.Pincode)
	}
}
