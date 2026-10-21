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

var _ datasource.DataSource = &AlertRuleDataSource{}

// AlertRuleDataSource looks up one alert rule by ID, with its
// recipients and webhook actions.
type AlertRuleDataSource struct {
	client *client.Client
}

// AlertRuleWebhookModel is one webhook action of
// `pangolin_alert_rule`, with its config document flattened.
type AlertRuleWebhookModel struct {
	ID                types.Int64  `tfsdk:"id"`
	URL               types.String `tfsdk:"url"`
	Enabled           types.Bool   `tfsdk:"enabled"`
	LastSentAt        types.Int64  `tfsdk:"last_sent_at"`
	Method            types.String `tfsdk:"method"`
	AuthType          types.String `tfsdk:"auth_type"`
	BearerToken       types.String `tfsdk:"bearer_token"`
	BasicCredentials  types.String `tfsdk:"basic_credentials"`
	CustomHeaderName  types.String `tfsdk:"custom_header_name"`
	CustomHeaderValue types.String `tfsdk:"custom_header_value"`
	Headers           types.Map    `tfsdk:"headers"`
	BodyTemplate      types.String `tfsdk:"body_template"`
}

// AlertRuleDataSourceModel mirrors [client.AlertRuleDetail].
type AlertRuleDataSourceModel struct {
	AlertRuleItemModel
	UserIDs        types.List              `tfsdk:"user_ids"`
	RoleIDs        types.List              `tfsdk:"role_ids"`
	Emails         types.List              `tfsdk:"emails"`
	WebhookActions []AlertRuleWebhookModel `tfsdk:"webhook_actions"`
}

// NewAlertRuleDataSource returns a new data source factory.
func NewAlertRuleDataSource() datasource.DataSource {
	return &AlertRuleDataSource{}
}

func (d *AlertRuleDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_alert_rule"
}

func (d *AlertRuleDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	attrs := alertRuleCommonAttributes()
	attrs["id"] = schema.Int64Attribute{Description: "The numeric alert rule ID to look up.", Required: true}
	attrs["user_ids"] = schema.ListAttribute{Description: "IDs of the users notified by email.", Computed: true, ElementType: types.StringType}
	attrs["role_ids"] = schema.ListAttribute{Description: "IDs of the roles whose members are notified by email.", Computed: true, ElementType: types.Int64Type}
	attrs["emails"] = schema.ListAttribute{Description: "External email addresses notified by email.", Computed: true, ElementType: types.StringType}
	attrs["webhook_actions"] = schema.ListNestedAttribute{
		Description: "Webhooks called when the rule fires, in creation order.",
		Computed:    true,
		NestedObject: schema.NestedAttributeObject{
			Attributes: map[string]schema.Attribute{
				"id":                  schema.Int64Attribute{Description: "Numeric webhook action ID.", Computed: true},
				"url":                 schema.StringAttribute{Description: "The webhook URL.", Computed: true},
				"enabled":             schema.BoolAttribute{Description: "Whether the webhook is called.", Computed: true},
				"last_sent_at":        schema.Int64Attribute{Description: "Last successful call (epoch milliseconds), null if never.", Computed: true},
				"method":              schema.StringAttribute{Description: "HTTP method (`POST` when the config does not set one).", Computed: true},
				"auth_type":           schema.StringAttribute{Description: "Authentication: `none`, `bearer`, `basic` or `custom`.", Computed: true},
				"bearer_token":        schema.StringAttribute{Description: "Bearer token.", Computed: true, Sensitive: true},
				"basic_credentials":   schema.StringAttribute{Description: "`username:password` basic credentials.", Computed: true, Sensitive: true},
				"custom_header_name":  schema.StringAttribute{Description: "Custom auth header name.", Computed: true},
				"custom_header_value": schema.StringAttribute{Description: "Custom auth header value.", Computed: true, Sensitive: true},
				"headers":             schema.MapAttribute{Description: "Extra headers, keyed by header name.", Computed: true, Sensitive: true, ElementType: types.StringType},
				"body_template":       schema.StringAttribute{Description: "Custom request body template, null when the default payload is used.", Computed: true},
			},
		},
	}

	resp.Schema = schema.Schema{
		Description: "Looks up a single alert rule by ID, including its email recipients and webhook " +
			"actions.\n\n" +
			"> **Note:** Webhook credentials are returned in clear by the server and end up in the " +
			"Terraform state (marked sensitive).\n\n" + alertRulesEnterpriseNote,
		Attributes: attrs,
	}
}

func (d *AlertRuleDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *AlertRuleDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config AlertRuleDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	got, err := d.client.GetAlertRule(ctx, int(config.ID.ValueInt64()))
	if err != nil {
		resp.Diagnostics.AddError("Failed to read alert rule", err.Error())
		return
	}

	state := alertRuleDetailToModel(ctx, got, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// alertRuleDetailToModel maps a [client.AlertRuleDetail] to the data
// source model. Recipient lists are empty (not null) when unset.
func alertRuleDetailToModel(ctx context.Context, got *client.AlertRuleDetail, diags *diag.Diagnostics) AlertRuleDataSourceModel {
	userIDs := []string{}
	roleIDs := []int64{}
	emails := []string{}
	for _, r := range got.Recipients {
		switch {
		case r.UserID != nil:
			userIDs = append(userIDs, *r.UserID)
		case r.RoleID != nil:
			roleIDs = append(roleIDs, int64(*r.RoleID))
		case r.Email != nil:
			emails = append(emails, *r.Email)
		}
	}

	m := AlertRuleDataSourceModel{
		AlertRuleItemModel: alertRuleItemFromClient(ctx, got.AlertRule, diags),
		WebhookActions:     []AlertRuleWebhookModel{},
	}
	var d diag.Diagnostics
	m.UserIDs, d = types.ListValueFrom(ctx, types.StringType, userIDs)
	diags.Append(d...)
	m.RoleIDs, d = types.ListValueFrom(ctx, types.Int64Type, roleIDs)
	diags.Append(d...)
	m.Emails, d = types.ListValueFrom(ctx, types.StringType, emails)
	diags.Append(d...)

	for _, w := range got.WebhookActions {
		wm := AlertRuleWebhookModel{
			ID:                types.Int64Value(int64(w.WebhookActionID)),
			URL:               types.StringValue(w.WebhookURL),
			Enabled:           types.BoolValue(w.Enabled),
			LastSentAt:        tfconv.Int64FromInt64Ptr(w.LastSentAt),
			Method:            types.StringValue("POST"),
			AuthType:          types.StringValue("none"),
			BearerToken:       types.StringNull(),
			BasicCredentials:  types.StringNull(),
			CustomHeaderName:  types.StringNull(),
			CustomHeaderValue: types.StringNull(),
			Headers:           types.MapNull(types.StringType),
			BodyTemplate:      types.StringNull(),
		}
		if cfg := w.Config; cfg != nil {
			if cfg.AuthType != "" {
				wm.AuthType = types.StringValue(cfg.AuthType)
			}
			if cfg.Method != nil && *cfg.Method != "" {
				wm.Method = types.StringValue(*cfg.Method)
			}
			wm.BearerToken = tfconv.StringFromNonEmptyPtr(cfg.BearerToken)
			wm.BasicCredentials = tfconv.StringFromNonEmptyPtr(cfg.BasicCredentials)
			wm.CustomHeaderName = tfconv.StringFromNonEmptyPtr(cfg.CustomHeaderName)
			wm.CustomHeaderValue = tfconv.StringFromNonEmptyPtr(cfg.CustomHeaderValue)
			if cfg.UseBodyTemplate != nil && *cfg.UseBodyTemplate {
				wm.BodyTemplate = tfconv.StringFromNonEmptyPtr(cfg.BodyTemplate)
			}
			headers := map[string]string{}
			for _, h := range cfg.Headers {
				headers[h.Key] = h.Value
			}
			wm.Headers, d = types.MapValueFrom(ctx, types.StringType, headers)
			diags.Append(d...)
		}
		m.WebhookActions = append(m.WebhookActions, wm)
	}
	return m
}
