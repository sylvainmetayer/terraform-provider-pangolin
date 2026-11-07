package resources

import (
	"context"
	"errors"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stackopshq/terraform-provider-pangolin/internal/client"
	"github.com/stackopshq/terraform-provider-pangolin/internal/tfconv"
)

var (
	_ resource.Resource                   = &CustomDNSRecordResource{}
	_ resource.ResourceWithImportState    = &CustomDNSRecordResource{}
	_ resource.ResourceWithValidateConfig = &CustomDNSRecordResource{}
)

// CustomDNSRecordResource manages a record served by Pangolin's own DNS
// server for a domain delegated to it (Pangolin 1.26.0+, Enterprise).
type CustomDNSRecordResource struct {
	client *client.Client
}

type CustomDNSRecordResourceModel struct {
	ID         types.Int64  `tfsdk:"id"`
	DomainID   types.String `tfsdk:"domain_id"`
	Name       types.String `tfsdk:"name"`
	Type       types.String `tfsdk:"type"`
	Values     types.List   `tfsdk:"values"`
	TTL        types.Int64  `tfsdk:"ttl"`
	Priority   types.Int64  `tfsdk:"priority"`
	Weight     types.Int64  `tfsdk:"weight"`
	Port       types.Int64  `tfsdk:"port"`
	Enabled    types.Bool   `tfsdk:"enabled"`
	FullDomain types.String `tfsdk:"full_domain"`
}

func NewCustomDNSRecordResource() resource.Resource {
	return &CustomDNSRecordResource{}
}

func (r *CustomDNSRecordResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_custom_dns_record"
}

// The server normalises names and values (lowercase, no trailing dot,
// relative name, TXT quotes stripped). Configs must already be in that
// form, so what is written is what is stored.
var (
	dnsLabel                  = `[a-z0-9_]([a-z0-9_-]{0,61}[a-z0-9_])?`
	customDNSNameRe           = regexp.MustCompile(`^(@|\*|(\*\.)?` + dnsLabel + `(\.` + dnsLabel + `)*)$`)
	customDNSHostRe           = regexp.MustCompile(`^` + dnsLabel + `(\.` + dnsLabel + `)+$`)
	customDNSTXTRe            = regexp.MustCompile(`^[\x20-\x7e]+$`)
	customDNSTypes            = []string{"A", "AAAA", "CNAME", "MX", "NS", "SRV", "TXT"}
	customDNSUint16Validators = []validator.Int64{int64validator.Between(0, 65535)}
)

func (r *CustomDNSRecordResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a custom DNS record served by Pangolin's own nameservers, for a domain delegated to Pangolin (`pangolin_domain` with `type = \"ns\"`, not shared with another organization). " +
			"Requires Pangolin 1.26.0 or later and an enterprise license.\n\n" +
			"Pangolin normalises records; the provider asks for that form instead of rewriting it, so what is written is what is stored: " +
			"`name` relative to the domain and lowercase (`@` for the apex), hostnames lowercase without a trailing dot, TXT values without surrounding quotes. " +
			"Pangolin keeps one record per name and type (and per priority / weight / port for MX and SRV): put several values in `values`. " +
			"It refuses names it serves itself (resources, login pages, redirects, `_acme-challenge`) and caps a domain at 100 records and a name at 10 values per type.\n\n" +
			"Import with `{domain_id}/{id}`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.Int64Attribute{
				Description: "The record ID.",
				Computed:    true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"domain_id": schema.StringAttribute{
				Description: "The ID of the domain (type `ns`). Changing it replaces the record.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"name": schema.StringAttribute{
				Description: "The name relative to the domain, lowercase: `@` for the apex, `www`, `_dmarc`, `*.apps` (a wildcard only as the leftmost label).",
				Required:    true,
				Validators: []validator.String{
					stringvalidator.RegexMatches(customDNSNameRe, "must be `@` or lowercase dot-separated labels (letters, digits, `-`, `_`) relative to the domain, with `*` only as the leftmost label, and no trailing dot"),
				},
			},
			"type": schema.StringAttribute{
				Description: "The record type: `A`, `AAAA`, `CNAME`, `MX`, `NS`, `SRV` or `TXT`.",
				Required:    true,
				Validators:  []validator.String{stringvalidator.OneOf(customDNSTypes...)},
			},
			"values": schema.ListAttribute{
				Description: "The record data, one resource record per entry (1 to 10, no duplicates): an IPv4 address for `A`, a lowercase IPv6 address for `AAAA`, a lowercase hostname without a trailing dot for `CNAME` (one value), `MX`, `NS` and `SRV`, printable ASCII without surrounding quotes for `TXT` (up to 1024 characters).",
				Required:    true,
				ElementType: types.StringType,
				Validators: []validator.List{
					listvalidator.SizeBetween(1, 10),
					listvalidator.UniqueValues(),
				},
			},
			"ttl": schema.Int64Attribute{
				Description: "Time to live in seconds, 60 to 86400. Defaults to `300`.",
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(300),
				Validators:  []validator.Int64{int64validator.Between(60, 86400)},
			},
			"priority": schema.Int64Attribute{
				Description: "Priority, required for `MX` and `SRV` and not allowed for other types.",
				Optional:    true,
				Validators:  customDNSUint16Validators,
			},
			"weight": schema.Int64Attribute{
				Description: "Weight, required for `SRV` and not allowed for other types.",
				Optional:    true,
				Validators:  customDNSUint16Validators,
			},
			"port": schema.Int64Attribute{
				Description: "Port, required for `SRV` and not allowed for other types.",
				Optional:    true,
				Validators:  customDNSUint16Validators,
			},
			"enabled": schema.BoolAttribute{
				Description: "Whether the record is served. Defaults to `true`.",
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(true),
			},
			"full_domain": schema.StringAttribute{
				Description: "The fully qualified name the record is served for.",
				Computed:    true,
			},
		},
	}
}

func (r *CustomDNSRecordResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// customDNSValueError returns why a value is not in the form the server
// stores, or "" when it is.
func customDNSValueError(recordType, v string) string {
	switch recordType {
	case "A":
		if ip := net.ParseIP(v); ip == nil || ip.To4() == nil || strings.Contains(v, ":") {
			return "must be an IPv4 address"
		}
	case "AAAA":
		if ip := net.ParseIP(v); ip == nil || !strings.Contains(v, ":") {
			return "must be an IPv6 address"
		}
		if v != strings.ToLower(v) {
			return fmt.Sprintf("must be lowercase (%q)", strings.ToLower(v))
		}
	case "CNAME", "MX", "NS", "SRV":
		if c := strings.TrimSuffix(strings.ToLower(v), "."); c != v && customDNSHostRe.MatchString(c) {
			return fmt.Sprintf("must be written %q (lowercase, no trailing dot)", c)
		}
		if !customDNSHostRe.MatchString(v) {
			return "must be a fully qualified hostname (lowercase, no trailing dot)"
		}
	case "TXT":
		if len(v) >= 2 && strings.HasPrefix(v, `"`) && strings.HasSuffix(v, `"`) {
			return "must not be wrapped in quotes (Pangolin strips them)"
		}
		if len(v) > 1024 || !customDNSTXTRe.MatchString(v) {
			return "must be 1 to 1024 printable ASCII characters"
		}
	}
	return ""
}

func validateCustomDNSRecord(ctx context.Context, m CustomDNSRecordResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics
	if m.Type.IsUnknown() {
		return diags
	}
	t := m.Type.ValueString()
	if !m.Values.IsUnknown() && !m.Values.IsNull() {
		var values []types.String
		diags.Append(m.Values.ElementsAs(ctx, &values, false)...)
		for i, v := range values {
			if v.IsUnknown() || v.IsNull() {
				continue
			}
			if msg := customDNSValueError(t, v.ValueString()); msg != "" {
				diags.AddAttributeError(path.Root("values").AtListIndex(i), "Invalid "+t+" value", fmt.Sprintf("%q %s.", v.ValueString(), msg))
			}
		}
		if t == "CNAME" && len(values) > 1 {
			diags.AddAttributeError(path.Root("values"), "Invalid CNAME record", "A CNAME record takes one value.")
		}
	}
	// The server stores null for the fields a type has no use for: a set
	// value would drift, a missing one is refused.
	for _, f := range []struct {
		name  string
		v     types.Int64
		types []string
	}{
		{"priority", m.Priority, []string{"MX", "SRV"}},
		{"weight", m.Weight, []string{"SRV"}},
		{"port", m.Port, []string{"SRV"}},
	} {
		needed := false
		for _, ft := range f.types {
			needed = needed || ft == t
		}
		switch {
		case needed && f.v.IsNull():
			diags.AddAttributeError(path.Root(f.name), "Missing "+f.name, fmt.Sprintf("%s records require %s.", t, f.name))
		case !needed && !f.v.IsNull() && !f.v.IsUnknown():
			diags.AddAttributeError(path.Root(f.name), "Unexpected "+f.name, fmt.Sprintf("%s is only used by %s records.", f.name, strings.Join(f.types, " and ")))
		}
	}
	return diags
}

func (r *CustomDNSRecordResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var cfg CustomDNSRecordResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(validateCustomDNSRecord(ctx, cfg)...)
}

// checkRelativeName refuses a name written as an FQDN of the domain,
// which the server would store relative (and drift). It needs the base
// domain, so it runs at apply, before the record is written.
func (r *CustomDNSRecordResource) checkRelativeName(ctx context.Context, m CustomDNSRecordResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics
	d, err := r.client.GetDomain(ctx, r.client.OrgID, m.DomainID.ValueString())
	if err != nil {
		diags.AddError("Failed to read the record's domain", err.Error())
		return diags
	}
	base := strings.ToLower(d.BaseDomain)
	name := m.Name.ValueString()
	if name == base {
		diags.AddAttributeError(path.Root("name"), "Name not relative to the domain", fmt.Sprintf("Write \"@\" for the apex of %s.", base))
	} else if rel, ok := strings.CutSuffix(name, "."+base); ok {
		diags.AddAttributeError(path.Root("name"), "Name not relative to the domain", fmt.Sprintf("Write %q: names are relative to %s.", rel, base))
	}
	return diags
}

func customDNSRecordRequest(ctx context.Context, m CustomDNSRecordResourceModel) (*client.CustomDNSRecordRequest, diag.Diagnostics) {
	var values []string
	diags := m.Values.ElementsAs(ctx, &values, false)
	intPtr := func(v types.Int64) *int {
		if v.IsNull() || v.IsUnknown() {
			return nil
		}
		i := int(v.ValueInt64())
		return &i
	}
	return &client.CustomDNSRecordRequest{
		Name:     m.Name.ValueString(),
		Type:     m.Type.ValueString(),
		Values:   values,
		TTL:      int(m.TTL.ValueInt64()),
		Priority: intPtr(m.Priority),
		Weight:   intPtr(m.Weight),
		Port:     intPtr(m.Port),
		Enabled:  m.Enabled.ValueBool(),
	}, diags
}

func customDNSRecordToModel(ctx context.Context, rec *client.CustomDNSRecord) (CustomDNSRecordResourceModel, diag.Diagnostics) {
	values, diags := types.ListValueFrom(ctx, types.StringType, rec.Values)
	return CustomDNSRecordResourceModel{
		ID:         types.Int64Value(int64(rec.CustomDNSRecordID)),
		DomainID:   types.StringValue(rec.DomainID),
		Name:       types.StringValue(rec.Name),
		Type:       types.StringValue(rec.Type),
		Values:     values,
		TTL:        types.Int64Value(int64(rec.TTL)),
		Priority:   tfconv.Int64FromIntPtr(rec.Priority),
		Weight:     tfconv.Int64FromIntPtr(rec.Weight),
		Port:       tfconv.Int64FromIntPtr(rec.Port),
		Enabled:    types.BoolValue(rec.Enabled),
		FullDomain: types.StringValue(rec.FullDomain),
	}, diags
}

// write creates (id 0) or updates the record. The name check reads the
// domain, so it only runs when the name is new.
func (r *CustomDNSRecordResource) write(ctx context.Context, plan CustomDNSRecordResourceModel, id int, nameChanged bool, diags *diag.Diagnostics, state *tfsdk.State) {
	if nameChanged {
		if diags.Append(r.checkRelativeName(ctx, plan)...); diags.HasError() {
			return
		}
	}
	body, d := customDNSRecordRequest(ctx, plan)
	if diags.Append(d...); diags.HasError() {
		return
	}
	var rec *client.CustomDNSRecord
	var err error
	if id == 0 {
		rec, err = r.client.CreateCustomDNSRecord(ctx, plan.DomainID.ValueString(), body)
	} else {
		rec, err = r.client.UpdateCustomDNSRecord(ctx, plan.DomainID.ValueString(), id, body)
	}
	if err != nil {
		diags.AddError("Failed to write custom DNS record", err.Error())
		return
	}
	m, d := customDNSRecordToModel(ctx, rec)
	if diags.Append(d...); diags.HasError() {
		return
	}
	diags.Append(state.Set(ctx, &m)...)
}

func (r *CustomDNSRecordResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan CustomDNSRecordResourceModel
	if resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...); resp.Diagnostics.HasError() {
		return
	}
	r.write(ctx, plan, 0, true, &resp.Diagnostics, &resp.State)
}

func (r *CustomDNSRecordResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state CustomDNSRecordResourceModel
	if resp.Diagnostics.Append(req.State.Get(ctx, &state)...); resp.Diagnostics.HasError() {
		return
	}
	rec, err := r.client.GetCustomDNSRecord(ctx, state.DomainID.ValueString(), int(state.ID.ValueInt64()))
	if err != nil {
		if errors.Is(err, client.ErrNotFound) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read custom DNS record", err.Error())
		return
	}
	m, d := customDNSRecordToModel(ctx, rec)
	if resp.Diagnostics.Append(d...); resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *CustomDNSRecordResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state CustomDNSRecordResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.write(ctx, plan, int(state.ID.ValueInt64()), !plan.Name.Equal(state.Name), &resp.Diagnostics, &resp.State)
}

func (r *CustomDNSRecordResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state CustomDNSRecordResourceModel
	if resp.Diagnostics.Append(req.State.Get(ctx, &state)...); resp.Diagnostics.HasError() {
		return
	}
	err := r.client.DeleteCustomDNSRecord(ctx, state.DomainID.ValueString(), int(state.ID.ValueInt64()))
	if err != nil && !errors.Is(err, client.ErrNotFound) {
		resp.Diagnostics.AddError("Failed to delete custom DNS record", err.Error())
	}
}

func (r *CustomDNSRecordResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	domainID, idStr, ok := strings.Cut(req.ID, "/")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if !ok || domainID == "" || err != nil || id <= 0 {
		resp.Diagnostics.AddError("Invalid import ID", fmt.Sprintf("Expected {domain_id}/{id}, got: %q", req.ID))
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("domain_id"), domainID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
}
