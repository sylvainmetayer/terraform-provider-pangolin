package resources

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stackopshq/terraform-provider-pangolin/internal/client"
)

var (
	_ resource.Resource                = &AIModelResource{}
	_ resource.ResourceWithImportState = &AIModelResource{}
)

// AIModelResource manages a model entry of an AI provider.
type AIModelResource struct {
	client *client.Client
}

// AIModelResourceModel describes the resource data model.
type AIModelResourceModel struct {
	ID         types.Int64  `tfsdk:"id"`
	ProviderID types.Int64  `tfsdk:"provider_id"`
	ModelKey   types.String `tfsdk:"model_key"`
	Name       types.String `tfsdk:"name"`
	ListType   types.String `tfsdk:"list_type"`
	Enabled    types.Bool   `tfsdk:"enabled"`
}

// NewAIModelResource returns a new resource factory.
func NewAIModelResource() resource.Resource {
	return &AIModelResource{}
}

func (r *AIModelResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_ai_model"
}

func (r *AIModelResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a model of an AI provider (Pangolin 1.22+): allows or blocks one upstream model key " +
			"for the inference resources that use the provider.",
		Attributes: map[string]schema.Attribute{
			"id": schema.Int64Attribute{
				Description:   "The numeric model ID.",
				Computed:      true,
				PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
			},
			"provider_id": schema.Int64Attribute{
				Description:   "ID of the `pangolin_ai_provider` the model belongs to. Changing it replaces the model.",
				Required:      true,
				PlanModifiers: []planmodifier.Int64{int64planmodifier.RequiresReplace()},
			},
			"model_key": schema.StringAttribute{
				Description: "The model identifier sent upstream, e.g. `gpt-4o`. Unique per provider.",
				Required:    true,
				Validators:  []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"name": schema.StringAttribute{
				Description: "The display name.",
				Required:    true,
				Validators:  []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"list_type": schema.StringAttribute{
				Description: "`allow` lets clients use the model, `block` refuses it. Defaults to `allow`.",
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("allow"),
				Validators:  []validator.String{stringvalidator.OneOf("allow", "block")},
			},
			"enabled": schema.BoolAttribute{
				Description: "Whether the model entry is active. Defaults to `true`.",
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(true),
			},
		},
	}
}

func (r *AIModelResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func aiModelRequest(m AIModelResourceModel) *client.AIModelRequest {
	return &client.AIModelRequest{
		ModelKey: m.ModelKey.ValueString(),
		Name:     m.Name.ValueString(),
		Enabled:  m.Enabled.ValueBoolPointer(),
		ListType: m.ListType.ValueString(),
	}
}

func aiModelToModel(in *client.AIModel) AIModelResourceModel {
	return AIModelResourceModel{
		ID:         types.Int64Value(int64(in.ModelID)),
		ProviderID: types.Int64Value(int64(in.ProviderID)),
		ModelKey:   types.StringValue(in.ModelKey),
		Name:       types.StringValue(in.Name),
		ListType:   types.StringValue(in.ListType),
		Enabled:    types.BoolValue(in.Enabled),
	}
}

func (r *AIModelResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan AIModelResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	created, err := r.client.CreateAIModel(ctx, int(plan.ProviderID.ValueInt64()), aiModelRequest(plan))
	if err != nil {
		resp.Diagnostics.AddError("Failed to create AI model", err.Error())
		return
	}
	state := aiModelToModel(created)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *AIModelResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state AIModelResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	got, err := r.client.GetAIModel(ctx, int(state.ID.ValueInt64()))
	if err != nil {
		if errors.Is(err, client.ErrNotFound) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read AI model", err.Error())
		return
	}
	state = aiModelToModel(got)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *AIModelResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan AIModelResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	updated, err := r.client.UpdateAIModel(ctx, int(plan.ID.ValueInt64()), aiModelRequest(plan))
	if err != nil {
		resp.Diagnostics.AddError("Failed to update AI model", err.Error())
		return
	}
	state := aiModelToModel(updated)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *AIModelResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state AIModelResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// Deleting the provider cascades to its models: a gone model is fine.
	if err := r.client.DeleteAIModel(ctx, int(state.ID.ValueInt64())); err != nil && !errors.Is(err, client.ErrNotFound) {
		resp.Diagnostics.AddError("Failed to delete AI model", err.Error())
	}
}

func (r *AIModelResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	id, err := strconv.ParseInt(req.ID, 10, 64)
	if err != nil {
		resp.Diagnostics.AddError("Invalid import ID", fmt.Sprintf("Expected a numeric AI model ID, got: %q", req.ID))
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
}
