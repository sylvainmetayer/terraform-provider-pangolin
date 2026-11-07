package resources

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stackopshq/terraform-provider-pangolin/internal/client"
)

var (
	_ resource.Resource                = &ClientRoleResource{}
	_ resource.ResourceWithImportState = &ClientRoleResource{}
)

// ClientRoleResource binds a role to a machine client.
type ClientRoleResource struct {
	client *client.Client
}

// ClientRoleResourceModel describes the resource data model.
type ClientRoleResourceModel struct {
	ClientID types.Int64 `tfsdk:"client_id"`
	RoleID   types.Int64 `tfsdk:"role_id"`
}

// NewClientRoleResource returns a new resource factory.
func NewClientRoleResource() resource.Resource {
	return &ClientRoleResource{}
}

func (r *ClientRoleResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_client_role"
}

func (r *ClientRoleResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Gives a role to a machine client (`pangolin_client`, Pangolin 1.24+, Enterprise): the client gets " +
			"the private resources (`pangolin_site_resource`) assigned to the role, instead of binding it to each one with " +
			"`pangolin_site_resource_client`. Cumulative: other roles of the client are kept. Clients owned by a user " +
			"cannot hold roles, they inherit their user's.\n\n" +
			"> **Note:** Both attributes are `RequiresReplace`: the binding is identified by its `(client_id, role_id)` pair.",
		Attributes: map[string]schema.Attribute{
			"client_id": schema.Int64Attribute{
				Description:   "The numeric ID of the machine client (`pangolin_client.id`).",
				Required:      true,
				PlanModifiers: []planmodifier.Int64{int64planmodifier.RequiresReplace()},
				Validators:    []validator.Int64{int64validator.AtLeast(1)},
			},
			"role_id": schema.Int64Attribute{
				Description:   "The numeric ID of the role.",
				Required:      true,
				PlanModifiers: []planmodifier.Int64{int64planmodifier.RequiresReplace()},
				Validators:    []validator.Int64{int64validator.AtLeast(1)},
			},
		},
	}
}

func (r *ClientRoleResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *ClientRoleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan ClientRoleResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.AddRoleToClient(ctx, int(plan.ClientID.ValueInt64()), int(plan.RoleID.ValueInt64())); err != nil {
		resp.Diagnostics.AddError("Failed to give the role to the client", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *ClientRoleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state ClientRoleResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	has, err := r.client.ClientHasRole(ctx, int(state.ClientID.ValueInt64()), int(state.RoleID.ValueInt64()))
	if err != nil {
		if errors.Is(err, client.ErrNotFound) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read client/role binding", err.Error())
		return
	}
	if !has {
		resp.State.RemoveResource(ctx)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *ClientRoleResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError("client_role update not supported",
		"Both attributes are RequiresReplace, so Update should never fire. File a bug if you see this error.")
}

func (r *ClientRoleResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state ClientRoleResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.RemoveRoleFromClient(ctx, int(state.ClientID.ValueInt64()), int(state.RoleID.ValueInt64()))
	if err != nil && !errors.Is(err, client.ErrNotFound) {
		resp.Diagnostics.AddError("Failed to remove the role from the client", err.Error())
	}
}

func (r *ClientRoleResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.Split(req.ID, "/")
	if len(parts) == 2 {
		clientID, err1 := strconv.ParseInt(parts[0], 10, 64)
		roleID, err2 := strconv.ParseInt(parts[1], 10, 64)
		if err1 == nil && err2 == nil {
			resp.Diagnostics.Append(resp.State.Set(ctx, &ClientRoleResourceModel{
				ClientID: types.Int64Value(clientID),
				RoleID:   types.Int64Value(roleID),
			})...)
			return
		}
	}
	resp.Diagnostics.AddError("Invalid import ID", fmt.Sprintf("Expected format: {client_id}/{role_id}, got: %q", req.ID))
}
