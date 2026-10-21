package resources

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stackopshq/terraform-provider-pangolin/internal/client"
	"github.com/stackopshq/terraform-provider-pangolin/internal/tfconv"
)

var (
	_ resource.Resource                   = &AlertRuleResource{}
	_ resource.ResourceWithImportState    = &AlertRuleResource{}
	_ resource.ResourceWithValidateConfig = &AlertRuleResource{}
)

// Event families. The server derives the source of an alert rule
// from its event type: site events take site IDs, health check
// events take health check IDs, resource events take resource IDs.
var (
	alertRuleSiteEvents        = []string{"site_online", "site_offline", "site_toggle"}
	alertRuleHealthCheckEvents = []string{"health_check_healthy", "health_check_unhealthy", "health_check_toggle"}
	alertRuleResourceEvents    = []string{"resource_healthy", "resource_unhealthy", "resource_degraded", "resource_toggle"}
)

const (
	alertRuleFamilySite        = "site"
	alertRuleFamilyHealthCheck = "health check"
	alertRuleFamilyResource    = "resource"
)

// alertRuleEventFamily returns the source family of an event type, or
// "" when the event type is unknown.
func alertRuleEventFamily(eventType string) string {
	for _, family := range []struct {
		name   string
		events []string
	}{
		{alertRuleFamilySite, alertRuleSiteEvents},
		{alertRuleFamilyHealthCheck, alertRuleHealthCheckEvents},
		{alertRuleFamilyResource, alertRuleResourceEvents},
	} {
		for _, e := range family.events {
			if e == eventType {
				return family.name
			}
		}
	}
	return ""
}

// AlertRuleResource manages a Pangolin alert rule (Enterprise / Cloud).
type AlertRuleResource struct {
	client *client.Client
}

// AlertRuleResourceModel describes the resource data model.
type AlertRuleResourceModel struct {
	ID              types.Int64  `tfsdk:"id"`
	Name            types.String `tfsdk:"name"`
	EventType       types.String `tfsdk:"event_type"`
	Enabled         types.Bool   `tfsdk:"enabled"`
	CooldownSeconds types.Int64  `tfsdk:"cooldown_seconds"`
	SiteIDs         types.Set    `tfsdk:"site_ids"`
	AllSites        types.Bool   `tfsdk:"all_sites"`
	HealthCheckIDs  types.Set    `tfsdk:"health_check_ids"`
	AllHealthChecks types.Bool   `tfsdk:"all_health_checks"`
	ResourceIDs     types.Set    `tfsdk:"resource_ids"`
	AllResources    types.Bool   `tfsdk:"all_resources"`
	UserIDs         types.Set    `tfsdk:"user_ids"`
	RoleIDs         types.Set    `tfsdk:"role_ids"`
	Emails          types.Set    `tfsdk:"emails"`
	WebhookActions  types.List   `tfsdk:"webhook_actions"`
	CreatedAt       types.Int64  `tfsdk:"created_at"`
}

// alertWebhookActionModel is one element of `webhook_actions`. The
// auth, header, method and body template attributes are flattened
// from the JSON config document the server stores per action.
type alertWebhookActionModel struct {
	URL               types.String `tfsdk:"url"`
	Enabled           types.Bool   `tfsdk:"enabled"`
	Method            types.String `tfsdk:"method"`
	AuthType          types.String `tfsdk:"auth_type"`
	BearerToken       types.String `tfsdk:"bearer_token"`
	BasicCredentials  types.String `tfsdk:"basic_credentials"`
	CustomHeaderName  types.String `tfsdk:"custom_header_name"`
	CustomHeaderValue types.String `tfsdk:"custom_header_value"`
	Headers           types.Map    `tfsdk:"headers"`
	BodyTemplate      types.String `tfsdk:"body_template"`
}

// alertWebhookActionAttrTypes is the object type of a
// `webhook_actions` element.
var alertWebhookActionAttrTypes = map[string]attr.Type{
	"url":                 types.StringType,
	"enabled":             types.BoolType,
	"method":              types.StringType,
	"auth_type":           types.StringType,
	"bearer_token":        types.StringType,
	"basic_credentials":   types.StringType,
	"custom_header_name":  types.StringType,
	"custom_header_value": types.StringType,
	"headers":             types.MapType{ElemType: types.StringType},
	"body_template":       types.StringType,
}

// NewAlertRuleResource returns a new resource factory.
func NewAlertRuleResource() resource.Resource {
	return &AlertRuleResource{}
}

func (r *AlertRuleResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_alert_rule"
}

func (r *AlertRuleResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	allEvents := append(append(append([]string{}, alertRuleSiteEvents...), alertRuleHealthCheckEvents...), alertRuleResourceEvents...)

	resp.Schema = schema.Schema{
		Description: "Manages a Pangolin alert rule: an event on a site, health check or resource " +
			"that sends emails and / or calls webhooks.\n\n" +
			"The event type decides the source: `site_*` events take `site_ids` or " +
			"`all_sites`, `health_check_*` events take `health_check_ids` or " +
			"`all_health_checks`, `resource_*` events take `resource_ids` or `all_resources`.\n\n" +
			"> **Note:** Requires Pangolin Enterprise Edition on self-host (or Pangolin Cloud). " +
			"Community Edition servers answer 404 on the alert rule endpoints.\n\n" +
			"> **Note:** The server returns webhook credentials (`bearer_token`, " +
			"`basic_credentials`, `custom_header_value`) in clear on read, so they are " +
			"imported and refreshed like any other attribute. The Terraform state contains " +
			"sensitive material and must be protected accordingly.\n\n" +
			"> **Note:** The server does not return `all_sites`, `all_health_checks` or " +
			"`all_resources`. The provider keeps the configured value; on import it is " +
			"inferred as `true` when the rule has no source ID of its family.",
		Attributes: map[string]schema.Attribute{
			"id": schema.Int64Attribute{
				Description: "The numeric alert rule ID.",
				Computed:    true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Description: "The alert rule name.",
				Required:    true,
				Validators: []validator.String{
					stringvalidator.LengthBetween(1, 255),
				},
			},
			"event_type": schema.StringAttribute{
				Description: "The event that triggers the rule. Site events: `site_online`, `site_offline`, " +
					"`site_toggle`. Health check events: `health_check_healthy`, `health_check_unhealthy`, " +
					"`health_check_toggle`. Resource events: `resource_healthy`, `resource_unhealthy`, " +
					"`resource_degraded`, `resource_toggle`.",
				Required: true,
				Validators: []validator.String{
					stringvalidator.OneOf(allEvents...),
				},
			},
			"enabled": schema.BoolAttribute{
				Description: "Whether the rule is enabled. Defaults to `true`.",
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(true),
			},
			"cooldown_seconds": schema.Int64Attribute{
				Description: "Minimum delay in seconds between two notifications of the rule. Defaults to `0`.",
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(0),
				Validators: []validator.Int64{
					int64validator.AtLeast(0),
				},
			},
			"site_ids": schema.SetAttribute{
				Description: "IDs of the sites watched by a `site_*` rule. Required for site events unless `all_sites` is `true`.",
				Optional:    true,
				ElementType: types.Int64Type,
				Validators: []validator.Set{
					setvalidator.ValueInt64sAre(int64validator.AtLeast(1)),
				},
			},
			"all_sites": schema.BoolAttribute{
				Description: "Watch every site of the organization (site events only). Defaults to `false`.",
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
			},
			"health_check_ids": schema.SetAttribute{
				Description: "IDs of the health checks watched by a `health_check_*` rule. Required for " +
					"health check events unless `all_health_checks` is `true`.",
				Optional:    true,
				ElementType: types.Int64Type,
				Validators: []validator.Set{
					setvalidator.ValueInt64sAre(int64validator.AtLeast(1)),
				},
			},
			"all_health_checks": schema.BoolAttribute{
				Description: "Watch every health check of the organization (health check events only). Defaults to `false`.",
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
			},
			"resource_ids": schema.SetAttribute{
				Description: "IDs of the resources watched by a `resource_*` rule. Required for resource " +
					"events unless `all_resources` is `true`.",
				Optional:    true,
				ElementType: types.Int64Type,
				Validators: []validator.Set{
					setvalidator.ValueInt64sAre(int64validator.AtLeast(1)),
				},
			},
			"all_resources": schema.BoolAttribute{
				Description: "Watch every resource of the organization (resource events only). Defaults to `false`.",
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
			},
			"user_ids": schema.SetAttribute{
				Description: "IDs of the users notified by email.",
				Optional:    true,
				ElementType: types.StringType,
				Validators: []validator.Set{
					setvalidator.ValueStringsAre(stringvalidator.LengthAtLeast(1)),
				},
			},
			"role_ids": schema.SetAttribute{
				Description: "IDs of the roles whose members are notified by email.",
				Optional:    true,
				ElementType: types.Int64Type,
			},
			"emails": schema.SetAttribute{
				Description: "External email addresses notified by email.",
				Optional:    true,
				ElementType: types.StringType,
				Validators: []validator.Set{
					setvalidator.ValueStringsAre(stringvalidator.LengthAtLeast(3)),
				},
			},
			"webhook_actions": schema.ListNestedAttribute{
				Description: "Webhooks called when the rule fires.",
				Optional:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"url": schema.StringAttribute{
							Description: "The webhook URL.",
							Required:    true,
							Validators: []validator.String{
								stringvalidator.LengthAtLeast(1),
							},
						},
						"enabled": schema.BoolAttribute{
							Description: "Whether the webhook is called. Defaults to `true`.",
							Optional:    true,
							Computed:    true,
							Default:     booldefault.StaticBool(true),
						},
						"method": schema.StringAttribute{
							Description: "HTTP method: `GET`, `POST`, `PUT` or `PATCH`. Defaults to `POST`.",
							Optional:    true,
							Computed:    true,
							Default:     stringdefault.StaticString("POST"),
							Validators: []validator.String{
								stringvalidator.OneOf("GET", "POST", "PUT", "PATCH"),
							},
						},
						"auth_type": schema.StringAttribute{
							Description: "Authentication: `none`, `bearer` (uses `bearer_token`), `basic` (uses " +
								"`basic_credentials`) or `custom` (uses `custom_header_name` and " +
								"`custom_header_value`). Defaults to `none`.",
							Optional: true,
							Computed: true,
							Default:  stringdefault.StaticString("none"),
							Validators: []validator.String{
								stringvalidator.OneOf("none", "bearer", "basic", "custom"),
							},
						},
						"bearer_token": schema.StringAttribute{
							Description: "Token sent as `Authorization: Bearer <token>` when `auth_type` is `bearer`.",
							Optional:    true,
							Sensitive:   true,
							Validators: []validator.String{
								stringvalidator.LengthAtLeast(1),
							},
						},
						"basic_credentials": schema.StringAttribute{
							Description: "`username:password` sent as HTTP basic auth when `auth_type` is `basic`.",
							Optional:    true,
							Sensitive:   true,
							Validators: []validator.String{
								stringvalidator.LengthAtLeast(1),
							},
						},
						"custom_header_name": schema.StringAttribute{
							Description: "Header name used when `auth_type` is `custom`.",
							Optional:    true,
							Validators: []validator.String{
								stringvalidator.LengthAtLeast(1),
							},
						},
						"custom_header_value": schema.StringAttribute{
							Description: "Header value used when `auth_type` is `custom`.",
							Optional:    true,
							Sensitive:   true,
							Validators: []validator.String{
								stringvalidator.LengthAtLeast(1),
							},
						},
						"headers": schema.MapAttribute{
							Description: "Extra headers sent with every call, keyed by header name. " +
								"Sensitive, since extra headers often carry API keys.",
							Optional:    true,
							Sensitive:   true,
							ElementType: types.StringType,
						},
						"body_template": schema.StringAttribute{
							Description: "Custom request body. Supports the `{{event}}`, `{{timestamp}}`, " +
								"`{{status}}` and `{{data}}` placeholders. When unset, Pangolin sends its " +
								"default JSON payload.",
							Optional: true,
							Validators: []validator.String{
								stringvalidator.LengthAtLeast(1),
							},
						},
					},
				},
			},
			"created_at": schema.Int64Attribute{
				Description: "Creation timestamp (epoch milliseconds).",
				Computed:    true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *AlertRuleResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Resource Configure Type", "Expected *client.Client")
		return
	}
	r.client = c
}

// ValidateConfig mirrors the server-side checks on the source fields
// (which the server only reports as a 400 at apply time) and rejects
// combinations it accepts but silently ignores, which would otherwise
// show up as a perpetual diff.
func (r *AlertRuleResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var config AlertRuleResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	validateAlertRuleSources(config, &resp.Diagnostics)
	validateAlertRuleWebhooks(ctx, config, &resp.Diagnostics)
}

// validateAlertRuleSources checks the source attributes against the
// event family. Unknown values are skipped.
func validateAlertRuleSources(config AlertRuleResourceModel, diags *diag.Diagnostics) {
	if config.EventType.IsNull() || config.EventType.IsUnknown() {
		return
	}
	family := alertRuleEventFamily(config.EventType.ValueString())
	if family == "" {
		return
	}

	sources := []struct {
		family  string
		ids     types.Set
		idsName string
		all     types.Bool
		allName string
	}{
		{alertRuleFamilySite, config.SiteIDs, "site_ids", config.AllSites, "all_sites"},
		{alertRuleFamilyHealthCheck, config.HealthCheckIDs, "health_check_ids", config.AllHealthChecks, "all_health_checks"},
		{alertRuleFamilyResource, config.ResourceIDs, "resource_ids", config.AllResources, "all_resources"},
	}
	for _, s := range sources {
		idsSet := !s.ids.IsNull() && !s.ids.IsUnknown() && len(s.ids.Elements()) > 0
		allTrue := !s.all.IsNull() && !s.all.IsUnknown() && s.all.ValueBool()
		if s.family != family {
			if idsSet {
				diags.AddAttributeError(path.Root(s.idsName), "Invalid alert rule source",
					fmt.Sprintf("`%s` must not be set for %s event types.", s.idsName, family))
			}
			if allTrue {
				diags.AddAttributeError(path.Root(s.allName), "Invalid alert rule source",
					fmt.Sprintf("`%s` only applies to %s event types.", s.allName, s.family))
			}
			continue
		}
		if allTrue && idsSet {
			diags.AddAttributeError(path.Root(s.idsName), "Invalid alert rule source",
				fmt.Sprintf("`%s` must not be set when `%s` is true: the server ignores the IDs.", s.idsName, s.allName))
			continue
		}
		idsKnown := !s.ids.IsUnknown()
		allKnown := !s.all.IsUnknown()
		if idsKnown && allKnown && !idsSet && !allTrue {
			diags.AddAttributeError(path.Root(s.idsName), "Missing alert rule source",
				fmt.Sprintf("%s event types need at least one ID in `%s`, or `%s = true`.", family, s.idsName, s.allName))
		}
	}
}

// validateAlertRuleWebhooks checks that each webhook carries the
// credential its auth type needs. Unknown values are skipped.
func validateAlertRuleWebhooks(ctx context.Context, config AlertRuleResourceModel, diags *diag.Diagnostics) {
	if config.WebhookActions.IsNull() || config.WebhookActions.IsUnknown() {
		return
	}
	for _, e := range config.WebhookActions.Elements() {
		if e.IsUnknown() {
			return
		}
	}
	var actions []alertWebhookActionModel
	diags.Append(config.WebhookActions.ElementsAs(ctx, &actions, false)...)
	if diags.HasError() {
		return
	}
	type credential struct {
		name  string
		value types.String
	}
	for i, a := range actions {
		if a.AuthType.IsNull() || a.AuthType.IsUnknown() {
			continue
		}
		var required []credential
		switch a.AuthType.ValueString() {
		case "bearer":
			required = []credential{{"bearer_token", a.BearerToken}}
		case "basic":
			required = []credential{{"basic_credentials", a.BasicCredentials}}
		case "custom":
			// The server accepts a custom header without a value but then
			// sends it empty, so every call fails authentication.
			required = []credential{{"custom_header_name", a.CustomHeaderName}, {"custom_header_value", a.CustomHeaderValue}}
		}
		for _, c := range required {
			if c.value.IsNull() {
				diags.AddAttributeError(path.Root("webhook_actions").AtListIndex(i).AtName(c.name),
					"Missing webhook credential",
					fmt.Sprintf("`%s` is required when `auth_type` is `%s`.", c.name, a.AuthType.ValueString()))
			}
		}
	}
}

func (r *AlertRuleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan AlertRuleResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body, diags := buildAlertRuleRequest(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	id, err := r.client.CreateAlertRule(ctx, body)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create alert rule", alertRuleErrorDetail(err))
		return
	}

	got, err := r.client.GetAlertRule(ctx, id)
	if err != nil {
		// Keep the ID in state so the rule is not orphaned: Terraform
		// marks it tainted and replaces it on the next apply.
		plan.ID = types.Int64Value(int64(id))
		plan.CreatedAt = types.Int64Null()
		resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
		resp.Diagnostics.AddError("Failed to read alert rule after create", err.Error())
		return
	}
	state, diags := alertRuleToModel(ctx, got, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *AlertRuleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state AlertRuleResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	got, err := r.client.GetAlertRule(ctx, int(state.ID.ValueInt64()))
	if err != nil {
		if errors.Is(err, client.ErrNotFound) {
			// A 404 also comes from a Community Edition server, which has
			// no alert rule routes at all. Only drop the rule from state
			// when the list route answers.
			if _, listErr := r.client.ListAlertRules(ctx, client.AlertRuleListQuery{}); listErr != nil {
				resp.Diagnostics.AddError("Failed to read alert rule", alertRuleErrorDetail(listErr))
				return
			}
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read alert rule", err.Error())
		return
	}
	next, diags := alertRuleToModel(ctx, got, state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &next)...)
}

func (r *AlertRuleResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan AlertRuleResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body, diags := buildAlertRuleRequest(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := int(plan.ID.ValueInt64())
	if err := r.client.UpdateAlertRule(ctx, id, body); err != nil {
		resp.Diagnostics.AddError("Failed to update alert rule", alertRuleErrorDetail(err))
		return
	}

	got, err := r.client.GetAlertRule(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read alert rule after update", err.Error())
		return
	}
	state, diags := alertRuleToModel(ctx, got, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *AlertRuleResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state AlertRuleResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.DeleteAlertRule(ctx, int(state.ID.ValueInt64())); err != nil {
		if errors.Is(err, client.ErrNotFound) {
			return
		}
		resp.Diagnostics.AddError("Failed to delete alert rule", err.Error())
	}
}

func (r *AlertRuleResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	id, err := strconv.ParseInt(req.ID, 10, 64)
	if err != nil {
		resp.Diagnostics.AddError("Invalid ID", fmt.Sprintf("Cannot parse alert rule ID %q as integer", req.ID))
		return
	}

	// Read fills the rest: with a null prior it infers the all_* flags
	// and keeps unset collections null.
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
}

// alertRuleErrorDetail adds a version hint to a 404 on write: the
// alert rule routes only exist on Enterprise / Cloud builds.
func alertRuleErrorDetail(err error) string {
	if errors.Is(err, client.ErrNotFound) {
		return err.Error() + "\n\nAlert rules require Pangolin Enterprise Edition (or Pangolin Cloud); " +
			"Community Edition servers do not expose these endpoints."
	}
	return err.Error()
}

// buildAlertRuleRequest converts a plan into the full create / update
// body. Every list is sent, empty when unset, because the update
// endpoint replaces each child list it receives.
func buildAlertRuleRequest(ctx context.Context, plan AlertRuleResourceModel) (*client.AlertRuleRequest, diag.Diagnostics) {
	var diags diag.Diagnostics

	body := &client.AlertRuleRequest{
		Name:            plan.Name.ValueString(),
		EventType:       plan.EventType.ValueString(),
		Enabled:         plan.Enabled.ValueBool(),
		CooldownSeconds: plan.CooldownSeconds.ValueInt64(),
		AllSites:        plan.AllSites.ValueBool(),
		AllHealthChecks: plan.AllHealthChecks.ValueBool(),
		AllResources:    plan.AllResources.ValueBool(),
	}

	body.SiteIDs = int64SetToInts(ctx, plan.SiteIDs, &diags)
	body.HealthCheckIDs = int64SetToInts(ctx, plan.HealthCheckIDs, &diags)
	body.ResourceIDs = int64SetToInts(ctx, plan.ResourceIDs, &diags)
	body.RoleIDs = int64SetToInts(ctx, plan.RoleIDs, &diags)
	body.UserIDs = stringSetToSortedSlice(ctx, plan.UserIDs, &diags)
	body.Emails = stringSetToSortedSlice(ctx, plan.Emails, &diags)

	if !plan.WebhookActions.IsNull() && !plan.WebhookActions.IsUnknown() {
		var actions []alertWebhookActionModel
		diags.Append(plan.WebhookActions.ElementsAs(ctx, &actions, false)...)
		for _, a := range actions {
			cfg, d := buildAlertWebhookConfig(ctx, a)
			diags.Append(d...)
			raw, err := json.Marshal(cfg)
			if err != nil {
				diags.AddError("Failed to encode webhook config", err.Error())
				continue
			}
			encoded := string(raw)
			body.WebhookActions = append(body.WebhookActions, client.AlertWebhookActionRequest{
				WebhookURL: a.URL.ValueString(),
				Config:     &encoded,
				Enabled:    a.Enabled.ValueBool(),
			})
		}
	}
	return body, diags
}

// buildAlertWebhookConfig maps the flattened webhook attributes to the
// JSON config document, in the shape the Pangolin UI writes.
func buildAlertWebhookConfig(ctx context.Context, a alertWebhookActionModel) (*client.AlertWebhookConfig, diag.Diagnostics) {
	var diags diag.Diagnostics
	cfg := &client.AlertWebhookConfig{
		AuthType:          a.AuthType.ValueString(),
		BearerToken:       a.BearerToken.ValueStringPointer(),
		BasicCredentials:  a.BasicCredentials.ValueStringPointer(),
		CustomHeaderName:  a.CustomHeaderName.ValueStringPointer(),
		CustomHeaderValue: a.CustomHeaderValue.ValueStringPointer(),
		Method:            a.Method.ValueStringPointer(),
	}
	if cfg.AuthType == "" {
		cfg.AuthType = "none"
	}
	if !a.Headers.IsNull() && !a.Headers.IsUnknown() {
		var headers map[string]string
		diags.Append(a.Headers.ElementsAs(ctx, &headers, false)...)
		keys := make([]string, 0, len(headers))
		for k := range headers {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			cfg.Headers = append(cfg.Headers, client.AlertWebhookHeader{Key: k, Value: headers[k]})
		}
	}
	if !a.BodyTemplate.IsNull() && !a.BodyTemplate.IsUnknown() {
		useTemplate := true
		cfg.UseBodyTemplate = &useTemplate
		cfg.BodyTemplate = a.BodyTemplate.ValueStringPointer()
	}
	return cfg, diags
}

// alertRuleToModel maps a server alert rule onto the TF model. prior
// carries the values the server does not return (the all_* flags) and
// decides between null and empty for the optional collections.
func alertRuleToModel(ctx context.Context, got *client.AlertRuleDetail, prior AlertRuleResourceModel) (AlertRuleResourceModel, diag.Diagnostics) {
	var diags diag.Diagnostics

	m := AlertRuleResourceModel{
		ID:              types.Int64Value(int64(got.AlertRuleID)),
		Name:            types.StringValue(got.Name),
		EventType:       types.StringValue(got.EventType),
		Enabled:         types.BoolValue(got.Enabled),
		CooldownSeconds: types.Int64Value(got.CooldownSeconds),
		CreatedAt:       types.Int64Value(got.CreatedAt),
	}

	m.SiteIDs = intsToInt64Set(got.SiteIDs, prior.SiteIDs, &diags)
	m.HealthCheckIDs = intsToInt64Set(got.HealthCheckIDs, prior.HealthCheckIDs, &diags)
	m.ResourceIDs = intsToInt64Set(got.ResourceIDs, prior.ResourceIDs, &diags)

	family := alertRuleEventFamily(got.EventType)
	m.AllSites = alertRuleAllFlag(prior.AllSites, family == alertRuleFamilySite && len(got.SiteIDs) == 0)
	m.AllHealthChecks = alertRuleAllFlag(prior.AllHealthChecks, family == alertRuleFamilyHealthCheck && len(got.HealthCheckIDs) == 0)
	m.AllResources = alertRuleAllFlag(prior.AllResources, family == alertRuleFamilyResource && len(got.ResourceIDs) == 0)

	var userIDs, emails []string
	var roleIDs []int
	for _, rcpt := range got.Recipients {
		switch {
		case rcpt.UserID != nil:
			userIDs = append(userIDs, *rcpt.UserID)
		case rcpt.RoleID != nil:
			roleIDs = append(roleIDs, *rcpt.RoleID)
		case rcpt.Email != nil:
			emails = append(emails, *rcpt.Email)
		}
	}
	m.UserIDs = stringsToSet(userIDs, prior.UserIDs, &diags)
	m.RoleIDs = intsToInt64Set(roleIDs, prior.RoleIDs, &diags)
	m.Emails = stringsToSet(emails, prior.Emails, &diags)

	m.WebhookActions = alertWebhookActionsToList(ctx, got.WebhookActions, prior.WebhookActions, &diags)
	return m, diags
}

// alertRuleAllFlag keeps a known prior all_* value (the server never
// returns it) and falls back to inferred on import.
func alertRuleAllFlag(prior types.Bool, inferred bool) types.Bool {
	if !prior.IsNull() && !prior.IsUnknown() {
		return prior
	}
	return types.BoolValue(inferred)
}

// alertWebhookActionsToList maps the server webhook actions onto the
// `webhook_actions` list. A missing config maps to the defaults
// (`auth_type = "none"`, `method = "POST"`), which is how the server
// treats it when sending.
func alertWebhookActionsToList(ctx context.Context, actions []client.AlertWebhookAction, prior types.List, diags *diag.Diagnostics) types.List {
	objType := types.ObjectType{AttrTypes: alertWebhookActionAttrTypes}
	if len(actions) == 0 && (prior.IsNull() || prior.IsUnknown()) {
		return types.ListNull(objType)
	}

	var priorActions []alertWebhookActionModel
	if !prior.IsNull() && !prior.IsUnknown() {
		diags.Append(prior.ElementsAs(ctx, &priorActions, false)...)
	}

	models := make([]alertWebhookActionModel, 0, len(actions))
	for i, a := range actions {
		m := alertWebhookActionModel{
			URL:               types.StringValue(a.WebhookURL),
			Enabled:           types.BoolValue(a.Enabled),
			Method:            types.StringValue("POST"),
			AuthType:          types.StringValue("none"),
			BearerToken:       types.StringNull(),
			BasicCredentials:  types.StringNull(),
			CustomHeaderName:  types.StringNull(),
			CustomHeaderValue: types.StringNull(),
			Headers:           types.MapNull(types.StringType),
			BodyTemplate:      types.StringNull(),
		}
		cfg := a.Config
		if cfg == nil && i < len(priorActions) && priorActions[i].URL.ValueString() == a.WebhookURL {
			// The server returns a null config when it cannot decrypt
			// it. The provider always writes one, so keep the prior
			// values rather than reporting the defaults.
			p := priorActions[i]
			m.Method, m.AuthType, m.Headers, m.BodyTemplate = p.Method, p.AuthType, p.Headers, p.BodyTemplate
			m.BearerToken, m.BasicCredentials = p.BearerToken, p.BasicCredentials
			m.CustomHeaderName, m.CustomHeaderValue = p.CustomHeaderName, p.CustomHeaderValue
			if p.AuthType.ValueString() != "none" || !p.Headers.IsNull() || !p.BodyTemplate.IsNull() {
				diags.AddWarning("Webhook config not readable",
					fmt.Sprintf("Pangolin returned no config for webhook %q (it could not decrypt it, for example after "+
						"a server secret change). The values from the previous state are kept; check the webhook in Pangolin.", a.WebhookURL))
			}
		}
		if cfg != nil {
			if cfg.AuthType != "" {
				m.AuthType = types.StringValue(cfg.AuthType)
			}
			if cfg.Method != nil && *cfg.Method != "" {
				m.Method = types.StringValue(*cfg.Method)
			}
			m.BearerToken = tfconv.StringFromNonEmptyPtr(cfg.BearerToken)
			m.BasicCredentials = tfconv.StringFromNonEmptyPtr(cfg.BasicCredentials)
			m.CustomHeaderName = tfconv.StringFromNonEmptyPtr(cfg.CustomHeaderName)
			m.CustomHeaderValue = tfconv.StringFromNonEmptyPtr(cfg.CustomHeaderValue)
			if cfg.UseBodyTemplate != nil && *cfg.UseBodyTemplate {
				m.BodyTemplate = tfconv.StringFromNonEmptyPtr(cfg.BodyTemplate)
			}
			if len(cfg.Headers) > 0 {
				headers := map[string]attr.Value{}
				for _, h := range cfg.Headers {
					headers[h.Key] = types.StringValue(h.Value)
				}
				mv, d := types.MapValue(types.StringType, headers)
				diags.Append(d...)
				m.Headers = mv
			} else if i < len(priorActions) && !priorActions[i].Headers.IsNull() && len(priorActions[i].Headers.Elements()) == 0 {
				// Keep an explicit `headers = {}` stable: the empty
				// list is dropped on the wire.
				m.Headers = priorActions[i].Headers
			}
		}
		models = append(models, m)
	}

	lv, d := types.ListValueFrom(ctx, objType, models)
	diags.Append(d...)
	return lv
}

// int64SetToInts converts a set of int64 into a sorted []int. Null and
// unknown sets give an empty slice.
func int64SetToInts(ctx context.Context, s types.Set, diags *diag.Diagnostics) []int {
	out := []int{}
	if s.IsNull() || s.IsUnknown() {
		return out
	}
	var vals []int64
	diags.Append(s.ElementsAs(ctx, &vals, false)...)
	for _, v := range vals {
		out = append(out, int(v))
	}
	sort.Ints(out)
	return out
}

// stringSetToSortedSlice converts a set of strings into a sorted
// []string. Null and unknown sets give an empty slice.
func stringSetToSortedSlice(ctx context.Context, s types.Set, diags *diag.Diagnostics) []string {
	out := []string{}
	if s.IsNull() || s.IsUnknown() {
		return out
	}
	diags.Append(s.ElementsAs(ctx, &out, false)...)
	sort.Strings(out)
	return out
}

// intsToInt64Set builds a set of int64 from server IDs. An empty
// server list stays null when the prior value was null, so an unset
// attribute does not flip to `[]`.
func intsToInt64Set(ids []int, prior types.Set, diags *diag.Diagnostics) types.Set {
	if len(ids) == 0 && (prior.IsNull() || prior.IsUnknown()) {
		return types.SetNull(types.Int64Type)
	}
	vals := make([]attr.Value, 0, len(ids))
	for _, id := range ids {
		vals = append(vals, types.Int64Value(int64(id)))
	}
	sv, d := types.SetValue(types.Int64Type, vals)
	diags.Append(d...)
	return sv
}

// stringsToSet is the string counterpart of [intsToInt64Set].
func stringsToSet(values []string, prior types.Set, diags *diag.Diagnostics) types.Set {
	if len(values) == 0 && (prior.IsNull() || prior.IsUnknown()) {
		return types.SetNull(types.StringType)
	}
	vals := make([]attr.Value, 0, len(values))
	for _, v := range values {
		vals = append(vals, types.StringValue(v))
	}
	sv, d := types.SetValue(types.StringType, vals)
	diags.Append(d...)
	return sv
}
