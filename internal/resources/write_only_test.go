package resources

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/stackopshq/terraform-provider-pangolin/internal/client"
)

// writeOnlyCase drives pangolin_resource_password and pangolin_resource_pincode
// through the same scenarios: name is the historical attribute, also the key
// of the request body.
type writeOnlyCase struct {
	name string
	path string
	new  func(c *client.Client) resource.Resource
}

var writeOnlyCases = []writeOnlyCase{
	{"password", "/resource/3/password", func(c *client.Client) resource.Resource { return &ResourcePasswordResource{client: c} }},
	{"pincode", "/resource/3/pincode", func(c *client.Client) resource.Resource { return &ResourcePincodeResource{client: c} }},
}

// woObject builds a value of the resource type, null except resource_id = 3
// and the given attributes.
func woObject(typ tftypes.Type, attrs map[string]any) tftypes.Value {
	vals := map[string]tftypes.Value{}
	for name, at := range typ.(tftypes.Object).AttributeTypes {
		vals[name] = tftypes.NewValue(at, nil)
	}
	vals["resource_id"] = tftypes.NewValue(tftypes.Number, 3)
	for k, v := range attrs {
		vals[k] = tftypes.NewValue(typ.(tftypes.Object).AttributeTypes[k], v)
	}
	return tftypes.NewValue(typ, vals)
}

func woAttr(t *testing.T, st tfsdk.State, name string) tftypes.Value {
	t.Helper()
	v, _, err := tftypes.WalkAttributePath(st.Raw, tftypes.NewAttributePath().WithAttributeName(name))
	if err != nil {
		t.Fatal(err)
	}
	return v.(tftypes.Value)
}

// The write-only value only exists in the configuration: Create and Update
// must take it from there and send it, while plan and state keep null.
func TestWriteOnly_SentFromConfig(t *testing.T) {
	ctx := context.Background()
	for _, wc := range writeOnlyCases {
		fake := &fakePangolin{}
		r := wc.new(fake.client(t, func(string, string) any { return map[string]any{} }))
		schemaResp, typ := resourceSchema(t, r)
		sch := schemaResp.Schema
		wo, ver := wc.name+"_wo", wc.name+"_wo_version"

		createResp := resource.CreateResponse{State: tfsdk.State{Schema: sch, Raw: tftypes.NewValue(typ, nil)}}
		r.Create(ctx, resource.CreateRequest{
			Config: tfsdk.Config{Schema: sch, Raw: woObject(typ, map[string]any{wo: "111111", ver: 1})},
			Plan:   tfsdk.Plan{Schema: sch, Raw: woObject(typ, map[string]any{ver: 1})},
		}, &createResp)
		if createResp.Diagnostics.HasError() {
			t.Fatalf("%s create: %v", wc.name, createResp.Diagnostics)
		}
		updateResp := resource.UpdateResponse{State: tfsdk.State{Schema: sch, Raw: tftypes.NewValue(typ, nil)}}
		r.Update(ctx, resource.UpdateRequest{
			Config: tfsdk.Config{Schema: sch, Raw: woObject(typ, map[string]any{wo: "222222", ver: 2})},
			Plan:   tfsdk.Plan{Schema: sch, Raw: woObject(typ, map[string]any{ver: 2})},
			State:  createResp.State,
		}, &updateResp)
		if updateResp.Diagnostics.HasError() {
			t.Fatalf("%s update: %v", wc.name, updateResp.Diagnostics)
		}

		if got, want := fake.paths(), []string{"POST " + wc.path, "POST " + wc.path}; !samePaths(got, want) {
			t.Fatalf("%s calls = %v, want %v", wc.name, got, want)
		}
		for i, v := range []string{"111111", "222222"} {
			if got := fake.calls[i].Body[wc.name]; got != v {
				t.Errorf("%s call %d sent %v, want %q", wc.name, i, got, v)
			}
		}
		if !woAttr(t, updateResp.State, wc.name).IsNull() || !woAttr(t, updateResp.State, wo).IsNull() ||
			!woAttr(t, updateResp.State, ver).Equal(tftypes.NewValue(tftypes.Number, 2)) {
			t.Errorf("%s state = %v, want null secrets and version 2", wc.name, updateResp.State.Raw)
		}
	}
}

// Switching from the historical attribute, already in the state, to the
// write-only one sends the new value and drops the old one from the state.
func TestWriteOnly_MigrationFromHistoricalAttribute(t *testing.T) {
	ctx := context.Background()
	for _, wc := range writeOnlyCases {
		fake := &fakePangolin{}
		r := wc.new(fake.client(t, func(string, string) any { return map[string]any{} }))
		schemaResp, typ := resourceSchema(t, r)
		sch := schemaResp.Schema
		wo, ver := wc.name+"_wo", wc.name+"_wo_version"

		resp := resource.UpdateResponse{State: tfsdk.State{Schema: sch, Raw: tftypes.NewValue(typ, nil)}}
		r.Update(ctx, resource.UpdateRequest{
			Config: tfsdk.Config{Schema: sch, Raw: woObject(typ, map[string]any{wo: "222222", ver: 1})},
			Plan:   tfsdk.Plan{Schema: sch, Raw: woObject(typ, map[string]any{ver: 1})},
			State:  tfsdk.State{Schema: sch, Raw: woObject(typ, map[string]any{wc.name: "111111"})},
		}, &resp)
		if resp.Diagnostics.HasError() {
			t.Fatalf("%s update: %v", wc.name, resp.Diagnostics)
		}
		if got, want := fake.paths(), []string{"POST " + wc.path}; !samePaths(got, want) {
			t.Fatalf("%s calls = %v, want %v", wc.name, got, want)
		}
		if got := fake.calls[0].Body[wc.name]; got != "222222" {
			t.Errorf("%s sent %v, want the write-only value", wc.name, got)
		}
		if !woAttr(t, resp.State, wc.name).IsNull() {
			t.Errorf("%s: the historical value stayed in the state: %v", wc.name, resp.State.Raw)
		}
	}
}

// The historical attribute keeps working as before: its value comes from the
// plan and stays in the state.
func TestWriteOnly_HistoricalAttributeUnchanged(t *testing.T) {
	ctx := context.Background()
	for _, wc := range writeOnlyCases {
		fake := &fakePangolin{}
		r := wc.new(fake.client(t, func(string, string) any { return map[string]any{} }))
		schemaResp, typ := resourceSchema(t, r)
		sch := schemaResp.Schema

		raw := woObject(typ, map[string]any{wc.name: "123456"})
		resp := resource.CreateResponse{State: tfsdk.State{Schema: sch, Raw: tftypes.NewValue(typ, nil)}}
		r.Create(ctx, resource.CreateRequest{Config: tfsdk.Config{Schema: sch, Raw: raw}, Plan: tfsdk.Plan{Schema: sch, Raw: raw}}, &resp)
		if resp.Diagnostics.HasError() {
			t.Fatalf("%s create: %v", wc.name, resp.Diagnostics)
		}
		if got := fake.calls[0].Body[wc.name]; got != "123456" {
			t.Errorf("%s sent %v, want 123456", wc.name, got)
		}
		if !woAttr(t, resp.State, wc.name).Equal(tftypes.NewValue(tftypes.String, "123456")) {
			t.Errorf("%s state = %v", wc.name, resp.State.Raw)
		}
	}
}
