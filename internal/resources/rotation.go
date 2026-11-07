package resources

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// rotationTriggerAttribute builds the `rotation_trigger` attribute shared by the
// resources whose credential can be re-keyed (site, client).
func rotationTriggerAttribute(secretAttr, what string) schema.StringAttribute {
	return schema.StringAttribute{
		Description: "Arbitrary value that triggers a rotation of `" + secretAttr + "` whenever it changes (for example `\"2026-11\"`). " +
			"On change, a new " + what + " secret is generated and `" + secretAttr + "` is updated in state. " +
			"The new secret takes effect immediately: Pangolin disconnects the running tunnel and the old secret is rejected, " +
			"so the connector stays down until it is restarted with the new secret. " +
			"Changing it from unset to a value also rotates. Not read back from the API, so it is unset after `terraform import`.",
		Optional: true,
	}
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
	if resp.Diagnostics.HasError() || planTrig.Equal(stateTrig) {
		return
	}
	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root(secretAttr), types.StringUnknown())...)
}
