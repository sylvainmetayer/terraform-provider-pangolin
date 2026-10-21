package resources

import (
	"context"
	"errors"
	"fmt"
	"regexp"
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
	_ resource.Resource                = &ResourcePolicyPincodeResource{}
	_ resource.ResourceWithImportState = &ResourcePolicyPincodeResource{}
)

// sixDigitRegexp matches the PIN code format the server accepts.
var sixDigitRegexp = regexp.MustCompile(`^\d{6}$`)

// ResourcePolicyPincodeResource manages the PIN code of a Pangolin resource policy.
type ResourcePolicyPincodeResource struct {
	client *client.Client
}

// ResourcePolicyPincodeModel describes the resource data model.
type ResourcePolicyPincodeModel struct {
	ResourcePolicyID types.Int64  `tfsdk:"resource_policy_id"`
	Pincode          types.String `tfsdk:"pincode"`
}

// NewResourcePolicyPincodeResource returns a new resource factory.
func NewResourcePolicyPincodeResource() resource.Resource {
	return &ResourcePolicyPincodeResource{}
}

func (r *ResourcePolicyPincodeResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_resource_policy_pincode"
}

func (r *ResourcePolicyPincodeResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Sets the 6-digit PIN code of a Pangolin resource policy (Enterprise). Every resource attached to the policy asks for it. " +
			"Destroying this resource removes the PIN code.\n\n" +
			"> **Note:** The PIN code cannot be read back from the Pangolin API. After import, you must set the `pincode` attribute manually to avoid drift detection issues.",
		Attributes: map[string]schema.Attribute{
			"resource_policy_id": schema.Int64Attribute{
				Description: "The ID of the resource policy to protect with a PIN code.",
				Required:    true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.RequiresReplace(),
				},
			},
			"pincode": schema.StringAttribute{
				Description: "The PIN code, exactly 6 digits.",
				Required:    true,
				Sensitive:   true,
				Validators: []validator.String{
					stringvalidator.RegexMatches(sixDigitRegexp, "must be exactly 6 digits"),
				},
			},
		},
	}
}

func (r *ResourcePolicyPincodeResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *ResourcePolicyPincodeResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan ResourcePolicyPincodeModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	pin := plan.Pincode.ValueString()
	if err := r.client.SetResourcePolicyPincode(ctx, int(plan.ResourcePolicyID.ValueInt64()), &pin); err != nil {
		resp.Diagnostics.AddError("Failed to set resource policy PIN code", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *ResourcePolicyPincodeResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state ResourcePolicyPincodeModel
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

	if policy.PincodeID == nil {
		// PIN code was removed externally - remove from state.
		resp.State.RemoveResource(ctx)
		return
	}

	// Credentials cannot be read back from the API; preserve existing state.
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *ResourcePolicyPincodeResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan ResourcePolicyPincodeModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	pin := plan.Pincode.ValueString()
	if err := r.client.SetResourcePolicyPincode(ctx, int(plan.ResourcePolicyID.ValueInt64()), &pin); err != nil {
		resp.Diagnostics.AddError("Failed to update resource policy PIN code", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *ResourcePolicyPincodeResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state ResourcePolicyPincodeModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.SetResourcePolicyPincode(ctx, int(state.ResourcePolicyID.ValueInt64()), nil); err != nil {
		if errors.Is(err, client.ErrNotFound) {
			return
		}
		resp.Diagnostics.AddError("Failed to remove resource policy PIN code", err.Error())
		return
	}
}

func (r *ResourcePolicyPincodeResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	policyID, err := strconv.ParseInt(req.ID, 10, 64)
	if err != nil {
		resp.Diagnostics.AddError("Invalid ID", fmt.Sprintf("Cannot parse resource policy ID %q as integer", req.ID))
		return
	}

	policy, err := r.client.GetResourcePolicy(ctx, int(policyID))
	if err != nil {
		resp.Diagnostics.AddError("Failed to import resource policy PIN code", err.Error())
		return
	}

	if policy.PincodeID == nil {
		resp.Diagnostics.AddError("No PIN code set", fmt.Sprintf("Resource policy %d does not have a PIN code configured", policyID))
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &ResourcePolicyPincodeModel{
		ResourcePolicyID: types.Int64Value(policyID),
		Pincode:          types.StringValue(""), // not recoverable after import
	})...)
}
