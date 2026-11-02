package resources

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// domainPlanWithType builds a plan whose only meaningful attribute is
// type, which is all domainCertSettingModifier reads.
func domainPlanWithType(t *testing.T, typ types.String) tfsdk.Plan {
	t.Helper()
	var sresp resource.SchemaResponse
	(&DomainResource{}).Schema(context.Background(), resource.SchemaRequest{}, &sresp)
	m := DomainResourceModel{
		ID:                 types.StringUnknown(),
		BaseDomain:         types.StringValue("apps.example.com"),
		Type:               typ,
		CertResolver:       types.StringUnknown(),
		PreferWildcardCert: types.BoolUnknown(),
		Verified:           types.BoolUnknown(),
		Failed:             types.BoolUnknown(),
		ErrorMessage:       types.StringUnknown(),
		ConfigManaged:      types.BoolUnknown(),
		DNSRecords:         types.ListUnknown(types.ObjectType{AttrTypes: domainDNSRecordAttrTypes}),
	}
	p := tfsdk.Plan{Schema: sresp.Schema}
	if diags := p.Set(context.Background(), &m); diags.HasError() {
		t.Fatalf("plan: %v", diags)
	}
	return p
}

func TestDomainCertSettingModifier_String(t *testing.T) {
	for _, tc := range []struct {
		name   string
		typ    types.String
		config types.String
		state  types.String
		want   types.String
	}{
		{"wildcard unset clears", types.StringValue("wildcard"), types.StringNull(), types.StringValue("le"), types.StringNull()},
		{"wildcard set keeps config", types.StringValue("wildcard"), types.StringValue("le"), types.StringNull(), types.StringValue("le")},
		{"cname unset keeps server value", types.StringValue("cname"), types.StringNull(), types.StringValue("le"), types.StringValue("le")},
		{"cname create plans null", types.StringValue("cname"), types.StringNull(), types.StringNull(), types.StringNull()},
		{"unknown type leaves plan", types.StringUnknown(), types.StringNull(), types.StringValue("le"), types.StringUnknown()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := planmodifier.StringRequest{
				Plan:        domainPlanWithType(t, tc.typ),
				ConfigValue: tc.config,
				StateValue:  tc.state,
				PlanValue:   types.StringUnknown(),
			}
			if !tc.config.IsNull() {
				req.PlanValue = tc.config
			}
			resp := &planmodifier.StringResponse{PlanValue: req.PlanValue}
			domainCertSettingModifier{}.PlanModifyString(context.Background(), req, resp)
			if !resp.PlanValue.Equal(tc.want) {
				t.Errorf("plan = %v, want %v", resp.PlanValue, tc.want)
			}
		})
	}
}

func TestDomainCertSettingModifier_Bool(t *testing.T) {
	for _, tc := range []struct {
		name   string
		typ    types.String
		config types.Bool
		state  types.Bool
		want   types.Bool
	}{
		{"wildcard unset defaults false", types.StringValue("wildcard"), types.BoolNull(), types.BoolValue(true), types.BoolValue(false)},
		{"wildcard set keeps config", types.StringValue("wildcard"), types.BoolValue(true), types.BoolNull(), types.BoolValue(true)},
		{"ns unset keeps server value", types.StringValue("ns"), types.BoolNull(), types.BoolValue(true), types.BoolValue(true)},
		{"ns create plans false", types.StringValue("ns"), types.BoolNull(), types.BoolNull(), types.BoolValue(false)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := planmodifier.BoolRequest{
				Plan:        domainPlanWithType(t, tc.typ),
				ConfigValue: tc.config,
				StateValue:  tc.state,
				PlanValue:   types.BoolUnknown(),
			}
			if !tc.config.IsNull() {
				req.PlanValue = tc.config
			}
			resp := &planmodifier.BoolResponse{PlanValue: req.PlanValue}
			domainCertSettingModifier{}.PlanModifyBool(context.Background(), req, resp)
			if !resp.PlanValue.Equal(tc.want) {
				t.Errorf("plan = %v, want %v", resp.PlanValue, tc.want)
			}
		})
	}
}

func TestDomainCertSettingModifier_DestroyLeavesPlan(t *testing.T) {
	var sresp resource.SchemaResponse
	(&DomainResource{}).Schema(context.Background(), resource.SchemaRequest{}, &sresp)
	plan := tfsdk.Plan{Schema: sresp.Schema, Raw: tftypes.NewValue(sresp.Schema.Type().TerraformType(context.Background()), nil)}
	resp := &planmodifier.StringResponse{PlanValue: types.StringNull()}
	domainCertSettingModifier{}.PlanModifyString(context.Background(), planmodifier.StringRequest{
		Plan: plan, ConfigValue: types.StringNull(), StateValue: types.StringValue("le"),
	}, resp)
	if !resp.PlanValue.IsNull() {
		t.Errorf("plan = %v, want null on destroy", resp.PlanValue)
	}
}

func TestValidateDomainCNAMERoot(t *testing.T) {
	for _, tc := range []struct {
		typ, domain string
		wantErr     bool
	}{
		{"cname", "example.com", true},
		{"cname", "portal.example.com", false},
		{"ns", "example.com", false},
		{"wildcard", "example.com", false},
	} {
		diags := validateDomainCNAMERoot(DomainResourceModel{Type: types.StringValue(tc.typ), BaseDomain: types.StringValue(tc.domain)})
		if diags.HasError() != tc.wantErr {
			t.Errorf("%s %s: HasError = %v, want %v", tc.typ, tc.domain, diags.HasError(), tc.wantErr)
		}
	}
}

func TestValidateDomainCertSettings_UnknownResolverIsSkipped(t *testing.T) {
	diags := validateDomainCertSettings(DomainResourceModel{
		Type: types.StringValue("cname"), CertResolver: types.StringUnknown(), PreferWildcardCert: types.BoolUnknown(),
	})
	if diags.HasError() {
		t.Errorf("unknown values must be checked at apply, got %v", diags)
	}
}

func TestDomainCreate_CnameWithResolverRefusedBeforeWrite(t *testing.T) {
	fake := &domainFakeServer{}
	r := &DomainResource{client: fake.client(t, false)}
	plan, state := domainPlan(t, r)
	// The type became known only at apply time.
	if diags := plan.SetAttribute(context.Background(), path.Root("type"), types.StringValue("cname")); diags.HasError() {
		t.Fatalf("set type: %v", diags)
	}
	resp := &resource.CreateResponse{State: state}
	r.Create(context.Background(), resource.CreateRequest{Plan: plan}, resp)
	if !resp.Diagnostics.HasError() {
		t.Fatal("expected an error for cert settings on a cname domain")
	}
	if len(fake.calls) != 0 {
		t.Errorf("calls = %v, want none", fake.calls)
	}
}

func TestDomainCreateErrorHint_OnlyOn501(t *testing.T) {
	if got := domainCreateErrorHint("cname", errors.New("API error (status 501): Creating NS or CNAME records is not supported")); !strings.Contains(got, "Enterprise") {
		t.Errorf("501 hint = %q", got)
	}
	if got := domainCreateErrorHint("cname", errors.New("API error (status 400): Domain is already added to this org")); got != "" {
		t.Errorf("400 hint = %q, want none", got)
	}
	if got := domainCreateErrorHint("wildcard", errors.New("API error (status 501): x")); got != "" {
		t.Errorf("wildcard hint = %q, want none", got)
	}
}

func TestKeepPlannedVerification(t *testing.T) {
	recType := types.ObjectType{AttrTypes: domainDNSRecordAttrTypes}
	planned, _ := types.ListValue(recType, []attr.Value{})
	plan := DomainResourceModel{
		Verified: types.BoolValue(false), Failed: types.BoolValue(false),
		ErrorMessage: types.StringNull(), DNSRecords: planned,
	}
	got := DomainResourceModel{
		Verified: types.BoolValue(true), Failed: types.BoolValue(true),
		ErrorMessage: types.StringValue("x"), DNSRecords: types.ListNull(recType),
	}
	keepPlannedVerification(&got, plan)
	if got.Verified.ValueBool() || got.Failed.ValueBool() || !got.ErrorMessage.IsNull() || got.DNSRecords.IsNull() {
		t.Errorf("got %+v, want the planned values", got)
	}

	unknownPlan := DomainResourceModel{
		Verified: types.BoolUnknown(), Failed: types.BoolUnknown(),
		ErrorMessage: types.StringUnknown(), DNSRecords: types.ListUnknown(recType),
	}
	fresh := DomainResourceModel{Verified: types.BoolValue(true)}
	keepPlannedVerification(&fresh, unknownPlan)
	if !fresh.Verified.ValueBool() {
		t.Errorf("unknown plan values must not override the server values")
	}
}
