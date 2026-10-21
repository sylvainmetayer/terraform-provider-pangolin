package resources

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
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
	_ resource.Resource                   = &HealthCheckResource{}
	_ resource.ResourceWithImportState    = &HealthCheckResource{}
	_ resource.ResourceWithValidateConfig = &HealthCheckResource{}
	_ resource.ResourceWithModifyPlan     = &HealthCheckResource{}
)

// HealthCheckResource manages a standalone (target-less) health check.
type HealthCheckResource struct {
	client *client.Client
}

// HealthCheckResourceModel describes the resource data model. The
// probe attributes drop the `hc_` prefix used on pangolin_target
// since the whole resource is a health check.
type HealthCheckResourceModel struct {
	ID                 types.Int64           `tfsdk:"id"`
	Name               types.String          `tfsdk:"name"`
	SiteID             types.Int64           `tfsdk:"site_id"`
	Enabled            types.Bool            `tfsdk:"enabled"`
	Mode               types.String          `tfsdk:"mode"`
	Hostname           types.String          `tfsdk:"hostname"`
	Port               types.Int64           `tfsdk:"port"`
	Path               types.String          `tfsdk:"path"`
	Scheme             types.String          `tfsdk:"scheme"`
	Method             types.String          `tfsdk:"method"`
	Interval           types.Int64           `tfsdk:"interval"`
	UnhealthyInterval  types.Int64           `tfsdk:"unhealthy_interval"`
	Timeout            types.Int64           `tfsdk:"timeout"`
	Headers            []TargetHCHeaderModel `tfsdk:"headers"`
	FollowRedirects    types.Bool            `tfsdk:"follow_redirects"`
	Status             types.Int64           `tfsdk:"status"`
	TLSServerName      types.String          `tfsdk:"tls_server_name"`
	HealthyThreshold   types.Int64           `tfsdk:"healthy_threshold"`
	UnhealthyThreshold types.Int64           `tfsdk:"unhealthy_threshold"`
	Health             types.String          `tfsdk:"health"`
}

// NewHealthCheckResource returns a new resource factory.
func NewHealthCheckResource() resource.Resource {
	return &HealthCheckResource{}
}

func (r *HealthCheckResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_health_check"
}

func (r *HealthCheckResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a standalone Pangolin health check: a probe run by a site's Newt " +
			"connector that is not attached to any target. Its health can drive alert rules.\n\n" +
			"> **Note:** Requires Pangolin Enterprise Edition (written against 1.24.0). The routes do not exist on the " +
			"OSS build, which answers 404.\n\n" +
			"> **Note:** Newt silently skips a probe that has no `hostname` or `port`, and an `http` " +
			"probe that has no `path`. The server only enforces `hostname` when `enabled = true`.\n\n" +
			"> **Note:** the API cannot clear `port` once set. Removing it from the configuration " +
			"keeps the last value. Removing `hostname`, `path`, `scheme`, `tls_server_name`, `status` " +
			"or `headers` clears them on the server.",
		Attributes: map[string]schema.Attribute{
			"id": schema.Int64Attribute{
				Description: "The numeric health check ID.",
				Computed:    true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Description: "Display name of the health check.",
				Required:    true,
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
			"site_id": schema.Int64Attribute{
				Description: "The ID of the site whose Newt connector runs the probe. Changing it " +
					"recreates the health check: an in-place site change on Pangolin 1.24 pushes the " +
					"probe to the new site but leaves it running on the old one.",
				Required: true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.RequiresReplace(),
				},
				Validators: []validator.Int64{
					int64validator.AtLeast(1),
				},
			},
			"enabled": schema.BoolAttribute{
				Description: "Whether the probe runs. Defaults to `false`, like the API.",
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
			},
			"mode": schema.StringAttribute{
				Description: "Probe mode: `http` or `tcp`. Defaults to `http`. The API also stores " +
					"`snmp` and `ping`, but Pangolin 1.24 does not implement them (the UI hides them and " +
					"Newt runs any non-`tcp` probe as `http`), so they are not accepted here.",
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString("http"),
				Validators: []validator.String{
					stringvalidator.OneOf("http", "tcp"),
				},
			},
			"hostname": schema.StringAttribute{
				Description: "Host the probe connects to. Required when `enabled = true`.",
				Optional:    true,
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
			"port": schema.Int64Attribute{
				Description: "Port the probe connects to.",
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
				Validators: []validator.Int64{
					int64validator.Between(1, 65535),
				},
			},
			"path": schema.StringAttribute{
				Description: "URL path requested by an `http` probe (e.g. `/health`).",
				Optional:    true,
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
			"scheme": schema.StringAttribute{
				Description: "Scheme used by an `http` probe (`http` or `https`).",
				Optional:    true,
				Validators: []validator.String{
					stringvalidator.OneOf("http", "https"),
				},
			},
			"method": schema.StringAttribute{
				Description: "HTTP method used by an `http` probe (e.g. `GET`, `HEAD`). Defaults to `GET`.",
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("GET"),
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
			"interval": schema.Int64Attribute{
				Description: "Probe interval in seconds while the check is healthy. Defaults to `30`.",
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(30),
				Validators: []validator.Int64{
					int64validator.AtLeast(1),
				},
			},
			"unhealthy_interval": schema.Int64Attribute{
				Description: "Probe interval in seconds while the check is unhealthy. Defaults to `30`.",
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(30),
				Validators: []validator.Int64{
					int64validator.AtLeast(1),
				},
			},
			"timeout": schema.Int64Attribute{
				Description: "Probe timeout in seconds. Defaults to `5`, the Pangolin UI and database " +
					"default (the API alone would store `1`).",
				Optional: true,
				Computed: true,
				Default:  int64default.StaticInt64(5),
				Validators: []validator.Int64{
					int64validator.AtLeast(1),
				},
			},
			"headers": schema.ListNestedAttribute{
				Description: "Request headers set on an `http` probe - list of `{name, value}` objects. " +
					"Leave unset (or `[]`) for no probe headers.",
				Optional: true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"name":  schema.StringAttribute{Description: "Header name.", Required: true},
						"value": schema.StringAttribute{Description: "Header value.", Required: true},
					},
				},
			},
			"follow_redirects": schema.BoolAttribute{
				Description: "Whether an `http` probe follows redirects. Defaults to `true`.",
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(true),
			},
			"status": schema.Int64Attribute{
				Description: "Expected HTTP status code of an `http` probe (e.g. `200`).",
				Optional:    true,
				Validators: []validator.Int64{
					int64validator.Between(100, 599),
				},
			},
			"tls_server_name": schema.StringAttribute{
				Description: "TLS SNI used when an `http` probe uses `https`.",
				Optional:    true,
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
			"healthy_threshold": schema.Int64Attribute{
				Description: "Consecutive successful probes required to mark the check healthy. Defaults to `1`.",
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(1),
				Validators: []validator.Int64{
					int64validator.AtLeast(1),
				},
			},
			"unhealthy_threshold": schema.Int64Attribute{
				Description: "Consecutive failed probes required to mark the check unhealthy. Defaults to `1`.",
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(1),
				Validators: []validator.Int64{
					int64validator.AtLeast(1),
				},
			},
			// No UseStateForUnknown: the update handler resets the health
			// (`unknown` when disabled, `unhealthy` when re-enabled), so a
			// planned prior value would not match the apply result.
			"health": schema.StringAttribute{
				Description: "Current health as reported by Pangolin (`unknown`, `healthy`, `unhealthy`).",
				Computed:    true,
			},
		},
	}
}

func (r *HealthCheckResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// ValidateConfig mirrors the server rule (`hostname` is required when
// the probe is enabled) and warns about an `http` probe without path,
// which Newt silently skips. An unknown `enabled` is skipped here:
// Terraform validates the config again once it is known, before the
// final plan of the apply. The missing-port warning lives in
// ModifyPlan, which can see the port kept from state.
func (r *HealthCheckResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var cfg HealthCheckResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !cfg.Enabled.ValueBool() {
		return
	}
	if cfg.Hostname.IsNull() {
		resp.Diagnostics.AddAttributeError(path.Root("hostname"), "Missing hostname",
			"`hostname` is required when `enabled = true`.")
	}
	mode := cfg.Mode.ValueString()
	if !cfg.Mode.IsUnknown() && mode != "tcp" && cfg.Path.IsNull() {
		resp.Diagnostics.AddAttributeWarning(path.Root("path"), "Probe will not run",
			"Newt skips an `http` health check that has no `path`.")
	}
}

// ModifyPlan warns when an enabled probe will end up with no port,
// which Newt skips. The planned port already carries the value kept
// from state (the API cannot clear it), so a port removed from the
// config but still stored on the server does not warn.
func (r *HealthCheckResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return
	}
	var plan HealthCheckResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !plan.Enabled.ValueBool() {
		return
	}
	noPort := plan.Port.IsNull()
	if plan.Port.IsUnknown() {
		// Still unknown after UseStateForUnknown: either the config
		// sets it from an unknown value (not a miss), or it is unset
		// and no port is stored.
		var cfgPort types.Int64
		resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("port"), &cfgPort)...)
		noPort = cfgPort.IsNull()
	}
	if noPort {
		resp.Diagnostics.AddAttributeWarning(path.Root("port"), "Probe will not run",
			"Newt skips a health check that has no `port`.")
	}
}

func (r *HealthCheckResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan HealthCheckResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body, err := buildHealthCheckRequest(plan, false)
	if err != nil {
		resp.Diagnostics.AddError("Invalid health check headers", err.Error())
		return
	}
	id, err := r.client.CreateHealthCheck(ctx, body)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create health check", healthCheckErrorDetail(err))
		return
	}

	hc, err := r.client.GetHealthCheck(ctx, id)
	if err != nil {
		// Keep the ID in state so the next apply does not orphan the
		// server-side row; the resource is marked tainted.
		plan.ID = types.Int64Value(int64(id))
		plan.Health = types.StringValue("unknown")
		// port is the only attribute that can still be unknown here
		// (Optional+Computed, unset on create); state must be known.
		if plan.Port.IsUnknown() {
			plan.Port = types.Int64Null()
		}
		resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
		resp.Diagnostics.AddError("Failed to read health check after create", err.Error())
		return
	}

	state := healthCheckToModel(hc, plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *HealthCheckResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state HealthCheckResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	hc, err := r.client.GetHealthCheck(ctx, int(state.ID.ValueInt64()))
	if err != nil {
		// ErrNotFound only means "the list succeeded and the row is
		// gone"; a 404 on the list route itself (OSS build, wrong org)
		// is ErrHealthChecksUnavailable and must not drop the state.
		if errors.Is(err, client.ErrNotFound) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read health check", healthCheckErrorDetail(err))
		return
	}

	next := healthCheckToModel(hc, state)
	resp.Diagnostics.Append(resp.State.Set(ctx, &next)...)
}

func (r *HealthCheckResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan HealthCheckResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body, err := buildHealthCheckRequest(plan, true)
	if err != nil {
		resp.Diagnostics.AddError("Invalid health check headers", err.Error())
		return
	}
	hc, err := r.client.UpdateHealthCheck(ctx, int(plan.ID.ValueInt64()), body)
	if err != nil {
		resp.Diagnostics.AddError("Failed to update health check", healthCheckErrorDetail(err))
		return
	}

	state := healthCheckToModel(hc, plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *HealthCheckResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state HealthCheckResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteHealthCheck(ctx, int(state.ID.ValueInt64()))
	if err != nil && !errors.Is(err, client.ErrNotFound) {
		resp.Diagnostics.AddError("Failed to delete health check", err.Error())
	}
}

func (r *HealthCheckResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	id, err := strconv.ParseInt(req.ID, 10, 64)
	if err != nil {
		resp.Diagnostics.AddError("Invalid ID", fmt.Sprintf("Cannot parse health check ID %q as integer", req.ID))
		return
	}

	hc, err := r.client.GetHealthCheck(ctx, int(id))
	if err != nil {
		resp.Diagnostics.AddError("Failed to import health check", healthCheckErrorDetail(err))
		return
	}
	if hc.ResourceID != nil {
		resp.Diagnostics.AddError("Not a standalone health check",
			fmt.Sprintf("Health check %d is attached to a target of resource %d. Manage it through the "+
				"`hc_*` attributes of `pangolin_target` instead.", id, *hc.ResourceID))
		return
	}
	if mode := tfconv.StringFromPtr(hc.HCMode).ValueString(); mode != "http" && mode != "tcp" {
		resp.Diagnostics.AddError("Unsupported health check mode",
			fmt.Sprintf("Health check %d uses mode %q. Pangolin 1.24 does not implement it (Newt runs it as "+
				"`http`), so this provider only manages `http` and `tcp` probes.", id, mode))
		return
	}

	state := healthCheckToModel(hc, HealthCheckResourceModel{})
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// healthCheckErrorDetail adds an Enterprise hint to 404s: the OSS
// build has no health-check routes at all.
func healthCheckErrorDetail(err error) string {
	if errors.Is(err, client.ErrHealthChecksUnavailable) {
		return err.Error() + ". Standalone health checks require Pangolin Enterprise Edition; " +
			"also check the provider organization."
	}
	if errors.Is(err, client.ErrNotFound) {
		return err.Error() + ". Standalone health checks require Pangolin Enterprise Edition; " +
			"also check that the health check exists in the provider organization."
	}
	return err.Error()
}

// buildHealthCheckRequest maps the plan onto the wire body. Attributes
// with a schema default are always known and always sent.
//
// The update schema cannot null the optional strings, so on update an
// unset hostname / path / scheme / tls_server_name is sent as "" to
// clear it (read back as null, see healthCheckToModel). On create they
// are simply omitted. headers and status go out as explicit nulls when
// unset, which clears them on update.
func buildHealthCheckRequest(plan HealthCheckResourceModel, update bool) (*client.HealthCheckRequest, error) {
	str := func(t types.String) *string {
		if t.IsNull() || t.IsUnknown() {
			if update {
				empty := ""
				return &empty
			}
			return nil
		}
		v := t.ValueString()
		return &v
	}
	intPtr := func(t types.Int64) *int {
		if t.IsNull() || t.IsUnknown() {
			return nil
		}
		v := int(t.ValueInt64())
		return &v
	}
	boolPtr := func(t types.Bool) *bool {
		if t.IsNull() || t.IsUnknown() {
			return nil
		}
		v := t.ValueBool()
		return &v
	}
	strVal := func(t types.String) *string {
		if t.IsNull() || t.IsUnknown() {
			return nil
		}
		v := t.ValueString()
		return &v
	}

	body := &client.HealthCheckRequest{
		Name:                 strVal(plan.Name),
		SiteID:               intPtr(plan.SiteID),
		HCEnabled:            boolPtr(plan.Enabled),
		HCMode:               strVal(plan.Mode),
		HCHostname:           str(plan.Hostname),
		HCPort:               intPtr(plan.Port),
		HCPath:               str(plan.Path),
		HCScheme:             str(plan.Scheme),
		HCMethod:             strVal(plan.Method),
		HCInterval:           intPtr(plan.Interval),
		HCUnhealthyInterval:  intPtr(plan.UnhealthyInterval),
		HCTimeout:            intPtr(plan.Timeout),
		HCFollowRedirects:    boolPtr(plan.FollowRedirects),
		HCStatus:             intPtr(plan.Status),
		HCTLSServerName:      str(plan.TLSServerName),
		HCHealthyThreshold:   intPtr(plan.HealthyThreshold),
		HCUnhealthyThreshold: intPtr(plan.UnhealthyThreshold),
	}
	if len(plan.Headers) > 0 {
		hs := make([]client.TargetHCHeader, len(plan.Headers))
		for i, h := range plan.Headers {
			hs[i] = client.TargetHCHeader{Name: h.Name.ValueString(), Value: h.Value.ValueString()}
		}
		raw, err := json.Marshal(hs)
		if err != nil {
			return nil, err
		}
		s := string(raw)
		body.HCHeaders = &s
	}
	return body, nil
}

// healthCheckToModel maps a *client.HealthCheck onto the model. Empty
// strings read back as null: the update path clears optional strings
// by sending "". When the server holds no headers, prior's
// representation (null or []) is kept so an explicit `headers = []`
// does not flip to null. Malformed hcHeaders fall back to no headers,
// as on pangolin_target.
func healthCheckToModel(hc *client.HealthCheck, prior HealthCheckResourceModel) HealthCheckResourceModel {
	nonEmpty := func(p *string) types.String {
		if p == nil || *p == "" {
			return types.StringNull()
		}
		return types.StringValue(*p)
	}

	headers, _ := client.ParseTargetHCHeaders(hc.HCHeaders)
	var hcHeaders []TargetHCHeaderModel
	if len(headers) > 0 {
		hcHeaders = make([]TargetHCHeaderModel, len(headers))
		for i, h := range headers {
			hcHeaders[i] = TargetHCHeaderModel{
				Name:  types.StringValue(h.Name),
				Value: types.StringValue(h.Value),
			}
		}
	} else if prior.Headers != nil {
		hcHeaders = []TargetHCHeaderModel{}
	}

	health := hc.HCHealth
	if health == "" {
		health = "unknown"
	}

	return HealthCheckResourceModel{
		ID:                 types.Int64Value(int64(hc.HealthCheckID)),
		Name:               types.StringValue(hc.Name),
		SiteID:             tfconv.Int64FromIntPtr(hc.SiteID),
		Enabled:            types.BoolValue(hc.HCEnabled),
		Mode:               tfconv.StringFromPtr(hc.HCMode),
		Hostname:           nonEmpty(hc.HCHostname),
		Port:               tfconv.Int64FromIntPtr(hc.HCPort),
		Path:               nonEmpty(hc.HCPath),
		Scheme:             nonEmpty(hc.HCScheme),
		Method:             tfconv.StringFromPtr(hc.HCMethod),
		Interval:           tfconv.Int64FromIntPtr(hc.HCInterval),
		UnhealthyInterval:  tfconv.Int64FromIntPtr(hc.HCUnhealthyInterval),
		Timeout:            tfconv.Int64FromIntPtr(hc.HCTimeout),
		Headers:            hcHeaders,
		FollowRedirects:    tfconv.BoolFromPtr(hc.HCFollowRedirects),
		Status:             tfconv.Int64FromIntPtr(hc.HCStatus),
		TLSServerName:      nonEmpty(hc.HCTLSServerName),
		HealthyThreshold:   tfconv.Int64FromIntPtr(hc.HCHealthyThreshold),
		UnhealthyThreshold: tfconv.Int64FromIntPtr(hc.HCUnhealthyThreshold),
		Health:             types.StringValue(health),
	}
}
