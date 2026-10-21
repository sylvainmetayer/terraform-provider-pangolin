package datasources

import (
	"context"
	"sort"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stackopshq/terraform-provider-pangolin/internal/client"
	"github.com/stackopshq/terraform-provider-pangolin/internal/tfconv"
)

var _ datasource.DataSource = &ResourcePoliciesDataSource{}

// ResourcePoliciesDataSource lists the shared resource policies of the
// org, with the resources attached to each one.
type ResourcePoliciesDataSource struct {
	client *client.Client
}

// ResourcePolicyAttachedResourceModel is a resource attached to a policy.
type ResourcePolicyAttachedResourceModel struct {
	ResourceID types.Int64  `tfsdk:"resource_id"`
	NiceID     types.String `tfsdk:"nice_id"`
	Name       types.String `tfsdk:"name"`
	FullDomain types.String `tfsdk:"full_domain"`
}

// ResourcePolicySummaryModel is one policy of the list.
type ResourcePolicySummaryModel struct {
	ID        types.Int64                           `tfsdk:"id"`
	NiceID    types.String                          `tfsdk:"nice_id"`
	Name      types.String                          `tfsdk:"name"`
	Resources []ResourcePolicyAttachedResourceModel `tfsdk:"resources"`
}

// ResourcePoliciesDataSourceModel describes the data source data model.
type ResourcePoliciesDataSourceModel struct {
	Query    types.String                 `tfsdk:"query"`
	Policies []ResourcePolicySummaryModel `tfsdk:"policies"`
}

// NewResourcePoliciesDataSource returns a new data source factory.
func NewResourcePoliciesDataSource() datasource.DataSource {
	return &ResourcePoliciesDataSource{}
}

func (d *ResourcePoliciesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_resource_policies"
}

func (d *ResourcePoliciesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Lists the shared resource policies (Enterprise) of the organization, with the resources attached to each one. " +
			"The per-resource inline policies are not listed. Use `pangolin_resource_policy` for the full settings of one policy.",
		Attributes: map[string]schema.Attribute{
			"query": schema.StringAttribute{
				Description: "Only return policies whose name or nice ID contains this text (case-insensitive).",
				Optional:    true,
			},
			"policies": schema.ListNestedAttribute{
				Description: "List of resource policies.",
				Computed:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":      schema.Int64Attribute{Description: "The resource policy ID.", Computed: true},
						"nice_id": schema.StringAttribute{Description: "Readable ID, unique per org.", Computed: true},
						"name":    schema.StringAttribute{Description: "Display name of the policy.", Computed: true},
						"resources": schema.ListNestedAttribute{
							Description: "Resources attached to the policy.",
							Computed:    true,
							NestedObject: schema.NestedAttributeObject{
								Attributes: map[string]schema.Attribute{
									"resource_id": schema.Int64Attribute{Description: "The resource ID.", Computed: true},
									"nice_id":     schema.StringAttribute{Description: "The resource nice ID.", Computed: true},
									"name":        schema.StringAttribute{Description: "The resource name.", Computed: true},
									"full_domain": schema.StringAttribute{Description: "The resource full domain, or `null` for non-HTTP resources.", Computed: true},
								},
							},
						},
					},
				},
			},
		},
	}
}

func (d *ResourcePoliciesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *ResourcePoliciesDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg ResourcePoliciesDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	policies, err := d.client.ListResourcePolicies(ctx, cfg.Query.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to list resource policies", err.Error())
		return
	}

	cfg.Policies = resourcePolicySummaryModels(policies)
	resp.Diagnostics.Append(resp.State.Set(ctx, &cfg)...)
}

// resourcePolicySummaryModels maps the list payload to models.
func resourcePolicySummaryModels(policies []client.ResourcePolicySummary) []ResourcePolicySummaryModel {
	out := []ResourcePolicySummaryModel{}
	for _, p := range policies {
		m := ResourcePolicySummaryModel{
			ID:        types.Int64Value(int64(p.ResourcePolicyID)),
			NiceID:    types.StringValue(p.NiceID),
			Name:      types.StringValue(p.Name),
			Resources: []ResourcePolicyAttachedResourceModel{},
		}
		attached := append([]client.ResourcePolicyAttachedResource(nil), p.Resources...)
		sort.Slice(attached, func(i, j int) bool { return attached[i].ResourceID < attached[j].ResourceID })
		for _, r := range attached {
			m.Resources = append(m.Resources, ResourcePolicyAttachedResourceModel{
				ResourceID: types.Int64Value(int64(r.ResourceID)),
				NiceID:     types.StringValue(r.NiceID),
				Name:       types.StringValue(r.Name),
				FullDomain: tfconv.StringFromPtr(r.FullDomain),
			})
		}
		out = append(out, m)
	}
	return out
}
