package resources

import (
	"context"
	"errors"
	"fmt"
	"net"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setplanmodifier"
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
	_ resource.ResourceWithModifyPlan     = &SitePrivateResource{}
)

// siteResourceModeGateway is the 1.24+ "exit node" mode: the site
// routes all traffic, and the server forces the values below whatever
// the request says.
const siteResourceModeGateway = "gateway"

// siteResourceModeSSH and siteResourceModeInference are the other modes
// whose port ranges and ICMP setting the server forces (see
// forcedSiteResourceValues).
const (
	siteResourceModeSSH       = "ssh"
	siteResourceModeInference = "inference"
)

// sshDefaultPort is the TCP port range the server forces for an ssh
// resource without destination_port.
const sshDefaultPort = "22"

// niceIDPattern is the server's niceId rule on update.
var niceIDPattern = regexp.MustCompile(`^[a-zA-Z0-9-]+$`)

const (
	gatewayDestination = "0.0.0.0/0"
	gatewayPortRange   = "*"
	gatewayDisableICMP = false
)

// httpTCPPortRange is the TCP port range the server forces when it
// creates an http site resource.
const httpTCPPortRange = "443,80"

// SitePrivateResource defines the resource implementation.
type SitePrivateResource struct {
	client *client.Client
}

// SitePrivateResourceModel describes the resource data model.
type SitePrivateResourceModel struct {
	ID               types.Int64  `tfsdk:"id"`
	NiceID           types.String `tfsdk:"nice_id"`
	SiteID           types.Int64  `tfsdk:"site_id"`
	SiteIDs          types.Set    `tfsdk:"site_ids"`
	AIProviderIDs    types.Set    `tfsdk:"ai_provider_ids"`
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
				Description: "The human-readable ID. Generated by the server when unset; letters, digits and dashes only.",
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
				Validators: []validator.String{
					stringvalidator.LengthBetween(1, 255),
					stringvalidator.RegexMatches(niceIDPattern, "may only contain letters, digits and dashes"),
				},
			},
			"site_id": schema.Int64Attribute{
				Description: "The site this resource belongs to. Deprecated: use `site_ids`, which also supports " +
					"several sites. When the resource has exactly one site, `site_id` reports it; otherwise it is " +
					"null. Conflicts with `site_ids`.",
				DeprecationMessage: "Use site_ids instead.",
				Optional:           true,
				Computed:           true,
				Validators: []validator.Int64{
					int64validator.AtLeast(1),
					int64validator.ConflictsWith(path.MatchRoot("site_ids")),
				},
			},
			"site_ids": schema.SetAttribute{
				Description: "The sites this resource is reachable through (several sites need Pangolin 1.19+). " +
					"Required, through this attribute or `site_id`, in every mode except `inference`, which has " +
					"no site. Changing the sites updates the resource in place. Conflicts with `site_id`.",
				ElementType: types.Int64Type,
				Optional:    true,
				Computed:    true,
				Validators: []validator.Set{
					setvalidator.SizeAtLeast(1),
					setvalidator.ValueInt64sAre(int64validator.AtLeast(1)),
				},
			},
			"ai_provider_ids": schema.SetAttribute{
				Description: "IDs of the AI providers attached to an `inference` resource (the providers " +
					"themselves are managed outside this provider for now). New attachments use the provider's own " +
					"allow / block lists (`inherit`); existing ones keep their access mode. Leaving the attribute unset " +
					"keeps the current attachments; `[]` removes them all. Only valid for `mode = \"inference\"`.",
				ElementType: types.Int64Type,
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.Set{
					setplanmodifier.UseStateForUnknown(),
				},
				Validators: []validator.Set{
					setvalidator.ValueInt64sAre(int64validator.AtLeast(1)),
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
					"`domain_id`, `subdomain`, `scheme` and `destination_port` have no use in this mode and are rejected.\n\n" +
					"`ssh` mode exposes an SSH server: `destination` is the host (IPv4 address, or a hostname together " +
					"with `alias`), optional when `auth_daemon_mode = \"native\"`. The server forces `tcp_port_range` to " +
					"`destination_port` (`\"22\"` when unset), `udp_port_range = \"\"` and `disable_icmp = true`. With " +
					"`auth_daemon_mode = \"native\"`, or `pam_mode = \"push\"` with the site auth daemon, the resource " +
					"takes a single site.\n\n" +
					"`inference` mode (Pangolin 1.22+) serves AI providers (`ai_provider_ids`) through the exit node: it has " +
					"no site and no required destination; the server forces `tcp_port_range = \"443,80\"`, " +
					"`udp_port_range = \"\"` and `disable_icmp = true`.",
				Required: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{
					stringvalidator.OneOf("host", "cidr", "http", siteResourceModeSSH, siteResourceModeInference, siteResourceModeGateway),
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
				Description: "TCP port range string. `*` for all, `` for none, or specific ports/ranges (e.g. `80,443,8080-8090`). For `mode = http` the server forces `\"443,80\"` and for `mode = gateway` it forces `\"*\"`; any other value is rejected in those modes.",
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
					gatewayForcedString(gatewayPortRange),
				},
			},
			"udp_port_range": schema.StringAttribute{
				Description: "UDP port range string. '*' for all, '' for none, or specific ports/ranges. For `mode = gateway` the server forces `\"*\"` and any other value is rejected. For `mode = http` the value has no effect: Pangolin 1.24 stores `\"\"` and older servers store it unused, so a non-empty value only raises a warning and is kept as configured in state.",
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
					gatewayForcedString(gatewayPortRange),
				},
			},
			"disable_icmp": schema.BoolAttribute{
				Description: "Whether to disable ICMP. The server defaults to `false` for `cidr` / `host` modes, forces `true` for `mode = http` and `false` for `mode = gateway` (any other value is rejected in those two modes); leaving this unset lets the server pick.",
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.UseStateForUnknown(),
					gatewayForcedBool(gatewayDisableICMP),
				},
			},
			"auth_daemon_mode": schema.StringAttribute{
				Description: "Auth daemon mode for `ssh` resources: `site` (the site's Newt), `remote` (a daemon on the " +
					"destination host, see `auth_daemon_port`) or `native` (the client connects to the host's own SSH " +
					"server; `destination` becomes optional). Defaults to `site`.",
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString("site"),
				Validators: []validator.String{
					stringvalidator.OneOf("site", "remote", "native"),
				},
			},
			"auth_daemon_port": schema.Int64Attribute{
				Description: "Port of the remote auth daemon (`auth_daemon_mode = \"remote\"`). Server-assigned when unset.",
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
				Validators: []validator.Int64{
					int64validator.Between(1, 65535),
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
				Description: "Whether the resource is enabled. New resources are enabled; setting `false` disables the " +
					"resource right after creating it (the create endpoint has no such field).",
				Optional: true,
				Computed: true,
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.UseStateForUnknown(),
				},
			},
			"ssl": schema.BoolAttribute{
				Description: "Whether Pangolin terminates TLS for `full_domain` (`http` and `inference` modes; a certificate " +
					"is requested on create). Ignored in the other modes.",
				Optional: true,
				Computed: true,
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

	createReq, diags := buildCreateSiteResourceRequest(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	siteRes, err := r.client.CreateSiteResource(ctx, createReq)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create site resource", createSiteResourceErrorDetail(plan.Mode.ValueString(), err))
		return
	}

	plan.ID = types.Int64Value(int64(siteRes.SiteResourceID))
	// Saved before the follow-up calls: if one fails, Terraform taints
	// the resource instead of losing track of it.
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), plan.ID)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// The create endpoint has no `enabled` field: disable it afterwards.
	if !plan.Enabled.IsNull() && !plan.Enabled.IsUnknown() && !plan.Enabled.ValueBool() {
		updateReq, diags := buildUpdateSiteResourceRequest(ctx, &plan)
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}
		updated, err := r.client.UpdateSiteResource(ctx, siteRes.SiteResourceID, updateReq)
		if err != nil {
			resp.Diagnostics.AddError("Failed to disable site resource after creation", err.Error())
			return
		}
		siteRes = updated
	}

	hydrateSiteResourceState(&plan, siteRes)
	resp.Diagnostics.Append(r.readAIProviders(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

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
	resp.Diagnostics.Append(r.readAIProviders(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *SitePrivateResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan SitePrivateResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state SitePrivateResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	updateReq, diags := buildUpdateSiteResourceRequest(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	siteRes, err := r.client.UpdateSiteResource(ctx, int(plan.ID.ValueInt64()), updateReq)
	if err != nil {
		resp.Diagnostics.AddError("Failed to update site resource", err.Error())
		return
	}

	if plan.Mode.ValueString() == siteResourceModeInference && !plan.AIProviderIDs.IsUnknown() && !plan.AIProviderIDs.Equal(state.AIProviderIDs) {
		resp.Diagnostics.Append(r.setAIProviders(ctx, int(plan.ID.ValueInt64()), plan.AIProviderIDs)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	hydrateSiteResourceState(&plan, siteRes)
	resp.Diagnostics.Append(r.readAIProviders(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// readAIProviders fills ai_provider_ids from the server for an inference
// resource, and nulls it for the other modes (which have none, and whose
// API key may lack the AI provider actions).
func (r *SitePrivateResource) readAIProviders(ctx context.Context, m *SitePrivateResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics
	if m.Mode.ValueString() != siteResourceModeInference {
		m.AIProviderIDs = types.SetNull(types.Int64Type)
		return diags
	}
	providers, err := r.client.ListSiteResourceAIProviders(ctx, int(m.ID.ValueInt64()))
	if err != nil {
		diags.AddError("Failed to read site resource AI providers", err.Error())
		return diags
	}
	ids := make([]int, 0, len(providers))
	for _, p := range providers {
		ids = append(ids, p.ProviderID)
	}
	m.AIProviderIDs = siteResourceIntSet(ids)
	return diags
}

// setAIProviders replaces the AI provider attachments with the given
// IDs. Providers that stay attached keep their current access mode and
// enabled flag (POST /ai-providers would otherwise reset them to
// inherit / enabled); new ones are attached in inherit mode.
func (r *SitePrivateResource) setAIProviders(ctx context.Context, siteResourceID int, ids types.Set) diag.Diagnostics {
	wanted, diags := siteResourceSetToInts(ctx, ids)
	if diags.HasError() {
		return diags
	}
	current, err := r.client.ListSiteResourceAIProviders(ctx, siteResourceID)
	if err != nil {
		diags.AddError("Failed to read site resource AI providers", err.Error())
		return diags
	}
	byID := make(map[int]client.SiteResourceAIProvider, len(current))
	for _, p := range current {
		byID[p.ProviderID] = p
	}
	inputs := make([]client.SiteResourceAIProviderInput, 0, len(wanted))
	for _, id := range wanted {
		in := client.SiteResourceAIProviderInput{ProviderID: id}
		if p, ok := byID[id]; ok {
			enabled := p.Enabled
			in.AccessMode = p.AccessMode
			in.Enabled = &enabled
		}
		inputs = append(inputs, in)
	}
	if err := r.client.SetSiteResourceAIProviders(ctx, siteResourceID, inputs); err != nil {
		diags.AddError("Failed to set site resource AI providers", err.Error())
	}
	return diags
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
	resp.Diagnostics.Append(r.readAIProviders(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// createSiteResourceErrorDetail adds a version hint to a create error
// in gateway mode: a pre-1.24 server rejects the mode itself with a
// validation error ("Invalid option: expected one of ... at \"mode\"").
// Errors that name another field (duplicate name, bad site_id) get no
// hint.
func createSiteResourceErrorDetail(mode string, err error) string {
	detail := err.Error()
	if mode == siteResourceModeGateway && errors.Is(err, client.ErrBadRequest) && strings.Contains(detail, `"mode"`) {
		detail += "\n\n`mode = \"gateway\"` requires Pangolin 1.24 or later: an older server " +
			"rejects the mode with a validation error on `mode`."
	}
	return detail
}

// buildCreateSiteResourceRequest maps the plan to the create payload.
// In gateway mode the server-forced attributes are left off the body
// (an empty `destination` would fail its validation). disable_icmp is
// always planned false there, which the omitempty tag already drops.
func buildCreateSiteResourceRequest(ctx context.Context, plan SitePrivateResourceModel) (*client.CreateSiteResourceRequest, diag.Diagnostics) {
	siteID, siteIDs, diags := siteResourceSites(ctx, plan)
	if diags.HasError() {
		return nil, diags
	}
	req := &client.CreateSiteResourceRequest{
		Name:            plan.Name.ValueString(),
		NiceID:          knownString(plan.NiceID),
		SiteID:          siteID,
		SiteIDs:         siteIDs,
		Mode:            plan.Mode.ValueString(),
		SSL:             tfconv.BoolToPtr(plan.SSL),
		AuthDaemonPort:  int(knownInt64(plan.AuthDaemonPort)),
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
	}
	if plan.Mode.ValueString() == siteResourceModeInference && !plan.AIProviderIDs.IsNull() && !plan.AIProviderIDs.IsUnknown() {
		ids, d := siteResourceSetToInts(ctx, plan.AIProviderIDs)
		diags.Append(d...)
		for _, id := range ids {
			req.AIProviders = append(req.AIProviders, client.SiteResourceAIProviderInput{ProviderID: id})
		}
	}
	return req, diags
}

// siteResourceSites returns the site attachment for a request: a single
// site goes in `siteId`, which every server version accepts, several in
// `siteIds` (1.24+). An inference resource has none.
func siteResourceSites(ctx context.Context, plan SitePrivateResourceModel) (int, []int, diag.Diagnostics) {
	var diags diag.Diagnostics
	if plan.Mode.ValueString() == siteResourceModeInference {
		return 0, nil, diags
	}
	var ids []int
	switch {
	case !plan.SiteIDs.IsNull() && !plan.SiteIDs.IsUnknown():
		ids, diags = siteResourceSetToInts(ctx, plan.SiteIDs)
	case !plan.SiteID.IsNull() && !plan.SiteID.IsUnknown():
		ids = []int{int(plan.SiteID.ValueInt64())}
	}
	if len(ids) == 1 {
		return ids[0], nil, diags
	}
	return 0, ids, diags
}

// siteResourceSetToInts returns the sorted elements of a known set of
// numbers.
func siteResourceSetToInts(ctx context.Context, set types.Set) ([]int, diag.Diagnostics) {
	var vals []int64
	diags := set.ElementsAs(ctx, &vals, false)
	ids := make([]int, 0, len(vals))
	for _, v := range vals {
		ids = append(ids, int(v))
	}
	sort.Ints(ids)
	return ids, diags
}

// siteResourceIntSet builds a set of numbers; nil gives an empty set.
func siteResourceIntSet(ids []int) types.Set {
	elems := make([]attr.Value, 0, len(ids))
	for _, id := range ids {
		elems = append(elems, types.Int64Value(int64(id)))
	}
	return types.SetValueMust(types.Int64Type, elems)
}

// knownString returns the value of a known string, "" otherwise.
func knownString(v types.String) string {
	if v.IsNull() || v.IsUnknown() {
		return ""
	}
	return v.ValueString()
}

// knownInt64 returns the value of a known number, 0 otherwise.
func knownInt64(v types.Int64) int64 {
	if v.IsNull() || v.IsUnknown() {
		return 0
	}
	return v.ValueInt64()
}

// buildUpdateSiteResourceRequest maps the plan to the update payload.
// The port ranges and disable_icmp are sent whenever they are known, so
// `""` and `false` reach the server instead of being dropped. The grant
// lists are left nil: the server keeps the current grants. A gateway
// resource sends its mode, so that the server re-applies the forced
// gateway values, and no destination or port ranges. An ssh or
// inference resource sends its mode too, for its forced port ranges and
// ICMP setting.
func buildUpdateSiteResourceRequest(ctx context.Context, plan *SitePrivateResourceModel) (*client.UpdateSiteResourceRequest, diag.Diagnostics) {
	siteID, siteIDs, diags := siteResourceSites(ctx, *plan)
	if diags.HasError() {
		return nil, diags
	}
	req := &client.UpdateSiteResourceRequest{
		Name:            plan.Name.ValueString(),
		NiceID:          knownString(plan.NiceID),
		SiteID:          siteID,
		SiteIDs:         siteIDs,
		Enabled:         tfconv.BoolToPtr(plan.Enabled),
		SSL:             tfconv.BoolToPtr(plan.SSL),
		AuthDaemonPort:  int(knownInt64(plan.AuthDaemonPort)),
		Destination:     plan.Destination.ValueString(),
		Alias:           plan.Alias.ValueString(),
		TCPPortRange:    tfconv.StringToPtr(plan.TCPPortRange),
		UDPPortRange:    tfconv.StringToPtr(plan.UDPPortRange),
		DisableICMP:     tfconv.BoolToPtr(plan.DisableICMP),
		AuthDaemonMode:  plan.AuthDaemonMode.ValueString(),
		PamMode:         plan.PamMode.ValueString(),
		DomainID:        plan.DomainID.ValueString(),
		Subdomain:       plan.Subdomain.ValueString(),
		Scheme:          plan.Scheme.ValueString(),
		DestinationPort: int(plan.DestinationPort.ValueInt64()),
	}
	switch plan.Mode.ValueString() {
	case siteResourceModeGateway:
		req.Mode = siteResourceModeGateway
		req.Destination = ""
		req.TCPPortRange = nil
		req.UDPPortRange = nil
	case siteResourceModeSSH, siteResourceModeInference:
		req.Mode = plan.Mode.ValueString()
	}
	return req, diags
}

// ValidateConfig requires `destination` outside gateway mode (it is
// Optional in the schema only because gateway mode has none) and
// rejects, in gateway mode, a value the server would overwrite: the
// apply would otherwise end in "Provider produced inconsistent result".
// It also rejects, in gateway mode, the attributes that mode has no
// use for (alias, domain_id, subdomain, scheme, destination_port).
// An unknown `mode` is skipped here: Terraform validates the config
// again with the final values when it plans the resource, so a mode
// known only at apply still gets these errors before any API call.
func (r *SitePrivateResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var cfg SitePrivateResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() || cfg.Mode.IsUnknown() || cfg.Mode.IsNull() {
		return
	}

	mode := cfg.Mode.ValueString()
	checkString := func(attr string, v types.String, forced string) {
		if !v.IsNull() && !v.IsUnknown() && v.ValueString() != forced {
			resp.Diagnostics.AddAttributeError(path.Root(attr), fmt.Sprintf("Value overridden in %s mode", mode),
				fmt.Sprintf("Pangolin forces this value for `mode = %q`; leave the attribute unset. The server sets %q.", mode, forced))
		}
	}
	checkBool := func(attr string, v types.Bool, forced bool) {
		if !v.IsNull() && !v.IsUnknown() && v.ValueBool() != forced {
			resp.Diagnostics.AddAttributeError(path.Root(attr), fmt.Sprintf("Value overridden in %s mode", mode),
				fmt.Sprintf("Pangolin forces this value for `mode = %q`; leave the attribute unset. The server sets %t.", mode, forced))
		}
	}

	validateSiteResourceSites(cfg, &resp.Diagnostics)
	if mode != siteResourceModeInference && !cfg.AIProviderIDs.IsNull() {
		resp.Diagnostics.AddAttributeError(path.Root("ai_provider_ids"), "AI providers need inference mode",
			"`ai_provider_ids` is only valid when `mode = \"inference\"`.")
	}

	switch mode {
	case siteResourceModeGateway:
		// checked below
	case siteResourceModeSSH, siteResourceModeInference:
		validateSSHOrInferenceConfig(cfg, resp)
		return
	case "host", "cidr", "http":
		if cfg.Destination.IsNull() {
			resp.Diagnostics.AddAttributeError(path.Root("destination"), "Missing destination",
				fmt.Sprintf("`destination` is required when `mode = %q`.", mode))
		}
		if mode == "http" {
			// The create handler forces these for an http resource
			// (tcp "443,80" since 1.20 and ICMP disabled), so any other
			// configured value ends in an inconsistent result.
			checkString("tcp_port_range", cfg.TCPPortRange, httpTCPPortRange)
			checkBool("disable_icmp", cfg.DisableICMP, true)
			// UDP has no use in http mode: 1.24 stores "", older
			// servers store the value unused. Warn only; the configured
			// value is kept in state (see hydrateSiteResourceState).
			if v := cfg.UDPPortRange; !v.IsNull() && !v.IsUnknown() && v.ValueString() != "" {
				resp.Diagnostics.AddAttributeWarning(path.Root("udp_port_range"), "Value ignored in http mode",
					"`udp_port_range` has no effect when `mode = \"http\"`; Pangolin 1.24 stores \"\" instead. "+
						"The configured value is kept in the Terraform state. Remove it to silence this warning.")
			}
		}
		return
	default:
		// An unsupported mode is already reported by the OneOf validator.
		return
	}

	checkString("destination", cfg.Destination, gatewayDestination)
	checkString("tcp_port_range", cfg.TCPPortRange, gatewayPortRange)
	checkString("udp_port_range", cfg.UDPPortRange, gatewayPortRange)
	checkBool("disable_icmp", cfg.DisableICMP, gatewayDisableICMP)

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

// validateSiteResourceSites requires a site (site_id or site_ids)
// except in inference mode, which has none, and allows a single site
// for the SSH daemon modes the server limits to one.
func validateSiteResourceSites(cfg SitePrivateResourceModel, diags *diag.Diagnostics) {
	mode := cfg.Mode.ValueString()
	hasSite := !cfg.SiteID.IsNull() || !cfg.SiteIDs.IsNull()
	if mode == siteResourceModeInference {
		if hasSite {
			diags.AddAttributeError(path.Root("site_ids"), "Inference resources have no site",
				"An `inference` resource is served through the exit node, not a site: remove `site_ids` / `site_id`.")
		}
		return
	}
	if !hasSite {
		diags.AddAttributeError(path.Root("site_ids"), "Missing site",
			fmt.Sprintf("`site_ids` (or the deprecated `site_id`) is required when `mode = %q`.", mode))
		return
	}
	if mode != siteResourceModeSSH || cfg.SiteIDs.IsNull() || cfg.SiteIDs.IsUnknown() || len(cfg.SiteIDs.Elements()) <= 1 {
		return
	}
	if cfg.AuthDaemonMode.IsUnknown() || cfg.PamMode.IsUnknown() {
		return
	}
	daemon := cfg.AuthDaemonMode.ValueString()
	if cfg.AuthDaemonMode.IsNull() {
		daemon = "site"
	}
	if daemon == "native" || (cfg.PamMode.ValueString() == "push" && daemon == "site") {
		diags.AddAttributeError(path.Root("site_ids"), "Single site required",
			"Pangolin allows only one site for an `ssh` resource with `auth_daemon_mode = \"native\"`, "+
				"or with `pam_mode = \"push\"` and the site auth daemon.")
	}
}

// validateSSHOrInferenceConfig checks the ssh and inference rules: the
// server-forced port ranges and ICMP setting, and for ssh a destination
// (unless the native auth daemon is used) that is an IPv4 address, or a
// hostname together with an alias.
func validateSSHOrInferenceConfig(cfg SitePrivateResourceModel, resp *resource.ValidateConfigResponse) {
	mode := cfg.Mode.ValueString()
	if mode == siteResourceModeSSH && !cfg.AuthDaemonMode.IsUnknown() {
		native := cfg.AuthDaemonMode.ValueString() == "native"
		if cfg.Destination.IsNull() && !native {
			resp.Diagnostics.AddAttributeError(path.Root("destination"), "Missing destination",
				"`destination` is required when `mode = \"ssh\"`, unless `auth_daemon_mode = \"native\"`.")
		}
	}
	if mode == siteResourceModeSSH && !cfg.Destination.IsNull() && !cfg.Destination.IsUnknown() && !cfg.Alias.IsUnknown() {
		if ip := net.ParseIP(cfg.Destination.ValueString()); (ip == nil || ip.To4() == nil) && cfg.Alias.IsNull() {
			resp.Diagnostics.AddAttributeError(path.Root("alias"), "Missing alias",
				"An `ssh` resource whose `destination` is a hostname (not an IPv4 address) needs an `alias`.")
		}
	}

	tcp, udp, icmp, known := forcedSiteResourceValues(mode, cfg.DestinationPort, cfg.DestinationPort.IsNull())
	if !known {
		return
	}
	forcedString := func(attr string, v types.String, forced string) {
		if !v.IsNull() && !v.IsUnknown() && v.ValueString() != forced {
			resp.Diagnostics.AddAttributeError(path.Root(attr), fmt.Sprintf("Value overridden in %s mode", mode),
				fmt.Sprintf("Pangolin forces this value for `mode = %q`; leave the attribute unset. The server sets %q.", mode, forced))
		}
	}
	forcedString("tcp_port_range", cfg.TCPPortRange, tcp)
	forcedString("udp_port_range", cfg.UDPPortRange, udp)
	if !cfg.DisableICMP.IsNull() && !cfg.DisableICMP.IsUnknown() && cfg.DisableICMP.ValueBool() != icmp {
		resp.Diagnostics.AddAttributeError(path.Root("disable_icmp"), fmt.Sprintf("Value overridden in %s mode", mode),
			fmt.Sprintf("Pangolin forces this value for `mode = %q`; leave the attribute unset. The server sets %t.", mode, icmp))
	}
}

// forcedSiteResourceValues returns the port ranges and ICMP setting the
// server forces for an ssh or inference resource. For ssh the TCP range
// is the destination port, `"22"` without one; known is false when
// that port is unknown, or for another mode.
func forcedSiteResourceValues(mode string, destinationPort types.Int64, portUnset bool) (tcp, udp string, disableICMP, known bool) {
	switch mode {
	case siteResourceModeInference:
		return httpTCPPortRange, "", true, true
	case siteResourceModeSSH:
		switch {
		case !destinationPort.IsNull() && !destinationPort.IsUnknown():
			return strconv.FormatInt(destinationPort.ValueInt64(), 10), "", true, true
		case portUnset:
			return sshDefaultPort, "", true, true
		}
	}
	return "", "", false, false
}

// ModifyPlan keeps site_id and site_ids in step (the one left unset in
// config follows the other), and plans the values the server forces in
// ssh and inference mode when they are left unset, so the plan shows
// them and an update re-plans them after a destination_port change.
func (r *SitePrivateResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return
	}
	var cfg, plan SitePrivateResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	modeKnown := !plan.Mode.IsNull() && !plan.Mode.IsUnknown()
	mode := plan.Mode.ValueString()

	switch {
	case !cfg.SiteIDs.IsNull():
		plan.SiteID = types.Int64Unknown()
		if !cfg.SiteIDs.IsUnknown() {
			ids, diags := siteResourceSetToInts(ctx, cfg.SiteIDs)
			resp.Diagnostics.Append(diags...)
			plan.SiteID = types.Int64Null()
			if len(ids) == 1 {
				plan.SiteID = types.Int64Value(int64(ids[0]))
			}
		}
	case !cfg.SiteID.IsNull():
		plan.SiteIDs = types.SetUnknown(types.Int64Type)
		if !cfg.SiteID.IsUnknown() {
			plan.SiteIDs = siteResourceIntSet([]int{int(cfg.SiteID.ValueInt64())})
		}
	case modeKnown && mode == siteResourceModeInference:
		plan.SiteID = types.Int64Null()
		plan.SiteIDs = siteResourceIntSet(nil)
	}

	if modeKnown && mode != siteResourceModeInference {
		plan.AIProviderIDs = types.SetNull(types.Int64Type)
	}

	if modeKnown && (mode == siteResourceModeSSH || mode == siteResourceModeInference) {
		tcp, udp, icmp, known := forcedSiteResourceValues(mode, plan.DestinationPort, plan.DestinationPort.IsNull() || (cfg.DestinationPort.IsNull() && plan.DestinationPort.IsUnknown()))
		if cfg.TCPPortRange.IsNull() {
			plan.TCPPortRange = types.StringUnknown()
			if known {
				plan.TCPPortRange = types.StringValue(tcp)
			}
		}
		if cfg.UDPPortRange.IsNull() && known {
			plan.UDPPortRange = types.StringValue(udp)
		}
		if cfg.DisableICMP.IsNull() && known {
			plan.DisableICMP = types.BoolValue(icmp)
		}
	}

	resp.Diagnostics.Append(resp.Plan.Set(ctx, &plan)...)
}

// httpUDPPortRange keeps a known non-empty prior/planned UDP range when an
// http-mode resource reads back "": Pangolin 1.24 forces "" there while
// the value has no effect in that mode, so keeping the configured value
// avoids an inconsistent result without hiding real drift elsewhere.
func httpUDPPortRange(prior types.String, siteRes *client.SiteResource) types.String {
	if siteRes.Mode == "http" && siteRes.UDPPortRange == "" &&
		!prior.IsNull() && !prior.IsUnknown() && prior.ValueString() != "" {
		return prior
	}
	return types.StringValue(siteRes.UDPPortRange)
}

// gatewayForcedModifier plans the value the server forces in gateway
// mode, so the plan shows it instead of "(known after apply)", and an
// in-place update re-plans it even if the state drifted from it (the
// update body then names the mode, and the server re-forces it).
// A replacement into gateway mode is re-planned by Terraform with a
// null prior state, so UseStateForUnknown cannot carry the previous
// mode's value over in that case. One type serves the string and the
// bool attributes: only the field matching the attribute is set.
type gatewayForcedModifier struct {
	str types.String
	b   types.Bool
}

func gatewayForcedString(value string) planmodifier.String {
	return gatewayForcedModifier{str: types.StringValue(value)}
}

func gatewayForcedBool(value bool) planmodifier.Bool {
	return gatewayForcedModifier{b: types.BoolValue(value)}
}

func (m gatewayForcedModifier) Description(_ context.Context) string {
	if !m.str.IsNull() {
		return fmt.Sprintf("Set to %q when mode is gateway.", m.str.ValueString())
	}
	return fmt.Sprintf("Set to %t when mode is gateway.", m.b.ValueBool())
}

func (m gatewayForcedModifier) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

// applies reports whether the forced value must be planned: the
// resource is not being destroyed, the attribute is unset in config
// and the planned mode is a known "gateway".
func (m gatewayForcedModifier) applies(ctx context.Context, plan tfsdk.Plan, configNull bool, diags *diag.Diagnostics) bool {
	return !plan.Raw.IsNull() && configNull && planIsGatewayMode(ctx, plan, diags)
}

func (m gatewayForcedModifier) PlanModifyString(ctx context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	if m.applies(ctx, req.Plan, req.ConfigValue.IsNull(), &resp.Diagnostics) {
		resp.PlanValue = m.str
	}
}

func (m gatewayForcedModifier) PlanModifyBool(ctx context.Context, req planmodifier.BoolRequest, resp *planmodifier.BoolResponse) {
	if m.applies(ctx, req.Plan, req.ConfigValue.IsNull(), &resp.Diagnostics) {
		resp.PlanValue = m.b
	}
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
// site_ids comes from SiteIDs when the response carries it (LIST /
// Import / Read codepath; an inference resource has an empty list), and
// site_id is its only element, or null with several sites. On
// Create/Update the upstream response omits siteIds entirely, so the
// planned values are preserved.
func hydrateSiteResourceState(state *SitePrivateResourceModel, siteRes *client.SiteResource) {
	state.NiceID = types.StringValue(siteRes.NiceID)
	state.Name = types.StringValue(siteRes.Name)
	state.Mode = types.StringValue(siteRes.Mode)
	state.Destination = types.StringValue(siteRes.Destination)
	state.Alias = types.StringValue(siteRes.Alias)
	state.TCPPortRange = types.StringValue(siteRes.TCPPortRange)
	state.UDPPortRange = httpUDPPortRange(state.UDPPortRange, siteRes)
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

	if siteRes.SiteIDs != nil {
		state.SiteIDs = siteResourceIntSet(siteRes.SiteIDs)
		if len(siteRes.SiteIDs) == 1 {
			state.SiteID = types.Int64Value(int64(siteRes.SiteIDs[0]))
		} else {
			state.SiteID = types.Int64Null()
		}
	}
	if state.SiteIDs.IsNull() || state.SiteIDs.IsUnknown() {
		// Create/Update without a prior LIST: derive from site_id.
		if !state.SiteID.IsNull() && !state.SiteID.IsUnknown() {
			state.SiteIDs = siteResourceIntSet([]int{int(state.SiteID.ValueInt64())})
		} else {
			state.SiteIDs = siteResourceIntSet(nil)
		}
	}
	if state.SiteID.IsUnknown() {
		state.SiteID = types.Int64Null()
	}
	if state.AIProviderIDs.IsUnknown() {
		state.AIProviderIDs = types.SetNull(types.Int64Type)
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
