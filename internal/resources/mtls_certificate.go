package resources

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/resourcevalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stackopshq/terraform-provider-pangolin/internal/client"
	"github.com/stackopshq/terraform-provider-pangolin/internal/tfconv"
)

var (
	_ resource.Resource                     = &MTLSCertificateResource{}
	_ resource.ResourceWithImportState      = &MTLSCertificateResource{}
	_ resource.ResourceWithConfigValidators = &MTLSCertificateResource{}
)

// MTLSCertificateResource manages one trusted CA certificate of a resource
// or a resource policy, for client-certificate (mTLS) authentication.
type MTLSCertificateResource struct {
	client *client.Client
}

type MTLSCertificateModel struct {
	ID               types.Int64  `tfsdk:"id"`
	ResourceID       types.Int64  `tfsdk:"resource_id"`
	ResourcePolicyID types.Int64  `tfsdk:"resource_policy_id"`
	Name             types.String `tfsdk:"name"`
	Certificate      types.String `tfsdk:"certificate"`
	EnableMTLS       types.Bool   `tfsdk:"enable_mtls"`
	Fingerprint      types.String `tfsdk:"fingerprint"`
	Subject          types.String `tfsdk:"subject"`
	Issuer           types.String `tfsdk:"issuer"`
}

func NewMTLSCertificateResource() resource.Resource {
	return &MTLSCertificateResource{}
}

func (r *MTLSCertificateResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_resource_mtls_certificate"
}

// certificateFingerprint checks that the input is exactly one PEM
// certificate and returns its SHA-256 fingerprint in the server's format
// (upper-case hex pairs joined by colons). Pangolin splits a bundle into
// several certificates, which one resource could not track, and refuses a
// private key.
func certificateFingerprint(in string) (string, error) {
	if strings.Contains(in, "PRIVATE KEY") {
		return "", errors.New("this looks like a private key; give the CA's public certificate only")
	}
	var der []byte
	rest := []byte(in)
	for {
		var b *pem.Block
		b, rest = pem.Decode(rest)
		if b == nil {
			break
		}
		if b.Type != "CERTIFICATE" {
			continue
		}
		if der != nil {
			return "", errors.New("the PEM holds several certificates; use one resource per CA certificate")
		}
		if _, err := x509.ParseCertificate(b.Bytes); err != nil {
			return "", fmt.Errorf("could not parse the certificate: %w", err)
		}
		der = b.Bytes
	}
	if der == nil {
		return "", errors.New("no PEM certificate found (expected a -----BEGIN CERTIFICATE----- block)")
	}
	sum := sha256.Sum256(der)
	parts := make([]string, len(sum))
	for i, b := range sum {
		parts[i] = fmt.Sprintf("%02X", b)
	}
	return strings.Join(parts, ":"), nil
}

type pemValidator struct{}

func (pemValidator) Description(context.Context) string { return "a single PEM CA certificate" }
func (v pemValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}
func (pemValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if _, err := certificateFingerprint(req.ConfigValue.ValueString()); err != nil {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid CA certificate", err.Error())
	}
}

func (r *MTLSCertificateResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages one trusted CA certificate for client-certificate (mTLS) authentication on a resource or a resource policy " +
			"(Pangolin 1.25.3 or later, enterprise license). Clients must then present a certificate signed by one of the CA certificates. " +
			"Set exactly one of `resource_id` and `resource_policy_id`.\n\n" +
			"Pangolin enforces mTLS only when it is enabled **and** at least one CA certificate exists, and refuses to enable it before. " +
			"That is why the switch is `enable_mtls` here and not an attribute of the resource: a resource that enabled mTLS itself would need its own certificate to exist first, a dependency cycle. " +
			"Set `enable_mtls = true` on one certificate; it is applied after the certificate is stored. mTLS needs `ssl = true` on an HTTP resource (the server refuses it on tcp / udp resources, and a shared policy that enables mTLS takes precedence over the resource's own setting).\n\n" +
			"Pangolin has no update route and never returns the PEM: changing `certificate` or `name` replaces the certificate, and the certificate is compared by its SHA-256 `fingerprint`. " +
			"A PEM bundle is refused: use one resource per CA certificate (at most 20 per resource or policy).\n\n" +
			"Removing the last CA certificate makes Pangolin switch mTLS off. Import with `resource/{resource_id}/{id}` or `policy/{resource_policy_id}/{id}`; " +
			"set `certificate` as usual afterwards (it must match the imported fingerprint).",
		Attributes: map[string]schema.Attribute{
			"id": schema.Int64Attribute{
				Description: "The certificate ID.",
				Computed:    true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"resource_id": schema.Int64Attribute{
				Description: "The resource trusting the CA. Without a shared policy the certificate goes to the resource's inline policy. Changing it replaces the certificate.",
				Optional:    true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.RequiresReplace(),
				},
				Validators: []validator.Int64{int64validator.AtLeast(1)},
			},
			"resource_policy_id": schema.Int64Attribute{
				Description: "The resource policy trusting the CA. Changing it replaces the certificate.",
				Optional:    true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.RequiresReplace(),
				},
				Validators: []validator.Int64{int64validator.AtLeast(1)},
			},
			"name": schema.StringAttribute{
				Description: "A label for the certificate. Changing it replaces the certificate.",
				Optional:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{stringvalidator.LengthBetween(1, 255)},
			},
			"certificate": schema.StringAttribute{
				Description: "The CA certificate, one PEM block. Changing it replaces the certificate.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					// A null state is an imported certificate: adopt the config
					// (Update checks its fingerprint) instead of replacing.
					// Otherwise only a different certificate replaces: a
					// whitespace or comment change keeps the fingerprint.
					stringplanmodifier.RequiresReplaceIf(func(ctx context.Context, req planmodifier.StringRequest, resp *stringplanmodifier.RequiresReplaceIfFuncResponse) {
						if req.StateValue.IsNull() || req.PlanValue.IsUnknown() {
							resp.RequiresReplace = !req.StateValue.IsNull()
							return
						}
						var stateFP types.String
						resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("fingerprint"), &stateFP)...)
						fp, _ := certificateFingerprint(req.PlanValue.ValueString())
						resp.RequiresReplace = fp != stateFP.ValueString()
					}, "Replaces the certificate when its fingerprint changes.", "Replaces the certificate when its fingerprint changes."),
				},
				Validators: []validator.String{pemValidator{}},
			},
			"enable_mtls": schema.BoolAttribute{
				Description: "Turn mTLS on for the resource or policy once this certificate is stored. Defaults to `false`, which leaves the setting alone (it does not turn mTLS off, other certificates may have enabled it). " +
					"Set it on one certificate only. Destroying the certificate does not turn mTLS off unless it is the last one.",
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(false),
			},
			"fingerprint": schema.StringAttribute{
				Description: "The SHA-256 fingerprint, as computed by Pangolin.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"subject": schema.StringAttribute{
				Description: "The certificate subject.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"issuer": schema.StringAttribute{
				Description: "The certificate issuer.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *MTLSCertificateResource) ConfigValidators(_ context.Context) []resource.ConfigValidator {
	return []resource.ConfigValidator{
		resourcevalidator.ExactlyOneOf(path.MatchRoot("resource_id"), path.MatchRoot("resource_policy_id")),
	}
}

func (r *MTLSCertificateResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (m MTLSCertificateModel) owner() client.MTLSOwner {
	return client.MTLSOwner{ResourceID: int(m.ResourceID.ValueInt64()), ResourcePolicyID: int(m.ResourcePolicyID.ValueInt64())}
}

func (m *MTLSCertificateModel) fill(c *client.MTLSCertificate) {
	m.ID = types.Int64Value(int64(c.ID))
	m.Name = tfconv.StringFromPtr(c.Name)
	m.Fingerprint = tfconv.StringFromPtr(c.Fingerprint)
	m.Subject = tfconv.StringFromPtr(c.Subject)
	m.Issuer = tfconv.StringFromPtr(c.Issuer)
}

func (r *MTLSCertificateResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan MTLSCertificateModel
	if resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...); resp.Diagnostics.HasError() {
		return
	}
	cert, err := r.client.CreateMTLSCertificate(ctx, plan.owner(), tfconv.StringToPtr(plan.Name), plan.Certificate.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to add mTLS CA certificate", err.Error())
		return
	}
	plan.fill(cert)
	enable := plan.EnableMTLS.ValueBool()
	// Saved before enabling: if that fails the certificate exists and the
	// resource is tainted, not forgotten.
	plan.EnableMTLS = types.BoolValue(false)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	if enable {
		if err := r.client.SetMTLSEnabled(ctx, plan.owner(), true); err != nil {
			resp.Diagnostics.AddError("Failed to enable mTLS", err.Error())
			return
		}
		plan.EnableMTLS = types.BoolValue(true)
		resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	}
}

func (r *MTLSCertificateResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state MTLSCertificateModel
	if resp.Diagnostics.Append(req.State.Get(ctx, &state)...); resp.Diagnostics.HasError() {
		return
	}
	cert, err := r.client.GetMTLSCertificate(ctx, state.owner(), int(state.ID.ValueInt64()))
	if err != nil {
		if errors.Is(err, client.ErrNotFound) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read mTLS CA certificate", err.Error())
		return
	}
	state.fill(cert)
	// Only a certificate that manages the switch tracks it.
	if state.EnableMTLS.ValueBool() {
		on, err := r.client.GetMTLSEnabled(ctx, state.owner())
		if err != nil {
			resp.Diagnostics.AddError("Failed to read mTLS setting", err.Error())
			return
		}
		state.EnableMTLS = types.BoolValue(on)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *MTLSCertificateResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state MTLSCertificateModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// Only enable_mtls changes in place, or the certificate of an imported
	// resource, which has to be the one that was imported.
	if fp, _ := certificateFingerprint(plan.Certificate.ValueString()); fp != state.Fingerprint.ValueString() {
		resp.Diagnostics.AddAttributeError(path.Root("certificate"), "Certificate does not match",
			fmt.Sprintf("The configured certificate has fingerprint %s, the imported one %s.", fp, state.Fingerprint.ValueString()))
		return
	}
	if !plan.EnableMTLS.Equal(state.EnableMTLS) {
		if err := r.client.SetMTLSEnabled(ctx, state.owner(), plan.EnableMTLS.ValueBool()); err != nil {
			resp.Diagnostics.AddError("Failed to update mTLS setting", err.Error())
			return
		}
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *MTLSCertificateResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state MTLSCertificateModel
	if resp.Diagnostics.Append(req.State.Get(ctx, &state)...); resp.Diagnostics.HasError() {
		return
	}
	err := r.client.DeleteMTLSCertificate(ctx, state.owner(), int(state.ID.ValueInt64()))
	if err != nil && !errors.Is(err, client.ErrNotFound) {
		resp.Diagnostics.AddError("Failed to delete mTLS CA certificate", err.Error())
	}
}

func (r *MTLSCertificateResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.Split(req.ID, "/")
	var id int64
	var err error
	if len(parts) == 3 {
		id, err = strconv.ParseInt(parts[2], 10, 64)
	}
	owner, ownerID := "", int64(0)
	if err == nil && len(parts) == 3 {
		owner = map[string]string{"resource": "resource_id", "policy": "resource_policy_id"}[parts[0]]
		ownerID, err = strconv.ParseInt(parts[1], 10, 64)
	}
	if len(parts) != 3 || owner == "" || err != nil || id <= 0 || ownerID <= 0 {
		resp.Diagnostics.AddError("Invalid import ID", fmt.Sprintf("Expected resource/{resource_id}/{id} or policy/{resource_policy_id}/{id}, got: %q", req.ID))
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(owner), ownerID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("enable_mtls"), false)...)
}
