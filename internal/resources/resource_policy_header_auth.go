package resources

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stackopshq/terraform-provider-pangolin/internal/client"
)

var (
	_ resource.Resource                = &ResourcePolicyHeaderAuthResource{}
	_ resource.ResourceWithImportState = &ResourcePolicyHeaderAuthResource{}
)

// ResourcePolicyHeaderAuthResource manages the header authentication of a Pangolin resource policy.
type ResourcePolicyHeaderAuthResource struct {
	client *client.Client
}

// ResourcePolicyHeaderAuthModel describes the resource data model.
type ResourcePolicyHeaderAuthModel struct {
	ResourcePolicyID      types.Int64  `tfsdk:"resource_policy_id"`
	User                  types.String `tfsdk:"user"`
	Password              types.String `tfsdk:"password"`
	ExtendedCompatibility types.Bool   `tfsdk:"extended_compatibility"`
}

// NewResourcePolicyHeaderAuthResource returns a new resource factory.
func NewResourcePolicyHeaderAuthResource() resource.Resource {
	return &ResourcePolicyHeaderAuthResource{}
}

func (r *ResourcePolicyHeaderAuthResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_resource_policy_header_auth"
}

func (r *ResourcePolicyHeaderAuthResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Sets header-based (HTTP basic) authentication on a Pangolin resource policy (Enterprise), for every resource attached to it. " +
			"Destroying this resource removes the header authentication.\n\n" +
			"> **Note:** Credentials cannot be read back from the Pangolin API. After import, you must set `user` and `password` manually to avoid drift detection issues.",
		Attributes: map[string]schema.Attribute{
			"resource_policy_id": schema.Int64Attribute{
				Description: "The ID of the resource policy to protect with header authentication.",
				Required:    true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.RequiresReplace(),
				},
			},
			"user": schema.StringAttribute{
				Description: "The username, 4 to 100 characters.",
				Required:    true,
				Validators: []validator.String{
					stringvalidator.LengthBetween(4, 100),
				},
			},
			"password": schema.StringAttribute{
				Description: "The password, 4 to 100 characters.",
				Required:    true,
				Sensitive:   true,
				Validators: []validator.String{
					stringvalidator.LengthBetween(4, 100),
				},
			},
			"extended_compatibility": schema.BoolAttribute{
				Description: "Whether to enable extended compatibility mode. Defaults to `false`.",
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
			},
		},
	}
}

func (r *ResourcePolicyHeaderAuthResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *ResourcePolicyHeaderAuthResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan ResourcePolicyHeaderAuthModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.SetResourcePolicyHeaderAuth(ctx, int(plan.ResourcePolicyID.ValueInt64()), resourcePolicyHeaderAuthCredentials(plan)); err != nil {
		resp.Diagnostics.AddError("Failed to set resource policy header auth", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *ResourcePolicyHeaderAuthResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state ResourcePolicyHeaderAuthModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	policy, err := r.client.GetResourcePolicy(ctx, int(state.ResourcePolicyID.ValueInt64()))
	if err != nil {
		if errors.Is(err, client.ErrNotFound) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read resource policy", err.Error())
		return
	}

	if !policy.HasHeaderAuth() {
		// Header auth was removed externally - remove from state.
		resp.State.RemoveResource(ctx)
		return
	}

	// Credentials cannot be read back from the API; the compatibility
	// flag can, so out-of-band changes to it show up as drift.
	if policy.HeaderAuth.ExtendedCompatibility != nil {
		state.ExtendedCompatibility = types.BoolValue(*policy.HeaderAuth.ExtendedCompatibility)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *ResourcePolicyHeaderAuthResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan ResourcePolicyHeaderAuthModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.SetResourcePolicyHeaderAuth(ctx, int(plan.ResourcePolicyID.ValueInt64()), resourcePolicyHeaderAuthCredentials(plan)); err != nil {
		resp.Diagnostics.AddError("Failed to update resource policy header auth", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *ResourcePolicyHeaderAuthResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state ResourcePolicyHeaderAuthModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.SetResourcePolicyHeaderAuth(ctx, int(state.ResourcePolicyID.ValueInt64()), nil); err != nil {
		if errors.Is(err, client.ErrNotFound) {
			return
		}
		resp.Diagnostics.AddError("Failed to remove resource policy header auth", err.Error())
		return
	}
}

func (r *ResourcePolicyHeaderAuthResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	policyID, err := strconv.ParseInt(req.ID, 10, 64)
	if err != nil {
		resp.Diagnostics.AddError("Invalid ID", fmt.Sprintf("Cannot parse resource policy ID %q as integer", req.ID))
		return
	}

	policy, err := r.client.GetResourcePolicy(ctx, int(policyID))
	if err != nil {
		resp.Diagnostics.AddError("Failed to import resource policy header auth", err.Error())
		return
	}

	if !policy.HasHeaderAuth() {
		resp.Diagnostics.AddError("No header auth set", fmt.Sprintf("Resource policy %d does not have header authentication configured", policyID))
		return
	}

	extended := false
	if policy.HeaderAuth.ExtendedCompatibility != nil {
		extended = *policy.HeaderAuth.ExtendedCompatibility
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &ResourcePolicyHeaderAuthModel{
		ResourcePolicyID:      types.Int64Value(policyID),
		User:                  types.StringValue(""), // not recoverable after import
		Password:              types.StringValue(""), // not recoverable after import
		ExtendedCompatibility: types.BoolValue(extended),
	})...)
}

func resourcePolicyHeaderAuthCredentials(plan ResourcePolicyHeaderAuthModel) *client.ResourcePolicyHeaderAuthCredentials {
	return &client.ResourcePolicyHeaderAuthCredentials{
		User:                  plan.User.ValueString(),
		Password:              plan.Password.ValueString(),
		ExtendedCompatibility: plan.ExtendedCompatibility.ValueBool(),
	}
}
