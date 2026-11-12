package resources

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework-validators/resourcevalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stackopshq/terraform-provider-pangolin/internal/client"
)

var (
	_ resource.Resource                     = &ResourcePasswordResource{}
	_ resource.ResourceWithImportState      = &ResourcePasswordResource{}
	_ resource.ResourceWithConfigValidators = &ResourcePasswordResource{}
)

// ResourcePasswordResource manages password authentication for a Pangolin HTTP resource.
type ResourcePasswordResource struct {
	client *client.Client
}

// ResourcePasswordModel describes the resource data model.
type ResourcePasswordModel struct {
	ResourceID        types.Int64  `tfsdk:"resource_id"`
	Password          types.String `tfsdk:"password"`
	PasswordWO        types.String `tfsdk:"password_wo"`
	PasswordWOVersion types.Int64  `tfsdk:"password_wo_version"`
}

// NewResourcePasswordResource returns a new resource factory.
func NewResourcePasswordResource() resource.Resource {
	return &ResourcePasswordResource{}
}

func (r *ResourcePasswordResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_resource_password"
}

func (r *ResourcePasswordResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Sets a password for accessing a Pangolin HTTP resource. Destroying this resource removes the password protection.\n\n" +
			"> **Note:** The password value cannot be read back from the Pangolin API. After import, you must set the `password` attribute manually to avoid drift detection issues. " +
			"In write-only mode, set `password_wo` and `password_wo_version`: the import leaves `password_wo_version` null, so the next apply sends `password_wo` once.\n\n" +
			"Set either `password` (kept in the state) or `password_wo` with `password_wo_version` (never written to the plan or the state; " +
			"requires Terraform >= 1.11 or OpenTofu >= 1.11; an older client fails validation with a message that only names Terraform 1.11). " +
			"Change `password_wo_version` to send a new `password_wo` to Pangolin: changing `password_wo` alone sends nothing.",
		Attributes: map[string]schema.Attribute{
			"resource_id": schema.Int64Attribute{
				Description: "The ID of the resource to protect with a password.",
				Required:    true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.RequiresReplace(),
				},
			},
			"password": schema.StringAttribute{
				Description: "The password required to access the resource, stored in the state. Exactly one of `password` and `password_wo` is required.",
				Optional:    true,
				Sensitive:   true,
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
			"password_wo": schema.StringAttribute{
				Description: "Write-only password: sent to Pangolin on create and when `password_wo_version` changes, never stored in the plan or the state. " +
					"Changing `password_wo` without changing `password_wo_version` sends nothing to Pangolin. Requires Terraform >= 1.11 or OpenTofu >= 1.11.",
				Optional:  true,
				Sensitive: true,
				WriteOnly: true,
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
			"password_wo_version": schema.Int64Attribute{
				Description: "Version of `password_wo`, required with it. Change it (any new value) to send the current `password_wo` to Pangolin: " +
					"a write-only value cannot be compared with the state, so a new `password_wo` with the same version sends nothing.",
				Optional: true,
			},
		},
	}
}

// ConfigValidators names both attributes in the messages, which the
// attribute-level validators did not.
func (r *ResourcePasswordResource) ConfigValidators(_ context.Context) []resource.ConfigValidator {
	return []resource.ConfigValidator{
		resourcevalidator.ExactlyOneOf(path.MatchRoot("password"), path.MatchRoot("password_wo")),
		resourcevalidator.RequiredTogether(path.MatchRoot("password_wo"), path.MatchRoot("password_wo_version")),
	}
}

func (r *ResourcePasswordResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *ResourcePasswordResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan ResourcePasswordModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	pw, diags := secretValue(ctx, req.Config, plan.Password, path.Root("password_wo"))
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.SetResourcePassword(ctx, int(plan.ResourceID.ValueInt64()), &pw); err != nil {
		resp.Diagnostics.AddError("Failed to set resource password", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *ResourcePasswordResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state ResourcePasswordModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	authState, err := r.client.GetResourceAuthState(ctx, int(state.ResourceID.ValueInt64()))
	if err != nil {
		if errors.Is(err, client.ErrNotFound) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read resource auth state", err.Error())
		return
	}

	if authState.PasswordID == nil {
		// Password was removed externally - remove from state.
		resp.State.RemoveResource(ctx)
		return
	}

	// Credentials cannot be read back from the API; preserve existing state.
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *ResourcePasswordResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan ResourcePasswordModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	pw, diags := secretValue(ctx, req.Config, plan.Password, path.Root("password_wo"))
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.SetResourcePassword(ctx, int(plan.ResourceID.ValueInt64()), &pw); err != nil {
		resp.Diagnostics.AddError("Failed to update resource password", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *ResourcePasswordResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state ResourcePasswordModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.SetResourcePassword(ctx, int(state.ResourceID.ValueInt64()), nil); err != nil && !errors.Is(err, client.ErrNotFound) {
		resp.Diagnostics.AddError("Failed to remove resource password", err.Error())
		return
	}
}

func (r *ResourcePasswordResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resourceID, err := strconv.ParseInt(req.ID, 10, 64)
	if err != nil {
		resp.Diagnostics.AddError("Invalid ID", fmt.Sprintf("Cannot parse resource ID %q as integer", req.ID))
		return
	}

	authState, err := r.client.GetResourceAuthState(ctx, int(resourceID))
	if err != nil {
		resp.Diagnostics.AddError("Failed to import resource password", err.Error())
		return
	}

	if authState.PasswordID == nil {
		resp.Diagnostics.AddError("No password set", fmt.Sprintf("Resource %d does not have a password configured", resourceID))
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &ResourcePasswordModel{
		ResourceID: types.Int64Value(resourceID),
		Password:   types.StringValue(""), // not recoverable after import
	})...)
}
