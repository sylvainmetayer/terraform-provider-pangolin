package resources

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stackopshq/terraform-provider-pangolin/internal/client"
	"github.com/stackopshq/terraform-provider-pangolin/internal/tfconv"
)

var (
	_ resource.Resource                   = &SitePrivateResource{}
	_ resource.ResourceWithImportState    = &SitePrivateResource{}
	_ resource.ResourceWithValidateConfig = &SitePrivateResource{}
)

// siteResourceModeGateway is the 1.24+ "exit node" mode: the site
// routes all traffic, and the server forces the values below whatever
// the request says.
const siteResourceModeGateway = "gateway"

const (
	gatewayDestination  = "0.0.0.0/0"
	gatewayPortRange    = "*"
	gatewayDisableICMP  = false
	gatewayForcedNotice = "Pangolin forces this value for `mode = \"gateway\"`; leave the attribute unset."
)

// SitePrivateResource defines the resource implementation.
type SitePrivateResource struct {
	client *client.Client
}

// SitePrivateResourceModel describes the resource data model.
type SitePrivateResourceModel struct {
	ID               types.Int64  `tfsdk:"id"`
	NiceID           types.String `tfsdk:"nice_id"`
	SiteID           types.Int64  `tfsdk:"site_id"`
	Name             types.String `tfsdk:"name"`
	Mode             types.String `tfsdk:"mode"`
	Destination      types.String `tfsdk:"destination"`
	Alias            types.String `tfsdk:"alias"`
	TCPPortRange     types.String `tfsdk:"tcp_port_range"`
	UDPPortRange     types.String `tfsdk:"udp_port_range"`
	DisableICMP      types.Bool   `tfsdk:"disable_icmp"`
	AuthDaemonMode   types.String `tfsdk:"auth_daemon_mode"`
	AuthDaemonPort   types.Int64  `tfsdk:"auth_daemon_port"`
	PamMode          types.String `tfsdk:"pam_mode"`
	Enabled          types.Bool   `tfsdk:"enabled"`
	SSL              types.Bool   `tfsdk:"ssl"`
	NetworkID        types.Int64  `tfsdk:"network_id"`
	DefaultNetworkID types.Int64  `tfsdk:"default_network_id"`
	Scheme           types.String `tfsdk:"scheme"`
	ProxyPort        types.Int64  `tfsdk:"proxy_port"`
	DestinationPort  types.Int64  `tfsdk:"destination_port"`
	AliasAddress     types.String `tfsdk:"alias_address"`
	DomainID         types.String `tfsdk:"domain_id"`
	Subdomain        types.String `tfsdk:"subdomain"`
	FullDomain       types.String `tfsdk:"full_domain"`
}

// NewSitePrivateResource returns a new resource factory.
func NewSitePrivateResource() resource.Resource {
	return &SitePrivateResource{}
}

func (r *SitePrivateResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_site_resource"
}

func (r *SitePrivateResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a Pangolin private site resource (VPN-accessible endpoint).",
		Attributes: map[string]schema.Attribute{
			"id": schema.Int64Attribute{
				Description: "The numeric ID of the site resource.",
				Computed:    true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"nice_id": schema.StringAttribute{
				Description: "The human-readable ID.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"site_id": schema.Int64Attribute{
				Description: "The site ID this resource belongs to.",
				Required:    true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.RequiresReplace(),
				},
			},
			"name": schema.StringAttribute{
				Description: "The name of the private resource.",
				Required:    true,
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
			"mode": schema.StringAttribute{
				Description: "The mode: `host`, `cidr`, `http`, or `gateway`.\n\n" +
					"`cidr` / `host` modes are L4 tunnels: the resource requires `alias` " +
					"and uses `tcp_port_range` / `udp_port_range` for traffic shaping.\n\n" +
					"`http` mode is an L7 HTTP proxy: the resource requires `domain_id`, " +
					"`subdomain`, `scheme` (`http` / `https`) and `destination_port`. The " +
					"server auto-assigns `tcp_port_range = \"443,80\"`, sets `disable_icmp = " +
					"true`, and leaves `alias` null.\n\n" +
					"`gateway` mode (Pangolin >= 1.24, \"exit node\" in the UI) routes all traffic through the " +
					"site. The server forces `destination = \"0.0.0.0/0\"`, `tcp_port_range` and " +
					"`udp_port_range` to `\"*\"` and `disable_icmp = false`: leave those unset. `alias`, " +
					"`domain_id`, `subdomain`, `scheme` and `destination_port` have no use in this mode and are rejected.",
				Required: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{
					stringvalidator.OneOf("host", "cidr", "http", siteResourceModeGateway),
				},
			},
			"destination": schema.StringAttribute{
				Description: "The destination: hostname (`host` mode), CIDR (`cidr` mode), or backend IP / hostname (`http` mode). " +
					"Required in those modes. Leave unset for `gateway` mode, where the server sets `0.0.0.0/0`.",
				Optional: true,
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
					gatewayForcedString(gatewayDestination),
				},
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
			"alias": schema.StringAttribute{
				Description: "The internal DNS alias (e.g. `myservice.internal`). Required for `cidr` / `host` modes; ignored / null for `http` mode.",
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"tcp_port_range": schema.StringAttribute{
				Description: "TCP port range string. `*` for all, `` for none, or specific ports/ranges (e.g. `80,443,8080-8090`). For `mode = http` the server auto-assigns `\"443,80\"` and ignores any value set here; for `mode = gateway` it forces `\"*\"`.",
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
					gatewayForcedString(gatewayPortRange),
				},
			},
			"udp_port_range": schema.StringAttribute{
				Description: "UDP port range string. '*' for all, '' for none, or specific ports/ranges. For `mode = gateway` the server forces `\"*\"`.",
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
					gatewayForcedString(gatewayPortRange),
				},
			},
			"disable_icmp": schema.BoolAttribute{
				Description: "Whether to disable ICMP. The server defaults to `false` for `cidr` / `host` modes and `true` for `mode = http`, and forces `false` for `mode = gateway`; leaving this unset lets the server pick.",
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.UseStateForUnknown(),
					gatewayForcedBool(gatewayDisableICMP),
				},
			},
			"auth_daemon_mode": schema.StringAttribute{
				Description: "Auth daemon mode: 'site' or 'remote'. Defaults to 'site'.",
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("site"),
			},
			"auth_daemon_port": schema.Int64Attribute{
				Description: "The auth daemon port (computed by the API).",
				Computed:    true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"pam_mode": schema.StringAttribute{
				Description: "PAM (Pluggable Authentication Module) mode for SSH-backed site resources. " +
					"One of `passthrough` or `push`. 1.19+ only - pre-1.19 servers leave this null.",
				Optional: true,
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
				Validators: []validator.String{
					stringvalidator.OneOf("passthrough", "push"),
				},
			},
			"enabled": schema.BoolAttribute{
				Description: "Whether the resource is enabled. Read-only here; the API returns `true` on creation. Use a future toggle endpoint to disable.",
				Computed:    true,
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.UseStateForUnknown(),
				},
			},
			"ssl": schema.BoolAttribute{
				Description: "Whether SSL is terminated by the proxy in front of this resource.",
				Computed:    true,
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.UseStateForUnknown(),
				},
			},
			"network_id": schema.Int64Attribute{
				Description: "The numeric network ID the resource is bound to.",
				Computed:    true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"default_network_id": schema.Int64Attribute{
				Description: "The default network ID, when set by the org configuration. Null when unset.",
				Computed:    true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"scheme": schema.StringAttribute{
				Description: "Protocol scheme of the upstream backend. Required for `mode = http`; ignored for `cidr` / `host`. One of `http`, `https`.",
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
				Validators: []validator.String{
					stringvalidator.OneOf("http", "https"),
				},
			},
			"proxy_port": schema.Int64Attribute{
				Description: "The proxy-facing port (server-assigned, read-only). The Pangolin API does NOT accept this as a create input - passing it returns `Validation error: Unrecognized key: \"proxyPort\"`.",
				Computed:    true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"destination_port": schema.Int64Attribute{
				Description: "Backend destination port. Required for `mode = http`; ignored for `cidr` / `host`.",
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"alias_address": schema.StringAttribute{
				Description: "The resolved alias address, when applicable. Null otherwise.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"domain_id": schema.StringAttribute{
				Description: "ID of the parent domain (use the `pangolin_domains` data source to discover). Required for `mode = http`; ignored for `cidr` / `host`.",
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"subdomain": schema.StringAttribute{
				Description: "Subdomain to host the proxy at - combined with `domain_id` to produce `full_domain`. Required for `mode = http`; ignored for `cidr` / `host`.",
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"full_domain": schema.StringAttribute{
				Description: "The full FQDN of the resource. Null otherwise.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *SitePrivateResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *SitePrivateResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan SitePrivateResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	siteRes, err := r.client.CreateSiteResource(ctx, buildCreateSiteResourceRequest(plan))
	if err != nil {
		resp.Diagnostics.AddError("Failed to create site resource", createSiteResourceErrorDetail(plan.Mode.ValueString(), err))
		return
	}

	plan.ID = types.Int64Value(int64(siteRes.SiteResourceID))
	hydrateSiteResourceState(&plan, siteRes)

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *SitePrivateResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state SitePrivateResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	siteRes, err := r.client.GetSiteResource(ctx, int(state.ID.ValueInt64()))
	if err != nil {
		if errors.Is(err, client.ErrNotFound) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read site resource", err.Error())
		return
	}

	hydrateSiteResourceState(&state, siteRes)

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *SitePrivateResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan SitePrivateResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	siteRes, err := r.client.UpdateSiteResource(ctx, int(plan.ID.ValueInt64()), buildUpdateSiteResourceRequest(plan))
	if err != nil {
		resp.Diagnostics.AddError("Failed to update site resource", err.Error())
		return
	}

	hydrateSiteResourceState(&plan, siteRes)

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *SitePrivateResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state SitePrivateResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteSiteResource(ctx, int(state.ID.ValueInt64()))
	if err != nil {
		resp.Diagnostics.AddError("Failed to delete site resource", err.Error())
		return
	}
}

func (r *SitePrivateResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	id, err := strconv.ParseInt(req.ID, 10, 64)
	if err != nil {
		resp.Diagnostics.AddError("Invalid ID", fmt.Sprintf("Cannot parse site resource ID %q as integer", req.ID))
		return
	}

	siteRes, err := r.client.GetSiteResource(ctx, int(id))
	if err != nil {
		resp.Diagnostics.AddError("Failed to import site resource", err.Error())
		return
	}

	state := SitePrivateResourceModel{ID: types.Int64Value(int64(siteRes.SiteResourceID))}
	hydrateSiteResourceState(&state, siteRes)

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// createSiteResourceErrorDetail adds a version hint to a create error
// in gateway mode: a pre-1.24 server rejects the mode itself with a
// generic 400 ("Invalid option: expected one of ... at \"mode\"").
func createSiteResourceErrorDetail(mode string, err error) string {
	detail := err.Error()
	if mode == siteResourceModeGateway && strings.Contains(detail, "(status 400)") {
		detail += "\n\n`mode = \"gateway\"` requires Pangolin 1.24 or later: an older server " +
			"rejects the mode with a validation error on `mode`."
	}
	return detail
}

// buildCreateSiteResourceRequest maps the plan to the create payload.
// In gateway mode the server-forced attributes are left off the body
// (an empty `destination` would fail its validation).
func buildCreateSiteResourceRequest(plan SitePrivateResourceModel) *client.CreateSiteResourceRequest {
	req := &client.CreateSiteResourceRequest{
		Name:            plan.Name.ValueString(),
		SiteID:          int(plan.SiteID.ValueInt64()),
		Mode:            plan.Mode.ValueString(),
		Destination:     plan.Destination.ValueString(),
		Alias:           plan.Alias.ValueString(),
		TCPPortRange:    plan.TCPPortRange.ValueString(),
		UDPPortRange:    plan.UDPPortRange.ValueString(),
		DisableICMP:     plan.DisableICMP.ValueBool(),
		AuthDaemonMode:  plan.AuthDaemonMode.ValueString(),
		PamMode:         plan.PamMode.ValueString(),
		DomainID:        plan.DomainID.ValueString(),
		Subdomain:       plan.Subdomain.ValueString(),
		Scheme:          plan.Scheme.ValueString(),
		DestinationPort: int(plan.DestinationPort.ValueInt64()),
		RoleIDs:         []int{},
		UserIDs:         []string{},
		ClientIDs:       []int{},
	}
	if plan.Mode.ValueString() == siteResourceModeGateway {
		req.Destination = ""
		req.TCPPortRange = ""
		req.UDPPortRange = ""
		req.DisableICMP = false
	}
	return req
}

// buildUpdateSiteResourceRequest maps the plan to the update payload.
// A gateway resource sends its mode, so that the server re-applies the
// forced gateway values, and no destination.
func buildUpdateSiteResourceRequest(plan SitePrivateResourceModel) *client.UpdateSiteResourceRequest {
	req := &client.UpdateSiteResourceRequest{
		Name:            plan.Name.ValueString(),
		SiteID:          int(plan.SiteID.ValueInt64()),
		Destination:     plan.Destination.ValueString(),
		Alias:           plan.Alias.ValueString(),
		TCPPortRange:    plan.TCPPortRange.ValueString(),
		UDPPortRange:    plan.UDPPortRange.ValueString(),
		DisableICMP:     plan.DisableICMP.ValueBool(),
		AuthDaemonMode:  plan.AuthDaemonMode.ValueString(),
		PamMode:         plan.PamMode.ValueString(),
		DomainID:        plan.DomainID.ValueString(),
		Subdomain:       plan.Subdomain.ValueString(),
		Scheme:          plan.Scheme.ValueString(),
		DestinationPort: int(plan.DestinationPort.ValueInt64()),
		RoleIDs:         []int{},
		UserIDs:         []string{},
		ClientIDs:       []int{},
	}
	if plan.Mode.ValueString() == siteResourceModeGateway {
		req.Mode = siteResourceModeGateway
		req.Destination = ""
		req.TCPPortRange = ""
		req.UDPPortRange = ""
		req.DisableICMP = false
	}
	return req
}

// ValidateConfig requires `destination` outside gateway mode (it is
// Optional in the schema only because gateway mode has none) and
// rejects, in gateway mode, a value the server would overwrite: the
// apply would otherwise end in "Provider produced inconsistent result".
// It also rejects, in gateway mode, the attributes that mode has no
// use for (alias, domain_id, subdomain, scheme, destination_port).
func (r *SitePrivateResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var cfg SitePrivateResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() || cfg.Mode.IsUnknown() || cfg.Mode.IsNull() {
		return
	}

	switch cfg.Mode.ValueString() {
	case siteResourceModeGateway:
		// checked below
	case "host", "cidr", "http":
		if cfg.Destination.IsNull() {
			resp.Diagnostics.AddAttributeError(path.Root("destination"), "Missing destination",
				fmt.Sprintf("`destination` is required when `mode = %q`.", cfg.Mode.ValueString()))
		}
		return
	default:
		// An unsupported mode is already reported by the OneOf validator.
		return
	}

	checkString := func(attr string, v types.String, forced string) {
		if !v.IsNull() && !v.IsUnknown() && v.ValueString() != forced {
			resp.Diagnostics.AddAttributeError(path.Root(attr), "Value overridden in gateway mode",
				fmt.Sprintf("%s The server sets %q.", gatewayForcedNotice, forced))
		}
	}
	checkString("destination", cfg.Destination, gatewayDestination)
	checkString("tcp_port_range", cfg.TCPPortRange, gatewayPortRange)
	checkString("udp_port_range", cfg.UDPPortRange, gatewayPortRange)
	if !cfg.DisableICMP.IsNull() && !cfg.DisableICMP.IsUnknown() && cfg.DisableICMP.ValueBool() {
		resp.Diagnostics.AddAttributeError(path.Root("disable_icmp"), "Value overridden in gateway mode",
			fmt.Sprintf("%s The server sets %t.", gatewayForcedNotice, gatewayDisableICMP))
	}

	// Attributes a gateway resource has no use for: the server would
	// store them silently, so refuse them rather than pretend they do
	// something.
	unused := func(attr string, set bool) {
		if set {
			resp.Diagnostics.AddAttributeError(path.Root(attr), "Attribute not supported in gateway mode",
				fmt.Sprintf("`%s` has no effect when `mode = \"gateway\"`; remove it.", attr))
		}
	}
	knownString := func(v types.String) bool { return !v.IsNull() && !v.IsUnknown() }
	unused("alias", knownString(cfg.Alias))
	unused("domain_id", knownString(cfg.DomainID))
	unused("subdomain", knownString(cfg.Subdomain))
	unused("scheme", knownString(cfg.Scheme))
	unused("destination_port", !cfg.DestinationPort.IsNull() && !cfg.DestinationPort.IsUnknown())
}

// gatewayForcedStringModifier plans the value the server forces in
// gateway mode, so the plan shows it instead of "(known after apply)",
// and an in-place update re-plans it even if the state drifted from it
// (the update body then names the mode, and the server re-forces it).
// A replacement into gateway mode is re-planned by Terraform with a
// null prior state, so UseStateForUnknown cannot carry the previous
// mode's value over in that case.
type gatewayForcedStringModifier struct{ value string }

func gatewayForcedString(value string) planmodifier.String {
	return gatewayForcedStringModifier{value: value}
}

func (m gatewayForcedStringModifier) Description(_ context.Context) string {
	return fmt.Sprintf("Set to %q when mode is gateway.", m.value)
}

func (m gatewayForcedStringModifier) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (m gatewayForcedStringModifier) PlanModifyString(ctx context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	if req.Plan.Raw.IsNull() || !req.ConfigValue.IsNull() || !planIsGatewayMode(ctx, req.Plan, &resp.Diagnostics) {
		return
	}
	resp.PlanValue = types.StringValue(m.value)
}

// gatewayForcedBoolModifier is the bool counterpart of
// gatewayForcedStringModifier.
type gatewayForcedBoolModifier struct{ value bool }

func gatewayForcedBool(value bool) planmodifier.Bool {
	return gatewayForcedBoolModifier{value: value}
}

func (m gatewayForcedBoolModifier) Description(_ context.Context) string {
	return fmt.Sprintf("Set to %t when mode is gateway.", m.value)
}

func (m gatewayForcedBoolModifier) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (m gatewayForcedBoolModifier) PlanModifyBool(ctx context.Context, req planmodifier.BoolRequest, resp *planmodifier.BoolResponse) {
	if req.Plan.Raw.IsNull() || !req.ConfigValue.IsNull() || !planIsGatewayMode(ctx, req.Plan, &resp.Diagnostics) {
		return
	}
	resp.PlanValue = types.BoolValue(m.value)
}

// planIsGatewayMode reports whether the planned `mode` is a known
// "gateway".
func planIsGatewayMode(ctx context.Context, plan tfsdk.Plan, diags *diag.Diagnostics) bool {
	var mode types.String
	diags.Append(plan.GetAttribute(ctx, path.Root("mode"), &mode)...)
	if diags.HasError() {
		return false
	}
	return !mode.IsUnknown() && !mode.IsNull() && mode.ValueString() == siteResourceModeGateway
}

// hydrateSiteResourceState copies every server-owned field from a
// freshly-fetched SiteResource into the TF state model. The caller
// must populate state.ID separately (it's the import-time key and
// not always present on the wire - e.g. on Create the response does
// carry siteResourceId, but we set it explicitly to keep the codepaths
// symmetric).
//
// site_id is derived from SiteIDs[0] when the slice is populated
// (LIST/Import codepath). On Create/Update the upstream response
// omits siteIds entirely, so the existing state value (the user's
// plan input) is preserved.
func hydrateSiteResourceState(state *SitePrivateResourceModel, siteRes *client.SiteResource) {
	state.NiceID = types.StringValue(siteRes.NiceID)
	state.Name = types.StringValue(siteRes.Name)
	state.Mode = types.StringValue(siteRes.Mode)
	state.Destination = types.StringValue(siteRes.Destination)
	state.Alias = types.StringValue(siteRes.Alias)
	state.TCPPortRange = types.StringValue(siteRes.TCPPortRange)
	state.UDPPortRange = types.StringValue(siteRes.UDPPortRange)
	state.DisableICMP = types.BoolValue(siteRes.DisableICMP)
	state.AuthDaemonMode = types.StringValue(siteRes.AuthDaemonMode)
	state.AuthDaemonPort = types.Int64Value(int64(siteRes.AuthDaemonPort))
	if siteRes.PamMode != "" {
		state.PamMode = types.StringValue(siteRes.PamMode)
	} else {
		state.PamMode = types.StringNull()
	}
	state.Enabled = types.BoolValue(siteRes.Enabled)
	state.SSL = types.BoolValue(siteRes.SSL)
	state.NetworkID = types.Int64Value(int64(siteRes.NetworkID))

	if len(siteRes.SiteIDs) > 0 {
		state.SiteID = types.Int64Value(int64(siteRes.SiteIDs[0]))
	}

	state.Scheme = tfconv.StringFromPtr(siteRes.Scheme)
	state.AliasAddress = tfconv.StringFromPtr(siteRes.AliasAddress)
	state.DomainID = tfconv.StringFromPtr(siteRes.DomainID)
	state.Subdomain = tfconv.StringFromPtr(siteRes.Subdomain)
	state.FullDomain = tfconv.StringFromPtr(siteRes.FullDomain)
	state.ProxyPort = tfconv.Int64FromIntPtr(siteRes.ProxyPort)
	state.DestinationPort = tfconv.Int64FromIntPtr(siteRes.DestinationPort)
	state.DefaultNetworkID = tfconv.Int64FromIntPtr(siteRes.DefaultNetworkID)
}
