package datasources

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stackopshq/terraform-provider-pangolin/internal/client"
	"github.com/stackopshq/terraform-provider-pangolin/internal/tfconv"
)

var _ datasource.DataSource = &AIProvidersDataSource{}

// AIProvidersDataSource lists the AI providers of the org.
type AIProvidersDataSource struct {
	client *client.Client
}

// AIProviderItemModel is one provider of the list.
type AIProviderItemModel struct {
	ID                  types.Int64  `tfsdk:"id"`
	NiceID              types.String `tfsdk:"nice_id"`
	Name                types.String `tfsdk:"name"`
	Type                types.String `tfsdk:"type"`
	UpstreamURL         types.String `tfsdk:"upstream_url"`
	AuthType            types.String `tfsdk:"auth_type"`
	RoutingMode         types.String `tfsdk:"routing_mode"`
	Capabilities        types.List   `tfsdk:"capabilities"`
	SkipTLSVerification types.Bool   `tfsdk:"skip_tls_verification"`
	Enabled             types.Bool   `tfsdk:"enabled"`
}

// AIProvidersDataSourceModel describes the data source data model.
type AIProvidersDataSourceModel struct {
	Query     types.String          `tfsdk:"query"`
	Providers []AIProviderItemModel `tfsdk:"providers"`
}

// NewAIProvidersDataSource returns a new data source factory.
func NewAIProvidersDataSource() datasource.DataSource { return &AIProvidersDataSource{} }

func (d *AIProvidersDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_ai_providers"
}

func (d *AIProvidersDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Lists the AI providers of the organization (AI gateway). The upstream API key and the custom " +
			"headers are not part of the list and are never exposed here.",
		Attributes: map[string]schema.Attribute{
			"query": schema.StringAttribute{
				Description: "Only return providers whose name contains this text (case-insensitive).",
				Optional:    true,
			},
			"providers": schema.ListNestedAttribute{
				Description: "List of AI providers, ordered by name.",
				Computed:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":                    schema.Int64Attribute{Description: "The numeric provider ID.", Computed: true},
						"nice_id":               schema.StringAttribute{Description: "Readable ID, unique per org.", Computed: true},
						"name":                  schema.StringAttribute{Description: "The provider name.", Computed: true},
						"type":                  schema.StringAttribute{Description: "The provider type (e.g. `openai`, `anthropic`, `custom`).", Computed: true},
						"upstream_url":          schema.StringAttribute{Description: "The upstream base URL, or `null` for the type default.", Computed: true},
						"auth_type":             schema.StringAttribute{Description: "How the gateway authenticates to the upstream.", Computed: true},
						"routing_mode":          schema.StringAttribute{Description: "How requests are routed to the upstream.", Computed: true},
						"capabilities":          schema.ListAttribute{Description: "Capabilities enabled on the provider.", Computed: true, ElementType: types.StringType},
						"skip_tls_verification": schema.BoolAttribute{Description: "Whether the upstream TLS certificate is not verified.", Computed: true},
						"enabled":               schema.BoolAttribute{Description: "Whether the provider is enabled.", Computed: true},
					},
				},
			},
		},
	}
}

func (d *AIProvidersDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *AIProvidersDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg AIProvidersDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	providers, err := d.client.ListAIProviders(ctx, cfg.Query.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to list AI providers", err.Error())
		return
	}
	cfg.Providers = []AIProviderItemModel{}
	for _, p := range providers {
		caps := p.Capabilities
		if caps == nil {
			caps = []string{}
		}
		capList, diags := types.ListValueFrom(ctx, types.StringType, caps)
		resp.Diagnostics.Append(diags...)
		cfg.Providers = append(cfg.Providers, AIProviderItemModel{
			ID:                  types.Int64Value(int64(p.ProviderID)),
			NiceID:              types.StringValue(p.NiceID),
			Name:                types.StringValue(p.Name),
			Type:                types.StringValue(p.Type),
			UpstreamURL:         tfconv.StringFromPtr(p.UpstreamURL),
			AuthType:            types.StringValue(p.AuthType),
			RoutingMode:         types.StringValue(p.RoutingMode),
			Capabilities:        capList,
			SkipTLSVerification: types.BoolValue(p.SkipTLSVerification),
			Enabled:             types.BoolValue(p.Enabled),
		})
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &cfg)...)
}
