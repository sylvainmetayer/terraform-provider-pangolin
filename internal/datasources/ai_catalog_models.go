package datasources

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stackopshq/terraform-provider-pangolin/internal/client"
)

var _ datasource.DataSource = &AICatalogModelsDataSource{}

// AICatalogModelsDataSource lists the upstream model keys Pangolin knows
// for an AI provider type.
type AICatalogModelsDataSource struct {
	client *client.Client
}

// AICatalogModelsDataSourceModel describes the data source data model.
type AICatalogModelsDataSourceModel struct {
	Type      types.String `tfsdk:"type"`
	Query     types.String `tfsdk:"query"`
	ModelKeys types.List   `tfsdk:"model_keys"`
}

// NewAICatalogModelsDataSource returns a new data source factory.
func NewAICatalogModelsDataSource() datasource.DataSource { return &AICatalogModelsDataSource{} }

func (d *AICatalogModelsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_ai_catalog_models"
}

func (d *AICatalogModelsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Lists the upstream model keys that Pangolin's built-in catalog knows for an AI provider type. " +
			"Useful to pick the `model_key` of a `pangolin_ai_model`. The catalog may be empty for the `custom` type.",
		Attributes: map[string]schema.Attribute{
			"type": schema.StringAttribute{
				Description: "The provider type: `openai`, `anthropic`, `googleGemini`, `vertexAi`, `bedrock`, " +
					"`microsoftFoundry`, `openRouter`, `vercelAiGateway` or `custom`.",
				Required: true,
			},
			"query": schema.StringAttribute{
				Description: "Only return model keys containing this text.",
				Optional:    true,
			},
			"model_keys": schema.ListAttribute{
				Description: "The catalog model keys.",
				Computed:    true,
				ElementType: types.StringType,
			},
		},
	}
}

func (d *AICatalogModelsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *AICatalogModelsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg AICatalogModelsDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	keys, err := d.client.ListAICatalogModels(ctx, cfg.Type.ValueString(), cfg.Query.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to list AI catalog models", err.Error())
		return
	}
	list, diags := types.ListValueFrom(ctx, types.StringType, keys)
	resp.Diagnostics.Append(diags...)
	cfg.ModelKeys = list
	resp.Diagnostics.Append(resp.State.Set(ctx, &cfg)...)
}
