package resources

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// subdomainChanges reports whether the plan moves an existing resource
// to another subdomain, which rebuilds its full domain and wildcard flag.
func subdomainChanges(ctx context.Context, plan tfsdk.Plan, state tfsdk.State) bool {
	if plan.Raw.IsNull() || state.Raw.IsNull() {
		return false
	}
	var p, s, mode types.String
	plan.GetAttribute(ctx, path.Root("subdomain"), &p)
	state.GetAttribute(ctx, path.Root("subdomain"), &s)
	plan.GetAttribute(ctx, path.Root("mode"), &mode)
	// tcp / udp ignore the subdomain: nothing moves.
	return !p.Equal(s) && (mode.IsUnknown() || resourceModeUsesDomain(mode.ValueString()))
}

// followSubdomain runs after UseStateForUnknown: the value it kept from
// the state is stale once the subdomain changes.
type followSubdomain struct{}

func (followSubdomain) Description(context.Context) string {
	return "Unknown when the subdomain changes."
}

func (m followSubdomain) MarkdownDescription(ctx context.Context) string { return m.Description(ctx) }

func (followSubdomain) PlanModifyString(ctx context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	if subdomainChanges(ctx, req.Plan, req.State) {
		resp.PlanValue = types.StringUnknown()
	}
}

func (followSubdomain) PlanModifyBool(ctx context.Context, req planmodifier.BoolRequest, resp *planmodifier.BoolResponse) {
	if subdomainChanges(ctx, req.Plan, req.State) {
		resp.PlanValue = types.BoolUnknown()
	}
}
