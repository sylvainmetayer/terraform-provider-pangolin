package resources

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stackopshq/terraform-provider-pangolin/internal/client"
	"github.com/stackopshq/terraform-provider-pangolin/internal/tfconv"
)

var (
	_ resource.Resource                   = &RedirectResource{}
	_ resource.ResourceWithImportState    = &RedirectResource{}
	_ resource.ResourceWithValidateConfig = &RedirectResource{}
)

const (
	redirectRewriteExact       = "exact"
	redirectRewriteStripPrefix = "stripPrefix"

	redirectParentChangeDescription = "Pangolin deletes a redirect along with its resource or domain, so moving to another one replaces the redirect: an in-place update would hit a 404 when that parent is itself being replaced."
)

// redirectDestinationHostRegexp catches at plan time what the server's
// `value === new URL(value).origin` refuses most often: a path or trailing
// slash, a query, a fragment, credentials, uppercase, a scheme without an
// origin. The server stays the judge of the rest, such as a default port.
var redirectDestinationHostRegexp = regexp.MustCompile(`^(https?|wss?|ftp)://[^A-Z/?#@\s]+$`)

// RedirectResource manages a Pangolin redirect.
type RedirectResource struct {
	client *client.Client
}

// RedirectResourceModel describes the resource data model.
type RedirectResourceModel struct {
	ID              types.Int64  `tfsdk:"id"`
	NiceID          types.String `tfsdk:"nice_id"`
	Name            types.String `tfsdk:"name"`
	ResourceID      types.Int64  `tfsdk:"resource_id"`
	DomainID        types.String `tfsdk:"domain_id"`
	Subdomain       types.String `tfsdk:"subdomain"`
	DestinationHost types.String `tfsdk:"destination_host"`
	PathMatchType   types.String `tfsdk:"path_match_type"`
	MatchPath       types.String `tfsdk:"match_path"`
	RewritePath     types.String `tfsdk:"rewrite_path"`
	RewritePathType types.String `tfsdk:"rewrite_path_type"`
	Priority        types.Int64  `tfsdk:"priority"`
	Permanent       types.Bool   `tfsdk:"permanent"`
	SSL             types.Bool   `tfsdk:"ssl"`
	Enabled         types.Bool   `tfsdk:"enabled"`
}

// NewRedirectResource returns a new resource factory.
func NewRedirectResource() resource.Resource {
	return &RedirectResource{}
}

func (r *RedirectResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_redirect"
}

func (r *RedirectResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a Pangolin redirect, attached either to a public resource or to a domain.",
		Attributes: map[string]schema.Attribute{
			"id": schema.Int64Attribute{
				Description:   "The numeric redirect ID.",
				Computed:      true,
				PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
			},
			"nice_id": schema.StringAttribute{
				Description:   "The readable ID Pangolin generated for the redirect.",
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Description: "The redirect name.",
				Required:    true,
				Validators:  []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"resource_id": schema.Int64Attribute{
				Description: "ID of the public resource the redirect is attached to. Exactly one of `resource_id` and `domain_id` must be set. Moving to another resource forces a replacement.",
				Optional:    true,
				Validators:  []validator.Int64{int64validator.AtLeast(1)},
				PlanModifiers: []planmodifier.Int64{int64planmodifier.RequiresReplaceIf(
					func(_ context.Context, req planmodifier.Int64Request, resp *int64planmodifier.RequiresReplaceIfFuncResponse) {
						resp.RequiresReplace = !req.StateValue.IsNull() && !req.PlanValue.IsNull()
					}, redirectParentChangeDescription, redirectParentChangeDescription,
				)},
			},
			"domain_id": schema.StringAttribute{
				Description: "ID of the domain the redirect is attached to. Exactly one of `resource_id` and `domain_id` must be set. Moving to another domain forces a replacement.",
				Optional:    true,
				Validators:  []validator.String{stringvalidator.LengthAtLeast(1)},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplaceIf(
					func(_ context.Context, req planmodifier.StringRequest, resp *stringplanmodifier.RequiresReplaceIfFuncResponse) {
						resp.RequiresReplace = !req.StateValue.IsNull() && !req.PlanValue.IsNull()
					}, redirectParentChangeDescription, redirectParentChangeDescription,
				)},
			},
			"subdomain": schema.StringAttribute{
				Description: "Subdomain of `domain_id` the redirect answers on. Unset means the base domain. Only valid with `domain_id`.",
				Optional:    true,
				Validators:  []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"destination_host": schema.StringAttribute{
				Description: "Destination as `scheme://host[:port]`, with no path, query or fragment. The request path, after any rewrite, is appended to it.",
				Required:    true,
				Validators: []validator.String{
					stringvalidator.RegexMatches(redirectDestinationHostRegexp, "must be a lowercase scheme://host[:port] with no path, query or fragment, such as https://example.com"),
				},
			},
			"path_match_type": schema.StringAttribute{
				Description: "How `match_path` is compared: `exact`, `prefix` or `regex`. Defaults to `regex`.",
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("regex"),
				Validators:  []validator.String{stringvalidator.OneOf("exact", "prefix", "regex")},
			},
			"match_path": schema.StringAttribute{
				Description: "Path the redirect applies to. Unset means every path.",
				Optional:    true,
				Validators:  []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"rewrite_path": schema.StringAttribute{
				Description: "Replacement path. Needs `rewrite_path_type`, and is required with it unless it is `stripPrefix`.",
				Optional:    true,
				Validators:  []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"rewrite_path_type": schema.StringAttribute{
				Description: "How the path is rewritten: `exact`, `prefix`, `regex` or `stripPrefix`. Unset keeps the original path. Every type but `exact` needs `match_path`.",
				Optional:    true,
				Validators:  []validator.String{stringvalidator.OneOf(redirectRewriteExact, "prefix", "regex", redirectRewriteStripPrefix)},
			},
			"priority": schema.Int64Attribute{
				Description: "Priority between 1 and 1000. Defaults to `100`, which lets Pangolin order the redirect.",
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(100),
				Validators:  []validator.Int64{int64validator.Between(1, 1000)},
			},
			"permanent": schema.BoolAttribute{
				Description: "Send a permanent redirect instead of a temporary one. Defaults to `false`.",
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
			},
			"ssl": schema.BoolAttribute{
				Description: "Serve the redirect over TLS. Defaults to `true`. Only `domain_id` redirects can turn it off: a `resource_id` redirect follows its resource.",
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(true),
			},
			"enabled": schema.BoolAttribute{
				Description: "Whether the redirect is enabled. Defaults to `true`.",
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(true),
			},
		},
	}
}

func (r *RedirectResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Resource Configure Type", "Expected *client.Client")
		return
	}
	r.client = c
}

// ValidateConfig mirrors the server's checks, those whose failure the server
// would only show as a permanent diff (ssl and subdomain on a resource
// redirect), and the rewrites it would store but never apply. Unknown values
// are left to the server.
func (r *RedirectResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var cfg RedirectResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(validateRedirect(cfg)...)
}

func validateRedirect(cfg RedirectResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics
	if !cfg.ResourceID.IsUnknown() && !cfg.DomainID.IsUnknown() && cfg.ResourceID.IsNull() == cfg.DomainID.IsNull() {
		diags.AddAttributeError(path.Root("resource_id"), "Invalid redirect target",
			"Exactly one of resource_id and domain_id must be set.")
	}
	if !cfg.ResourceID.IsNull() && !cfg.ResourceID.IsUnknown() {
		if !cfg.Subdomain.IsNull() && !cfg.Subdomain.IsUnknown() {
			diags.AddAttributeError(path.Root("subdomain"), "Unsupported subdomain",
				"subdomain is only valid with domain_id: a resource_id redirect answers on its resource's domain.")
		}
		if !cfg.SSL.IsNull() && !cfg.SSL.IsUnknown() && !cfg.SSL.ValueBool() {
			diags.AddAttributeError(path.Root("ssl"), "Unsupported ssl",
				"ssl cannot be false with resource_id: the redirect follows the ssl setting of its resource.")
		}
	}
	switch {
	case cfg.RewritePathType.IsUnknown():
	case cfg.RewritePathType.IsNull():
		// The server keeps the request path when the type is null, whatever
		// rewrite_path says.
		if !cfg.RewritePath.IsNull() && !cfg.RewritePath.IsUnknown() {
			diags.AddAttributeError(path.Root("rewrite_path_type"), "Missing rewrite_path_type",
				"rewrite_path has no effect without rewrite_path_type.")
		}
	default:
		rewriteType := cfg.RewritePathType.ValueString()
		if rewriteType != redirectRewriteStripPrefix && cfg.RewritePath.IsNull() {
			diags.AddAttributeError(path.Root("rewrite_path"), "Missing rewrite_path",
				fmt.Sprintf("rewrite_path is required unless rewrite_path_type is %q.", redirectRewriteStripPrefix))
		}
		// Without a matched path the server only honors an exact rewrite.
		if rewriteType != redirectRewriteExact && cfg.MatchPath.IsNull() {
			diags.AddAttributeError(path.Root("match_path"), "Missing match_path",
				fmt.Sprintf("rewrite_path_type %q has no effect without match_path.", rewriteType))
		}
	}
	return diags
}

func redirectRequest(m RedirectResourceModel) *client.RedirectRequest {
	return &client.RedirectRequest{
		Name:            m.Name.ValueString(),
		ResourceID:      int64PtrToIntPtr(m.ResourceID),
		DomainID:        m.DomainID.ValueStringPointer(),
		Subdomain:       m.Subdomain.ValueStringPointer(),
		DestinationHost: m.DestinationHost.ValueString(),
		PathMatchType:   m.PathMatchType.ValueString(),
		MatchPath:       m.MatchPath.ValueStringPointer(),
		RewritePath:     m.RewritePath.ValueStringPointer(),
		RewritePathType: m.RewritePathType.ValueStringPointer(),
		Priority:        int(m.Priority.ValueInt64()),
		Permanent:       m.Permanent.ValueBool(),
		SSL:             m.SSL.ValueBool(),
		Enabled:         m.Enabled.ValueBool(),
	}
}

func redirectToModel(in *client.Redirect) RedirectResourceModel {
	m := RedirectResourceModel{
		ID:              types.Int64Value(int64(in.RedirectID)),
		NiceID:          types.StringValue(in.NiceID),
		Name:            types.StringValue(in.Name),
		ResourceID:      tfconv.Int64FromIntPtr(in.ResourceID),
		DomainID:        tfconv.StringFromPtr(in.DomainID),
		Subdomain:       tfconv.StringFromPtr(in.Subdomain),
		DestinationHost: types.StringValue(in.DestinationHost),
		PathMatchType:   types.StringValue(in.PathMatchType),
		MatchPath:       tfconv.StringFromPtr(in.MatchPath),
		RewritePath:     tfconv.StringFromPtr(in.RewritePath),
		RewritePathType: tfconv.StringFromPtr(in.RewritePathType),
		Priority:        types.Int64Value(100),
		Permanent:       types.BoolValue(in.Permanent),
		SSL:             types.BoolValue(in.SSL),
		Enabled:         types.BoolValue(in.Enabled),
	}
	if in.Priority != nil {
		m.Priority = types.Int64Value(int64(*in.Priority))
	}
	return m
}

func (r *RedirectResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan RedirectResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	created, err := r.client.CreateRedirect(ctx, redirectRequest(plan))
	if err != nil {
		resp.Diagnostics.AddError("Failed to create redirect", err.Error())
		return
	}
	state := redirectToModel(created)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *RedirectResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state RedirectResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	got, err := r.client.GetRedirect(ctx, int(state.ID.ValueInt64()))
	if err != nil {
		if errors.Is(err, client.ErrNotFound) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read redirect", err.Error())
		return
	}
	state = redirectToModel(got)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *RedirectResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan RedirectResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	updated, err := r.client.UpdateRedirect(ctx, int(plan.ID.ValueInt64()), redirectRequest(plan))
	if err != nil {
		resp.Diagnostics.AddError("Failed to update redirect", err.Error())
		return
	}
	state := redirectToModel(updated)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *RedirectResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state RedirectResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DeleteRedirect(ctx, int(state.ID.ValueInt64())); err != nil && !errors.Is(err, client.ErrNotFound) {
		resp.Diagnostics.AddError("Failed to delete redirect", err.Error())
	}
}

func (r *RedirectResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	id, err := strconv.ParseInt(req.ID, 10, 64)
	if err != nil {
		resp.Diagnostics.AddError("Invalid import ID", fmt.Sprintf("Expected a numeric redirect ID, got: %q", req.ID))
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
}
