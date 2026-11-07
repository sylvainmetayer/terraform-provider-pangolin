package resources

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
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
	_ resource.Resource                = &AIBudgetResource{}
	_ resource.ResourceWithImportState = &AIBudgetResource{}
)

// AIBudgetResource manages a spending cap of the AI gateway.
type AIBudgetResource struct {
	client *client.Client
}

// AIBudgetResourceModel describes the resource data model.
type AIBudgetResourceModel struct {
	ID              types.Int64   `tfsdk:"id"`
	ProviderID      types.Int64   `tfsdk:"provider_id"`
	ModelID         types.Int64   `tfsdk:"model_id"`
	ResourceID      types.Int64   `tfsdk:"resource_id"`
	SiteResourceID  types.Int64   `tfsdk:"site_resource_id"`
	RoleID          types.Int64   `tfsdk:"role_id"`
	VirtualAPIKeyID types.String  `tfsdk:"virtual_api_key_id"`
	Amount          types.Float64 `tfsdk:"amount"`
	Unit            types.String  `tfsdk:"unit"`
	Period          types.String  `tfsdk:"period"`
	Enforcement     types.String  `tfsdk:"enforcement"`
	Enabled         types.Bool    `tfsdk:"enabled"`
}

// NewAIBudgetResource returns a new resource factory.
func NewAIBudgetResource() resource.Resource {
	return &AIBudgetResource{}
}

func (r *AIBudgetResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_ai_budget"
}

// aiBudgetScopes are the attributes a budget can attach to; at most one.
var aiBudgetScopes = []string{"provider_id", "model_id", "resource_id", "site_resource_id", "role_id", "virtual_api_key_id"}

func aiBudgetConflicts(self string) []path.Expression {
	var out []path.Expression
	for _, s := range aiBudgetScopes {
		if s != self {
			out = append(out, path.MatchRoot(s))
		}
	}
	return out
}

func aiBudgetIntScope(self, desc string) schema.Int64Attribute {
	return schema.Int64Attribute{
		Description: desc + " At most one scope attribute may be set; none means the whole organization.",
		Optional:    true,
		Validators: []validator.Int64{
			int64validator.AtLeast(1),
			int64validator.ConflictsWith(aiBudgetConflicts(self)...),
		},
	}
}

func (r *AIBudgetResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a spending cap of the AI gateway (Pangolin 1.26+), in USD or tokens per period. " +
			"It applies to one AI provider, AI model, public inference resource, private inference resource, role " +
			"or virtual API key, or to the whole organization when no scope is set. " +
			"A scope holds one budget per unit and period.",
		Attributes: map[string]schema.Attribute{
			"id": schema.Int64Attribute{
				Description:   "The numeric budget ID.",
				Computed:      true,
				PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
			},
			"provider_id":      aiBudgetIntScope("provider_id", "ID of the `pangolin_ai_provider` the budget applies to."),
			"model_id":         aiBudgetIntScope("model_id", "ID of the `pangolin_ai_model` the budget applies to."),
			"resource_id":      aiBudgetIntScope("resource_id", "ID of the public inference `pangolin_resource` the budget applies to."),
			"site_resource_id": aiBudgetIntScope("site_resource_id", "ID of the private inference `pangolin_site_resource` the budget applies to."),
			"role_id":          aiBudgetIntScope("role_id", "ID of the `pangolin_role` the budget applies to."),
			"virtual_api_key_id": schema.StringAttribute{
				Description: "ID of the `pangolin_virtual_api_key` the budget applies to. " +
					"At most one scope attribute may be set; none means the whole organization.",
				Optional: true,
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
					stringvalidator.ConflictsWith(aiBudgetConflicts("virtual_api_key_id")...),
				},
			},
			"amount": schema.Float64Attribute{
				Description: "The cap, in `unit`. Must be greater than zero.",
				Required:    true,
			},
			"unit": schema.StringAttribute{
				Description: "`usd` or `tokens`.",
				Required:    true,
				Validators:  []validator.String{stringvalidator.OneOf("usd", "tokens")},
			},
			"period": schema.StringAttribute{
				Description: "The reset period: `hourly`, `daily`, `weekly`, `monthly`, `yearly` or `lifetime`. Defaults to `monthly`.",
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("monthly"),
				Validators:  []validator.String{stringvalidator.OneOf("hourly", "daily", "weekly", "monthly", "yearly", "lifetime")},
			},
			"enforcement": schema.StringAttribute{
				Description: "`hard` refuses requests over the cap, `soft` only reports it. Defaults to `hard`.",
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("hard"),
				Validators:  []validator.String{stringvalidator.OneOf("hard", "soft")},
			},
			"enabled": schema.BoolAttribute{
				Description: "Whether the budget is active. Defaults to `true`.",
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(true),
			},
		},
	}
}

func (r *AIBudgetResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func budgetIntPtr(v types.Int64) *int {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	i := int(v.ValueInt64())
	return &i
}

func budgetIntValue(v *int) types.Int64 {
	if v == nil {
		return types.Int64Null()
	}
	return types.Int64Value(int64(*v))
}

func aiBudgetCreateRequest(m AIBudgetResourceModel) *client.AIBudgetRequest {
	return &client.AIBudgetRequest{
		ProviderID:      budgetIntPtr(m.ProviderID),
		ModelID:         budgetIntPtr(m.ModelID),
		ResourceID:      budgetIntPtr(m.ResourceID),
		SiteResourceID:  budgetIntPtr(m.SiteResourceID),
		RoleID:          budgetIntPtr(m.RoleID),
		VirtualAPIKeyID: m.VirtualAPIKeyID.ValueStringPointer(),
		Amount:          m.Amount.ValueFloat64(),
		Unit:            m.Unit.ValueString(),
		Period:          m.Period.ValueString(),
		Enforcement:     m.Enforcement.ValueString(),
		Enabled:         m.Enabled.ValueBoolPointer(),
	}
}

func aiBudgetUpdateRequest(m AIBudgetResourceModel) *client.AIBudgetUpdateRequest {
	return &client.AIBudgetUpdateRequest{
		ProviderID:      budgetIntPtr(m.ProviderID),
		ModelID:         budgetIntPtr(m.ModelID),
		ResourceID:      budgetIntPtr(m.ResourceID),
		SiteResourceID:  budgetIntPtr(m.SiteResourceID),
		RoleID:          budgetIntPtr(m.RoleID),
		VirtualAPIKeyID: m.VirtualAPIKeyID.ValueStringPointer(),
		Amount:          m.Amount.ValueFloat64(),
		Unit:            m.Unit.ValueString(),
		Period:          m.Period.ValueString(),
		Enforcement:     m.Enforcement.ValueString(),
		Enabled:         m.Enabled.ValueBoolPointer(),
	}
}

func aiBudgetToModel(b *client.AIBudget) AIBudgetResourceModel {
	return AIBudgetResourceModel{
		ID:              types.Int64Value(int64(b.BudgetID)),
		ProviderID:      budgetIntValue(b.ProviderID),
		ModelID:         budgetIntValue(b.ModelID),
		ResourceID:      budgetIntValue(b.ResourceID),
		SiteResourceID:  budgetIntValue(b.SiteResourceID),
		RoleID:          budgetIntValue(b.RoleID),
		VirtualAPIKeyID: types.StringPointerValue(b.VirtualAPIKeyID),
		Amount:          types.Float64Value(b.Amount),
		Unit:            types.StringValue(b.Unit),
		Period:          types.StringValue(b.Period),
		Enforcement:     types.StringValue(b.Enforcement),
		Enabled:         types.BoolValue(b.Enabled),
	}
}

func (r *AIBudgetResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan AIBudgetResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	created, err := r.client.CreateAIBudget(ctx, aiBudgetCreateRequest(plan))
	if err != nil {
		resp.Diagnostics.AddError("Failed to create AI budget", err.Error())
		return
	}
	state := aiBudgetToModel(created)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *AIBudgetResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state AIBudgetResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	got, err := r.client.GetAIBudget(ctx, int(state.ID.ValueInt64()))
	if err != nil {
		if errors.Is(err, client.ErrNotFound) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read AI budget", err.Error())
		return
	}
	state = aiBudgetToModel(got)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *AIBudgetResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan AIBudgetResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	updated, err := r.client.UpdateAIBudget(ctx, int(plan.ID.ValueInt64()), aiBudgetUpdateRequest(plan))
	if err != nil {
		resp.Diagnostics.AddError("Failed to update AI budget", err.Error())
		return
	}
	state := aiBudgetToModel(updated)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *AIBudgetResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state AIBudgetResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// Deleting the scope cascades to its budgets: a gone budget is fine.
	if err := r.client.DeleteAIBudget(ctx, int(state.ID.ValueInt64())); err != nil && !errors.Is(err, client.ErrNotFound) {
		resp.Diagnostics.AddError("Failed to delete AI budget", err.Error())
	}
}

func (r *AIBudgetResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	id, err := strconv.ParseInt(req.ID, 10, 64)
	if err != nil {
		resp.Diagnostics.AddError("Invalid import ID", fmt.Sprintf("Expected a numeric AI budget ID, got: %q", req.ID))
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
}
