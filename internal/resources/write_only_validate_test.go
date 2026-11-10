package resources_test

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/stackopshq/terraform-provider-pangolin/internal/provider"
	"github.com/stackopshq/terraform-provider-pangolin/internal/resources"
)

// validateWriteOnly runs ValidateResourceConfig through the protocol server,
// as Terraform/OpenTofu do at plan time, for a password/pincode resource.
func validateWriteOnly(t *testing.T, r resource.Resource, typeName string, woAllowed bool, attrs map[string]tftypes.Value) []string {
	t.Helper()
	ctx := context.Background()
	var sr resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &sr)
	typ := sr.Schema.Type().TerraformType(ctx).(tftypes.Object)
	vals := map[string]tftypes.Value{}
	for name, at := range typ.AttributeTypes {
		vals[name] = tftypes.NewValue(at, nil)
	}
	for k, v := range attrs {
		vals[k] = v
	}
	cfg, err := tfprotov6.NewDynamicValue(typ, tftypes.NewValue(typ, vals))
	if err != nil {
		t.Fatal(err)
	}
	srv, err := providerserver.NewProtocol6WithError(provider.New("test")())()
	if err != nil {
		t.Fatal(err)
	}
	resp, err := srv.ValidateResourceConfig(ctx, &tfprotov6.ValidateResourceConfigRequest{
		TypeName:           typeName,
		Config:             &cfg,
		ClientCapabilities: &tfprotov6.ValidateResourceConfigClientCapabilities{WriteOnlyAttributesAllowed: woAllowed},
	})
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, d := range resp.Diagnostics {
		if d.Severity == tfprotov6.DiagnosticSeverityError {
			out = append(out, d.Summary+": "+d.Detail)
		}
	}
	return out
}

func TestWriteOnlySecretValidation(t *testing.T) {
	str := func(s string) tftypes.Value { return tftypes.NewValue(tftypes.String, s) }
	num := func(n int64) tftypes.Value { return tftypes.NewValue(tftypes.Number, n) }
	unknown := tftypes.NewValue(tftypes.String, tftypes.UnknownValue)
	id := num(1)

	for _, rc := range []struct {
		r        resource.Resource
		typeName string
		name     string
		value    string
	}{
		{resources.NewResourcePasswordResource(), "pangolin_resource_password", "password", "s3cret"},
		{resources.NewResourcePincodeResource(), "pangolin_resource_pincode", "pincode", "123456"},
	} {
		wo, ver := rc.name+"_wo", rc.name+"_wo_version"
		cases := []struct {
			desc      string
			woAllowed bool
			attrs     map[string]tftypes.Value
			wantErr   string
		}{
			{"historical attribute", true, map[string]tftypes.Value{rc.name: str(rc.value)}, ""},
			{"historical attribute on an old client", false, map[string]tftypes.Value{rc.name: str(rc.value)}, ""},
			{"write-only with version", true, map[string]tftypes.Value{wo: str(rc.value), ver: num(1)}, ""},
			{"unknown historical value", true, map[string]tftypes.Value{rc.name: unknown}, ""},
			{"unknown write-only value", true, map[string]tftypes.Value{wo: unknown, ver: num(1)}, ""},
			{"unknown version", true, map[string]tftypes.Value{wo: str(rc.value), ver: tftypes.NewValue(tftypes.Number, tftypes.UnknownValue)}, ""},
			{"neither", true, nil, "Missing Attribute Configuration: Exactly one of these attributes must be configured: [" + rc.name + "," + wo + "]"},
			{"both", true, map[string]tftypes.Value{rc.name: str(rc.value), wo: str(rc.value), ver: num(1)}, "Invalid Attribute Combination: Exactly one of these attributes must be configured: [" + rc.name + "," + wo + "]"},
			{"write-only without version", true, map[string]tftypes.Value{wo: str(rc.value)}, "These attributes must be configured together: [" + wo + "," + ver + "]"},
			{"version without write-only", true, map[string]tftypes.Value{rc.name: str(rc.value), ver: num(1)}, "These attributes must be configured together: [" + wo + "," + ver + "]"},
			{"write-only on an old client", false, map[string]tftypes.Value{wo: str(rc.value), ver: num(1)}, "Write-only attributes are only supported"},
		}
		for _, c := range cases {
			attrs := map[string]tftypes.Value{"resource_id": id}
			for k, v := range c.attrs {
				attrs[k] = v
			}
			errs := validateWriteOnly(t, rc.r, rc.typeName, c.woAllowed, attrs)
			if c.wantErr == "" && len(errs) > 0 {
				t.Errorf("%s / %s: unexpected errors %v", rc.typeName, c.desc, errs)
			}
			if c.wantErr != "" && !strings.Contains(strings.Join(errs, "\n"), c.wantErr) {
				t.Errorf("%s / %s: errors %v, want one containing %q", rc.typeName, c.desc, errs, c.wantErr)
			}
		}
	}
}

// A new write-only value with the same version must plan no change: the
// client then never calls ApplyResourceChange, so nothing reaches Pangolin.
func TestWriteOnly_ValueChangeWithoutVersionPlansNothing(t *testing.T) {
	ctx := context.Background()
	for _, rc := range []struct {
		r        resource.Resource
		typeName string
		name     string
	}{
		{resources.NewResourcePasswordResource(), "pangolin_resource_password", "password"},
		{resources.NewResourcePincodeResource(), "pangolin_resource_pincode", "pincode"},
	} {
		var sr resource.SchemaResponse
		rc.r.Schema(ctx, resource.SchemaRequest{}, &sr)
		typ := sr.Schema.Type().TerraformType(ctx).(tftypes.Object)
		obj := func(wo any) tftypes.Value {
			vals := map[string]tftypes.Value{
				"resource_id":           tftypes.NewValue(tftypes.Number, 3),
				rc.name:                 tftypes.NewValue(tftypes.String, nil),
				rc.name + "_wo":         tftypes.NewValue(tftypes.String, wo),
				rc.name + "_wo_version": tftypes.NewValue(tftypes.Number, 1),
			}
			return tftypes.NewValue(typ, vals)
		}
		dyn := func(v tftypes.Value) *tfprotov6.DynamicValue {
			d, err := tfprotov6.NewDynamicValue(typ, v)
			if err != nil {
				t.Fatal(err)
			}
			return &d
		}
		srv, err := providerserver.NewProtocol6WithError(provider.New("test")())()
		if err != nil {
			t.Fatal(err)
		}
		prior := obj(nil) // what the last apply stored: the write-only value is null
		resp, err := srv.PlanResourceChange(ctx, &tfprotov6.PlanResourceChangeRequest{
			TypeName:         rc.typeName,
			PriorState:       dyn(prior),
			ProposedNewState: dyn(obj("new-secret")),
			Config:           dyn(obj("new-secret")),
		})
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range resp.Diagnostics {
			t.Fatalf("%s: %s: %s", rc.typeName, d.Summary, d.Detail)
		}
		planned, err := resp.PlannedState.Unmarshal(typ)
		if err != nil {
			t.Fatal(err)
		}
		if !planned.Equal(prior) || len(resp.RequiresReplace) > 0 {
			t.Errorf("%s: planned %v, want the prior state %v", rc.typeName, planned, prior)
		}
	}
}
