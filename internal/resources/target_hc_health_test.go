package resources

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// targetPlanState builds a framework Plan and State for the target
// schema, as the framework hands them to an attribute plan modifier
// on update: hc_health already marked unknown in the plan because the
// resource has a planned change.
func targetPlanState(t *testing.T, planHCEnabled types.Bool, priorHealth string) (tfsdk.Plan, tfsdk.State) {
	t.Helper()
	ctx := context.Background()
	var sr resource.SchemaResponse
	(&TargetResource{}).Schema(ctx, resource.SchemaRequest{}, &sr)

	base := TargetResourceModel{
		ID: types.Int64Value(44), ResourceID: types.Int64Value(74), SiteID: types.Int64Value(6),
		IP: types.StringValue("paperless"), Port: types.Int64Value(8000),
		Method: types.StringValue("http"), Enabled: types.BoolValue(true),
		HCEnabled: types.BoolValue(true), HCHealth: types.StringValue(priorHealth),
	}
	state := tfsdk.State{Schema: sr.Schema}
	if d := state.Set(ctx, &base); d.HasError() {
		t.Fatalf("state: %v", d)
	}

	planned := base
	planned.HCEnabled = planHCEnabled
	planned.HCHealth = types.StringUnknown()
	plan := tfsdk.Plan{Schema: sr.Schema}
	if d := plan.Set(ctx, &planned); d.HasError() {
		t.Fatalf("plan: %v", d)
	}
	return plan, state
}

func runHCHealthModifier(t *testing.T, plan tfsdk.Plan, state tfsdk.State) types.String {
	t.Helper()
	ctx := context.Background()
	var planValue, stateValue types.String
	plan.GetAttribute(ctx, path.Root("hc_health"), &planValue)
	state.GetAttribute(ctx, path.Root("hc_health"), &stateValue)
	req := planmodifier.StringRequest{
		Path: path.Root("hc_health"), Plan: plan, State: state,
		PlanValue: planValue, StateValue: stateValue,
	}
	resp := &planmodifier.StringResponse{PlanValue: req.PlanValue}
	hcHealthPlanModifier{}.PlanModifyString(ctx, req, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("modifier: %v", resp.Diagnostics)
	}
	return resp.PlanValue
}

// Disabling the health check: Pangolin resets hcHealth to "unknown".
// Planning the prior "healthy"/"unhealthy" made the apply fail with
// "Provider produced inconsistent result after apply".
func TestHCHealthPlanModifier_DisablingPlansUnknownString(t *testing.T) {
	for _, prior := range []string{"healthy", "unhealthy", "unknown"} {
		plan, state := targetPlanState(t, types.BoolValue(false), prior)
		got := runHCHealthModifier(t, plan, state)
		if got.IsUnknown() || got.ValueString() != "unknown" {
			t.Errorf("prior %q: want \"unknown\", got %v", prior, got)
		}
	}
}

// Enabled (or being turned on): only the server knows the answer
// ("unhealthy" when turned on, whatever the probe says otherwise).
func TestHCHealthPlanModifier_EnabledStaysUnknown(t *testing.T) {
	for _, prior := range []string{"healthy", "unknown"} {
		plan, state := targetPlanState(t, types.BoolValue(true), prior)
		if got := runHCHealthModifier(t, plan, state); !got.IsUnknown() {
			t.Errorf("prior %q: want unknown (known after apply), got %v", prior, got)
		}
	}
}

// No planned change: the framework hands over the state value, which
// must stay untouched so the plan remains empty.
func TestHCHealthPlanModifier_KnownValueUntouched(t *testing.T) {
	plan, state := targetPlanState(t, types.BoolValue(false), "healthy")
	ctx := context.Background()
	req := planmodifier.StringRequest{
		Path: path.Root("hc_health"), Plan: plan, State: state,
		PlanValue: types.StringValue("healthy"), StateValue: types.StringValue("healthy"),
	}
	resp := &planmodifier.StringResponse{PlanValue: req.PlanValue}
	hcHealthPlanModifier{}.PlanModifyString(ctx, req, resp)
	if resp.PlanValue.ValueString() != "healthy" {
		t.Errorf("want state value kept, got %v", resp.PlanValue)
	}
}

// Create: no prior state, value stays unknown until the server answers.
func TestHCHealthPlanModifier_CreateStaysUnknown(t *testing.T) {
	plan, state := targetPlanState(t, types.BoolValue(false), "healthy")
	nullState := tfsdk.State{Schema: state.Schema} // zero Raw: null object
	ctx := context.Background()
	req := planmodifier.StringRequest{
		Path: path.Root("hc_health"), Plan: plan, State: nullState,
		PlanValue: types.StringUnknown(), StateValue: types.StringNull(),
	}
	resp := &planmodifier.StringResponse{PlanValue: req.PlanValue}
	hcHealthPlanModifier{}.PlanModifyString(ctx, req, resp)
	if !resp.PlanValue.IsUnknown() {
		t.Errorf("want unknown on create, got %v", resp.PlanValue)
	}
}

// The schema must wire the modifier, not UseStateForUnknown.
func TestTargetSchema_HCHealthUsesHCHealthPlanModifier(t *testing.T) {
	var sr resource.SchemaResponse
	(&TargetResource{}).Schema(context.Background(), resource.SchemaRequest{}, &sr)
	attr, ok := sr.Schema.Attributes["hc_health"].(schema.StringAttribute)
	if !ok {
		t.Fatalf("hc_health is not a StringAttribute")
	}
	if len(attr.PlanModifiers) != 1 {
		t.Fatalf("want exactly one plan modifier, got %d", len(attr.PlanModifiers))
	}
	if _, ok := attr.PlanModifiers[0].(hcHealthPlanModifier); !ok {
		t.Errorf("hc_health plan modifier is %T, want hcHealthPlanModifier", attr.PlanModifiers[0])
	}
}
