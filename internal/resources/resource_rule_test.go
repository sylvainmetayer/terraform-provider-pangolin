package resources

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stackopshq/terraform-provider-pangolin/internal/client"
)

// buildSetRuleRequestFromPlan mirrors the Create/Update inline mapping.
func buildSetRuleRequestFromPlan(plan ResourceRuleModel) *client.SetResourceRuleRequest {
	return &client.SetResourceRuleRequest{
		Action:   plan.Action.ValueString(),
		Match:    plan.Match.ValueString(),
		Value:    plan.Value.ValueString(),
		Priority: int(plan.Priority.ValueInt64()),
		Enabled:  plan.Enabled.ValueBool(),
	}
}

func TestResourceRule_WireToState(t *testing.T) {
	// Real list payload: no resourceId on the rule (null on 1.24 inline
	// policies), so resource_id must survive from the prior state.
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
	req := buildSetRuleRequestFromPlan(plan)
	if req.Action != "DROP" || req.Match != "IP" || req.Value != "1.2.3.4" ||
		req.Priority != 1 || req.Enabled != false {
		t.Errorf("request = %+v", req)
	}
	raw, err := json.Marshal(req)
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
	req := buildSetRuleRequestFromPlan(plan)
	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// Emulate the server echo: it adds ruleId, not a usable resourceId.
	envelope := `{"ruleId":1,` + string(raw)[1:]
	var rule client.ResourceRule
	if err := json.Unmarshal([]byte(envelope), &rule); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	back := applyResourceRuleResponse(plan, &rule)
	if back != plan {
		t.Errorf("round trip = %+v, want %+v", back, plan)
	}
}

// TestResourceRule_ImportUsesAddressResourceID pins the import mapping:
// resource_id comes from the "{resource_id}/{rule_id}" address even when
// the wire rule carries 0.
func TestResourceRule_ImportUsesAddressResourceID(t *testing.T) {
	rule := &client.ResourceRule{RuleID: 5, Action: "DROP", Match: "COUNTRY_IS_NOT", Value: "FR", Priority: 2, Enabled: true}
	m := applyResourceRuleResponse(ResourceRuleModel{ResourceID: types.Int64Value(13)}, rule)
	if m.ResourceID.ValueInt64() != 13 {
		t.Errorf("ResourceID = %d, want 13 (from the import address)", m.ResourceID.ValueInt64())
	}
	if m.ID.ValueInt64() != 5 || m.Match.ValueString() != "COUNTRY_IS_NOT" {
		t.Errorf("model = %+v", m)
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

	for _, v := range []string{"CIDR", "IP", "PATH", "COUNTRY", "COUNTRY_IS_NOT", "ASN", "REGION", "METHOD"} {
		if !validate(v) {
			t.Errorf("match %q rejected, want accepted", v)
		}
	}
	for _, v := range []string{"country", "HOST", ""} {
		if validate(v) {
			t.Errorf("match %q accepted, want rejected", v)
		}
	}
}
