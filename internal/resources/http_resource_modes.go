package resources

import (
	"context"
	"errors"
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
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stackopshq/terraform-provider-pangolin/internal/client"
)

var _ resource.ResourceWithConfigValidators = &HTTPResource{}

// resourceModes lists every mode PUT /org/{org}/resource accepts on
// Pangolin 1.24, in documentation order. `ssh` / `rdp` / `vnc` exist
// since 1.20, `inference` since 1.22.
var resourceModes = []string{"http", "ssh", "rdp", "vnc", "inference", "tcp", "udp"}

// resourceModeTraits says how the server handles one resource mode.
type resourceModeTraits struct {
	// usesDomain: the resource is addressed by a domain, created with
	// `http: true` and updated through the server's HTTP handler
	// (updateHttpResource). Only tcp / udp take the raw L4 path.
	usesDomain bool
	// requiresDomain: create needs domain_id and refuses proxy_port.
	// `http` keeps its historical behavior (server-side 400) instead.
	requiresDomain bool
}

var resourceModeTable = map[string]resourceModeTraits{
	"http":      {usesDomain: true},
	"ssh":       {usesDomain: true, requiresDomain: true},
	"rdp":       {usesDomain: true, requiresDomain: true},
	"vnc":       {usesDomain: true, requiresDomain: true},
	"inference": {usesDomain: true, requiresDomain: true},
	"tcp":       {},
	"udp":       {},
}

// resourceModeOf returns the effective mode: an empty mode is the
// `http` default.
func resourceModeOf(mode string) string {
	if mode == "" {
		return "http"
	}
	return mode
}

// resourceModeUsesDomain reports whether a mode is addressed by a domain.
func resourceModeUsesDomain(mode string) bool {
	return resourceModeTable[resourceModeOf(mode)].usesDomain
}

// resourceModeRequiresDomain reports the modes whose create requires a
// domain_id and refuses proxy_port.
func resourceModeRequiresDomain(mode string) bool {
	return resourceModeTable[resourceModeOf(mode)].requiresDomain
}

// addResourceModeAttributes adds the mode-specific attributes to the
// pangolin_resource schema.
func addResourceModeAttributes(attrs map[string]schema.Attribute) {
	attrs["ai_provider_ids"] = schema.SetAttribute{
		Description: "IDs of the AI providers attached to an `inference` resource (Pangolin >= 1.22). " +
			"Providers are attached in inherit mode, using each provider's own allow/block lists. " +
			"When set, the attachments on the server are made to match it (providers attached out " +
			"of band are detached); set `[]` to detach every provider. Leave unset to never touch " +
			"the attachments: the value then only reports what the server holds. Only valid with " +
			"`mode = \"inference\"`. Reading it needs the API key to be allowed to list the " +
			"resource's AI models; without that permission the last known value is kept and a " +
			"warning is shown.",
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
	m := resourceModeOf(mode.ValueString())

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

	// `[]` is refused too: the attribute is always null outside
	// inference, so an empty set could never match the applied value.
	if m != "inference" && !providers.IsNull() && !providers.IsUnknown() {
		resp.Diagnostics.AddAttributeError(path.Root("ai_provider_ids"), "Unsupported ai_provider_ids",
			fmt.Sprintf("`ai_provider_ids` only applies to `mode = \"inference\"` resources, not to `mode = %q`. Remove it.", m))
	}
}

// buildHTTPResourceCreateRequest builds the PUT /org/{org}/resource
// body. Domain modes are sent with `http: true` plus the literal mode
// (omitted for `http`, so pre-1.19 servers keep their payload); L4
// modes keep the legacy `http: false` + protocol pair.
func buildHTTPResourceCreateRequest(ctx context.Context, plan HTTPResourceModel) (*client.CreateResourceRequest, diag.Diagnostics) {
	var diags diag.Diagnostics
	mode := resourceModeOf(plan.Mode.ValueString())
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

// aiProviderIDsConfigured reports whether ai_provider_ids is set in the
// configuration. Only then does the provider manage the attachments.
func aiProviderIDsConfigured(ctx context.Context, config tfsdk.Config) (bool, diag.Diagnostics) {
	var v types.Set
	diags := config.GetAttribute(ctx, path.Root("ai_provider_ids"), &v)
	return !v.IsNull(), diags
}

// applyAIProviders sets ai_provider_ids on m after a create or update.
// Outside inference it is always null. When the attribute is set in the
// configuration, the server attachments are made to match it (attach
// missing, detach extra) and the planned value is kept, so no second
// read is needed. When it is unset, the attachments are never touched:
// the planned value (the prior state, through UseStateForUnknown) is
// kept, or read from the server when it is still unknown (create).
//
// prior is the value the server is believed to hold: the state on
// update, the providers sent in the create body on create. It is the
// fallback when the API key cannot list the attachments (403), and the
// value kept when an attach or detach call fails.
func (r *HTTPResource) applyAIProviders(ctx context.Context, m *HTTPResourceModel, prior types.Set, configured bool) diag.Diagnostics {
	var diags diag.Diagnostics
	if m.Mode.ValueString() != "inference" {
		m.AIProviderIDs = types.SetNull(types.Int64Type)
		return diags
	}
	if !configured || m.AIProviderIDs.IsUnknown() {
		if m.AIProviderIDs.IsUnknown() {
			return r.refreshAIProviders(ctx, m)
		}
		return diags
	}

	want, d := aiProviderIDsFromSet(ctx, m.AIProviderIDs)
	diags.Append(d...)
	if diags.HasError() {
		return diags
	}
	resourceID := int(m.ID.ValueInt64())
	current, err := r.client.ListResourceAIProviders(ctx, resourceID)
	if err != nil {
		if !errors.Is(err, client.ErrForbidden) {
			diags.AddError("Failed to list resource AI providers", err.Error())
			m.AIProviderIDs = knownSetOrNull(prior)
			return diags
		}
		diags.AddWarning("Cannot list resource AI providers",
			"The API key is not allowed to list the resource's AI models, so ai_provider_ids is "+
				"reconciled against the last known value instead of the server list. Providers "+
				"attached outside Terraform are not detected. Error: "+err.Error())
		ids, d := aiProviderIDsFromSet(ctx, prior)
		diags.Append(d...)
		current = make([]client.ResourceAIProvider, len(ids))
		for i, id := range ids {
			current[i] = client.ResourceAIProvider{ProviderID: id}
		}
	}

	toAdd, toRemove := diffAIProviderIDs(want, current)
	for _, id := range toAdd {
		if err := r.client.AddResourceAIProvider(ctx, resourceID, id); err != nil {
			diags.AddError("Failed to attach AI provider", fmt.Sprintf("provider %d: %s", id, err))
			m.AIProviderIDs = knownSetOrNull(prior)
			return diags
		}
	}
	for _, id := range toRemove {
		if err := r.client.RemoveResourceAIProvider(ctx, resourceID, id); err != nil {
			diags.AddError("Failed to detach AI provider", fmt.Sprintf("provider %d: %s", id, err))
			m.AIProviderIDs = knownSetOrNull(prior)
			return diags
		}
	}
	return diags
}

// refreshAIProviders reads ai_provider_ids back from the server for
// inference resources and sets it to null for every other mode. When the
// API key cannot list the attachments (403), the current value is kept
// with a warning instead of failing the read.
func (r *HTTPResource) refreshAIProviders(ctx context.Context, m *HTTPResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics
	if m.Mode.ValueString() != "inference" {
		m.AIProviderIDs = types.SetNull(types.Int64Type)
		return diags
	}
	providers, err := r.client.ListResourceAIProviders(ctx, int(m.ID.ValueInt64()))
	if err != nil {
		if errors.Is(err, client.ErrForbidden) {
			diags.AddWarning("Cannot read resource AI providers",
				"The API key is not allowed to list the resource's AI models; the last known "+
					"ai_provider_ids is kept. Error: "+err.Error())
		} else {
			diags.AddError("Failed to list resource AI providers", err.Error())
		}
		// State must never hold an unknown value, even on error.
		m.AIProviderIDs = knownSetOrNull(m.AIProviderIDs)
		return diags
	}
	m.AIProviderIDs = aiProviderIDSet(providers)
	return diags
}

// knownSetOrNull returns s, or a typed null when s is unknown.
func knownSetOrNull(s types.Set) types.Set {
	if s.IsUnknown() {
		return types.SetNull(types.Int64Type)
	}
	return s
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
