package resources

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/stackopshq/terraform-provider-pangolin/internal/client"
)

func TestResourceRule_WireToState(t *testing.T) {
	// Real list payload: resourceId is null on 1.24 inline policies, so
	// resource_id must survive from the prior state.
	var rule client.ResourceRule
	raw := `{"ruleId":9,"resourceId":null,"action":"ACCEPT","match":"CIDR","value":"10.0.0.0/8","priority":50,"enabled":true}`
	if err := json.Unmarshal([]byte(raw), &rule); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	m := applyResourceRuleResponse(ResourceRuleModel{ResourceID: types.Int64Value(3)}, &rule)
	if m.ID.ValueInt64() != 9 || m.ResourceID.ValueInt64() != 3 {
		t.Errorf("ids: %d / %d", m.ID.ValueInt64(), m.ResourceID.ValueInt64())
	}
	if m.Action.ValueString() != "ACCEPT" || m.Match.ValueString() != "CIDR" {
		t.Errorf("action/match wrong")
	}
	if m.Value.ValueString() != "10.0.0.0/8" {
		t.Errorf("value = %q", m.Value.ValueString())
	}
	if m.Priority.ValueInt64() != 50 || !m.Enabled.ValueBool() {
		t.Errorf("priority/enabled wrong")
	}
}

func TestResourceRule_StateToRequest(t *testing.T) {
	plan := ResourceRuleModel{
		Action:   types.StringValue("DROP"),
		Match:    types.StringValue("IP"),
		Value:    types.StringValue("1.2.3.4"),
		Priority: types.Int64Value(1),
		Enabled:  types.BoolValue(false),
	}
	raw, err := json.Marshal(resourceRuleRequestFromModel(plan))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// Every field is Required at the schema level, so the payload
	// is deterministic and can be exact-matched.
	want := `{"action":"DROP","match":"IP","value":"1.2.3.4","priority":1,"enabled":false}`
	if string(raw) != want {
		t.Errorf("payload = %s, want %s", raw, want)
	}
}

func TestResourceRule_RoundTrip(t *testing.T) {
	plan := ResourceRuleModel{
		ID:         types.Int64Value(1),
		ResourceID: types.Int64Value(2),
		Action:     types.StringValue("PASS"),
		Match:      types.StringValue("PATH"),
		Value:      types.StringValue("/admin"),
		Priority:   types.Int64Value(10),
		Enabled:    types.BoolValue(true),
	}
	raw, err := json.Marshal(resourceRuleRequestFromModel(plan))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// Emulate the server echo: it adds ruleId, not a usable resourceId.
	envelope := `{"ruleId":1,"resourceId":null,` + string(raw)[1:]
	var rule client.ResourceRule
	if err := json.Unmarshal([]byte(envelope), &rule); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	back := applyResourceRuleResponse(plan, &rule)
	if back != plan {
		t.Errorf("round trip = %+v, want %+v", back, plan)
	}
}

// emptyResourceRuleState returns a null state for the resource schema, as
// the framework hands it to ImportState.
func emptyResourceRuleState(t *testing.T) tfsdk.State {
	t.Helper()
	var sresp resource.SchemaResponse
	NewResourceRuleResource().Schema(context.Background(), resource.SchemaRequest{}, &sresp)
	return tfsdk.State{
		Schema: sresp.Schema,
		Raw:    tftypes.NewValue(sresp.Schema.Type().TerraformType(context.Background()), nil),
	}
}

// TestResourceRule_ImportState runs ImportState itself: resource_id must
// come from the "{resource_id}/{rule_id}" address, since the rules list
// does not carry it.
func TestResourceRule_ImportState(t *testing.T) {
	r := &ResourceRuleResource{}
	resp := &resource.ImportStateResponse{State: emptyResourceRuleState(t)}
	r.ImportState(context.Background(), resource.ImportStateRequest{ID: "13/5"}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("ImportState: %v", resp.Diagnostics)
	}
	var m ResourceRuleModel
	if diags := resp.State.Get(context.Background(), &m); diags.HasError() {
		t.Fatalf("state get: %v", diags)
	}
	if m.ResourceID.ValueInt64() != 13 || m.ID.ValueInt64() != 5 {
		t.Errorf("resource_id/id = %d/%d, want 13/5", m.ResourceID.ValueInt64(), m.ID.ValueInt64())
	}
}

func TestResourceRule_ImportStateInvalidID(t *testing.T) {
	for _, id := range []string{"13", "x/5", "13/y", ""} {
		r := &ResourceRuleResource{}
		resp := &resource.ImportStateResponse{State: emptyResourceRuleState(t)}
		r.ImportState(context.Background(), resource.ImportStateRequest{ID: id}, resp)
		if !resp.Diagnostics.HasError() {
			t.Errorf("import ID %q accepted, want an error", id)
		}
	}
}

// TestResourceRule_MatchValidator runs the schema's own validators so a
// match type dropped from the list fails here, not on users' plans.
func TestResourceRule_MatchValidator(t *testing.T) {
	var resp resource.SchemaResponse
	NewResourceRuleResource().Schema(context.Background(), resource.SchemaRequest{}, &resp)
	attr, ok := resp.Schema.Attributes["match"].(schema.StringAttribute)
	if !ok {
		t.Fatalf("match attribute missing or not a string")
	}

	validate := func(v string) bool {
		req := validator.StringRequest{Path: path.Root("match"), ConfigValue: types.StringValue(v)}
		for _, val := range attr.Validators {
			var vresp validator.StringResponse
			val.ValidateString(context.Background(), req, &vresp)
			if vresp.Diagnostics.HasError() {
				return false
			}
		}
		return true
	}

	for _, v := range resourceRuleMatchTypes {
		if !validate(v) {
			t.Errorf("match %q rejected, want accepted", v)
		}
	}
	for _, v := range []string{"country", "HOST", ""} {
		if validate(v) {
			t.Errorf("match %q accepted, want rejected", v)
		}
	}

	// Pin the list to RESOURCE_RULE_MATCH_TYPES in Pangolin 1.24
	// (server/lib/validators.ts) so a type removed by mistake is caught.
	for _, v := range []string{"CIDR", "IP", "PATH", "COUNTRY", "COUNTRY_IS_NOT", "ASN", "REGION", "METHOD"} {
		if !slices.Contains(resourceRuleMatchTypes, v) {
			t.Errorf("resourceRuleMatchTypes lacks %q, which Pangolin 1.24 accepts", v)
		}
	}
}
