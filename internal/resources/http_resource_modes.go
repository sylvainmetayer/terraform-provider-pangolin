package resources

import (
	"context"
	"fmt"
	"sort"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stackopshq/terraform-provider-pangolin/internal/client"
)

var _ resource.ResourceWithConfigValidators = &HTTPResource{}

// resourceModes lists every mode PUT /org/{org}/resource accepts on
// Pangolin 1.24. `ssh` / `rdp` / `vnc` exist since 1.20, `inference`
// since 1.22.
var resourceModes = []string{"http", "ssh", "rdp", "vnc", "inference", "tcp", "udp"}

// resourceModeUsesDomain reports whether a mode is addressed by a
// domain. Those modes share the server's HTTP create and update
// handlers (updateHttpResource); only tcp / udp take the raw L4 path.
// An empty mode is the `http` default.
func resourceModeUsesDomain(mode string) bool {
	switch mode {
	case "", "http", "ssh", "rdp", "vnc", "inference":
		return true
	}
	return false
}

// resourceModeRequiresDomain reports the modes whose create requires a
// domain_id and refuses proxy_port. `http` is left out on purpose: its
// historical behavior (server-side 400) is unchanged.
func resourceModeRequiresDomain(mode string) bool {
	switch mode {
	case "ssh", "rdp", "vnc", "inference":
		return true
	}
	return false
}

// addResourceModeAttributes adds the mode-specific attributes to the
// pangolin_resource schema.
func addResourceModeAttributes(attrs map[string]schema.Attribute) {
	attrs["ai_provider_ids"] = schema.SetAttribute{
		Description: "IDs of the AI providers attached to an `inference` resource (Pangolin >= 1.22). " +
			"Providers are attached in inherit mode, using each provider's own allow/block lists. " +
			"Leave unset to keep whatever is attached on the server; set `[]` to detach every " +
			"provider. Only valid with `mode = \"inference\"`. Reading it needs the API key to be " +
			"allowed to list the resource's AI models.",
		ElementType: types.Int64Type,
		Optional:    true,
		Computed:    true,
		PlanModifiers: []planmodifier.Set{
			setplanmodifier.UseStateForUnknown(),
		},
		Validators: []validator.Set{
			setvalidator.ValueInt64sAre(int64validator.AtLeast(1)),
		},
	}
}

// ConfigValidators enforces the per-mode rules the server applies at
// create time, so a wrong combination fails at plan instead of apply.
func (r *HTTPResource) ConfigValidators(_ context.Context) []resource.ConfigValidator {
	return []resource.ConfigValidator{resourceModeConfigValidator{}}
}

type resourceModeConfigValidator struct{}

func (v resourceModeConfigValidator) Description(_ context.Context) string {
	return "Checks domain_id, proxy_port and ai_provider_ids against the resource mode."
}

func (v resourceModeConfigValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v resourceModeConfigValidator) ValidateResource(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var mode types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("mode"), &mode)...)
	if resp.Diagnostics.HasError() || mode.IsUnknown() {
		return
	}
	m := mode.ValueString()
	if mode.IsNull() {
		m = "http"
	}

	var domainID types.String
	var proxyPort types.Int64
	var providers types.Set
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("domain_id"), &domainID)...)
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("proxy_port"), &proxyPort)...)
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("ai_provider_ids"), &providers)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if resourceModeRequiresDomain(m) {
		if domainID.IsNull() {
			resp.Diagnostics.AddAttributeError(path.Root("domain_id"), "Missing domain_id",
				fmt.Sprintf("`domain_id` is required when `mode = %q`: the resource is reached through a domain.", m))
		}
		if !proxyPort.IsNull() {
			resp.Diagnostics.AddAttributeError(path.Root("proxy_port"), "Unsupported proxy_port",
				fmt.Sprintf("`proxy_port` only applies to `tcp` / `udp` resources, not to `mode = %q`.", m))
		}
	}

	if m != "inference" && !providers.IsNull() && !providers.IsUnknown() && len(providers.Elements()) > 0 {
		resp.Diagnostics.AddAttributeError(path.Root("ai_provider_ids"), "Unsupported ai_provider_ids",
			fmt.Sprintf("AI providers can only be attached to `mode = \"inference\"` resources, not to `mode = %q`.", m))
	}
}

// buildHTTPResourceCreateRequest builds the PUT /org/{org}/resource
// body. Domain modes are sent with `http: true` plus the literal mode
// (omitted for `http`, so pre-1.19 servers keep their payload); L4
// modes keep the legacy `http: false` + protocol pair.
func buildHTTPResourceCreateRequest(ctx context.Context, plan HTTPResourceModel) (*client.CreateResourceRequest, diag.Diagnostics) {
	var diags diag.Diagnostics
	mode := plan.Mode.ValueString()
	if mode == "" {
		mode = "http"
	}
	req := &client.CreateResourceRequest{
		Name:     plan.Name.ValueString(),
		HTTP:     resourceModeUsesDomain(mode),
		DomainID: plan.DomainID.ValueString(),
		Protocol: plan.Protocol.ValueString(),
	}
	if mode != "http" {
		req.Mode = mode
	}
	if !plan.Subdomain.IsNull() && !plan.Subdomain.IsUnknown() {
		subdomain := plan.Subdomain.ValueString()
		req.Subdomain = &subdomain
	}
	if !plan.ProxyPort.IsNull() && !plan.ProxyPort.IsUnknown() {
		v := int(plan.ProxyPort.ValueInt64())
		req.ProxyPort = &v
	}
	if mode == "inference" {
		ids, d := aiProviderIDsFromSet(ctx, plan.AIProviderIDs)
		diags.Append(d...)
		for _, id := range ids {
			req.AIProviders = append(req.AIProviders, client.ResourceAIProviderAttachment{ProviderID: id})
		}
	}
	return req, diags
}

// syncAIProviders makes the providers attached on the server match the
// planned ai_provider_ids of an inference resource. The server list is
// the reference, so providers attached out of band are detached too.
// A null or unknown plan value leaves the attachments alone.
func (r *HTTPResource) syncAIProviders(ctx context.Context, resourceID int, plan HTTPResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics
	if plan.Mode.ValueString() != "inference" || plan.AIProviderIDs.IsNull() || plan.AIProviderIDs.IsUnknown() {
		return diags
	}
	want, d := aiProviderIDsFromSet(ctx, plan.AIProviderIDs)
	diags.Append(d...)
	if diags.HasError() {
		return diags
	}
	current, err := r.client.ListResourceAIProviders(ctx, resourceID)
	if err != nil {
		diags.AddError("Failed to list resource AI providers", err.Error())
		return diags
	}
	toAdd, toRemove := diffAIProviderIDs(want, current)
	for _, id := range toAdd {
		if err := r.client.AddResourceAIProvider(ctx, resourceID, id); err != nil {
			diags.AddError("Failed to attach AI provider", fmt.Sprintf("provider %d: %s", id, err))
			return diags
		}
	}
	for _, id := range toRemove {
		if err := r.client.RemoveResourceAIProvider(ctx, resourceID, id); err != nil {
			diags.AddError("Failed to detach AI provider", fmt.Sprintf("provider %d: %s", id, err))
			return diags
		}
	}
	return diags
}

// refreshAIProviders reads ai_provider_ids back from the server for
// inference resources and sets it to null for every other mode.
func (r *HTTPResource) refreshAIProviders(ctx context.Context, m *HTTPResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics
	if m.Mode.ValueString() != "inference" {
		m.AIProviderIDs = types.SetNull(types.Int64Type)
		return diags
	}
	providers, err := r.client.ListResourceAIProviders(ctx, int(m.ID.ValueInt64()))
	if err != nil {
		diags.AddError("Failed to list resource AI providers", err.Error())
		// State must never hold an unknown value, even on error.
		if m.AIProviderIDs.IsUnknown() {
			m.AIProviderIDs = types.SetNull(types.Int64Type)
		}
		return diags
	}
	m.AIProviderIDs = aiProviderIDSet(providers)
	return diags
}

// diffAIProviderIDs returns the provider IDs to attach and to detach,
// both sorted, to move from the current attachments to want.
func diffAIProviderIDs(want []int, current []client.ResourceAIProvider) (toAdd, toRemove []int) {
	wantSet := make(map[int]bool, len(want))
	for _, id := range want {
		wantSet[id] = true
	}
	have := make(map[int]bool, len(current))
	for _, p := range current {
		have[p.ProviderID] = true
		if !wantSet[p.ProviderID] {
			toRemove = append(toRemove, p.ProviderID)
		}
	}
	for _, id := range want {
		if !have[id] {
			toAdd = append(toAdd, id)
		}
	}
	sort.Ints(toAdd)
	sort.Ints(toRemove)
	return toAdd, toRemove
}

// aiProviderIDSet converts the attached providers to the state set.
func aiProviderIDSet(providers []client.ResourceAIProvider) types.Set {
	elems := make([]attr.Value, len(providers))
	for i, p := range providers {
		elems[i] = types.Int64Value(int64(p.ProviderID))
	}
	return types.SetValueMust(types.Int64Type, elems)
}

// aiProviderIDsFromSet converts a known set of int64 to a sorted []int; a
// null or unknown set yields nil.
func aiProviderIDsFromSet(ctx context.Context, s types.Set) ([]int, diag.Diagnostics) {
	if s.IsNull() || s.IsUnknown() {
		return nil, nil
	}
	var vals []int64
	diags := s.ElementsAs(ctx, &vals, false)
	out := make([]int, len(vals))
	for i, v := range vals {
		out[i] = int(v)
	}
	sort.Ints(out)
	return out, diags
}
