package datasources

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stackopshq/terraform-provider-pangolin/internal/client"
	"github.com/stackopshq/terraform-provider-pangolin/internal/tfconv"
)

var _ datasource.DataSource = &AlertRulesDataSource{}

// alertRulesEnterpriseNote is appended to the alert rule data source
// descriptions.
const alertRulesEnterpriseNote = "> **Note:** Requires Pangolin Enterprise Edition on self-host (or Pangolin Cloud). " +
	"Community Edition servers answer 404 on the alert rule endpoints."

// AlertRulesDataSource lists the alert rules of the org.
type AlertRulesDataSource struct {
	client *client.Client
}

// AlertRuleItemModel mirrors [client.AlertRule].
type AlertRuleItemModel struct {
	ID              types.Int64  `tfsdk:"id"`
	Name            types.String `tfsdk:"name"`
	EventType       types.String `tfsdk:"event_type"`
	Enabled         types.Bool   `tfsdk:"enabled"`
	CooldownSeconds types.Int64  `tfsdk:"cooldown_seconds"`
	LastTriggeredAt types.Int64  `tfsdk:"last_triggered_at"`
	CreatedAt       types.Int64  `tfsdk:"created_at"`
	UpdatedAt       types.Int64  `tfsdk:"updated_at"`
	SiteIDs         types.List   `tfsdk:"site_ids"`
	HealthCheckIDs  types.List   `tfsdk:"health_check_ids"`
	ResourceIDs     types.List   `tfsdk:"resource_ids"`
}

// AlertRulesDataSourceModel is the top-level data source shape.
type AlertRulesDataSourceModel struct {
	Query      types.String         `tfsdk:"query"`
	Enabled    types.Bool           `tfsdk:"enabled"`
	AlertRules []AlertRuleItemModel `tfsdk:"alert_rules"`
}

// NewAlertRulesDataSource returns a new data source factory.
func NewAlertRulesDataSource() datasource.DataSource {
	return &AlertRulesDataSource{}
}

func (d *AlertRulesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_alert_rules"
}

// alertRuleCommonAttributes returns the computed attributes shared by
// `pangolin_alert_rules` items and `pangolin_alert_rule`, minus `id`.
func alertRuleCommonAttributes() map[string]schema.Attribute {
	return map[string]schema.Attribute{
		"name":              schema.StringAttribute{Description: "The alert rule name.", Computed: true},
		"event_type":        schema.StringAttribute{Description: "The event that triggers the rule (e.g. `site_offline`, `health_check_unhealthy`, `resource_degraded`).", Computed: true},
		"enabled":           schema.BoolAttribute{Description: "Whether the rule is enabled.", Computed: true},
		"cooldown_seconds":  schema.Int64Attribute{Description: "Minimum delay in seconds between two notifications.", Computed: true},
		"last_triggered_at": schema.Int64Attribute{Description: "Last time the rule fired (epoch milliseconds), null if never.", Computed: true},
		"created_at":        schema.Int64Attribute{Description: "Creation timestamp (epoch milliseconds).", Computed: true},
		"updated_at":        schema.Int64Attribute{Description: "Last update timestamp (epoch milliseconds).", Computed: true},
		"site_ids":          schema.ListAttribute{Description: "IDs of the watched sites. Empty for non-site rules and for rules watching every site.", Computed: true, ElementType: types.Int64Type},
		"health_check_ids":  schema.ListAttribute{Description: "IDs of the watched health checks. Empty for non-health-check rules and for rules watching every health check.", Computed: true, ElementType: types.Int64Type},
		"resource_ids":      schema.ListAttribute{Description: "IDs of the watched resources. Empty for non-resource rules and for rules watching every resource.", Computed: true, ElementType: types.Int64Type},
	}
}

func (d *AlertRulesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	item := alertRuleCommonAttributes()
	item["id"] = schema.Int64Attribute{Description: "Numeric alert rule ID.", Computed: true}

	resp.Schema = schema.Schema{
		Description: "Lists the alert rules of the organization. Recipients and webhook actions are " +
			"not part of the list payload; read them with `pangolin_alert_rule`.\n\n" + alertRulesEnterpriseNote,
		Attributes: map[string]schema.Attribute{
			"query": schema.StringAttribute{
				Description: "Only return rules whose name contains this string (case-insensitive).",
				Optional:    true,
			},
			"enabled": schema.BoolAttribute{
				Description: "Only return enabled (`true`) or disabled (`false`) rules.",
				Optional:    true,
			},
			"alert_rules": schema.ListNestedAttribute{
				Description: "List of alert rules.",
				Computed:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: item,
				},
			},
		},
	}
}

func (d *AlertRulesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *AlertRulesDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config AlertRulesDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	q := client.AlertRuleListQuery{Query: config.Query.ValueString()}
	if !config.Enabled.IsNull() {
		q.Enabled = config.Enabled.ValueBoolPointer()
	}
	rules, err := d.client.ListAlertRules(ctx, q)
	if err != nil {
		resp.Diagnostics.AddError("Failed to list alert rules", err.Error())
		return
	}

	state := AlertRulesDataSourceModel{
		Query:      config.Query,
		Enabled:    config.Enabled,
		AlertRules: []AlertRuleItemModel{},
	}
	for _, r := range rules {
		state.AlertRules = append(state.AlertRules, alertRuleItemFromClient(r, &resp.Diagnostics))
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// alertRuleItemFromClient maps a [client.AlertRule] to its TF model.
func alertRuleItemFromClient(r client.AlertRule, diags *diag.Diagnostics) AlertRuleItemModel {
	return AlertRuleItemModel{
		ID:              types.Int64Value(int64(r.AlertRuleID)),
		Name:            types.StringValue(r.Name),
		EventType:       types.StringValue(r.EventType),
		Enabled:         types.BoolValue(r.Enabled),
		CooldownSeconds: types.Int64Value(r.CooldownSeconds),
		LastTriggeredAt: tfconv.Int64FromInt64Ptr(r.LastTriggeredAt),
		CreatedAt:       types.Int64Value(r.CreatedAt),
		UpdatedAt:       types.Int64Value(r.UpdatedAt),
		SiteIDs:         alertRuleIDList(r.SiteIDs, diags),
		HealthCheckIDs:  alertRuleIDList(r.HealthCheckIDs, diags),
		ResourceIDs:     alertRuleIDList(r.ResourceIDs, diags),
	}
}

// alertRuleIDList converts server IDs to a list of int64, empty (not
// null) when the server sends none.
func alertRuleIDList(ids []int, diags *diag.Diagnostics) types.List {
	vals := make([]int64, 0, len(ids))
	for _, id := range ids {
		vals = append(vals, int64(id))
	}
	lv, d := types.ListValueFrom(context.Background(), types.Int64Type, vals)
	diags.Append(d...)
	return lv
}
