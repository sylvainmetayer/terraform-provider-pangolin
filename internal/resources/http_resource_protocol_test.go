package resources

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// httpResourceObject builds a pangolin_resource object value where every
// attribute is null except the given ones.
func httpResourceObject(t *testing.T, s schema.Schema, set map[string]tftypes.Value) tftypes.Value {
	t.Helper()
	objType := s.Type().TerraformType(context.Background()).(tftypes.Object)
	vals := map[string]tftypes.Value{}
	for name, typ := range objType.AttributeTypes {
		if v, ok := set[name]; ok {
			vals[name] = v
			continue
		}
		vals[name] = tftypes.NewValue(typ, nil)
	}
	return tftypes.NewValue(objType, vals)
}

func httpResourceSchema(t *testing.T) schema.Schema {
	t.Helper()
	var resp resource.SchemaResponse
	(&HTTPResource{}).Schema(context.Background(), resource.SchemaRequest{}, &resp)
	return resp.Schema
}

func TestProtocolFromModeModifier(t *testing.T) {
	s := httpResourceSchema(t)
	for _, tc := range []struct {
		name  string
		mode  tftypes.Value
		state types.String
		want  types.String
	}{
		{"udp", tftypes.NewValue(tftypes.String, "udp"), types.StringNull(), types.StringValue("udp")},
		{"tcp", tftypes.NewValue(tftypes.String, "tcp"), types.StringNull(), types.StringValue("tcp")},
		{"http new", tftypes.NewValue(tftypes.String, "http"), types.StringNull(), types.StringValue("tcp")},
		{"http keeps state", tftypes.NewValue(tftypes.String, "http"), types.StringValue("udp"), types.StringValue("udp")},
		{"mode unknown", tftypes.NewValue(tftypes.String, tftypes.UnknownValue), types.StringNull(), types.StringUnknown()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan := tfsdk.Plan{Schema: s, Raw: httpResourceObject(t, s, map[string]tftypes.Value{"mode": tc.mode})}
			req := planmodifier.StringRequest{
				Path:        path.Root("protocol"),
				Plan:        plan,
				ConfigValue: types.StringNull(),
				StateValue:  tc.state,
				PlanValue:   types.StringUnknown(),
			}
			resp := &planmodifier.StringResponse{PlanValue: req.PlanValue}
			protocolFromModeModifier{}.PlanModifyString(context.Background(), req, resp)
			if resp.Diagnostics.HasError() {
				t.Fatalf("diags: %v", resp.Diagnostics)
			}
			if !resp.PlanValue.Equal(tc.want) {
				t.Errorf("plan = %v, want %v", resp.PlanValue, tc.want)
			}
		})
	}
}

func TestProtocolFromModeModifier_ConfiguredValueKept(t *testing.T) {
	s := httpResourceSchema(t)
	plan := tfsdk.Plan{Schema: s, Raw: httpResourceObject(t, s, map[string]tftypes.Value{
		"mode": tftypes.NewValue(tftypes.String, "udp"),
	})}
	req := planmodifier.StringRequest{
		Path:        path.Root("protocol"),
		Plan:        plan,
		ConfigValue: types.StringValue("udp"),
		PlanValue:   types.StringValue("udp"),
	}
	resp := &planmodifier.StringResponse{PlanValue: req.PlanValue}
	protocolFromModeModifier{}.PlanModifyString(context.Background(), req, resp)
	if resp.PlanValue.ValueString() != "udp" {
		t.Errorf("plan = %v, want the configured udp", resp.PlanValue)
	}
}

func TestProtocolMatchesModeValidator(t *testing.T) {
	s := httpResourceSchema(t)
	for _, tc := range []struct {
		name     string
		mode     tftypes.Value
		protocol string
		wantErr  bool
	}{
		{"udp matches", tftypes.NewValue(tftypes.String, "udp"), "udp", false},
		{"udp mismatch", tftypes.NewValue(tftypes.String, "udp"), "tcp", true},
		{"tcp mismatch", tftypes.NewValue(tftypes.String, "tcp"), "udp", true},
		{"http tcp", tftypes.NewValue(tftypes.String, "http"), "tcp", false},
		{"http udp", tftypes.NewValue(tftypes.String, "http"), "udp", true},
		{"default mode udp", tftypes.NewValue(tftypes.String, nil), "udp", true},
		{"mode unknown", tftypes.NewValue(tftypes.String, tftypes.UnknownValue), "udp", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := tfsdk.Config{Schema: s, Raw: httpResourceObject(t, s, map[string]tftypes.Value{
				"mode":     tc.mode,
				"protocol": tftypes.NewValue(tftypes.String, tc.protocol),
			})}
			req := validator.StringRequest{
				Path:        path.Root("protocol"),
				Config:      cfg,
				ConfigValue: types.StringValue(tc.protocol),
			}
			resp := &validator.StringResponse{}
			protocolMatchesModeValidator{}.ValidateString(context.Background(), req, resp)
			if got := resp.Diagnostics.HasError(); got != tc.wantErr {
				t.Errorf("error = %v, want %v (%v)", got, tc.wantErr, resp.Diagnostics)
			}
		})
	}
}

func TestHTTPResourceSchema_ProtocolDeprecated(t *testing.T) {
	var resp resource.SchemaResponse
	(&HTTPResource{}).Schema(context.Background(), resource.SchemaRequest{}, &resp)
	attr, ok := resp.Schema.Attributes["protocol"]
	if !ok {
		t.Fatal("protocol attribute missing")
	}
	if got := attr.GetDeprecationMessage(); got != httpResourceProtocolDeprecation {
		t.Errorf("DeprecationMessage = %q, want %q", got, httpResourceProtocolDeprecation)
	}
}
