package datasources

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stackopshq/terraform-provider-pangolin/internal/client"
	"github.com/stackopshq/terraform-provider-pangolin/internal/tfconv"
)

var _ datasource.DataSource = &RedirectsDataSource{}

// RedirectsDataSource lists the redirects of the org.
type RedirectsDataSource struct {
	client *client.Client
}

// RedirectItemModel is one redirect of the list.
type RedirectItemModel struct {
	ID                 types.Int64  `tfsdk:"id"`
	NiceID             types.String `tfsdk:"nice_id"`
	Name               types.String `tfsdk:"name"`
	ResourceID         types.Int64  `tfsdk:"resource_id"`
	ResourceName       types.String `tfsdk:"resource_name"`
	ResourceNiceID     types.String `tfsdk:"resource_nice_id"`
	ResourceFullDomain types.String `tfsdk:"resource_full_domain"`
	DomainID           types.String `tfsdk:"domain_id"`
	BaseDomain         types.String `tfsdk:"base_domain"`
	Subdomain          types.String `tfsdk:"subdomain"`
	DestinationHost    types.String `tfsdk:"destination_host"`
	PathMatchType      types.String `tfsdk:"path_match_type"`
	MatchPath          types.String `tfsdk:"match_path"`
	RewritePath        types.String `tfsdk:"rewrite_path"`
	RewritePathType    types.String `tfsdk:"rewrite_path_type"`
	Priority           types.Int64  `tfsdk:"priority"`
	Permanent          types.Bool   `tfsdk:"permanent"`
	SSL                types.Bool   `tfsdk:"ssl"`
	Enabled            types.Bool   `tfsdk:"enabled"`
}

// RedirectsDataSourceModel describes the data source data model.
type RedirectsDataSourceModel struct {
	Query      types.String        `tfsdk:"query"`
	Type       types.String        `tfsdk:"type"`
	Enabled    types.Bool          `tfsdk:"enabled"`
	ResourceID types.Int64         `tfsdk:"resource_id"`
	DomainID   types.String        `tfsdk:"domain_id"`
	Redirects  []RedirectItemModel `tfsdk:"redirects"`
}

// NewRedirectsDataSource returns a new data source factory.
func NewRedirectsDataSource() datasource.DataSource { return &RedirectsDataSource{} }

func (d *RedirectsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_redirects"
}

func (d *RedirectsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Lists the redirects of the organization, sorted by name.",
		Attributes: map[string]schema.Attribute{
			"query": schema.StringAttribute{
				Description: "Only return redirects matching this text (case-insensitive) in its name, match path or destination host.",
				Optional:    true,
			},
			"type": schema.StringAttribute{
				Description: "Only return `permanent` (308) or `temporary` (307) redirects.",
				Optional:    true,
			},
			"enabled": schema.BoolAttribute{
				Description: "Only return enabled (`true`) or disabled (`false`) redirects.",
				Optional:    true,
			},
			"resource_id": schema.Int64Attribute{
				Description: "Only return redirects attached to this resource.",
				Optional:    true,
			},
			"domain_id": schema.StringAttribute{
				Description: "Only return redirects attached to this domain.",
				Optional:    true,
			},
			"redirects": schema.ListNestedAttribute{
				Description: "List of redirects.",
				Computed:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":                   schema.Int64Attribute{Description: "The numeric redirect ID.", Computed: true},
						"nice_id":              schema.StringAttribute{Description: "Readable ID, unique per org.", Computed: true},
						"name":                 schema.StringAttribute{Description: "The redirect name.", Computed: true},
						"resource_id":          schema.Int64Attribute{Description: "The resource the redirect is attached to, or `null`.", Computed: true},
						"resource_name":        schema.StringAttribute{Description: "Name of the attached resource, or `null`.", Computed: true},
						"resource_nice_id":     schema.StringAttribute{Description: "Nice ID of the attached resource, or `null`.", Computed: true},
						"resource_full_domain": schema.StringAttribute{Description: "Full domain of the attached resource, or `null`.", Computed: true},
						"domain_id":            schema.StringAttribute{Description: "The domain the redirect is attached to, or `null`.", Computed: true},
						"base_domain":          schema.StringAttribute{Description: "Base domain of `domain_id`, or `null`.", Computed: true},
						"subdomain":            schema.StringAttribute{Description: "The subdomain, or `null`.", Computed: true},
						"destination_host":     schema.StringAttribute{Description: "Where requests are redirected to.", Computed: true},
						"path_match_type":      schema.StringAttribute{Description: "How `match_path` is matched: `exact`, `prefix` or `regex`.", Computed: true},
						"match_path":           schema.StringAttribute{Description: "The path to match, or `null`.", Computed: true},
						"rewrite_path":         schema.StringAttribute{Description: "The rewritten path, or `null`.", Computed: true},
						"rewrite_path_type":    schema.StringAttribute{Description: "How the path is rewritten, or `null`.", Computed: true},
						"priority":             schema.Int64Attribute{Description: "Matching priority, or `null`.", Computed: true},
						"permanent":            schema.BoolAttribute{Description: "Whether the redirect is permanent (308) rather than temporary (307).", Computed: true},
						"ssl":                  schema.BoolAttribute{Description: "Whether the redirect is served over HTTPS.", Computed: true},
						"enabled":              schema.BoolAttribute{Description: "Whether the redirect is enabled.", Computed: true},
					},
				},
			},
		},
	}
}

func (d *RedirectsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *RedirectsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg RedirectsDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	q := client.RedirectListQuery{
		Query:    cfg.Query.ValueString(),
		Type:     cfg.Type.ValueString(),
		Enabled:  cfg.Enabled.ValueBoolPointer(),
		DomainID: cfg.DomainID.ValueString(),
	}
	if !cfg.ResourceID.IsNull() {
		v := int(cfg.ResourceID.ValueInt64())
		q.ResourceID = &v
	}
	items, err := d.client.ListRedirects(ctx, q)
	if err != nil {
		resp.Diagnostics.AddError("Failed to list redirects", err.Error())
		return
	}
	cfg.Redirects = []RedirectItemModel{}
	for _, r := range items {
		cfg.Redirects = append(cfg.Redirects, RedirectItemModel{
			ID:                 types.Int64Value(int64(r.RedirectID)),
			NiceID:             types.StringValue(r.NiceID),
			Name:               types.StringValue(r.Name),
			ResourceID:         tfconv.Int64FromIntPtr(r.ResourceID),
			ResourceName:       tfconv.StringFromPtr(r.ResourceName),
			ResourceNiceID:     tfconv.StringFromPtr(r.ResourceNiceID),
			ResourceFullDomain: tfconv.StringFromPtr(r.ResourceFullDomain),
			DomainID:           tfconv.StringFromPtr(r.DomainID),
			BaseDomain:         tfconv.StringFromPtr(r.BaseDomain),
			Subdomain:          tfconv.StringFromPtr(r.Subdomain),
			DestinationHost:    types.StringValue(r.DestinationHost),
			PathMatchType:      types.StringValue(r.PathMatchType),
			MatchPath:          tfconv.StringFromPtr(r.MatchPath),
			RewritePath:        tfconv.StringFromPtr(r.RewritePath),
			RewritePathType:    tfconv.StringFromPtr(r.RewritePathType),
			Priority:           tfconv.Int64FromIntPtr(r.Priority),
			Permanent:          types.BoolValue(r.Permanent),
			SSL:                types.BoolValue(r.SSL),
			Enabled:            types.BoolValue(r.Enabled),
		})
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &cfg)...)
}
