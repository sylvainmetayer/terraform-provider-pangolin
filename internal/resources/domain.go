package resources

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stackopshq/terraform-provider-pangolin/internal/client"
	"github.com/stackopshq/terraform-provider-pangolin/internal/tfconv"
)

var (
	_ resource.Resource                   = &DomainResource{}
	_ resource.ResourceWithImportState    = &DomainResource{}
	_ resource.ResourceWithValidateConfig = &DomainResource{}
)

// domainTypeWildcard is the only domain type whose cert-resolver
// settings the server lets you change, and the only type Community
// Edition can create.
const domainTypeWildcard = "wildcard"

// baseDomainRegexp mirrors the server's subdomainSchema, restricted
// to lowercase: the server lowercases the value, so an uppercase
// config would never match what is read back.
var baseDomainRegexp = regexp.MustCompile(`^([a-z0-9](?:[a-z0-9-]*[a-z0-9])?\.)*[a-z0-9](?:[a-z0-9-]*[a-z0-9])?$`)

// domainDNSRecordAttrTypes is the object type of one dns_records entry.
var domainDNSRecordAttrTypes = map[string]attr.Type{
	"record_type": types.StringType,
	"base_domain": types.StringType,
	"value":       types.StringType,
}

// DomainResource manages a domain of the provider organization.
type DomainResource struct {
	client *client.Client
}

// DomainResourceModel describes the resource data model.
type DomainResourceModel struct {
	ID                 types.String `tfsdk:"id"`
	BaseDomain         types.String `tfsdk:"base_domain"`
	Type               types.String `tfsdk:"type"`
	CertResolver       types.String `tfsdk:"cert_resolver"`
	PreferWildcardCert types.Bool   `tfsdk:"prefer_wildcard_cert"`
	Verified           types.Bool   `tfsdk:"verified"`
	Failed             types.Bool   `tfsdk:"failed"`
	ErrorMessage       types.String `tfsdk:"error_message"`
	ConfigManaged      types.Bool   `tfsdk:"config_managed"`
	DNSRecords         types.List   `tfsdk:"dns_records"`
}

// NewDomainResource returns a new resource factory.
func NewDomainResource() resource.Resource {
	return &DomainResource{}
}

func (r *DomainResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_domain"
}

func (r *DomainResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a domain of the provider organization. `wildcard` domains point a wildcard A record at the Pangolin server and are usable at once. " +
			"`ns` and `cname` domains are delegated to Pangolin and stay unverified until the DNS records listed in `dns_records` exist at your DNS provider; " +
			"Pangolin then checks them periodically and sets `verified`. `ns` and `cname` require Pangolin Enterprise Edition or Pangolin Cloud: " +
			"Community Edition only accepts `wildcard`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "The domain ID.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"base_domain": schema.StringAttribute{
				Description: "The domain name, lowercase (e.g. `example.com` or `apps.example.com`). A `cname` domain cannot be a root domain. Changing it forces a new domain.",
				Required:    true,
				Validators: []validator.String{
					stringvalidator.RegexMatches(baseDomainRegexp, "must be a lowercase domain name such as apps.example.com"),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"type": schema.StringAttribute{
				Description: "The domain type: `wildcard`, `ns` or `cname`. Changing it forces a new domain.",
				Required:    true,
				Validators: []validator.String{
					stringvalidator.OneOf("ns", "cname", domainTypeWildcard),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"cert_resolver": schema.StringAttribute{
				Description: "Traefik certificate resolver used for this domain. Only settable for `type = \"wildcard\"`, where removing it clears the resolver. " +
					"On `ns` / `cname` domains the server cannot change it after creation, so the provider only reports the server value.",
				Optional: true,
				Computed: true,
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
				PlanModifiers: []planmodifier.String{
					domainCertSettingModifier{},
				},
			},
			"prefer_wildcard_cert": schema.BoolAttribute{
				Description: "Request a wildcard certificate instead of one certificate per subdomain. Only settable for `type = \"wildcard\"`, where it defaults to `false`. " +
					"On `ns` / `cname` domains the server cannot change it after creation, so the provider only reports the server value.",
				Optional: true,
				Computed: true,
				PlanModifiers: []planmodifier.Bool{
					domainCertSettingModifier{},
				},
			},
			"verified": schema.BoolAttribute{
				Description: "Whether Pangolin has verified the domain's DNS records. Always `true` for `wildcard` domains. Updated on refresh.",
				Computed:    true,
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.UseStateForUnknown(),
				},
			},
			"failed": schema.BoolAttribute{
				Description: "Whether Pangolin stopped retrying the DNS verification. Retry it from the Pangolin UI once the records are fixed. Updated on refresh.",
				Computed:    true,
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.UseStateForUnknown(),
				},
			},
			"error_message": schema.StringAttribute{
				Description: "Last DNS verification error, if any. Updated on refresh.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"config_managed": schema.BoolAttribute{
				Description: "Whether the domain is declared in the Pangolin config file. Such domains cannot be deleted through the API.",
				Computed:    true,
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.UseStateForUnknown(),
				},
			},
			"dns_records": schema.ListNestedAttribute{
				Description: "DNS records to create at your DNS provider, sorted by record ID. For `wildcard` domains the A records carry the server IP when Pangolin knows it, otherwise a placeholder. " +
					"Updated on refresh; an in-place update keeps the known records, so `for_each` over them stays plannable.",
				Computed: true,
				PlanModifiers: []planmodifier.List{
					listplanmodifier.UseStateForUnknown(),
				},
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"record_type": schema.StringAttribute{
							Description: "Record type: `NS`, `CNAME`, `A` or `TXT`.",
							Computed:    true,
						},
						"base_domain": schema.StringAttribute{
							Description: "Record name.",
							Computed:    true,
						},
						"value": schema.StringAttribute{
							Description: "Record value.",
							Computed:    true,
						},
					},
				},
			},
		},
	}
}

func (r *DomainResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// ValidateConfig rejects cert settings on ns / cname domains (the
// server stores them on create but refuses every later update, so
// they could never be changed or removed) and a cname on a root
// domain (refused by the server). Create checks the cert settings
// again once every value is known.
func (r *DomainResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var cfg DomainResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(validateDomainCertSettings(cfg)...)
	resp.Diagnostics.Append(validateDomainCNAMERoot(cfg)...)
}

// validateDomainCNAMERoot mirrors the server's isSecondLevelDomain
// check: a cname cannot sit on a two-label name such as example.com.
func validateDomainCNAMERoot(cfg DomainResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics
	if cfg.Type.IsUnknown() || cfg.Type.ValueString() != "cname" || cfg.BaseDomain.IsUnknown() || cfg.BaseDomain.IsNull() {
		return diags
	}
	if strings.Count(cfg.BaseDomain.ValueString(), ".") == 1 {
		diags.AddAttributeError(path.Root("base_domain"), "cname on a root domain",
			"A cname domain cannot be a root domain (RFC 1912 section 2.4 prohibits CNAME records at the zone apex). Use a subdomain such as apps.example.com.")
	}
	return diags
}

func validateDomainCertSettings(cfg DomainResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics
	if cfg.Type.IsNull() || cfg.Type.IsUnknown() || cfg.Type.ValueString() == domainTypeWildcard {
		return diags
	}
	if !cfg.CertResolver.IsNull() && !cfg.CertResolver.IsUnknown() {
		diags.AddAttributeError(path.Root("cert_resolver"), "Unsupported cert_resolver",
			fmt.Sprintf("cert_resolver is only supported for type = %q domains.", domainTypeWildcard))
	}
	if !cfg.PreferWildcardCert.IsNull() && !cfg.PreferWildcardCert.IsUnknown() && cfg.PreferWildcardCert.ValueBool() {
		diags.AddAttributeError(path.Root("prefer_wildcard_cert"), "Unsupported prefer_wildcard_cert",
			fmt.Sprintf("prefer_wildcard_cert is only supported for type = %q domains.", domainTypeWildcard))
	}
	return diags
}

func (r *DomainResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan DomainResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Values unknown at validate time are known now: refuse cert
	// settings on ns / cname before anything is written.
	resp.Diagnostics.Append(validateDomainCertSettings(plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	createReq := &client.CreateDomainRequest{
		Type:       plan.Type.ValueString(),
		BaseDomain: plan.BaseDomain.ValueString(),
	}
	if plan.Type.ValueString() == domainTypeWildcard {
		createReq.CertResolver = plan.CertResolver.ValueStringPointer()
		createReq.PreferWildcardCert = plan.PreferWildcardCert.ValueBoolPointer()
	}

	domainID, err := r.client.CreateDomain(ctx, r.client.OrgID, createReq)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create domain", err.Error()+domainCreateErrorHint(plan.Type.ValueString(), err))
		return
	}

	// Save the ID before reading back, so a failed read taints the
	// domain instead of leaving it on the server untracked.
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), domainID)...)
	if resp.Diagnostics.HasError() {
		return
	}

	state, diags := r.readDomain(ctx, plan, domainID)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *DomainResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state DomainResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	domain, err := r.client.GetDomain(ctx, r.client.OrgID, state.ID.ValueString())
	if err != nil {
		if errors.Is(err, client.ErrNotFound) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read domain", err.Error())
		return
	}

	newState, diags := r.mapDomain(ctx, state, domain)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &newState)...)
}

func (r *DomainResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state DomainResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// base_domain and type force replacement, so only the cert
	// settings of a wildcard domain can reach this point.
	if plan.Type.ValueString() == domainTypeWildcard {
		updateReq := &client.UpdateDomainRequest{
			CertResolver:       plan.CertResolver.ValueStringPointer(),
			PreferWildcardCert: plan.PreferWildcardCert.ValueBoolPointer(),
		}
		if err := r.client.UpdateDomain(ctx, r.client.OrgID, state.ID.ValueString(), updateReq); err != nil {
			resp.Diagnostics.AddError("Failed to update domain", err.Error())
			return
		}
	}

	newState, diags := r.readDomain(ctx, plan, state.ID.ValueString())
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	keepPlannedVerification(&newState, plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &newState)...)
}

// keepPlannedVerification keeps the verification attributes the plan
// carried over from state (UseStateForUnknown): an update only changes
// cert settings, and a verification that progressed meanwhile would
// otherwise differ from the plan. The next refresh picks it up.
func keepPlannedVerification(m *DomainResourceModel, plan DomainResourceModel) {
	if !plan.Verified.IsUnknown() {
		m.Verified = plan.Verified
	}
	if !plan.Failed.IsUnknown() {
		m.Failed = plan.Failed
	}
	if !plan.ErrorMessage.IsUnknown() {
		m.ErrorMessage = plan.ErrorMessage
	}
	if !plan.DNSRecords.IsUnknown() {
		m.DNSRecords = plan.DNSRecords
	}
}

func (r *DomainResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state DomainResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.DeleteDomain(ctx, r.client.OrgID, state.ID.ValueString()); err != nil {
		if errors.Is(err, client.ErrNotFound) {
			return
		}
		detail := err.Error()
		if state.ConfigManaged.ValueBool() {
			detail += "\n\nThis domain is declared in the Pangolin config file and cannot be deleted through the API. " +
				"Remove it from the config file, or drop it from the Terraform state with `terraform state rm`."
		}
		resp.Diagnostics.AddError("Failed to delete domain", detail)
	}
}

func (r *DomainResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// Read fills every attribute from the server.
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// readDomain fetches the domain and its DNS records and maps them on
// top of prior (the plan or the previous state). Used after writes,
// where a missing domain is an error.
func (r *DomainResource) readDomain(ctx context.Context, prior DomainResourceModel, domainID string) (DomainResourceModel, diag.Diagnostics) {
	var diags diag.Diagnostics
	domain, err := r.client.GetDomain(ctx, r.client.OrgID, domainID)
	if err != nil {
		diags.AddError("Failed to read domain", err.Error())
		return prior, diags
	}
	return r.mapDomain(ctx, prior, domain)
}

// mapDomain fetches the DNS records of domain and maps both onto prior.
func (r *DomainResource) mapDomain(ctx context.Context, prior DomainResourceModel, domain *client.Domain) (DomainResourceModel, diag.Diagnostics) {
	var diags diag.Diagnostics
	records, err := r.client.ListDomainDNSRecords(ctx, r.client.OrgID, domain.DomainID)
	if err != nil && !errors.Is(err, client.ErrNotFound) {
		// The endpoint answers 404 when a domain has no records.
		diags.AddError("Failed to read domain DNS records", err.Error())
		return prior, diags
	}

	return applyDomainResponse(prior, domain, records)
}

// applyDomainResponse maps the server's domain and DNS records onto m.
func applyDomainResponse(m DomainResourceModel, domain *client.Domain, records []client.DomainDNSRecord) (DomainResourceModel, diag.Diagnostics) {
	m.ID = types.StringValue(domain.DomainID)
	m.BaseDomain = types.StringValue(domain.BaseDomain)
	m.Type = types.StringValue(domain.Type)
	m.CertResolver = tfconv.StringFromPtr(nonEmptyStringPtr(domain.CertResolver))
	m.PreferWildcardCert = types.BoolValue(domain.PreferWildcardCert)
	m.Verified = types.BoolValue(domain.Verified)
	m.Failed = types.BoolValue(domain.Failed)
	m.ErrorMessage = tfconv.StringFromPtr(nonEmptyStringPtr(domain.ErrorMessage))
	m.ConfigManaged = types.BoolValue(domain.ConfigManaged)

	sorted := append([]client.DomainDNSRecord(nil), records...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })
	objs := make([]attr.Value, 0, len(sorted))
	var diags diag.Diagnostics
	for _, rec := range sorted {
		obj, d := types.ObjectValue(domainDNSRecordAttrTypes, map[string]attr.Value{
			"record_type": types.StringValue(rec.RecordType),
			"base_domain": types.StringValue(rec.BaseDomain),
			"value":       types.StringValue(rec.Value),
		})
		diags.Append(d...)
		objs = append(objs, obj)
	}
	list, d := types.ListValue(types.ObjectType{AttrTypes: domainDNSRecordAttrTypes}, objs)
	diags.Append(d...)
	m.DNSRecords = list
	return m, diags
}

// nonEmptyStringPtr maps an empty string to nil, so "" from the
// server reads as null.
func nonEmptyStringPtr(s *string) *string {
	if s == nil || *s == "" {
		return nil
	}
	return s
}

// domainCreateErrorHint explains a create refused because the edition
// does not support the type: Community Edition answers 501 for ns and
// cname. Other errors (duplicate, overlap, root cname) get no hint.
func domainCreateErrorHint(domainType string, err error) string {
	if domainType == domainTypeWildcard || !strings.Contains(err.Error(), "(status 501)") {
		return ""
	}
	return "\n\nns and cname domains require Pangolin Enterprise Edition or Pangolin Cloud; " +
		"Community Edition only accepts type = \"wildcard\"."
}

// domainCertSettingModifier plans cert_resolver / prefer_wildcard_cert
// when they are absent from the config. On a wildcard domain, absent
// means cleared (null resolver, prefer_wildcard_cert = false). On ns
// and cname domains the server cannot change them after creation, so
// the provider plans what the server holds (or the create defaults),
// never a change it could not apply.
type domainCertSettingModifier struct{}

func (m domainCertSettingModifier) Description(_ context.Context) string {
	return "Plans the cleared value on wildcard domains and the server value on ns / cname domains when unset."
}

func (m domainCertSettingModifier) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

// plannedType returns the planned domain type, and false when the
// modifier must leave the plan alone (destroy, unknown type).
func (m domainCertSettingModifier) plannedType(ctx context.Context, plan tfsdk.Plan) (string, bool) {
	if plan.Raw.IsNull() {
		return "", false
	}
	var typ types.String
	if diags := plan.GetAttribute(ctx, path.Root("type"), &typ); diags.HasError() || typ.IsUnknown() || typ.IsNull() {
		return "", false
	}
	return typ.ValueString(), true
}

func (m domainCertSettingModifier) PlanModifyString(ctx context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	if !req.ConfigValue.IsNull() {
		return
	}
	typ, ok := m.plannedType(ctx, req.Plan)
	if !ok {
		return
	}
	if typ != domainTypeWildcard && !req.StateValue.IsNull() {
		resp.PlanValue = req.StateValue
		return
	}
	resp.PlanValue = types.StringNull()
}

func (m domainCertSettingModifier) PlanModifyBool(ctx context.Context, req planmodifier.BoolRequest, resp *planmodifier.BoolResponse) {
	if !req.ConfigValue.IsNull() {
		return
	}
	typ, ok := m.plannedType(ctx, req.Plan)
	if !ok {
		return
	}
	if typ != domainTypeWildcard && !req.StateValue.IsNull() {
		resp.PlanValue = req.StateValue
		return
	}
	resp.PlanValue = types.BoolValue(false)
}
