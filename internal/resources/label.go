package resources

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"

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
	_ resource.Resource                = &LabelResource{}
	_ resource.ResourceWithImportState = &LabelResource{}
)

var labelColorPattern = regexp.MustCompile(`^#?([0-9a-fA-F]{6}|[0-9a-fA-F]{3})$`)

// LabelResource manages an organization label.
type LabelResource struct {
	client *client.Client
}

// LabelResourceModel describes the resource data model.
type LabelResourceModel struct {
	ID    types.Int64  `tfsdk:"id"`
	Name  types.String `tfsdk:"name"`
	Color types.String `tfsdk:"color"`
}

// NewLabelResource returns a new resource factory.
func NewLabelResource() resource.Resource {
	return &LabelResource{}
}

func (r *LabelResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_label"
}

func (r *LabelResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a label of the organization. Attach it with `pangolin_label_attachment`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.Int64Attribute{
				Description:   "The numeric label ID.",
				Computed:      true,
				PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Description: "The label name, unique in the organization regardless of case.",
				Required:    true,
				Validators:  []validator.String{stringvalidator.LengthBetween(1, 255)},
			},
			"color": schema.StringAttribute{
				Description: "The label color, as 3 or 6 hexadecimal digits with an optional leading `#`.",
				Required:    true,
				Validators:  []validator.String{stringvalidator.RegexMatches(labelColorPattern, "must be 3 or 6 hexadecimal digits, with an optional leading #")},
			},
		},
	}
}

func (r *LabelResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func labelToModel(in *client.Label) LabelResourceModel {
	return LabelResourceModel{
		ID:    types.Int64Value(int64(in.LabelID)),
		Name:  types.StringValue(in.Name),
		Color: types.StringValue(in.Color),
	}
}

func (r *LabelResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan LabelResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	created, err := r.client.CreateLabel(ctx, &client.LabelRequest{Name: plan.Name.ValueString(), Color: plan.Color.ValueString()})
	if err != nil {
		resp.Diagnostics.AddError("Failed to create label", err.Error())
		return
	}
	state := labelToModel(created)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *LabelResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state LabelResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	got, err := r.client.GetLabel(ctx, int(state.ID.ValueInt64()))
	if err != nil {
		if errors.Is(err, client.ErrNotFound) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read label", err.Error())
		return
	}
	state = labelToModel(got)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *LabelResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan LabelResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	updated, err := r.client.UpdateLabel(ctx, int(plan.ID.ValueInt64()), &client.LabelRequest{Name: plan.Name.ValueString(), Color: plan.Color.ValueString()})
	if err != nil {
		resp.Diagnostics.AddError("Failed to update label", err.Error())
		return
	}
	state := labelToModel(updated)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *LabelResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state LabelResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DeleteLabel(ctx, int(state.ID.ValueInt64())); err != nil && !errors.Is(err, client.ErrNotFound) {
		resp.Diagnostics.AddError("Failed to delete label", err.Error())
	}
}

func (r *LabelResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	id, err := strconv.ParseInt(req.ID, 10, 64)
	if err != nil {
		resp.Diagnostics.AddError("Invalid import ID", fmt.Sprintf("Expected a numeric label ID, got: %q", req.ID))
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
}
