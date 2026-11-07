package datasources

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stackopshq/terraform-provider-pangolin/internal/client"
)

var _ datasource.DataSource = &AIModelsDataSource{}

// AIModelsDataSource lists the model entries of one AI provider.
type AIModelsDataSource struct {
	client *client.Client
}

// AIModelItemModel is one model entry of the list.
type AIModelItemModel struct {
	ID       types.Int64  `tfsdk:"id"`
	ModelKey types.String `tfsdk:"model_key"`
	Name     types.String `tfsdk:"name"`
	ListType types.String `tfsdk:"list_type"`
	Enabled  types.Bool   `tfsdk:"enabled"`
}

// AIModelsDataSourceModel describes the data source data model.
type AIModelsDataSourceModel struct {
	ProviderID types.Int64        `tfsdk:"provider_id"`
	Query      types.String       `tfsdk:"query"`
	Models     []AIModelItemModel `tfsdk:"models"`
}

// NewAIModelsDataSource returns a new data source factory.
func NewAIModelsDataSource() datasource.DataSource { return &AIModelsDataSource{} }

func (d *AIModelsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_ai_models"
}

func (d *AIModelsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Lists the model entries (allow or block rules for one upstream model key) of an AI provider.",
		Attributes: map[string]schema.Attribute{
			"provider_id": schema.Int64Attribute{
				Description: "The numeric ID of the AI provider.",
				Required:    true,
			},
			"query": schema.StringAttribute{
				Description: "Only return models whose name contains this text (case-insensitive).",
				Optional:    true,
			},
			"models": schema.ListNestedAttribute{
				Description: "List of model entries, ordered by name.",
				Computed:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":        schema.Int64Attribute{Description: "The numeric model ID.", Computed: true},
						"model_key": schema.StringAttribute{Description: "The upstream model key.", Computed: true},
						"name":      schema.StringAttribute{Description: "The display name.", Computed: true},
						"list_type": schema.StringAttribute{Description: "Whether the entry allows or blocks the model.", Computed: true},
						"enabled":   schema.BoolAttribute{Description: "Whether the entry is enabled.", Computed: true},
					},
				},
			},
		},
	}
}

func (d *AIModelsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *AIModelsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg AIModelsDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	models, err := d.client.ListAIModels(ctx, int(cfg.ProviderID.ValueInt64()), cfg.Query.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to list AI models", err.Error())
		return
	}
	cfg.Models = []AIModelItemModel{}
	for _, m := range models {
		cfg.Models = append(cfg.Models, AIModelItemModel{
			ID:       types.Int64Value(int64(m.ModelID)),
			ModelKey: types.StringValue(m.ModelKey),
			Name:     types.StringValue(m.Name),
			ListType: types.StringValue(m.ListType),
			Enabled:  types.BoolValue(m.Enabled),
		})
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &cfg)...)
}
