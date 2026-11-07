package resources

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"regexp"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
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
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stackopshq/terraform-provider-pangolin/internal/client"
	"github.com/stackopshq/terraform-provider-pangolin/internal/tfconv"
)

var (
	_ resource.Resource                   = &HTTPResource{}
	_ resource.ResourceWithImportState    = &HTTPResource{}
	_ resource.ResourceWithValidateConfig = &HTTPResource{}
)

// HTTPResource defines the resource implementation.
type HTTPResource struct {
	client *client.Client
}

// ResourceHeaderModel mirrors one injected request header for HCL input.
type ResourceHeaderModel struct {
	Name  types.String `tfsdk:"name"`
	Value types.String `tfsdk:"value"`
}

// HTTPResourceModel describes the resource data model.
type HTTPResourceModel struct {
	ID                     types.Int64  `tfsdk:"id"`
	NiceID                 types.String `tfsdk:"nice_id"`
	Name                   types.String `tfsdk:"name"`
	Subdomain              types.String `tfsdk:"subdomain"`
	FullDomain             types.String `tfsdk:"full_domain"`
	DomainID               types.String `tfsdk:"domain_id"`
	Protocol               types.String `tfsdk:"protocol"`
	SSO                    types.Bool   `tfsdk:"sso"`
	SSL                    types.Bool   `tfsdk:"ssl"`
	Enabled                types.Bool   `tfsdk:"enabled"`
	BlockAccess            types.Bool   `tfsdk:"block_access"`
	EmailWhitelistEnabled  types.Bool   `tfsdk:"email_whitelist_enabled"`
	ApplyRules             types.Bool   `tfsdk:"apply_rules"`
	StickySession          types.Bool   `tfsdk:"sticky_session"`
	LoadBalancingStrategy  types.String `tfsdk:"load_balancing_strategy"`
	ForwardIdentityHeaders types.Bool   `tfsdk:"forward_identity_headers"`
	TLSServerName          types.String `tfsdk:"tls_server_name"`

	// 1.19+ mode / PAM / auth-daemon (create-only).
	Mode           types.String `tfsdk:"mode"`
	ProxyPort      types.Int64  `tfsdk:"proxy_port"`
	PamMode        types.String `tfsdk:"pam_mode"`
	AuthDaemonMode types.String `tfsdk:"auth_daemon_mode"`
	AuthDaemonPort types.Int64  `tfsdk:"auth_daemon_port"`

	// 1.22+ inference mode: attached AI providers.
	AIProviderIDs types.Set `tfsdk:"ai_provider_ids"`

	// 1.19+ mutable routing / auth injection.
	SetHostHeader types.String          `tfsdk:"set_host_header"`
	EnableProxy   types.Bool            `tfsdk:"enable_proxy"`
	Headers       []ResourceHeaderModel `tfsdk:"headers"`
	// 1.24+ response headers.
	ResponseHeaders []ResourceHeaderModel `tfsdk:"response_headers"`
	SkipToIdpID     types.Int64           `tfsdk:"skip_to_idp_id"`
	PostAuthPath    types.String          `tfsdk:"post_auth_path"`

	// 1.19+ mutable proxy-protocol config.
	ProxyProtocol        types.Bool  `tfsdk:"proxy_protocol"`
	ProxyProtocolVersion types.Int64 `tfsdk:"proxy_protocol_version"`

	// 1.19+ mutable maintenance mode.
	MaintenanceModeEnabled   types.Bool   `tfsdk:"maintenance_mode_enabled"`
	MaintenanceModeType      types.String `tfsdk:"maintenance_mode_type"`
	MaintenanceTitle         types.String `tfsdk:"maintenance_title"`
	MaintenanceMessage       types.String `tfsdk:"maintenance_message"`
	MaintenanceEstimatedTime types.String `tfsdk:"maintenance_estimated_time"`

	// 1.19+ read-only identity / policy backing.
	ResourceGuid            types.String `tfsdk:"resource_guid"`
	Wildcard                types.Bool   `tfsdk:"wildcard"`
	MatchBaseDomain         types.Bool   `tfsdk:"match_base_domain"`
	Health                  types.String `tfsdk:"health"`
	ResourcePolicyID        types.Int64  `tfsdk:"resource_policy_id"`
	DefaultResourcePolicyID types.Int64  `tfsdk:"default_resource_policy_id"`
}

var subdomainRegexp = regexp.MustCompile(`^(\*|(\*\.)?[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)*)$`)

// NewHTTPResource returns a new resource factory.
func NewHTTPResource() resource.Resource {
	return &HTTPResource{}
}

func (r *HTTPResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_resource"
}

func (r *HTTPResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a Pangolin resource - the public-facing endpoint that Pangolin exposes. " +
			"The `mode` attribute picks the transport: `http` (reverse-proxy the historical default), " +
			"`ssh` / `rdp` / `vnc` (browser-based access, Pangolin >= 1.20), `inference` (AI gateway " +
			"endpoint, Pangolin >= 1.22) or `tcp` / `udp` (raw L4). All modes except `tcp` / `udp` are " +
			"addressed by `domain_id` + `subdomain`; L4 resources are addressed by `proxy_port` on the " +
			"Pangolin edge.\n\n" +
			"> **Note:** the 1.19+ attributes (`mode`, `pam_mode`, `auth_daemon_*`, `set_host_header`, " +
			"`enable_proxy`, `headers`, `proxy_protocol*`, `maintenance_*`, `post_auth_path`, " +
			"`skip_to_idp_id`, `resource_guid`, `health`, `resource_policy_id`, `default_resource_policy_id`) " +
			"are only meaningful on Pangolin >= 1.19. On older servers they either fall back to the " +
			"server default or stay absent from the wire (Optional+Computed keeps state consistent).",
		Attributes: map[string]schema.Attribute{
			"id": schema.Int64Attribute{
				Description: "The numeric ID of the resource.",
				Computed:    true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"nice_id": schema.StringAttribute{
				Description: "The human-readable ID of the resource.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Description: "The name of the resource.",
				Required:    true,
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
			"subdomain": schema.StringAttribute{
				Description: "The subdomain for the resource. Used by every mode except `tcp` / `udp`; " +
					"ignored for L4 modes. Set to null to use the base domain. One or more dot-separated labels (`app`, `app.team`); " +
					"an HTTP resource can also take a wildcard as its leftmost label (`*`, `*.apps`), an enterprise feature that needs a wildcard certificate on the domain.",
				Optional: true,
				Validators: []validator.String{
					// Dot-separated RFC 1123 labels (lowercase: the server lowercases,
					// so an uppercase value would drift), optionally under a leading
					// "*" wildcard, as Pangolin's subdomain and wildcard schemas allow.
					stringvalidator.RegexMatches(subdomainRegexp,
						"must be dot-separated DNS labels (lowercase letters, digits and hyphens; a label cannot start or end with a hyphen), optionally under a leading `*` wildcard (`*`, `*.apps`)",
					),
				},
			},
			"full_domain": schema.StringAttribute{
				Description: "The full domain of the resource (computed). Empty for L4 modes.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
					followSubdomain{},
				},
			},
			"domain_id": schema.StringAttribute{
				Description: "The domain ID to attach this resource to. Required for `http`, `ssh`, " +
					"`rdp`, `vnc` and `inference`; leave unset for L4 modes.",
				Optional: true,
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"protocol": schema.StringAttribute{
				Description: "Deprecated: use `mode`. The L4 protocol (tcp or udp). Legacy alias: Pangolin " +
					"derives the transport from `mode`, so this follows `mode` for `tcp` / `udp` resources " +
					"(setting another value is an error) and is `tcp` for `http`, where it is ignored. Leave it unset.",
				DeprecationMessage: httpResourceProtocolDeprecation,
				Optional:           true,
				Computed:           true,
				PlanModifiers: []planmodifier.String{
					protocolFromModeModifier{},
				},
				Validators: []validator.String{
					stringvalidator.OneOf("tcp", "udp"),
					protocolMatchesModeValidator{},
				},
			},
			"mode": schema.StringAttribute{
				Description: "The resource mode. One of `http` (reverse proxy, default), `ssh`, `rdp` " +
					"or `vnc` (browser-based access, Pangolin >= 1.20), `inference` (AI gateway endpoint, " +
					"Pangolin >= 1.22; see `ai_provider_ids`), `tcp` or `udp` (raw L4). `ssh` / `rdp` / " +
					"`vnc` / `inference` require `domain_id` and do not take `proxy_port`. Targets created " +
					"for the resource inherit its mode. Changing this attribute forces replacement.",
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString("http"),
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{
					stringvalidator.OneOf(resourceModes...),
				},
			},
			"proxy_port": schema.Int64Attribute{
				Description: "The proxy port that the Pangolin edge listens on for this L4 resource. " +
					"Required for `tcp` / `udp` modes; must be left unset for `ssh` / `rdp` / `vnc` / " +
					"`inference` and ignored for `http`. " +
					"Changing this attribute forces replacement.",
				Optional: true,
				Computed: true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.RequiresReplace(),
					int64planmodifier.UseStateForUnknown(),
				},
				Validators: []validator.Int64{
					int64validator.Between(1, 65535),
				},
			},
			"pam_mode": schema.StringAttribute{
				Description: "PAM (Pluggable Authentication Module) mode for SSH resources. One of " +
					"`passthrough` or `push`. Only meaningful for `mode = \"ssh\"`.",
				Optional: true,
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
				Validators: []validator.String{
					stringvalidator.OneOf("passthrough", "push"),
				},
			},
			"auth_daemon_mode": schema.StringAttribute{
				Description: "Auth-daemon deployment mode for SSH resources. One of `site`, `remote` or `native`.",
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
				Validators: []validator.String{
					stringvalidator.OneOf("site", "remote", "native"),
				},
			},
			"auth_daemon_port": schema.Int64Attribute{
				Description: "TCP port the auth daemon listens on when `auth_daemon_mode` is set.",
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
				Validators: []validator.Int64{
					int64validator.Between(1, 65535),
				},
			},
			"sso": schema.BoolAttribute{
				Description: "Enable Pangolin SSO authentication on this resource. Set to false to make the resource publicly accessible.",
				Optional:    true,
				Computed:    true,
			},
			"ssl": schema.BoolAttribute{
				Description: "Enable SSL towards the backend.",
				Optional:    true,
				Computed:    true,
			},
			"enabled": schema.BoolAttribute{
				Description: "Enable or disable the resource.",
				Optional:    true,
				Computed:    true,
			},
			"block_access": schema.BoolAttribute{
				Description: "Block all access to the resource.",
				Optional:    true,
				Computed:    true,
			},
			"email_whitelist_enabled": schema.BoolAttribute{
				Description: "Enable the email whitelist on this resource.",
				Optional:    true,
				Computed:    true,
			},
			"apply_rules": schema.BoolAttribute{
				Description: "Enable evaluation of access rules on this resource.",
				Optional:    true,
				Computed:    true,
			},
			"sticky_session": schema.BoolAttribute{
				Description: "Enable sticky sessions (persistent sessions) on this resource.",
				Optional:    true,
				Computed:    true,
			},
			"load_balancing_strategy": schema.StringAttribute{
				Description: "How an HTTP resource spreads requests over its targets: `wrr` (weighted round robin, the server default), `p2c` (power of two choices), `hrw` (highest random weight, hashes the client IP) or `leasttime` (lowest response time).",
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
				Validators: []validator.String{
					stringvalidator.OneOf("wrr", "p2c", "hrw", "leasttime"),
				},
			},
			"forward_identity_headers": schema.BoolAttribute{
				Description: "Send the authenticated user's identity to the backend in the `Remote-User`, `Remote-Email`, `Remote-Name`, `Remote-Role` and `Remote-User-Id` headers. The server default is `true`.",
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.UseStateForUnknown(),
				},
			},
			"tls_server_name": schema.StringAttribute{
				Description: "TLS server name for the backend. Set to null to clear.",
				Optional:    true,
				Computed:    true,
			},
			"set_host_header": schema.StringAttribute{
				Description: "Override the upstream `Host` header. Only meaningful for `mode = \"http\"`. " +
					"Set to null to keep the incoming host.",
				Optional: true,
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"enable_proxy": schema.BoolAttribute{
				Description: "Whether the upstream HTTP proxy layer (headers, path rewriting, buffering) " +
					"is on. Read-only in the current provider: the server-side POST endpoint refuses this " +
					"key with `Unrecognized key: \"enableProxy\"`. Configure it out-of-band and Terraform " +
					"will surface the effective value.",
				Computed: true,
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.UseStateForUnknown(),
				},
			},
			"headers": schema.ListNestedAttribute{
				Description: "Static request headers injected on the upstream request - list of `{name, value}` objects. " +
					"Only for `mode = \"http\"`. This attribute is authoritative: headers set outside Terraform (e.g. in " +
					"the Pangolin UI) are removed on the next apply when the attribute is absent from the config, as are the " +
					"ones it no longer lists. Removing the attribute, or setting it to `[]`, clears the headers on the server. " +
					"Header values are sensitive. Cannot be marked Computed because the Go slice model type in the plugin " +
					"framework does not carry an \"unknown\" sentinel.",
				Optional: true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"name":  schema.StringAttribute{Description: "Header name.", Required: true},
						"value": schema.StringAttribute{Description: "Header value.", Required: true, Sensitive: true},
					},
				},
			},
			"response_headers": schema.ListNestedAttribute{
				Description: "Static headers added to the response sent back to the client - list of `{name, value}` " +
					"objects. Requires Pangolin >= 1.24: older servers reject the key, so leave it unset there. Only for `mode = \"http\"`. " +
					"This attribute is authoritative: headers set outside Terraform (e.g. in the Pangolin UI) are removed " +
					"on the next apply when the attribute is absent from the config. " +
					"Removing the attribute, or setting it to `[]`, clears the headers on the server. Limitation: the " +
					"clear is only sent when the prior state holds response headers (older servers refuse the key), so " +
					"headers added outside Terraform are only removed by an apply that refreshed first (not with " +
					"`-refresh=false`). Header values are sensitive.",
				Optional: true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"name":  schema.StringAttribute{Description: "Header name.", Required: true},
						"value": schema.StringAttribute{Description: "Header value.", Required: true, Sensitive: true},
					},
				},
			},
			"skip_to_idp_id": schema.Int64Attribute{
				Description: "Bypass the Pangolin login page and jump straight to the given IdP.",
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"post_auth_path": schema.StringAttribute{
				Description: "Landing path after a successful login (e.g. `/dashboard`).",
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"proxy_protocol": schema.BoolAttribute{
				Description: "Whether PROXY-protocol encapsulation is on for the upstream L4 socket. " +
					"Read-only in the current provider - the server-side POST endpoint refuses this key.",
				Computed: true,
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.UseStateForUnknown(),
				},
			},
			"proxy_protocol_version": schema.Int64Attribute{
				Description: "PROXY-protocol version (1 or 2). Read-only in the current provider - " +
					"the server-side POST endpoint refuses this key.",
				Computed: true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"maintenance_mode_enabled": schema.BoolAttribute{
				Description: "Serve a maintenance page (or reject the L4 connection) instead of proxying.",
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.UseStateForUnknown(),
				},
			},
			"maintenance_mode_type": schema.StringAttribute{
				Description: "Maintenance type - free-form label surfaced on the maintenance page.",
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"maintenance_title": schema.StringAttribute{
				Description: "Maintenance page title.",
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"maintenance_message": schema.StringAttribute{
				Description: "Maintenance page body message.",
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"maintenance_estimated_time": schema.StringAttribute{
				Description: "Free-form estimated end-of-maintenance timestamp / duration (surfaced on the maintenance page).",
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"resource_guid": schema.StringAttribute{
				Description: "Server-assigned globally unique identifier (1.19+).",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"wildcard": schema.BoolAttribute{
				Description: "Whether the resource matches a wildcard subdomain (1.19+).",
				Computed:    true,
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.UseStateForUnknown(),
					followSubdomain{},
				},
			},
			"match_base_domain": schema.BoolAttribute{
				Description: "For an HTTP resource on a wildcard subdomain (`subdomain = \"*.apps\"`): also serve the host the wildcard sits under, so `*.apps.example.com` also answers on `apps.example.com`. " +
					"Requires Pangolin 1.26.0 or later (an older server refuses the field); the server refuses it on a non-wildcard subdomain and turns it off when the subdomain stops being a wildcard. Null when the server does not report it.",
				Optional: true,
				Computed: true,
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.UseStateForUnknown(),
					followSubdomain{},
				},
			},
			"health": schema.StringAttribute{
				Description: "Server-reported aggregate health status (1.19+).",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"resource_policy_id": schema.Int64Attribute{
				Description: "ID of the shared resource policy (`pangolin_resource_policy`, Enterprise) this resource is attached to. " +
					"Set a policy ID to attach the resource, or `0` to detach it and fall back to its own inline policy. " +
					"Leaving it unset keeps the current attachment, including one made outside Terraform, so upgrading " +
					"the provider or omitting the attribute never detaches a resource. Only shared policies are accepted: " +
					"the inline default policy of another resource is refused before any change.\n\n" +
					"> **Note:** Pangolin wipes the resource-level password, PIN code, header auth, email whitelist, rules and " +
					"user / role grants (except the admin role) whenever the attachment changes, so the policy fully governs " +
					"access. Resource-level `pangolin_resource_password`, `pangolin_resource_rule`, `pangolin_resource_role` " +
					"and similar resources then drift and are recreated on the next apply. While attached, `sso`, " +
					"`email_whitelist_enabled`, `apply_rules` and `skip_to_idp_id` set on this resource override the shared " +
					"policy for this resource only.",
				Optional: true,
				Computed: true,
				Validators: []validator.Int64{
					int64validator.AtLeast(0),
				},
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"default_resource_policy_id": schema.Int64Attribute{
				Description: "ID of the auto-created default resource-policy backing this resource (1.19+). " +
					"Read-only.",
				Computed: true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
		},
	}
	addResourceModeAttributes(resp.Schema.Attributes)
}

func (r *HTTPResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *HTTPResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan HTTPResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// buildHTTPResourceCreateRequest picks the payload per mode: domain
	// modes (http, ssh, rdp, vnc, inference) go with `http: true`, the
	// tcp / udp L4 modes with `http: false`.
	createReq, diags := buildHTTPResourceCreateRequest(ctx, plan)
	resp.Diagnostics.Append(diags...)
	aiConfigured, d := aiProviderIDsConfigured(ctx, req.Config)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}
	// pam_mode / auth_daemon_* are refused by the server at Create
	// (400 "Unrecognized keys"). They are applied on the follow-up
	// Update call - buildHTTPResourceUpdateRequest picks them up
	// from the plan.

	if r.checkSharedPolicy(ctx, plan.ResourcePolicyID, &resp.Diagnostics); resp.Diagnostics.HasError() {
		return
	}

	resource, err := r.client.CreateResource(ctx, createReq)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create resource", err.Error())
		return
	}

	plan.ID = types.Int64Value(int64(resource.ResourceID))
	plan.NiceID = types.StringValue(resource.NiceID)
	// Save the ID before any follow-up call: if one fails, Terraform
	// taints the resource instead of losing track of it.
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), plan.ID)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// The providers sent in the create body are what the server holds.
	createdProviders := plan.AIProviderIDs

	// L4 resources (mode = tcp/udp) reject almost every scalar on
	// the follow-up POST /resource/{id} - only `name`, `enabled`,
	// and `proxyPort` are accepted, and proxyPort is create-only.
	// So we hydrate from the Create response directly and skip the
	// Update round-trip. Domain modes (http, ssh, rdp, vnc,
	// inference) share the server's HTTP update handler and keep the
	// historical Create + Update pattern so user-set sso/ssl/etc. are
	// pushed. A shared policy is attached through POST /resource/{id},
	// which accepts `resourcePolicyId` for every mode. For domain modes
	// it goes in its own call first: see attachResourcePolicy.
	policyID := resourcePolicyIDUpdate(plan.ResourcePolicyID, types.Int64Null())
	if resourceModeUsesDomain(plan.Mode.ValueString()) {
		if policyID != nil {
			if _, err := r.attachResourcePolicy(ctx, plan, policyID); err != nil {
				resp.Diagnostics.AddError("Failed to attach resource policy after creation", err.Error())
				return
			}
		}
		updated, err := r.client.UpdateResource(ctx, int(plan.ID.ValueInt64()), buildHTTPResourceUpdateRequest(plan))
		if err != nil {
			resp.Diagnostics.AddError("Failed to apply resource settings after creation", err.Error())
			r.rollBackCreate(ctx, int(plan.ID.ValueInt64()), resp)
			return
		}
		resource = updated
	} else if policyID != nil {
		updated, err := r.client.UpdateResource(ctx, int(plan.ID.ValueInt64()), &client.UpdateResourceRequest{
			Name:             plan.Name.ValueString(),
			ResourcePolicyID: policyID,
		})
		if err != nil {
			resp.Diagnostics.AddError("Failed to attach resource policy after creation", err.Error())
			return
		}
		resource = updated
	}
	plan = applyHTTPResourceResponse(plan, resource)
	resp.Diagnostics.Append(r.applyAIProviders(ctx, &plan, createdProviders, aiConfigured)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// rollBackCreate deletes a resource whose follow-up settings update
// failed in Create, so it is not left on the server outside the
// state (the next apply would create a duplicate, or fail on the
// subdomain). If the delete fails too, only the ID is saved: Terraform
// marks the resource tainted and replaces it on the next apply.
// Either way the next apply recreates the resource, so a rollback is
// also right when the update did reach the server (e.g. a response
// that failed to decode).
func (r *HTTPResource) rollBackCreate(ctx context.Context, id int, resp *resource.CreateResponse) {
	err := r.client.DeleteResource(ctx, id)
	if err == nil {
		resp.Diagnostics.AddWarning(
			"Partially created resource rolled back",
			fmt.Sprintf("Resource %d was created but its settings could not be applied, so it was deleted again. "+
				"The next apply creates it from scratch.", id),
		)
		// Create saved the ID before the follow-up: drop it, the object is gone.
		resp.State.RemoveResource(ctx)
		return
	}
	resp.Diagnostics.AddError(
		"Failed to delete the partially created resource",
		fmt.Sprintf("Resource %d was created but its settings could not be applied, and deleting it failed: %s. "+
			"It is kept in the state as tainted so the next apply replaces it.", id, err.Error()),
	)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), types.Int64Value(int64(id)))...)
}

// ValidateConfig rejects non-empty header lists on L4 modes: the tcp /
// udp update body has no header keys and the provider never sends them
// there, so the configured value would never reach the server and the
// apply would end with an inconsistent result. An empty list, or a
// value still unknown at validation, is left alone: it round-trips as
// is (and Terraform validates the config again once it is known).
func (r *HTTPResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var mode, subdomain types.String
	var matchBase types.Bool
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("mode"), &mode)...)
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("subdomain"), &subdomain)...)
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("match_base_domain"), &matchBase)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// The server refuses true off an HTTP wildcard, or turns it off when
	// the resource leaves one: either way the apply would fail.
	if matchBase.ValueBool() {
		if !mode.IsNull() && !mode.IsUnknown() && mode.ValueString() != "http" {
			resp.Diagnostics.AddAttributeError(path.Root("match_base_domain"), "match_base_domain needs an HTTP resource",
				fmt.Sprintf("`match_base_domain = true` only applies to `mode = \"http\"`, not %q.", mode.ValueString()))
		}
		if !subdomain.IsUnknown() && !strings.HasPrefix(subdomain.ValueString(), "*") {
			resp.Diagnostics.AddAttributeError(path.Root("match_base_domain"), "match_base_domain needs a wildcard subdomain",
				"`match_base_domain = true` requires a wildcard `subdomain` (`*` or `*.apps`).")
		}
	}
	if mode.IsNull() || mode.IsUnknown() || mode.ValueString() == "http" {
		return
	}
	for _, attr := range []string{"headers", "response_headers"} {
		var hs types.List
		resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root(attr), &hs)...)
		if !hs.IsNull() && !hs.IsUnknown() && len(hs.Elements()) > 0 {
			resp.Diagnostics.AddAttributeError(
				path.Root(attr),
				"Headers not supported in L4 mode",
				fmt.Sprintf("`%s` only applies to `mode = \"http\"`; Pangolin does not inject headers on %s resources.", attr, mode.ValueString()),
			)
		}
	}
}

func (r *HTTPResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state HTTPResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resource, err := r.client.GetResource(ctx, int(state.ID.ValueInt64()))
	if err != nil {
		if errors.Is(err, client.ErrNotFound) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read resource", err.Error())
		return
	}

	state = applyHTTPResourceResponse(state, resource)
	resp.Diagnostics.Append(r.refreshAIProviders(ctx, &state)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *HTTPResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state HTTPResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	aiConfigured, d := aiProviderIDsConfigured(ctx, req.Config)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	// See the note in Create: L4 resources accept only `name` and
	// `enabled` on POST /resource/{id}. Build a narrow payload for
	// them so we don't 400 on an unrelated field the user could not
	// have written anyway.
	updateReq := buildHTTPResourceUpdateRequest(plan)
	clearRemovedHeaders(updateReq, plan, state)
	moveToNewSubdomain(updateReq, plan, state)
	policyID := resourcePolicyIDUpdate(plan.ResourcePolicyID, state.ResourcePolicyID)
	if policyID != nil {
		if r.checkSharedPolicy(ctx, plan.ResourcePolicyID, &resp.Diagnostics); resp.Diagnostics.HasError() {
			return
		}
	}
	if !resourceModeUsesDomain(plan.Mode.ValueString()) {
		updateReq = &client.UpdateResourceRequest{
			Name:             plan.Name.ValueString(),
			Enabled:          updateReq.Enabled,
			ResourcePolicyID: policyID,
		}
	} else if policyID != nil {
		if _, err := r.attachResourcePolicy(ctx, plan, policyID); err != nil {
			resp.Diagnostics.AddError("Failed to update resource policy attachment", err.Error())
			return
		}
	}
	res, err := r.client.UpdateResource(ctx, int(plan.ID.ValueInt64()), updateReq)
	if err != nil {
		resp.Diagnostics.AddError("Failed to update resource", err.Error())
		return
	}

	plan = applyHTTPResourceResponse(plan, res)
	resp.Diagnostics.Append(r.applyAIProviders(ctx, &plan, state.AIProviderIDs, aiConfigured)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *HTTPResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state HTTPResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteResource(ctx, int(state.ID.ValueInt64()))
	if err != nil {
		resp.Diagnostics.AddError("Failed to delete resource", err.Error())
		return
	}
}

func (r *HTTPResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	id, err := strconv.ParseInt(req.ID, 10, 64)
	if err != nil {
		resp.Diagnostics.AddError("Invalid ID", fmt.Sprintf("Cannot parse resource ID %q as integer", req.ID))
		return
	}

	res, err := r.client.GetResource(ctx, int(id))
	if err != nil {
		resp.Diagnostics.AddError("Failed to import resource", err.Error())
		return
	}

	state := HTTPResourceModel{ID: types.Int64Value(int64(res.ResourceID))}
	state = applyHTTPResourceResponse(state, res)
	resp.Diagnostics.Append(r.refreshAIProviders(ctx, &state)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// moveToNewSubdomain sends domain_id when the subdomain changes: only then
// does the server rebuild the full domain (and its certificate). Sent on
// that change only, so other updates keep their payload.
func moveToNewSubdomain(req *client.UpdateResourceRequest, plan, state HTTPResourceModel) {
	if !plan.Subdomain.Equal(state.Subdomain) && !plan.DomainID.IsNull() && !plan.DomainID.IsUnknown() {
		req.DomainID = plan.DomainID.ValueString()
	}
	// The value carried over from the state (false on a non-wildcard
	// subdomain, which the server refuses) is not a change: leave it out.
	if plan.Subdomain.Equal(state.Subdomain) && plan.MatchBaseDomain.Equal(state.MatchBaseDomain) {
		req.MatchBaseDomain = nil
	}
}

// buildHTTPResourceUpdateRequest builds an UpdateResourceRequest from the model,
// sending only fields that the user explicitly set (non-unknown).
func buildHTTPResourceUpdateRequest(plan HTTPResourceModel) *client.UpdateResourceRequest {
	req := &client.UpdateResourceRequest{
		Name: plan.Name.ValueString(),
	}
	if !plan.Subdomain.IsNull() && !plan.Subdomain.IsUnknown() {
		s := plan.Subdomain.ValueString()
		req.Subdomain = &s
	}
	if !plan.SSO.IsUnknown() {
		v := plan.SSO.ValueBool()
		req.SSO = &v
	}
	if !plan.SSL.IsUnknown() {
		v := plan.SSL.ValueBool()
		req.SSL = &v
	}
	if !plan.Enabled.IsUnknown() {
		v := plan.Enabled.ValueBool()
		req.Enabled = &v
	}
	if !plan.BlockAccess.IsUnknown() {
		v := plan.BlockAccess.ValueBool()
		req.BlockAccess = &v
	}
	if !plan.EmailWhitelistEnabled.IsUnknown() {
		v := plan.EmailWhitelistEnabled.ValueBool()
		req.EmailWhitelistEnabled = &v
	}
	if !plan.ApplyRules.IsUnknown() {
		v := plan.ApplyRules.ValueBool()
		req.ApplyRules = &v
	}
	if !plan.StickySession.IsUnknown() {
		v := plan.StickySession.ValueBool()
		req.StickySession = &v
	}
	req.LoadBalancingStrategy = tfconv.StringToPtr(plan.LoadBalancingStrategy)
	req.ForwardIdentityHeaders = tfconv.BoolToPtr(plan.ForwardIdentityHeaders)
	req.MatchBaseDomain = tfconv.BoolToPtr(plan.MatchBaseDomain)
	if !plan.TLSServerName.IsUnknown() {
		if plan.TLSServerName.IsNull() {
			req.TLSServerName = nil
		} else {
			s := plan.TLSServerName.ValueString()
			req.TLSServerName = &s
		}
	}

	// 1.19+ mutable fields - only send when the user set the value
	// (i.e. not Unknown coming from a Computed default). Empty strings
	// mean "empty string on wire", nulls mean "clear the field".
	if !plan.SetHostHeader.IsUnknown() {
		if plan.SetHostHeader.IsNull() {
			req.SetHostHeader = nil
		} else {
			s := plan.SetHostHeader.ValueString()
			req.SetHostHeader = &s
		}
	}
	if !isNullList(plan.Headers) {
		hs := headersToWire(plan.Headers)
		req.Headers = &hs
	}
	if !isNullList(plan.ResponseHeaders) {
		hs := headersToWire(plan.ResponseHeaders)
		req.ResponseHeaders = &hs
	}
	if !plan.SkipToIdpID.IsUnknown() {
		if plan.SkipToIdpID.IsNull() {
			req.SkipToIdpID = nil
		} else {
			v := int(plan.SkipToIdpID.ValueInt64())
			req.SkipToIdpID = &v
		}
	}
	if !plan.PostAuthPath.IsUnknown() {
		if plan.PostAuthPath.IsNull() {
			req.PostAuthPath = nil
		} else {
			s := plan.PostAuthPath.ValueString()
			req.PostAuthPath = &s
		}
	}
	if !plan.MaintenanceModeEnabled.IsUnknown() {
		v := plan.MaintenanceModeEnabled.ValueBool()
		req.MaintenanceModeEnabled = &v
	}
	if !plan.MaintenanceModeType.IsUnknown() && !plan.MaintenanceModeType.IsNull() {
		req.MaintenanceModeType = plan.MaintenanceModeType.ValueString()
	}
	if !plan.MaintenanceTitle.IsUnknown() {
		if plan.MaintenanceTitle.IsNull() {
			req.MaintenanceTitle = nil
		} else {
			s := plan.MaintenanceTitle.ValueString()
			req.MaintenanceTitle = &s
		}
	}
	if !plan.MaintenanceMessage.IsUnknown() {
		if plan.MaintenanceMessage.IsNull() {
			req.MaintenanceMessage = nil
		} else {
			s := plan.MaintenanceMessage.ValueString()
			req.MaintenanceMessage = &s
		}
	}
	if !plan.MaintenanceEstimatedTime.IsUnknown() {
		if plan.MaintenanceEstimatedTime.IsNull() {
			req.MaintenanceEstimatedTime = nil
		} else {
			s := plan.MaintenanceEstimatedTime.ValueString()
			req.MaintenanceEstimatedTime = &s
		}
	}
	if !plan.PamMode.IsUnknown() && !plan.PamMode.IsNull() {
		req.PamMode = plan.PamMode.ValueString()
	}
	if !plan.AuthDaemonMode.IsUnknown() && !plan.AuthDaemonMode.IsNull() {
		req.AuthDaemonMode = plan.AuthDaemonMode.ValueString()
	}
	if !plan.AuthDaemonPort.IsUnknown() && !plan.AuthDaemonPort.IsNull() {
		v := int(plan.AuthDaemonPort.ValueInt64())
		req.AuthDaemonPort = &v
	}
	return req
}

// checkSharedPolicy refuses, before any write, an attachment to a
// policy that is not a shared one (another resource's inline default
// policy, which the server would accept). 0, null and unknown are no-ops.
func (r *HTTPResource) checkSharedPolicy(ctx context.Context, id types.Int64, diags *diag.Diagnostics) {
	if policyIDOrZero(id) == 0 {
		return
	}
	shared, err := r.client.IsSharedResourcePolicy(ctx, int(id.ValueInt64()))
	if err != nil {
		diags.AddError("Failed to check resource policy", err.Error())
		return
	}
	if !shared {
		diags.AddAttributeError(path.Root("resource_policy_id"), "Not a shared resource policy",
			fmt.Sprintf("Resource policy %d is not a shared policy of this organization (it may be the inline "+
				"default policy of another resource). Use the ID of a `pangolin_resource_policy`.", id.ValueInt64()))
	}
}

// attachResourcePolicy sends a policy (de)attachment on its own. The
// http update handler decides whether sso, email_whitelist_enabled,
// apply_rules and skip_to_idp_id go to the inline policy or become
// overrides from the attachment as it was before the request, so
// changing both in one POST would put those settings in the wrong place.
func (r *HTTPResource) attachResourcePolicy(ctx context.Context, plan HTTPResourceModel, policyID *json.RawMessage) (*client.Resource, error) {
	return r.client.UpdateResource(ctx, int(plan.ID.ValueInt64()), &client.UpdateResourceRequest{
		Name:             plan.Name.ValueString(),
		ResourcePolicyID: policyID,
	})
}

// resourcePolicyIDUpdate returns the resourcePolicyId payload value
// for a planned attachment, or nil when it does not change. Null or
// unknown (attribute unset) keeps the current attachment; 0 detaches
// the resource and is sent as an explicit null.
func resourcePolicyIDUpdate(plan, prior types.Int64) *json.RawMessage {
	if plan.IsUnknown() || plan.IsNull() {
		return nil
	}
	if plan.ValueInt64() == policyIDOrZero(prior) {
		return nil
	}
	if plan.ValueInt64() == 0 {
		return client.ResourcePolicyIDValue(nil)
	}
	id := int(plan.ValueInt64())
	return client.ResourcePolicyIDValue(&id)
}

// policyIDOrZero maps "no shared policy" (null, unknown or 0) to 0.
func policyIDOrZero(v types.Int64) int64 {
	if v.IsNull() || v.IsUnknown() {
		return 0
	}
	return v.ValueInt64()
}

// resourcePolicyIDFromServer maps the server's resourcePolicyId into
// the model. A detached resource reads back null; when the model holds
// the `0` the user configured to detach it, the 0 is kept so the plan
// stays clean.
func resourcePolicyIDFromServer(current types.Int64, server *int) types.Int64 {
	if server == nil && !current.IsUnknown() && !current.IsNull() && current.ValueInt64() == 0 {
		return current
	}
	return tfconv.Int64FromIntPtr(server)
}

// isNullList reports whether a nested-list model slice came in from
// Terraform as a null value (attribute absent from the config): the
// framework decodes a null list into a nil slice, and an explicit
// empty list (`headers = []`) into a non-nil empty one.
func isNullList(hs []ResourceHeaderModel) bool {
	return hs == nil
}

// clearRemovedHeaders turns a header list absent from the config into
// an explicit clear. buildHTTPResourceUpdateRequest leaves a null
// `headers` off the payload, which the server reads as "unchanged", so
// without this the injected headers survived their removal and the
// plan showed the same diff forever. An empty list clears them on
// every 1.19+ server (up to 1.23 it is stored as "[]", 1.24 stores
// null). It is sent on every update, whatever the prior state holds,
// so headers added outside Terraform are removed even by an apply
// that skipped the refresh.
//
// Response headers (1.24+ only) are cleared with `null`, which the
// 1.24 schema accepts (`.nullable().optional()`). That key is only
// sent when the prior state had response headers: pre-1.24 servers
// refuse it, and they never report any.
func clearRemovedHeaders(req *client.UpdateResourceRequest, plan, state HTTPResourceModel) {
	if isNullList(plan.Headers) {
		empty := []client.ResourceHeader{}
		req.Headers = &empty
	}
	if isNullList(plan.ResponseHeaders) && len(state.ResponseHeaders) > 0 {
		var none []client.ResourceHeader
		req.ResponseHeaders = &none
	}
}

// headersToWire maps configured headers to the wire shape.
func headersToWire(hs []ResourceHeaderModel) []client.ResourceHeader {
	out := make([]client.ResourceHeader, len(hs))
	for i, h := range hs {
		out[i] = client.ResourceHeader{
			Name:  h.Name.ValueString(),
			Value: h.Value.ValueString(),
		}
	}
	return out
}

// headerModelsFromWire maps a wire header list to the model. An empty
// wire list maps to null, except when the prior model holds an explicit
// empty list (`headers = []` in the config): keeping it empty there
// avoids an "inconsistent result after apply" on the null/[] mismatch.
func headerModelsFromWire(prior []ResourceHeaderModel, wire []client.ResourceHeader) []ResourceHeaderModel {
	if len(wire) == 0 {
		if prior != nil && len(prior) == 0 {
			return []ResourceHeaderModel{}
		}
		return nil
	}
	hs := make([]ResourceHeaderModel, len(wire))
	for i, h := range wire {
		hs[i] = ResourceHeaderModel{
			Name:  types.StringValue(h.Name),
			Value: types.StringValue(h.Value),
		}
	}
	return hs
}

// applyHTTPResourceResponse copies API response fields into the model.
func applyHTTPResourceResponse(m HTTPResourceModel, res *client.Resource) HTTPResourceModel {
	m.NiceID = types.StringValue(res.NiceID)
	m.Name = types.StringValue(res.Name)
	m.FullDomain = types.StringValue(res.FullDomain)
	m.DomainID = types.StringValue(res.DomainID)
	m.Protocol = resolveHTTPResourceProtocol(m.Protocol, res)
	if res.Subdomain != "" {
		m.Subdomain = types.StringValue(res.Subdomain)
	} else {
		m.Subdomain = types.StringNull()
	}
	m.SSO = types.BoolValue(res.SSO)
	m.SSL = types.BoolValue(res.SSL)
	m.Enabled = types.BoolValue(res.Enabled)
	m.BlockAccess = types.BoolValue(res.BlockAccess)
	m.EmailWhitelistEnabled = types.BoolValue(res.EmailWhitelistEnabled)
	m.ApplyRules = types.BoolValue(res.ApplyRules)
	m.StickySession = types.BoolValue(res.StickySession)
	m.LoadBalancingStrategy = tfconv.StringFromPtr(res.LoadBalancingStrategy)
	m.ForwardIdentityHeaders = tfconv.BoolFromPtr(res.ForwardIdentityHeaders)
	if res.TLSServerName != nil {
		m.TLSServerName = types.StringValue(*res.TLSServerName)
	} else {
		m.TLSServerName = types.StringNull()
	}

	// 1.19+ scalars. Empty strings from the server are surfaced as
	// null in state so the plan diff stays clean on pre-1.19 servers.
	if res.Mode != "" {
		m.Mode = types.StringValue(res.Mode)
	} else {
		m.Mode = types.StringValue("http")
	}
	if res.PamMode != "" {
		m.PamMode = types.StringValue(res.PamMode)
	} else {
		m.PamMode = types.StringNull()
	}
	if res.AuthDaemonMode != "" {
		m.AuthDaemonMode = types.StringValue(res.AuthDaemonMode)
	} else {
		m.AuthDaemonMode = types.StringNull()
	}
	m.ProxyPort = tfconv.Int64FromIntPtr(res.ProxyPort)
	m.AuthDaemonPort = tfconv.Int64FromIntPtr(res.AuthDaemonPort)

	m.SetHostHeader = tfconv.StringFromPtr(res.SetHostHeader)
	m.EnableProxy = tfconv.BoolFromPtr(res.EnableProxy)
	m.SkipToIdpID = tfconv.Int64FromIntPtr(res.SkipToIdpID)
	m.PostAuthPath = tfconv.StringFromPtr(res.PostAuthPath)
	m.ProxyProtocol = tfconv.BoolFromPtr(res.ProxyProtocol)
	m.ProxyProtocolVersion = tfconv.Int64FromIntPtr(res.ProxyProtocolVersion)

	m.MaintenanceModeEnabled = tfconv.BoolFromPtr(res.MaintenanceModeEnabled)
	if res.MaintenanceModeType != "" {
		m.MaintenanceModeType = types.StringValue(res.MaintenanceModeType)
	} else {
		m.MaintenanceModeType = types.StringNull()
	}
	m.MaintenanceTitle = tfconv.StringFromPtr(res.MaintenanceTitle)
	m.MaintenanceMessage = tfconv.StringFromPtr(res.MaintenanceMessage)
	m.MaintenanceEstimatedTime = tfconv.StringFromPtr(res.MaintenanceEstimatedTime)

	// Read-only identity / policy backing.
	m.ResourceGuid = types.StringValue(res.ResourceGuid)
	m.Wildcard = types.BoolValue(res.Wildcard)
	m.MatchBaseDomain = tfconv.BoolFromPtr(res.MatchBaseDomain)
	if res.Health != "" {
		m.Health = types.StringValue(res.Health)
	} else {
		m.Health = types.StringNull()
	}
	m.ResourcePolicyID = resourcePolicyIDFromServer(m.ResourcePolicyID, res.ResourcePolicyID)
	m.DefaultResourcePolicyID = tfconv.Int64FromIntPtr(res.DefaultResourcePolicyID)

	// Headers - round-trip the list, read from `requestHeaders` on
	// 1.24+ and from `headers` before. An empty wire list maps to
	// null, see headerModelsFromWire. A response carrying neither key
	// says nothing about them, so the known value is kept.
	if res.ReportsRequestHeaders() {
		m.Headers = headerModelsFromWire(m.Headers, res.EffectiveRequestHeaders())
	}
	m.ResponseHeaders = headerModelsFromWire(m.ResponseHeaders, res.ResponseHeaders)
	return m
}
