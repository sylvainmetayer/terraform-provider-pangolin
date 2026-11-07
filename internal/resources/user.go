package resources

import (
	"context"
	"errors"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stackopshq/terraform-provider-pangolin/internal/client"
)

var (
	_ resource.Resource                = &UserResource{}
	_ resource.ResourceWithImportState = &UserResource{}
)

// UserResource manages a Pangolin organization user.
type UserResource struct {
	client *client.Client
}

// UserResourceModel describes the resource data model.
type UserResourceModel struct {
	ID              types.String `tfsdk:"id"`
	Username        types.String `tfsdk:"username"`
	RoleID          types.Int64  `tfsdk:"role_id"`
	Email           types.String `tfsdk:"email"`
	Name            types.String `tfsdk:"name"`
	Type            types.String `tfsdk:"type"`
	IdpID           types.Int64  `tfsdk:"idp_id"`
	AutoProvisioned types.Bool   `tfsdk:"auto_provisioned"`
}

// NewUserResource returns a new resource factory.
func NewUserResource() resource.Resource {
	return &UserResource{}
}

func (r *UserResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_user"
}

func (r *UserResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages an OIDC user of a Pangolin organization. The identity provider (`idp_id`) must be linked to the org, for example with `pangolin_idp_org`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "The user ID.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"username": schema.StringAttribute{
				Description: "The username.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
			"role_id": schema.Int64Attribute{
				Description: "The role ID given to the user at creation. Changing it replaces the user.",
				Required:    true,
				PlanModifiers: []planmodifier.Int64{
					// An import of a user with several roles leaves it null: take
					// the configured one without replacing the user.
					int64planmodifier.RequiresReplaceIf(func(_ context.Context, req planmodifier.Int64Request, resp *int64planmodifier.RequiresReplaceIfFuncResponse) {
						resp.RequiresReplace = !req.StateValue.IsNull()
					}, "Replaces the user when the role changes.", "Replaces the user when the role changes."),
				},
			},
			"email": schema.StringAttribute{
				Description: "The user email address. Set when the user is created: Pangolin has no API to change it.",
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
					createOnlyString{},
				},
			},
			"name": schema.StringAttribute{
				Description: "The display name of the user. Set when the user is created: Pangolin has no API to change it.",
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
					createOnlyString{},
				},
			},
			"type": schema.StringAttribute{
				Description: "The user type: 'oidc' or 'internal'. Defaults to 'oidc'.",
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
					stringplanmodifier.RequiresReplace(),
				},
			},
			"idp_id": schema.Int64Attribute{
				Description: "ID of the identity provider the user signs in with. `oidc` is the only user type the server accepts, and it needs an IdP.",
				Required:    true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.RequiresReplace(),
				},
			},
			"auto_provisioned": schema.BoolAttribute{
				Description: "Whether the user was auto-provisioned.",
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *UserResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *UserResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan UserResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	userType := plan.Type.ValueString()
	if userType == "" {
		userType = "oidc"
	}

	err := r.client.CreateUser(ctx, &client.CreateUserRequest{
		Username: plan.Username.ValueString(),
		RoleID:   int(plan.RoleID.ValueInt64()),
		Email:    plan.Email.ValueString(),
		Name:     plan.Name.ValueString(),
		Type:     userType,
		IdpID:    int(plan.IdpID.ValueInt64()),
	})
	if err != nil {
		resp.Diagnostics.AddError("Failed to create user", err.Error())
		return
	}

	// The create answers with an empty object: find the new user's ID.
	user, err := r.client.GetUserByUsername(ctx, r.client.OrgID, plan.Username.ValueString(), int(plan.IdpID.ValueInt64()))
	if err != nil {
		resp.Diagnostics.AddError("Failed to read the created user", err.Error())
		return
	}
	// The create call cannot set auto_provisioned: apply it afterwards so
	// a configured value does not clash with the server's default.
	if want := plan.AutoProvisioned; !want.IsUnknown() && !want.IsNull() && want.ValueBool() != user.AutoProvisioned {
		if _, err := r.client.UpdateUser(ctx, user.UserID, &client.UpdateUserRequest{AutoProvisioned: want.ValueBool()}); err != nil {
			resp.Diagnostics.AddError("Failed to set auto_provisioned", err.Error())
			// The user exists: keep it in state so it is not orphaned.
			userToModel(user, &plan)
			resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
			return
		}
		user.AutoProvisioned = want.ValueBool()
	}
	userToModel(user, &plan)

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *UserResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state UserResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	user, err := r.client.GetUser(ctx, state.ID.ValueString())
	if err != nil {
		if errors.Is(err, client.ErrNotFound) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read user", err.Error())
		return
	}

	userToModel(user, &state)

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *UserResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan UserResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	_, err := r.client.UpdateUser(ctx, plan.ID.ValueString(), &client.UpdateUserRequest{
		AutoProvisioned: plan.AutoProvisioned.ValueBool(),
	})
	if err != nil {
		resp.Diagnostics.AddError("Failed to update user", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *UserResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state UserResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteUser(ctx, state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to delete user", err.Error())
		return
	}
}

func (r *UserResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	user, err := r.client.GetUser(ctx, req.ID)
	if err != nil {
		resp.Diagnostics.AddError("Failed to import user", err.Error())
		return
	}

	var state UserResourceModel
	userToModel(user, &state)
	// With several roles, the one given at creation is unknown: leave it
	// to the config.
	if len(user.Roles) == 1 {
		state.RoleID = types.Int64Value(int64(user.Roles[0].RoleID))
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// userToModel copies the server's view of a user onto the model. The
// role is left alone: role_id is the role given at creation, and other
// roles come from pangolin_user_role.
func userToModel(u *client.UserByUsernameResponse, m *UserResourceModel) {
	m.ID = types.StringValue(u.UserID)
	m.Username = types.StringValue(u.Username)
	// Name and email are create-time inputs: keep a configured value (the
	// server lowercases the email, and keeps the old name of a user that
	// already existed), fill them only when unset or imported.
	if m.Email.IsNull() || m.Email.IsUnknown() {
		m.Email = types.StringPointerValue(u.Email)
	}
	if m.Name.IsNull() || m.Name.IsUnknown() {
		m.Name = types.StringPointerValue(u.Name)
	}
	m.Type = types.StringValue(u.Type)
	m.AutoProvisioned = types.BoolValue(u.AutoProvisioned)
	if u.IdpID != nil {
		m.IdpID = types.Int64Value(int64(*u.IdpID))
	} else {
		m.IdpID = types.Int64Null()
	}
}

// createOnlyString refuses a change to a value the server cannot update.
// Replacing the user would not help: removing a user only drops it from
// the org, and adding it back keeps its old name and email.
type createOnlyString struct{}

func (createOnlyString) Description(context.Context) string {
	return "Refuses a change after creation."
}

func (m createOnlyString) MarkdownDescription(ctx context.Context) string { return m.Description(ctx) }

func (createOnlyString) PlanModifyString(_ context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	if req.StateValue.IsNull() || req.PlanValue.IsUnknown() || req.PlanValue.IsNull() ||
		strings.EqualFold(req.StateValue.ValueString(), req.PlanValue.ValueString()) {
		return
	}
	resp.Diagnostics.AddAttributeError(req.Path, "Cannot change a user's "+req.Path.String(),
		"Pangolin has no API to change it once the user exists. Put back "+req.StateValue.String()+", or change it in the identity provider.")
}
