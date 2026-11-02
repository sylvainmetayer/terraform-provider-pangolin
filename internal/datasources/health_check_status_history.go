package datasources

import (
	"context"
	"math"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stackopshq/terraform-provider-pangolin/internal/client"
	"github.com/stackopshq/terraform-provider-pangolin/internal/tfconv"
)

var _ datasource.DataSource = &HealthCheckStatusHistoryDataSource{}

// HealthCheckStatusHistoryDataSource exposes the daily uptime buckets
// of one health check.
type HealthCheckStatusHistoryDataSource struct {
	client *client.Client
}

// HealthCheckDowntimeWindowModel mirrors [client.HealthCheckDowntimeWindow].
type HealthCheckDowntimeWindowModel struct {
	Start  types.Int64  `tfsdk:"start"`
	End    types.Int64  `tfsdk:"end"`
	Status types.String `tfsdk:"status"`
}

// HealthCheckStatusDayModel mirrors [client.HealthCheckStatusDay].
type HealthCheckStatusDayModel struct {
	Date                 types.String                     `tfsdk:"date"`
	UptimePercent        types.Float64                    `tfsdk:"uptime_percent"`
	TotalDowntimeSeconds types.Int64                      `tfsdk:"total_downtime_seconds"`
	Status               types.String                     `tfsdk:"status"`
	DowntimeWindows      []HealthCheckDowntimeWindowModel `tfsdk:"downtime_windows"`
}

// HealthCheckStatusHistoryDataSourceModel is the top-level shape.
type HealthCheckStatusHistoryDataSourceModel struct {
	HealthCheckID        types.Int64                 `tfsdk:"health_check_id"`
	Days                 types.Int64                 `tfsdk:"days"`
	TZOffsetMinutes      types.Int64                 `tfsdk:"tz_offset_minutes"`
	OverallUptimePercent types.Float64               `tfsdk:"overall_uptime_percent"`
	TotalDowntimeSeconds types.Int64                 `tfsdk:"total_downtime_seconds"`
	History              []HealthCheckStatusDayModel `tfsdk:"history"`
}

// NewHealthCheckStatusHistoryDataSource returns a new data source factory.
func NewHealthCheckStatusHistoryDataSource() datasource.DataSource {
	return &HealthCheckStatusHistoryDataSource{}
}

func (d *HealthCheckStatusHistoryDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_health_check_status_history"
}

func (d *HealthCheckStatusHistoryDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Daily uptime history of a health check (standalone or attached to a target), " +
			"oldest day first.\n\n" +
			"> **Note:** Requires Pangolin Enterprise Edition (written against 1.24.0). The API key needs the `getTarget` action.",
		Attributes: map[string]schema.Attribute{
			"health_check_id": schema.Int64Attribute{
				Description: "The health check ID.",
				Required:    true,
			},
			"days": schema.Int64Attribute{
				Description: "Number of days to return, 1 to 365. The server defaults to 90.",
				Optional:    true,
				Validators: []validator.Int64{
					int64validator.Between(1, 365),
				},
			},
			"tz_offset_minutes": schema.Int64Attribute{
				Description: "Minutes added to UTC to anchor the day boundaries (e.g. `120` for UTC+2), " +
					"-720 to 840. The server defaults to 0 (UTC).",
				Optional: true,
				Validators: []validator.Int64{
					int64validator.Between(-720, 840),
				},
			},
			"overall_uptime_percent": schema.Float64Attribute{
				Description: "Uptime over the whole window, 0 to 100.",
				Computed:    true,
			},
			"total_downtime_seconds": schema.Int64Attribute{
				Description: "Downtime over the whole window, in seconds.",
				Computed:    true,
			},
			"history": schema.ListNestedAttribute{
				Description: "One entry per day.",
				Computed:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"date":                   schema.StringAttribute{Description: "Day (`YYYY-MM-DD`).", Computed: true},
						"uptime_percent":         schema.Float64Attribute{Description: "Uptime of the day, 0 to 100.", Computed: true},
						"total_downtime_seconds": schema.Int64Attribute{Description: "Downtime of the day, in seconds.", Computed: true},
						"status":                 schema.StringAttribute{Description: "`good`, `degraded`, `bad`, `no_data` or `unknown`.", Computed: true},
						"downtime_windows": schema.ListNestedAttribute{
							Description: "Outage windows of the day.",
							Computed:    true,
							NestedObject: schema.NestedAttributeObject{
								Attributes: map[string]schema.Attribute{
									"start":  schema.Int64Attribute{Description: "Start (epoch seconds).", Computed: true},
									"end":    schema.Int64Attribute{Description: "End (epoch seconds), null while ongoing.", Computed: true},
									"status": schema.StringAttribute{Description: "Status during the window.", Computed: true},
								},
							},
						},
					},
				},
			},
		},
	}
}

func (d *HealthCheckStatusHistoryDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *HealthCheckStatusHistoryDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg HealthCheckStatusHistoryDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	optInt := func(t types.Int64) *int {
		if t.IsNull() || t.IsUnknown() {
			return nil
		}
		v := int(t.ValueInt64())
		return &v
	}
	hist, err := d.client.GetHealthCheckStatusHistory(ctx, int(cfg.HealthCheckID.ValueInt64()),
		optInt(cfg.Days), optInt(cfg.TZOffsetMinutes))
	if err != nil {
		resp.Diagnostics.AddError("Failed to read health check status history", err.Error())
		return
	}

	cfg.OverallUptimePercent = types.Float64Value(hist.OverallUptimePercent)
	cfg.TotalDowntimeSeconds = types.Int64Value(int64(math.Round(hist.TotalDowntimeSeconds)))
	cfg.History = make([]HealthCheckStatusDayModel, 0, len(hist.Days))
	for _, day := range hist.Days {
		windows := make([]HealthCheckDowntimeWindowModel, 0, len(day.DowntimeWindows))
		for _, w := range day.DowntimeWindows {
			windows = append(windows, HealthCheckDowntimeWindowModel{
				Start:  types.Int64Value(w.Start),
				End:    tfconv.Int64FromInt64Ptr(w.End),
				Status: types.StringValue(w.Status),
			})
		}
		cfg.History = append(cfg.History, HealthCheckStatusDayModel{
			Date:                 types.StringValue(day.Date),
			UptimePercent:        types.Float64Value(day.UptimePercent),
			TotalDowntimeSeconds: types.Int64Value(int64(math.Round(day.TotalDowntimeSeconds))),
			Status:               types.StringValue(day.Status),
			DowntimeWindows:      windows,
		})
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &cfg)...)
}
