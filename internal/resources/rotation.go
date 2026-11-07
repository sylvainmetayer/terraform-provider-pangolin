package resources

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// rotationTriggerAttribute builds the `rotation_trigger` attribute shared by the
// resources whose credential can be re-keyed (site, client).
func rotationTriggerAttribute(secretAttr, what string) schema.StringAttribute {
	return schema.StringAttribute{
		Description: "Arbitrary value that triggers a rotation of `" + secretAttr + "` whenever it changes (for example `\"2026-11\"`). " +
			"On change, a new " + what + " secret is generated and `" + secretAttr + "` is updated in state. " +
			"By default the new secret takes effect immediately: Pangolin disconnects the running tunnel and the old secret is rejected, " +
			"so the connector stays down until it is restarted with the new secret (see `rotation_disconnect`). " +
			"Changing it from unset to a value also rotates; removing it does not. Not read back from the API, so it is unset after `terraform import`.",
		Optional: true,
	}
}

func rotationDisconnectAttribute(what string) schema.BoolAttribute {
	return schema.BoolAttribute{
		Description: "Whether a rotation disconnects the running " + what + " (default `true`). With `false` the tunnel stays up on its current session, " +
			"and the old secret is only refused at the next reconnect: restart the connector with the new secret soon, since any reconnect (network loss, restart) before that fails.",
		Optional: true,
		Computed: true,
		Default:  booldefault.StaticBool(true),
	}
}

// rotationDisconnectOrDefault fills a state written before the attribute
// existed, so the default does not show as a change in every plan.
func rotationDisconnectOrDefault(v types.Bool) types.Bool {
	if v.IsNull() || v.IsUnknown() {
		return types.BoolValue(true)
	}
	return v
}

// rotationRequested reports whether a plan/state pair of rotation_trigger values asks
// for a rotation. Clearing the trigger (value -> null) is not a request: it must not
// re-key the secret and cut the running tunnel.
func rotationRequested(planTrig, stateTrig types.String) bool {
	return !planTrig.IsNull() && !planTrig.Equal(stateTrig)
}

// markSecretUnknownOnRotation marks the secret attribute unknown in the plan when
// rotation_trigger changes on an existing resource, so dependents see the new value
// after apply instead of a stale one.
func markSecretUnknownOnRotation(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse, secretAttr string) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return
	}
	var planTrig, stateTrig types.String
	resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root("rotation_trigger"), &planTrig)...)
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("rotation_trigger"), &stateTrig)...)
	if resp.Diagnostics.HasError() || !rotationRequested(planTrig, stateTrig) {
		return
	}
	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root(secretAttr), types.StringUnknown())...)
}
