package resources

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stackopshq/terraform-provider-pangolin/internal/client"
)

// alertRuleTestPlan returns a plan with every attribute set the way
// the framework hands it to Create (defaults applied).
func alertRuleTestPlan(t *testing.T) AlertRuleResourceModel {
	t.Helper()
	ctx := context.Background()
	webhooks, d := types.ListValueFrom(ctx, types.ObjectType{AttrTypes: alertWebhookActionAttrTypes}, []alertWebhookActionModel{
		{
			URL:               types.StringValue("https://hooks.example.com/a"),
			Enabled:           types.BoolValue(true),
			Method:            types.StringValue("PUT"),
			AuthType:          types.StringValue("bearer"),
			BearerToken:       types.StringValue("tok"),
			BasicCredentials:  types.StringNull(),
			CustomHeaderName:  types.StringNull(),
			CustomHeaderValue: types.StringNull(),
			Headers: types.MapValueMust(types.StringType, map[string]attr.Value{
				"X-B": types.StringValue("2"),
				"X-A": types.StringValue("1"),
			}),
			BodyTemplate: types.StringValue(`{"e":"{{event}}"}`),
		},
		{
			URL:               types.StringValue("https://hooks.example.com/b"),
			Enabled:           types.BoolValue(false),
			Method:            types.StringValue("POST"),
			AuthType:          types.StringValue("none"),
			BearerToken:       types.StringNull(),
			BasicCredentials:  types.StringNull(),
			CustomHeaderName:  types.StringNull(),
			CustomHeaderValue: types.StringNull(),
			Headers:           types.MapNull(types.StringType),
			BodyTemplate:      types.StringNull(),
		},
	})
	if d.HasError() {
		t.Fatalf("build webhooks: %v", d)
	}
	return AlertRuleResourceModel{
		ID:              types.Int64Unknown(),
		Name:            types.StringValue("db unhealthy"),
		EventType:       types.StringValue("health_check_unhealthy"),
		Enabled:         types.BoolValue(true),
		CooldownSeconds: types.Int64Value(300),
		SiteIDs:         types.SetNull(types.Int64Type),
		AllSites:        types.BoolValue(false),
		HealthCheckIDs:  types.SetValueMust(types.Int64Type, []attr.Value{types.Int64Value(4), types.Int64Value(3)}),
		AllHealthChecks: types.BoolValue(false),
		ResourceIDs:     types.SetNull(types.Int64Type),
		AllResources:    types.BoolValue(false),
		UserIDs:         types.SetValueMust(types.StringType, []attr.Value{types.StringValue("u1")}),
		RoleIDs:         types.SetValueMust(types.Int64Type, []attr.Value{types.Int64Value(9)}),
		Emails:          types.SetValueMust(types.StringType, []attr.Value{types.StringValue("ops@example.com")}),
		WebhookActions:  webhooks,
		CreatedAt:       types.Int64Unknown(),
	}
}

// serverEcho turns a request body into the GET payload the server
// would return for it: config decoded back to an object, one
// recipient row per user / role / email.
func serverEcho(t *testing.T, id int, body *client.AlertRuleRequest) *client.AlertRuleDetail {
	t.Helper()
	got := &client.AlertRuleDetail{
		AlertRule: client.AlertRule{
			AlertRuleID:     id,
			Name:            body.Name,
			EventType:       body.EventType,
			Enabled:         body.Enabled,
			CooldownSeconds: body.CooldownSeconds,
			CreatedAt:       1700000000000,
			UpdatedAt:       1700000000000,
			SiteIDs:         []int{},
			HealthCheckIDs:  []int{},
			ResourceIDs:     []int{},
		},
	}
	if !body.AllSites {
		got.SiteIDs = body.SiteIDs
	}
	if !body.AllHealthChecks {
		got.HealthCheckIDs = body.HealthCheckIDs
	}
	if !body.AllResources {
		got.ResourceIDs = body.ResourceIDs
	}
	for _, u := range body.UserIDs {
		got.Recipients = append(got.Recipients, client.AlertRuleRecipient{UserID: &u})
	}
	for _, r := range body.RoleIDs {
		got.Recipients = append(got.Recipients, client.AlertRuleRecipient{RoleID: &r})
	}
	for _, e := range body.Emails {
		got.Recipients = append(got.Recipients, client.AlertRuleRecipient{Email: &e})
	}
	for i, w := range body.WebhookActions {
		var cfg *client.AlertWebhookConfig
		if w.Config != nil {
			cfg = &client.AlertWebhookConfig{}
			if err := json.Unmarshal([]byte(*w.Config), cfg); err != nil {
				t.Fatalf("webhook config is not JSON: %v", err)
			}
		}
		got.WebhookActions = append(got.WebhookActions, client.AlertWebhookAction{
			WebhookActionID: i + 1,
			WebhookURL:      w.WebhookURL,
			Enabled:         w.Enabled,
			Config:          cfg,
		})
	}
	return got
}

func TestBuildAlertRuleRequest_FullPlan(t *testing.T) {
	body, diags := buildAlertRuleRequest(context.Background(), alertRuleTestPlan(t))
	if diags.HasError() {
		t.Fatalf("diags: %v", diags)
	}
	if body.Name != "db unhealthy" || body.EventType != "health_check_unhealthy" || body.CooldownSeconds != 300 || !body.Enabled {
		t.Errorf("scalars = %+v", body)
	}
	if len(body.HealthCheckIDs) != 2 || body.HealthCheckIDs[0] != 3 || body.HealthCheckIDs[1] != 4 {
		t.Errorf("HealthCheckIDs = %v, want sorted [3 4]", body.HealthCheckIDs)
	}
	if body.SiteIDs == nil || len(body.SiteIDs) != 0 || body.ResourceIDs == nil {
		t.Errorf("unset source lists must be empty, not nil: %v / %v", body.SiteIDs, body.ResourceIDs)
	}
	if len(body.UserIDs) != 1 || len(body.RoleIDs) != 1 || body.RoleIDs[0] != 9 || len(body.Emails) != 1 {
		t.Errorf("recipients = %v %v %v", body.UserIDs, body.RoleIDs, body.Emails)
	}
	if len(body.WebhookActions) != 2 {
		t.Fatalf("webhooks = %+v", body.WebhookActions)
	}

	var cfg map[string]any
	if err := json.Unmarshal([]byte(*body.WebhookActions[0].Config), &cfg); err != nil {
		t.Fatalf("config: %v", err)
	}
	if cfg["authType"] != "bearer" || cfg["bearerToken"] != "tok" || cfg["method"] != "PUT" ||
		cfg["useBodyTemplate"] != true || cfg["bodyTemplate"] != `{"e":"{{event}}"}` {
		t.Errorf("config = %v", cfg)
	}
	headers, _ := cfg["headers"].([]any)
	if len(headers) != 2 || headers[0].(map[string]any)["key"] != "X-A" {
		t.Errorf("headers must be sorted by key: %v", cfg["headers"])
	}
	if _, ok := cfg["basicCredentials"]; ok {
		t.Errorf("unset credentials must be omitted: %v", cfg)
	}

	second := *body.WebhookActions[1].Config
	if body.WebhookActions[1].Enabled || strings.Contains(second, "useBodyTemplate") || strings.Contains(second, "headers") {
		t.Errorf("second webhook = %+v %s", body.WebhookActions[1], second)
	}
}

func TestAlertRuleToModel_RoundTripHasNoDiff(t *testing.T) {
	ctx := context.Background()
	plan := alertRuleTestPlan(t)
	body, diags := buildAlertRuleRequest(ctx, plan)
	if diags.HasError() {
		t.Fatalf("diags: %v", diags)
	}

	m, diags := alertRuleToModel(ctx, serverEcho(t, 17, body), plan)
	if diags.HasError() {
		t.Fatalf("diags: %v", diags)
	}
	if m.ID.ValueInt64() != 17 || m.CreatedAt.ValueInt64() != 1700000000000 {
		t.Errorf("ID / CreatedAt = %v / %v", m.ID, m.CreatedAt)
	}
	checks := []struct {
		name      string
		got, want attr.Value
	}{
		{"name", m.Name, plan.Name},
		{"event_type", m.EventType, plan.EventType},
		{"enabled", m.Enabled, plan.Enabled},
		{"cooldown_seconds", m.CooldownSeconds, plan.CooldownSeconds},
		{"site_ids", m.SiteIDs, plan.SiteIDs},
		{"all_sites", m.AllSites, plan.AllSites},
		{"health_check_ids", m.HealthCheckIDs, plan.HealthCheckIDs},
		{"all_health_checks", m.AllHealthChecks, plan.AllHealthChecks},
		{"resource_ids", m.ResourceIDs, plan.ResourceIDs},
		{"all_resources", m.AllResources, plan.AllResources},
		{"user_ids", m.UserIDs, plan.UserIDs},
		{"role_ids", m.RoleIDs, plan.RoleIDs},
		{"emails", m.Emails, plan.Emails},
		{"webhook_actions", m.WebhookActions, plan.WebhookActions},
	}
	for _, c := range checks {
		if !c.got.Equal(c.want) {
			t.Errorf("%s drifted:\n got  %v\n want %v", c.name, c.got, c.want)
		}
	}
}

func TestAlertRuleToModel_EmptyListsKeepPriorShape(t *testing.T) {
	got := &client.AlertRuleDetail{AlertRule: client.AlertRule{AlertRuleID: 1, EventType: "site_online", SiteIDs: []int{}}}
	prior := AlertRuleResourceModel{
		SiteIDs:        types.SetValueMust(types.Int64Type, []attr.Value{}),
		HealthCheckIDs: types.SetNull(types.Int64Type),
		ResourceIDs:    types.SetNull(types.Int64Type),
		UserIDs:        types.SetValueMust(types.StringType, []attr.Value{}),
		RoleIDs:        types.SetNull(types.Int64Type),
		Emails:         types.SetNull(types.StringType),
		WebhookActions: types.ListNull(types.ObjectType{AttrTypes: alertWebhookActionAttrTypes}),
		AllSites:       types.BoolValue(true),
	}
	m, diags := alertRuleToModel(context.Background(), got, prior)
	if diags.HasError() {
		t.Fatalf("diags: %v", diags)
	}
	if m.SiteIDs.IsNull() || len(m.SiteIDs.Elements()) != 0 {
		t.Errorf("explicit [] site_ids must stay [], got %v", m.SiteIDs)
	}
	if m.UserIDs.IsNull() {
		t.Errorf("explicit [] user_ids must stay [], got null")
	}
	if !m.HealthCheckIDs.IsNull() || !m.Emails.IsNull() || !m.WebhookActions.IsNull() {
		t.Errorf("unset collections must stay null: %v %v %v", m.HealthCheckIDs, m.Emails, m.WebhookActions)
	}
}

func TestAlertRuleToModel_AllFlagsPriorAndImport(t *testing.T) {
	got := &client.AlertRuleDetail{AlertRule: client.AlertRule{AlertRuleID: 1, EventType: "resource_toggle"}}

	// Import: prior is null, the flag is inferred from the empty IDs.
	m, _ := alertRuleToModel(context.Background(), got, AlertRuleResourceModel{})
	if !m.AllResources.ValueBool() || m.AllSites.ValueBool() || m.AllHealthChecks.ValueBool() {
		t.Errorf("import inference = %v %v %v", m.AllSites, m.AllHealthChecks, m.AllResources)
	}

	// Refresh: a known prior value wins, the server never returns it.
	m, _ = alertRuleToModel(context.Background(), got, AlertRuleResourceModel{
		AllSites:        types.BoolValue(false),
		AllHealthChecks: types.BoolValue(false),
		AllResources:    types.BoolValue(false),
	})
	if m.AllResources.ValueBool() {
		t.Errorf("prior all_resources = false must be kept")
	}
}

func TestAlertRuleToModel_WebhookWithoutConfigUsesDefaults(t *testing.T) {
	got := &client.AlertRuleDetail{
		AlertRule: client.AlertRule{AlertRuleID: 1, EventType: "site_online"},
		WebhookActions: []client.AlertWebhookAction{
			{WebhookActionID: 1, WebhookURL: "https://x.example.com", Enabled: true},
		},
	}
	m, diags := alertRuleToModel(context.Background(), got, AlertRuleResourceModel{})
	if diags.HasError() {
		t.Fatalf("diags: %v", diags)
	}
	var actions []alertWebhookActionModel
	m.WebhookActions.ElementsAs(context.Background(), &actions, false)
	if len(actions) != 1 || actions[0].Method.ValueString() != "POST" || actions[0].AuthType.ValueString() != "none" {
		t.Errorf("actions = %+v", actions)
	}
	if !actions[0].BearerToken.IsNull() || !actions[0].Headers.IsNull() || !actions[0].BodyTemplate.IsNull() {
		t.Errorf("optional webhook fields must be null: %+v", actions[0])
	}
}

func TestValidateAlertRuleSources(t *testing.T) {
	ids := func(v ...int64) types.Set {
		vals := make([]attr.Value, 0, len(v))
		for _, i := range v {
			vals = append(vals, types.Int64Value(i))
		}
		return types.SetValueMust(types.Int64Type, vals)
	}
	base := func(event string) AlertRuleResourceModel {
		return AlertRuleResourceModel{
			EventType:       types.StringValue(event),
			SiteIDs:         types.SetNull(types.Int64Type),
			HealthCheckIDs:  types.SetNull(types.Int64Type),
			ResourceIDs:     types.SetNull(types.Int64Type),
			AllSites:        types.BoolNull(),
			AllHealthChecks: types.BoolNull(),
			AllResources:    types.BoolNull(),
		}
	}

	cases := []struct {
		name    string
		mutate  func(*AlertRuleResourceModel)
		event   string
		wantErr string
	}{
		{"site ids ok", func(m *AlertRuleResourceModel) { m.SiteIDs = ids(1) }, "site_offline", ""},
		{"all sites ok", func(m *AlertRuleResourceModel) { m.AllSites = types.BoolValue(true) }, "site_offline", ""},
		{"site source missing", func(m *AlertRuleResourceModel) {}, "site_offline", "need at least one ID in `site_ids`"},
		{"ids with all", func(m *AlertRuleResourceModel) { m.SiteIDs = ids(1); m.AllSites = types.BoolValue(true) }, "site_online", "must not be set when `all_sites` is true"},
		{"wrong family ids", func(m *AlertRuleResourceModel) { m.HealthCheckIDs = ids(2); m.SiteIDs = ids(1) }, "site_online", "`health_check_ids` must not be set for site event types"},
		{"wrong family all", func(m *AlertRuleResourceModel) { m.ResourceIDs = ids(1); m.AllSites = types.BoolValue(true) }, "resource_toggle", "`all_sites` only applies to site event types"},
		{"health check ok", func(m *AlertRuleResourceModel) { m.HealthCheckIDs = ids(3) }, "health_check_toggle", ""},
		{"unknown ids skipped", func(m *AlertRuleResourceModel) { m.ResourceIDs = types.SetUnknown(types.Int64Type) }, "resource_degraded", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := base(c.event)
			c.mutate(&m)
			var diags diag.Diagnostics
			validateAlertRuleSources(m, &diags)
			if c.wantErr == "" {
				if diags.HasError() {
					t.Errorf("unexpected diags: %v", diags)
				}
				return
			}
			if !diags.HasError() || !strings.Contains(diags.Errors()[0].Detail(), c.wantErr) {
				t.Errorf("diags = %v, want error containing %q", diags, c.wantErr)
			}
		})
	}
}

func TestValidateAlertRuleWebhooks_MissingCredential(t *testing.T) {
	plan := alertRuleTestPlan(t)
	var actions []alertWebhookActionModel
	plan.WebhookActions.ElementsAs(context.Background(), &actions, false)
	actions[0].BearerToken = types.StringNull()
	plan.WebhookActions = types.ListValueMust(types.ObjectType{AttrTypes: alertWebhookActionAttrTypes}, []attr.Value{
		types.ObjectValueMust(alertWebhookActionAttrTypes, map[string]attr.Value{
			"url": actions[0].URL, "enabled": actions[0].Enabled, "method": actions[0].Method,
			"auth_type": actions[0].AuthType, "bearer_token": actions[0].BearerToken,
			"basic_credentials": actions[0].BasicCredentials, "custom_header_name": actions[0].CustomHeaderName,
			"custom_header_value": actions[0].CustomHeaderValue, "headers": actions[0].Headers,
			"body_template": actions[0].BodyTemplate,
		}),
	})

	var diags diag.Diagnostics
	validateAlertRuleWebhooks(context.Background(), plan, &diags)
	if !diags.HasError() || !strings.Contains(diags.Errors()[0].Detail(), "`bearer_token` is required") {
		t.Errorf("diags = %v", diags)
	}
}

func TestValidateAlertRuleWebhooks_CustomNeedsValue(t *testing.T) {
	plan := alertRuleTestPlan(t)
	var actions []alertWebhookActionModel
	plan.WebhookActions.ElementsAs(context.Background(), &actions, false)
	a := actions[0]
	a.AuthType = types.StringValue("custom")
	a.BearerToken = types.StringNull()
	a.CustomHeaderName = types.StringValue("X-Api-Key")
	plan.WebhookActions = types.ListValueMust(types.ObjectType{AttrTypes: alertWebhookActionAttrTypes}, []attr.Value{
		types.ObjectValueMust(alertWebhookActionAttrTypes, map[string]attr.Value{
			"url": a.URL, "enabled": a.Enabled, "method": a.Method,
			"auth_type": a.AuthType, "bearer_token": a.BearerToken,
			"basic_credentials": a.BasicCredentials, "custom_header_name": a.CustomHeaderName,
			"custom_header_value": a.CustomHeaderValue, "headers": a.Headers,
			"body_template": a.BodyTemplate,
		}),
	})

	var diags diag.Diagnostics
	validateAlertRuleWebhooks(context.Background(), plan, &diags)
	if len(diags.Errors()) != 1 || !strings.Contains(diags.Errors()[0].Detail(), "`custom_header_value` is required") {
		t.Errorf("diags = %v", diags)
	}
}

func TestAlertRuleToModel_NullConfigKeepsPriorWebhook(t *testing.T) {
	ctx := context.Background()
	plan := alertRuleTestPlan(t)
	body, diags := buildAlertRuleRequest(ctx, plan)
	if diags.HasError() {
		t.Fatalf("diags: %v", diags)
	}
	got := serverEcho(t, 17, body)
	// The server could not decrypt the first webhook's config.
	got.WebhookActions[0].Config = nil

	m, diags := alertRuleToModel(ctx, got, plan)
	if diags.HasError() {
		t.Fatalf("diags: %v", diags)
	}
	if !m.WebhookActions.Equal(plan.WebhookActions) {
		t.Errorf("webhook_actions drifted:\n got  %v\n want %v", m.WebhookActions, plan.WebhookActions)
	}
	if len(diags.Warnings()) != 1 {
		t.Errorf("want one warning, got %v", diags)
	}
}
