package resources

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stackopshq/terraform-provider-pangolin/internal/client"
)

// `protocol` is a legacy alias of the L4 transport. Since Pangolin 1.19
// the transport lives in `mode` (the resources table has no protocol
// column, and createResource ignores `protocol` when `mode` is set), so
// the provider derives it from mode: "tcp" / "udp" for L4 resources and
// "tcp" for http, where it is ignored. Create, Read and Import therefore
// store the same value, and the plan never flips.

// httpResourceProtocolDeprecation is the deprecation message of the
// `protocol` attribute.
const httpResourceProtocolDeprecation = "Use `mode` instead. `protocol` is derived from `mode` " +
	"and will be removed in the next major version."

// resolveHTTPResourceProtocol picks the state value for `protocol`.
// For L4 modes it follows mode. Otherwise a value echoed by a pre-1.19
// server wins, then the known prior (planned / state) value, then "tcp".
func resolveHTTPResourceProtocol(prior types.String, res *client.Resource) types.String {
	switch res.Mode {
	case "tcp", "udp":
		return types.StringValue(res.Mode)
	}
	if res.Protocol != "" {
		return types.StringValue(res.Protocol)
	}
	if !prior.IsNull() && !prior.IsUnknown() {
		return prior
	}
	return types.StringValue("tcp")
}

// protocolFromModeModifier plans `protocol` from `mode` when the
// attribute is left out of the config.
type protocolFromModeModifier struct{}

func (protocolFromModeModifier) Description(context.Context) string {
	return "Defaults protocol to the L4 mode (tcp / udp), or tcp for http."
}

func (m protocolFromModeModifier) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (protocolFromModeModifier) PlanModifyString(ctx context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	if !req.ConfigValue.IsNull() || req.Plan.Raw.IsNull() {
		return
	}
	var mode types.String
	resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root("mode"), &mode)...)
	if resp.Diagnostics.HasError() || mode.IsUnknown() {
		return
	}
	switch mode.ValueString() {
	case "tcp", "udp":
		resp.PlanValue = types.StringValue(mode.ValueString())
	default:
		if !req.StateValue.IsNull() && !req.StateValue.IsUnknown() {
			resp.PlanValue = req.StateValue
			return
		}
		resp.PlanValue = types.StringValue("tcp")
	}
}

// protocolMatchesModeValidator rejects a configured `protocol` the
// server cannot apply: one that differs from an L4 mode (mode wins
// server-side), or "udp" on an http resource (refused by the server).
type protocolMatchesModeValidator struct{}

func (protocolMatchesModeValidator) Description(context.Context) string {
	return "protocol must match an L4 mode, and cannot be udp for http resources."
}

func (v protocolMatchesModeValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (protocolMatchesModeValidator) ValidateString(ctx context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	var mode types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("mode"), &mode)...)
	if resp.Diagnostics.HasError() || mode.IsUnknown() {
		return
	}
	modeValue := mode.ValueString()
	if mode.IsNull() {
		modeValue = "http"
	}
	protocol := req.ConfigValue.ValueString()
	switch modeValue {
	case "tcp", "udp":
		if protocol != modeValue {
			resp.Diagnostics.AddAttributeError(req.Path, "Protocol does not match mode",
				fmt.Sprintf("protocol = %q conflicts with mode = %q. Pangolin derives the transport from "+
					"mode; remove protocol or set it to %q.", protocol, modeValue, modeValue))
		}
	case "http":
		if protocol == "udp" {
			resp.Diagnostics.AddAttributeError(req.Path, "Protocol not supported for http",
				`protocol = "udp" is refused by Pangolin for http resources; remove it or use mode = "udp".`)
		}
	}
}
