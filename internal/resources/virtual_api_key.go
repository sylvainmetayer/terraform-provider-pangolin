package resources

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stackopshq/terraform-provider-pangolin/internal/client"
)

var (
	_ resource.Resource                = &VirtualAPIKeyResource{}
	_ resource.ResourceWithImportState = &VirtualAPIKeyResource{}
)

// VirtualAPIKeyResource manages a manual virtual API key.
type VirtualAPIKeyResource struct {
	client *client.Client
}

// VirtualAPIKeyResourceModel describes the resource data model.
type VirtualAPIKeyResourceModel struct {
	ID              types.String `tfsdk:"id"`
	Name            types.String `tfsdk:"name"`
	Description     types.String `tfsdk:"description"`
	UserID          types.String `tfsdk:"user_id"`
	AllResources    types.Bool   `tfsdk:"all_resources"`
	ResourceIDs     types.Set    `tfsdk:"resource_ids"`
	ValidForSeconds types.Int64  `tfsdk:"valid_for_seconds"`
	Secret          types.String `tfsdk:"secret"`
}

// NewVirtualAPIKeyResource returns a new resource factory.
func NewVirtualAPIKeyResource() resource.Resource {
	return &VirtualAPIKeyResource{}
}

func (r *VirtualAPIKeyResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_virtual_api_key"
}

func (r *VirtualAPIKeyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a manual virtual API key (Pangolin 1.26+): a bearer key that lets a client call " +
			"the public inference resources of the AI gateway. Set either `all_resources` or `resource_ids`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description:   "The virtual API key ID.",
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Description: "The name of the key.",
				Required:    true,
				Validators:  []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"description": schema.StringAttribute{
				Description: "A free-text description.",
				Optional:    true,
			},
			"user_id": schema.StringAttribute{
				Description: "ID of an organization member the key is attributed to.",
				Optional:    true,
			},
			"all_resources": schema.BoolAttribute{
				Description: "Allow the key on every public inference resource of the organization, " +
					"including the ones created later. Defaults to `false`.",
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(false),
			},
			"resource_ids": schema.SetAttribute{
				Description: "IDs of the public inference `pangolin_resource` the key may call. " +
					"Required unless `all_resources` is `true`, in which case leave it unset.",
				Optional:    true,
				ElementType: types.Int64Type,
				Validators:  []validator.Set{setvalidator.SizeAtLeast(1)},
			},
			"valid_for_seconds": schema.Int64Attribute{
				Description: "Lifetime of the key in seconds, counted from the apply that sets it. Unset means the key " +
					"never expires. It cannot be read back, so it is not checked for drift and not set on import.",
				Optional:   true,
				Validators: []validator.Int64{int64validator.AtLeast(1)},
			},
			"secret": schema.StringAttribute{
				Description: "The key value clients send as a bearer token.",
				Computed:    true,
				Sensitive:   true,
			},
		},
	}
}

func (r *VirtualAPIKeyResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// virtualAPIKeyRequest builds the strict request body. validForSeconds is
// only sent when it differs from prior (null on removal, to clear it).
func virtualAPIKeyRequest(ctx context.Context, plan VirtualAPIKeyResourceModel, prior *VirtualAPIKeyResourceModel) (*client.VirtualAPIKeyRequest, error) {
	req := &client.VirtualAPIKeyRequest{
		Name:         plan.Name.ValueString(),
		AllResources: plan.AllResources.ValueBool(),
		ResourceIDs:  []int{},
	}
	if !plan.Description.IsNull() {
		req.Description = plan.Description.ValueString()
	}
	if !plan.UserID.IsNull() {
		req.UserID = plan.UserID.ValueString()
	}
	if !plan.ResourceIDs.IsNull() && !plan.AllResources.ValueBool() {
		var ids []int64
		if d := plan.ResourceIDs.ElementsAs(ctx, &ids, false); d.HasError() {
			return nil, fmt.Errorf("resource_ids: %v", d)
		}
		for _, id := range ids {
			req.ResourceIDs = append(req.ResourceIDs, int(id))
		}
	}
	if prior == nil || !plan.ValidForSeconds.Equal(prior.ValidForSeconds) {
		if plan.ValidForSeconds.IsNull() {
			if prior != nil {
				req.ValidForSeconds = json.RawMessage("null")
			}
		} else {
			req.ValidForSeconds = json.RawMessage(fmt.Sprint(plan.ValidForSeconds.ValueInt64()))
		}
	}
	return req, nil
}

// virtualAPIKeyToModel keeps prior's valid_for_seconds: the server only
// returns an absolute expiry.
func virtualAPIKeyToModel(ctx context.Context, prior VirtualAPIKeyResourceModel, k *client.VirtualAPIKey) (VirtualAPIKeyResourceModel, error) {
	m := VirtualAPIKeyResourceModel{
		ID:              types.StringValue(k.VirtualAPIKeyID),
		Name:            types.StringPointerValue(k.Name),
		Description:     types.StringPointerValue(k.Description),
		UserID:          types.StringPointerValue(k.UserID),
		AllResources:    types.BoolValue(k.AllResources),
		ResourceIDs:     types.SetNull(types.Int64Type),
		ValidForSeconds: prior.ValidForSeconds,
		Secret:          types.StringValue(k.Secret),
	}
	if len(k.ResourceIDs) > 0 {
		ids := make([]int64, len(k.ResourceIDs))
		for i, id := range k.ResourceIDs {
			ids[i] = int64(id)
		}
		set, d := types.SetValueFrom(ctx, types.Int64Type, ids)
		if d.HasError() {
			return prior, fmt.Errorf("resource_ids: %v", d)
		}
		m.ResourceIDs = set
	}
	return m, nil
}

func (r *VirtualAPIKeyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan VirtualAPIKeyResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	body, err := virtualAPIKeyRequest(ctx, plan, nil)
	if err != nil {
		resp.Diagnostics.AddError("Invalid virtual API key", err.Error())
		return
	}
	created, err := r.client.CreateVirtualAPIKey(ctx, body)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create virtual API key", err.Error())
		return
	}
	state, err := virtualAPIKeyToModel(ctx, plan, created)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read virtual API key", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *VirtualAPIKeyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state VirtualAPIKeyResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	got, err := r.client.GetVirtualAPIKey(ctx, state.ID.ValueString())
	if err != nil {
		if errors.Is(err, client.ErrNotFound) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read virtual API key", err.Error())
		return
	}
	state, err = virtualAPIKeyToModel(ctx, state, got)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read virtual API key", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *VirtualAPIKeyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, prior VirtualAPIKeyResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &prior)...)
	if resp.Diagnostics.HasError() {
		return
	}
	body, err := virtualAPIKeyRequest(ctx, plan, &prior)
	if err != nil {
		resp.Diagnostics.AddError("Invalid virtual API key", err.Error())
		return
	}
	updated, err := r.client.UpdateVirtualAPIKey(ctx, prior.ID.ValueString(), body)
	if err != nil {
		resp.Diagnostics.AddError("Failed to update virtual API key", err.Error())
		return
	}
	state, err := virtualAPIKeyToModel(ctx, plan, updated)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read virtual API key", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *VirtualAPIKeyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state VirtualAPIKeyResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DeleteVirtualAPIKey(ctx, state.ID.ValueString()); err != nil && !errors.Is(err, client.ErrNotFound) {
		resp.Diagnostics.AddError("Failed to delete virtual API key", err.Error())
	}
}

func (r *VirtualAPIKeyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}
