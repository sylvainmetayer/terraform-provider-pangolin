package datasources

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stackopshq/terraform-provider-pangolin/internal/client"
	"github.com/stackopshq/terraform-provider-pangolin/internal/tfconv"
)

var _ datasource.DataSource = &AIBudgetsDataSource{}

// AIBudgetsDataSource lists the AI budgets of the org.
type AIBudgetsDataSource struct {
	client *client.Client
}

// AIBudgetItemModel is one budget of the list.
type AIBudgetItemModel struct {
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

// AIBudgetsDataSourceModel describes the data source data model.
type AIBudgetsDataSourceModel struct {
	Budgets []AIBudgetItemModel `tfsdk:"budgets"`
}

// NewAIBudgetsDataSource returns a new data source factory.
func NewAIBudgetsDataSource() datasource.DataSource { return &AIBudgetsDataSource{} }

func (d *AIBudgetsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_ai_budgets"
}

func (d *AIBudgetsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Lists the AI gateway budgets (spending caps) of the organization. At most one scope " +
			"attribute is set per budget; none set means the whole organization.",
		Attributes: map[string]schema.Attribute{
			"budgets": schema.ListNestedAttribute{
				Description: "List of AI budgets, ordered by ID.",
				Computed:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":                 schema.Int64Attribute{Description: "The numeric budget ID.", Computed: true},
						"provider_id":        schema.Int64Attribute{Description: "Scope: AI provider ID, or `null`.", Computed: true},
						"model_id":           schema.Int64Attribute{Description: "Scope: AI model ID, or `null`.", Computed: true},
						"resource_id":        schema.Int64Attribute{Description: "Scope: public resource ID, or `null`.", Computed: true},
						"site_resource_id":   schema.Int64Attribute{Description: "Scope: private (site) resource ID, or `null`.", Computed: true},
						"role_id":            schema.Int64Attribute{Description: "Scope: role ID, or `null`.", Computed: true},
						"virtual_api_key_id": schema.StringAttribute{Description: "Scope: virtual API key ID, or `null`.", Computed: true},
						"amount":             schema.Float64Attribute{Description: "The cap, in `unit`.", Computed: true},
						"unit":               schema.StringAttribute{Description: "The unit of `amount`.", Computed: true},
						"period":             schema.StringAttribute{Description: "The reset period.", Computed: true},
						"enforcement":        schema.StringAttribute{Description: "What happens when the cap is reached.", Computed: true},
						"enabled":            schema.BoolAttribute{Description: "Whether the budget is enabled.", Computed: true},
					},
				},
			},
		},
	}
}

func (d *AIBudgetsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected DataSource Configure Type", "Expected *client.Client")
		return
	}
	d.client = c
}

func (d *AIBudgetsDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	budgets, err := d.client.ListAIBudgets(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Failed to list AI budgets", err.Error())
		return
	}
	state := AIBudgetsDataSourceModel{Budgets: []AIBudgetItemModel{}}
	for _, b := range budgets {
		state.Budgets = append(state.Budgets, AIBudgetItemModel{
			ID:              types.Int64Value(int64(b.BudgetID)),
			ProviderID:      tfconv.Int64FromIntPtr(b.ProviderID),
			ModelID:         tfconv.Int64FromIntPtr(b.ModelID),
			ResourceID:      tfconv.Int64FromIntPtr(b.ResourceID),
			SiteResourceID:  tfconv.Int64FromIntPtr(b.SiteResourceID),
			RoleID:          tfconv.Int64FromIntPtr(b.RoleID),
			VirtualAPIKeyID: tfconv.StringFromPtr(b.VirtualAPIKeyID),
			Amount:          types.Float64Value(b.Amount),
			Unit:            types.StringValue(b.Unit),
			Period:          types.StringValue(b.Period),
			Enforcement:     types.StringValue(b.Enforcement),
			Enabled:         types.BoolValue(b.Enabled),
		})
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
