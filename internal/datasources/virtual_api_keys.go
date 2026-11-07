package datasources

import (
	"context"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stackopshq/terraform-provider-pangolin/internal/client"
	"github.com/stackopshq/terraform-provider-pangolin/internal/tfconv"
)

var _ datasource.DataSource = &VirtualAPIKeysDataSource{}

// VirtualAPIKeysDataSource lists the manual virtual API keys of the org.
type VirtualAPIKeysDataSource struct {
	client *client.Client
}

// VirtualAPIKeyItemModel is one key of the list. It has no secret.
type VirtualAPIKeyItemModel struct {
	ID           types.String `tfsdk:"id"`
	Name         types.String `tfsdk:"name"`
	Description  types.String `tfsdk:"description"`
	UserID       types.String `tfsdk:"user_id"`
	LastChars    types.String `tfsdk:"last_chars"`
	AllResources types.Bool   `tfsdk:"all_resources"`
	ResourceIDs  types.List   `tfsdk:"resource_ids"`
	ExpiresAt    types.Int64  `tfsdk:"expires_at"`
	LastUsedAt   types.Int64  `tfsdk:"last_used_at"`
	CreatedAt    types.Int64  `tfsdk:"created_at"`
}

// VirtualAPIKeysDataSourceModel describes the data source data model.
type VirtualAPIKeysDataSourceModel struct {
	Search      types.String             `tfsdk:"search"`
	UserID      types.String             `tfsdk:"user_id"`
	ResourceID  types.Int64              `tfsdk:"resource_id"`
	VirtualKeys []VirtualAPIKeyItemModel `tfsdk:"virtual_api_keys"`
}

// NewVirtualAPIKeysDataSource returns a new data source factory.
func NewVirtualAPIKeysDataSource() datasource.DataSource { return &VirtualAPIKeysDataSource{} }

func (d *VirtualAPIKeysDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_virtual_api_keys"
}

func (d *VirtualAPIKeysDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Lists the manual virtual API keys (AI gateway) of the organization. The list never carries " +
			"the secret; only its last four characters are returned, to tell keys apart.",
		Attributes: map[string]schema.Attribute{
			"search": schema.StringAttribute{
				Description: "Only return keys whose name, description or last characters contain this text (case-insensitive).",
				Optional:    true,
				Validators:  []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"user_id": schema.StringAttribute{
				Description: "Only return keys bound to this user ID.",
				Optional:    true,
				Validators:  []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"resource_id": schema.Int64Attribute{
				Description: "Only return keys that may call this public resource (including keys allowed on all resources).",
				Optional:    true,
			},
			"virtual_api_keys": schema.ListNestedAttribute{
				Description: "List of virtual API keys, ordered by name.",
				Computed:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":            schema.StringAttribute{Description: "The virtual API key ID.", Computed: true},
						"name":          schema.StringAttribute{Description: "The key name, or `null`.", Computed: true},
						"description":   schema.StringAttribute{Description: "The key description, or `null`.", Computed: true},
						"user_id":       schema.StringAttribute{Description: "The user the key is bound to, or `null`.", Computed: true},
						"last_chars":    schema.StringAttribute{Description: "The last four characters of the secret.", Computed: true},
						"all_resources": schema.BoolAttribute{Description: "Whether the key may call every public inference resource.", Computed: true},
						"resource_ids":  schema.ListAttribute{Description: "IDs of the resources the key may call (empty when `all_resources` is true).", Computed: true, ElementType: types.Int64Type},
						"expires_at":    schema.Int64Attribute{Description: "Expiry (epoch milliseconds), `null` if it never expires.", Computed: true},
						"last_used_at":  schema.Int64Attribute{Description: "Last use (epoch milliseconds), `null` if never used.", Computed: true},
						"created_at":    schema.Int64Attribute{Description: "Creation timestamp (epoch milliseconds).", Computed: true},
					},
				},
			},
		},
	}
}

func (d *VirtualAPIKeysDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *VirtualAPIKeysDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg VirtualAPIKeysDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var resourceID *int
	if !cfg.ResourceID.IsNull() {
		v := int(cfg.ResourceID.ValueInt64())
		resourceID = &v
	}
	keys, err := d.client.ListVirtualAPIKeys(ctx, cfg.Search.ValueString(), cfg.UserID.ValueString(), resourceID)
	if err != nil {
		resp.Diagnostics.AddError("Failed to list virtual API keys", err.Error())
		return
	}
	cfg.VirtualKeys = []VirtualAPIKeyItemModel{}
	for _, k := range keys {
		ids := make([]int64, 0, len(k.ResourceIDs))
		for _, id := range k.ResourceIDs {
			ids = append(ids, int64(id))
		}
		idList, diags := types.ListValueFrom(ctx, types.Int64Type, ids)
		resp.Diagnostics.Append(diags...)
		cfg.VirtualKeys = append(cfg.VirtualKeys, VirtualAPIKeyItemModel{
			ID:           types.StringValue(k.VirtualAPIKeyID),
			Name:         tfconv.StringFromPtr(k.Name),
			Description:  tfconv.StringFromPtr(k.Description),
			UserID:       tfconv.StringFromPtr(k.UserID),
			LastChars:    types.StringValue(k.LastChars),
			AllResources: types.BoolValue(k.AllResources),
			ResourceIDs:  idList,
			ExpiresAt:    tfconv.Int64FromInt64Ptr(k.ExpiresAt),
			LastUsedAt:   tfconv.Int64FromInt64Ptr(k.LastUsedAt),
			CreatedAt:    types.Int64Value(k.CreatedAt),
		})
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &cfg)...)
}
