package resources

import (
	"context"
	"errors"
	"fmt"
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
)

var (
	_ resource.Resource                   = &RedirectResource{}
	_ resource.ResourceWithImportState    = &RedirectResource{}
	_ resource.ResourceWithValidateConfig = &RedirectResource{}
)

const redirectRewriteStripPrefix = "stripPrefix"

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
				Description: "ID of the public resource the redirect is attached to. Exactly one of `resource_id` and `domain_id` must be set.",
				Optional:    true,
				Validators:  []validator.Int64{int64validator.AtLeast(1)},
			},
			"domain_id": schema.StringAttribute{
				Description: "ID of the domain the redirect is attached to. Exactly one of `resource_id` and `domain_id` must be set.",
				Optional:    true,
				Validators:  []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"subdomain": schema.StringAttribute{
				Description: "Subdomain of `domain_id` the redirect answers on. Unset means the base domain. Only valid with `domain_id`.",
				Optional:    true,
				Validators:  []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"destination_host": schema.StringAttribute{
				Description: "Destination as `scheme://host[:port]`, with no path, query or fragment. The request path, after any rewrite, is appended to it.",
				Required:    true,
				Validators:  []validator.String{stringvalidator.LengthAtLeast(1)},
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
				Description: "Replacement path. Required when `rewrite_path_type` is set, unless it is `stripPrefix`.",
				Optional:    true,
				Validators:  []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"rewrite_path_type": schema.StringAttribute{
				Description: "How the path is rewritten: `exact`, `prefix`, `regex` or `stripPrefix`. Unset keeps the original path.",
				Optional:    true,
				Validators:  []validator.String{stringvalidator.OneOf("exact", "prefix", "regex", redirectRewriteStripPrefix)},
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

// ValidateConfig mirrors the server's checks, and those whose failure the
// server would only show as a permanent diff (ssl and subdomain on a
// resource redirect). Unknown values are left to the server.
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
		if !cfg.Subdomain.IsNull() {
			diags.AddAttributeError(path.Root("subdomain"), "Unsupported subdomain",
				"subdomain is only valid with domain_id: a resource_id redirect answers on its resource's domain.")
		}
		if !cfg.SSL.IsNull() && !cfg.SSL.IsUnknown() && !cfg.SSL.ValueBool() {
			diags.AddAttributeError(path.Root("ssl"), "Unsupported ssl",
				"ssl cannot be false with resource_id: the redirect follows the ssl setting of its resource.")
		}
	}
	if !cfg.RewritePathType.IsNull() && !cfg.RewritePathType.IsUnknown() &&
		cfg.RewritePathType.ValueString() != redirectRewriteStripPrefix && cfg.RewritePath.IsNull() {
		diags.AddAttributeError(path.Root("rewrite_path"), "Missing rewrite_path",
			fmt.Sprintf("rewrite_path is required unless rewrite_path_type is %q.", redirectRewriteStripPrefix))
	}
	return diags
}

func redirectRequest(m RedirectResourceModel) *client.RedirectRequest {
	req := &client.RedirectRequest{
		Name:            m.Name.ValueString(),
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
	if !m.ResourceID.IsNull() {
		id := int(m.ResourceID.ValueInt64())
		req.ResourceID = &id
	}
	return req
}

func redirectToModel(in *client.Redirect) RedirectResourceModel {
	m := RedirectResourceModel{
		ID:              types.Int64Value(int64(in.RedirectID)),
		NiceID:          types.StringValue(in.NiceID),
		Name:            types.StringValue(in.Name),
		ResourceID:      types.Int64Null(),
		DomainID:        types.StringPointerValue(in.DomainID),
		Subdomain:       types.StringPointerValue(in.Subdomain),
		DestinationHost: types.StringValue(in.DestinationHost),
		PathMatchType:   types.StringValue(in.PathMatchType),
		MatchPath:       types.StringPointerValue(in.MatchPath),
		RewritePath:     types.StringPointerValue(in.RewritePath),
		RewritePathType: types.StringPointerValue(in.RewritePathType),
		Priority:        types.Int64Value(100),
		Permanent:       types.BoolValue(in.Permanent),
		SSL:             types.BoolValue(in.SSL),
		Enabled:         types.BoolValue(in.Enabled),
	}
	if in.ResourceID != nil {
		m.ResourceID = types.Int64Value(int64(*in.ResourceID))
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
