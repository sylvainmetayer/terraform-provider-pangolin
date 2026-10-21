package datasources

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stackopshq/terraform-provider-pangolin/internal/client"
	"github.com/stackopshq/terraform-provider-pangolin/internal/tfconv"
)

var _ datasource.DataSource = &HealthChecksDataSource{}

// HealthChecksDataSource lists the health checks of the org: the
// standalone ones and those attached to targets.
type HealthChecksDataSource struct {
	client *client.Client
}

// HealthCheckItemModel mirrors [client.HealthCheck]. headers is
// decoded into the same typed `{name, value}` list as
// pangolin_resource_targets.
type HealthCheckItemModel struct {
	ID                 types.Int64         `tfsdk:"id"`
	Name               types.String        `tfsdk:"name"`
	Standalone         types.Bool          `tfsdk:"standalone"`
	SiteID             types.Int64         `tfsdk:"site_id"`
	SiteName           types.String        `tfsdk:"site_name"`
	SiteNiceID         types.String        `tfsdk:"site_nice_id"`
	ResourceID         types.Int64         `tfsdk:"resource_id"`
	ResourceName       types.String        `tfsdk:"resource_name"`
	ResourceNiceID     types.String        `tfsdk:"resource_nice_id"`
	Enabled            types.Bool          `tfsdk:"enabled"`
	Health             types.String        `tfsdk:"health"`
	Mode               types.String        `tfsdk:"mode"`
	Hostname           types.String        `tfsdk:"hostname"`
	Port               types.Int64         `tfsdk:"port"`
	Path               types.String        `tfsdk:"path"`
	Scheme             types.String        `tfsdk:"scheme"`
	Method             types.String        `tfsdk:"method"`
	Interval           types.Int64         `tfsdk:"interval"`
	UnhealthyInterval  types.Int64         `tfsdk:"unhealthy_interval"`
	Timeout            types.Int64         `tfsdk:"timeout"`
	Headers            []HCHeaderItemModel `tfsdk:"headers"`
	FollowRedirects    types.Bool          `tfsdk:"follow_redirects"`
	Status             types.Int64         `tfsdk:"status"`
	TLSServerName      types.String        `tfsdk:"tls_server_name"`
	HealthyThreshold   types.Int64         `tfsdk:"healthy_threshold"`
	UnhealthyThreshold types.Int64         `tfsdk:"unhealthy_threshold"`
}

// HealthChecksDataSourceModel is the top-level data source shape.
type HealthChecksDataSourceModel struct {
	Query        types.String           `tfsdk:"query"`
	Mode         types.String           `tfsdk:"mode"`
	SiteID       types.Int64            `tfsdk:"site_id"`
	ResourceID   types.Int64            `tfsdk:"resource_id"`
	Health       types.String           `tfsdk:"health"`
	Enabled      types.Bool             `tfsdk:"enabled"`
	HealthChecks []HealthCheckItemModel `tfsdk:"health_checks"`
}

// NewHealthChecksDataSource returns a new data source factory.
func NewHealthChecksDataSource() datasource.DataSource {
	return &HealthChecksDataSource{}
}

func (d *HealthChecksDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_health_checks"
}

func (d *HealthChecksDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Lists the health checks of the organization, newest first: the standalone ones " +
			"(managed by `pangolin_health_check`) and the probes attached to targets " +
			"(`standalone = false`, with the `resource_*` attributes set). All filters are optional " +
			"and applied server-side.\n\n" +
			"> **Note:** Requires Pangolin Enterprise Edition (written against 1.24.0).",
		Attributes: map[string]schema.Attribute{
			"query": schema.StringAttribute{
				Description: "Case-insensitive substring match on the name.",
				Optional:    true,
			},
			"mode": schema.StringAttribute{
				Description: "Keep only this probe mode: `http`, `tcp`, `snmp` or `ping`.",
				Optional:    true,
				Validators: []validator.String{
					stringvalidator.OneOf("http", "tcp", "snmp", "ping"),
				},
			},
			"site_id": schema.Int64Attribute{
				Description: "Keep only the health checks run by this site.",
				Optional:    true,
			},
			"resource_id": schema.Int64Attribute{
				Description: "Keep only the probes attached to a target of this resource.",
				Optional:    true,
			},
			"health": schema.StringAttribute{
				Description: "Keep only this health: `healthy`, `unhealthy` or `unknown`.",
				Optional:    true,
				Validators: []validator.String{
					stringvalidator.OneOf("healthy", "unhealthy", "unknown"),
				},
			},
			"enabled": schema.BoolAttribute{
				Description: "Keep only the enabled (`true`) or disabled (`false`) health checks.",
				Optional:    true,
			},
			"health_checks": schema.ListNestedAttribute{
				Description: "List of health checks.",
				Computed:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":                 schema.Int64Attribute{Description: "Numeric health check ID.", Computed: true},
						"name":               schema.StringAttribute{Description: "Display name.", Computed: true},
						"standalone":         schema.BoolAttribute{Description: "`false` for a probe attached to a target.", Computed: true},
						"site_id":            schema.Int64Attribute{Description: "ID of the site running the probe.", Computed: true},
						"site_name":          schema.StringAttribute{Description: "Name of the site running the probe.", Computed: true},
						"site_nice_id":       schema.StringAttribute{Description: "Nice ID of the site running the probe.", Computed: true},
						"resource_id":        schema.Int64Attribute{Description: "Resource of the target the probe is attached to (null when standalone).", Computed: true},
						"resource_name":      schema.StringAttribute{Description: "Name of that resource.", Computed: true},
						"resource_nice_id":   schema.StringAttribute{Description: "Nice ID of that resource.", Computed: true},
						"enabled":            schema.BoolAttribute{Description: "Whether the probe runs.", Computed: true},
						"health":             schema.StringAttribute{Description: "`unknown`, `healthy` or `unhealthy`.", Computed: true},
						"mode":               schema.StringAttribute{Description: "Probe mode.", Computed: true},
						"hostname":           schema.StringAttribute{Description: "Probed host.", Computed: true},
						"port":               schema.Int64Attribute{Description: "Probed port.", Computed: true},
						"path":               schema.StringAttribute{Description: "Probed URL path.", Computed: true},
						"scheme":             schema.StringAttribute{Description: "Probe scheme.", Computed: true},
						"method":             schema.StringAttribute{Description: "Probe HTTP method.", Computed: true},
						"interval":           schema.Int64Attribute{Description: "Interval in seconds while healthy.", Computed: true},
						"unhealthy_interval": schema.Int64Attribute{Description: "Interval in seconds while unhealthy.", Computed: true},
						"timeout":            schema.Int64Attribute{Description: "Timeout in seconds.", Computed: true},
						"headers": schema.ListNestedAttribute{
							Description: "Probe request headers - list of `{name, value}` objects.",
							Computed:    true,
							NestedObject: schema.NestedAttributeObject{
								Attributes: map[string]schema.Attribute{
									"name":  schema.StringAttribute{Description: "Header name.", Computed: true},
									"value": schema.StringAttribute{Description: "Header value.", Computed: true},
								},
							},
						},
						"follow_redirects":    schema.BoolAttribute{Description: "Whether redirects are followed.", Computed: true},
						"status":              schema.Int64Attribute{Description: "Expected HTTP status code.", Computed: true},
						"tls_server_name":     schema.StringAttribute{Description: "TLS SNI.", Computed: true},
						"healthy_threshold":   schema.Int64Attribute{Description: "Successes required to turn healthy.", Computed: true},
						"unhealthy_threshold": schema.Int64Attribute{Description: "Failures required to turn unhealthy.", Computed: true},
					},
				},
			},
		},
	}
}

func (d *HealthChecksDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *HealthChecksDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg HealthChecksDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	opts := &client.ListHealthChecksOptions{
		Query:      cfg.Query.ValueString(),
		Mode:       cfg.Mode.ValueString(),
		SiteID:     int(cfg.SiteID.ValueInt64()),
		ResourceID: int(cfg.ResourceID.ValueInt64()),
		Health:     cfg.Health.ValueString(),
	}
	if !cfg.Enabled.IsNull() && !cfg.Enabled.IsUnknown() {
		v := cfg.Enabled.ValueBool()
		opts.Enabled = &v
	}

	checks, err := d.client.ListHealthChecks(ctx, opts)
	if err != nil {
		resp.Diagnostics.AddError("Failed to list health checks", err.Error())
		return
	}

	cfg.HealthChecks = make([]HealthCheckItemModel, 0, len(checks))
	for i := range checks {
		cfg.HealthChecks = append(cfg.HealthChecks, healthCheckItemFromAPI(&checks[i]))
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &cfg)...)
}

// healthCheckItemFromAPI maps one row. An empty hcHealth reads as
// `unknown`, like on pangolin_health_check.
func healthCheckItemFromAPI(hc *client.HealthCheck) HealthCheckItemModel {
	health := hc.HCHealth
	if health == "" {
		health = "unknown"
	}
	return HealthCheckItemModel{
		ID:                 types.Int64Value(int64(hc.HealthCheckID)),
		Name:               types.StringValue(hc.Name),
		Standalone:         types.BoolValue(hc.ResourceID == nil),
		SiteID:             tfconv.Int64FromIntPtr(hc.SiteID),
		SiteName:           tfconv.StringFromPtr(hc.SiteName),
		SiteNiceID:         tfconv.StringFromPtr(hc.SiteNiceID),
		ResourceID:         tfconv.Int64FromIntPtr(hc.ResourceID),
		ResourceName:       tfconv.StringFromPtr(hc.ResourceName),
		ResourceNiceID:     tfconv.StringFromPtr(hc.ResourceNiceID),
		Enabled:            types.BoolValue(hc.HCEnabled),
		Health:             types.StringValue(health),
		Mode:               tfconv.StringFromPtr(hc.HCMode),
		Hostname:           tfconv.StringFromPtr(hc.HCHostname),
		Port:               tfconv.Int64FromIntPtr(hc.HCPort),
		Path:               tfconv.StringFromPtr(hc.HCPath),
		Scheme:             tfconv.StringFromPtr(hc.HCScheme),
		Method:             tfconv.StringFromPtr(hc.HCMethod),
		Interval:           tfconv.Int64FromIntPtr(hc.HCInterval),
		UnhealthyInterval:  tfconv.Int64FromIntPtr(hc.HCUnhealthyInterval),
		Timeout:            tfconv.Int64FromIntPtr(hc.HCTimeout),
		Headers:            decodeTargetHCHeaders(hc.HCHeaders),
		FollowRedirects:    tfconv.BoolFromPtr(hc.HCFollowRedirects),
		Status:             tfconv.Int64FromIntPtr(hc.HCStatus),
		TLSServerName:      tfconv.StringFromPtr(hc.HCTLSServerName),
		HealthyThreshold:   tfconv.Int64FromIntPtr(hc.HCHealthyThreshold),
		UnhealthyThreshold: tfconv.Int64FromIntPtr(hc.HCUnhealthyThreshold),
	}
}
