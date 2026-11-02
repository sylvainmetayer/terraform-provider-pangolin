package resources

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stackopshq/terraform-provider-pangolin/internal/client"
)

var (
	_ resource.Resource                = &ResourcePolicyPasswordResource{}
	_ resource.ResourceWithImportState = &ResourcePolicyPasswordResource{}
)

// ResourcePolicyPasswordResource manages the password of a Pangolin resource policy.
type ResourcePolicyPasswordResource struct {
	client *client.Client
}

// ResourcePolicyPasswordModel describes the resource data model.
type ResourcePolicyPasswordModel struct {
	ResourcePolicyID types.Int64  `tfsdk:"resource_policy_id"`
	Password         types.String `tfsdk:"password"`
}

// NewResourcePolicyPasswordResource returns a new resource factory.
func NewResourcePolicyPasswordResource() resource.Resource {
	return &ResourcePolicyPasswordResource{}
}

func (r *ResourcePolicyPasswordResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_resource_policy_password"
}

func (r *ResourcePolicyPasswordResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Sets the password of a Pangolin resource policy (Enterprise). Every resource attached to the policy asks for it. " +
			"Destroying this resource removes the password.\n\n" +
			"> **Note:** The password value cannot be read back from the Pangolin API. After import, you must set the `password` attribute manually to avoid drift detection issues.",
		Attributes: map[string]schema.Attribute{
			"resource_policy_id": schema.Int64Attribute{
				Description: "The ID of the resource policy to protect with a password.",
				Required:    true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.RequiresReplace(),
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
		},
	}
}

func (r *ResourcePolicyPasswordResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *ResourcePolicyPasswordResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan ResourcePolicyPasswordModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	pw := plan.Password.ValueString()
	if err := r.client.SetResourcePolicyPassword(ctx, int(plan.ResourcePolicyID.ValueInt64()), &pw); err != nil {
		resp.Diagnostics.AddError("Failed to set resource policy password", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *ResourcePolicyPasswordResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state ResourcePolicyPasswordModel
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

	if policy.PasswordID == nil {
		// Password was removed externally - remove from state.
		resp.State.RemoveResource(ctx)
		return
	}

	// Credentials cannot be read back from the API; preserve existing state.
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *ResourcePolicyPasswordResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan ResourcePolicyPasswordModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	pw := plan.Password.ValueString()
	if err := r.client.SetResourcePolicyPassword(ctx, int(plan.ResourcePolicyID.ValueInt64()), &pw); err != nil {
		resp.Diagnostics.AddError("Failed to update resource policy password", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *ResourcePolicyPasswordResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state ResourcePolicyPasswordModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.SetResourcePolicyPassword(ctx, int(state.ResourcePolicyID.ValueInt64()), nil); err != nil {
		if errors.Is(err, client.ErrNotFound) {
			return
		}
		resp.Diagnostics.AddError("Failed to remove resource policy password", err.Error())
		return
	}
}

func (r *ResourcePolicyPasswordResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	policyID, err := strconv.ParseInt(req.ID, 10, 64)
	if err != nil {
		resp.Diagnostics.AddError("Invalid ID", fmt.Sprintf("Cannot parse resource policy ID %q as integer", req.ID))
		return
	}

	policy, err := r.client.GetResourcePolicy(ctx, int(policyID))
	if err != nil {
		resp.Diagnostics.AddError("Failed to import resource policy password", err.Error())
		return
	}

	if policy.PasswordID == nil {
		resp.Diagnostics.AddError("No password set", fmt.Sprintf("Resource policy %d does not have a password configured", policyID))
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &ResourcePolicyPasswordModel{
		ResourcePolicyID: types.Int64Value(policyID),
		Password:         types.StringValue(""), // not recoverable after import
	})...)
}
