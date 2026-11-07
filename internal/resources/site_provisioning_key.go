package resources

import (
	"context"
	"errors"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
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
	_ resource.Resource                = &SiteProvisioningKeyResource{}
	_ resource.ResourceWithImportState = &SiteProvisioningKeyResource{}
)

// SiteProvisioningKeyResource manages a site provisioning key.
type SiteProvisioningKeyResource struct {
	client *client.Client
}

// SiteProvisioningKeyResourceModel describes the resource data model.
type SiteProvisioningKeyResourceModel struct {
	ID              types.String `tfsdk:"id"`
	Name            types.String `tfsdk:"name"`
	Key             types.String `tfsdk:"key"`
	LastChars       types.String `tfsdk:"last_chars"`
	MaxBatchSize    types.Int64  `tfsdk:"max_batch_size"`
	ValidUntil      types.String `tfsdk:"valid_until"`
	ApproveNewSites types.Bool   `tfsdk:"approve_new_sites"`
	NumUsed         types.Int64  `tfsdk:"num_used"`
	LastUsed        types.String `tfsdk:"last_used"`
	CreatedAt       types.String `tfsdk:"created_at"`
}

// NewSiteProvisioningKeyResource returns a new resource factory.
func NewSiteProvisioningKeyResource() resource.Resource {
	return &SiteProvisioningKeyResource{}
}

func (r *SiteProvisioningKeyResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_site_provisioning_key"
}

// rfc3339Validator checks that a string is an RFC 3339 timestamp.
type rfc3339Validator struct{}

func (rfc3339Validator) Description(context.Context) string { return "must be an RFC 3339 timestamp" }
func (v rfc3339Validator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}
func (rfc3339Validator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if _, err := time.Parse(time.RFC3339, req.ConfigValue.ValueString()); err != nil {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid timestamp",
			"Expected an RFC 3339 timestamp such as 2027-01-01T00:00:00Z.")
	}
}

func (r *SiteProvisioningKeyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a site provisioning key (Pangolin 1.17+, Enterprise): a Newt started with " +
			"`--provisioning-key <key>` registers a new site by itself, without creating the site first.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description:   "The provisioning key ID.",
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Description:   "The key name. The server cannot rename a key: changing it replaces the key.",
				Required:      true,
				Validators:    []validator.String{stringvalidator.LengthBetween(1, 255)},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"key": schema.StringAttribute{
				Description:   "The key to give Newt. Only returned on creation: empty after import.",
				Computed:      true,
				Sensitive:     true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"last_chars": schema.StringAttribute{
				Description:   "The last characters of the key, as shown in the dashboard.",
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"max_batch_size": schema.Int64Attribute{
				Description: "How many sites the key can create in total. Unset means no limit.",
				Optional:    true,
				Validators:  []validator.Int64{int64validator.Between(1, 1_000_000)},
			},
			"valid_until": schema.StringAttribute{
				Description:   "Expiry as an RFC 3339 timestamp, e.g. `2027-01-01T00:00:00Z`. Unset means no expiry.",
				Optional:      true,
				Computed:      true,
				Validators:    []validator.String{rfc3339Validator{}},
				PlanModifiers: []planmodifier.String{sameInstantModifier{}},
			},
			"approve_new_sites": schema.BoolAttribute{
				Description: "Whether sites created with the key are approved right away; `false` leaves them pending " +
					"approval in the dashboard. Defaults to `true`.",
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(true),
			},
			"num_used": schema.Int64Attribute{
				Description:   "How many sites the key has created.",
				Computed:      true,
				PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
			},
			"last_used": schema.StringAttribute{
				Description:   "When the key was last used, null if never.",
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"created_at": schema.StringAttribute{
				Description:   "When the key was created.",
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *SiteProvisioningKeyResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// sameInstantModifier plans no change when the configured timestamp is
// the instant already in state written differently (after an import the
// state holds the server's toISOString() form).
type sameInstantModifier struct{}

func (sameInstantModifier) Description(context.Context) string {
	return "ignores a timestamp written differently for the same instant"
}
func (m sameInstantModifier) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}
func (sameInstantModifier) PlanModifyString(_ context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	// Unset in config: no expiry. Computed, so plan null rather than unknown.
	if req.ConfigValue.IsNull() {
		resp.PlanValue = types.StringNull()
		return
	}
	if req.StateValue.IsNull() || req.PlanValue.IsUnknown() {
		return
	}
	if got := sameInstant(req.PlanValue, req.StateValue.ValueStringPointer()); got.Equal(req.PlanValue) {
		resp.PlanValue = req.StateValue
	}
}

// sameInstant keeps the configured timestamp when the server returns the
// same instant in another form (it stores toISOString(), with millis).
func sameInstant(prior types.String, server *string) types.String {
	if server == nil {
		return types.StringNull()
	}
	if !prior.IsNull() && !prior.IsUnknown() {
		a, err1 := time.Parse(time.RFC3339, prior.ValueString())
		b, err2 := time.Parse(time.RFC3339, *server)
		if err1 == nil && err2 == nil && a.Equal(b) {
			return prior
		}
	}
	return types.StringValue(*server)
}

// mergeSiteProvisioningKey copies the server row into the model. The key
// itself is never read back, so the model keeps its own.
func mergeSiteProvisioningKey(m *SiteProvisioningKeyResourceModel, in *client.SiteProvisioningKey) {
	m.ID = types.StringValue(in.ID)
	m.Name = types.StringValue(in.Name)
	m.LastChars = types.StringValue(in.LastChars)
	m.MaxBatchSize = tfconv.Int64FromIntPtr(in.MaxBatchSize)
	m.ValidUntil = sameInstant(m.ValidUntil, in.ValidUntil)
	m.ApproveNewSites = types.BoolValue(in.ApproveNewSites)
	m.NumUsed = types.Int64Value(int64(in.NumUsed))
	m.LastUsed = tfconv.StringFromPtr(in.LastUsed)
	m.CreatedAt = types.StringValue(in.CreatedAt)
	if m.Key.IsUnknown() || m.Key.IsNull() {
		m.Key = types.StringValue("")
	}
}

func (r *SiteProvisioningKeyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan SiteProvisioningKeyResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	created, err := r.client.CreateSiteProvisioningKey(ctx, &client.CreateSiteProvisioningKeyRequest{
		Name:            plan.Name.ValueString(),
		MaxBatchSize:    int64PtrToIntPtr(plan.MaxBatchSize),
		ValidUntil:      plan.ValidUntil.ValueString(),
		ApproveNewSites: plan.ApproveNewSites.ValueBool(),
	})
	if err != nil {
		resp.Diagnostics.AddError("Failed to create site provisioning key", err.Error())
		return
	}
	plan.Key = types.StringValue(created.Key)
	mergeSiteProvisioningKey(&plan, created)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *SiteProvisioningKeyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state SiteProvisioningKeyResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	got, err := r.client.GetSiteProvisioningKey(ctx, state.ID.ValueString())
	if err != nil {
		if errors.Is(err, client.ErrNotFound) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read site provisioning key", err.Error())
		return
	}
	mergeSiteProvisioningKey(&state, got)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *SiteProvisioningKeyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state SiteProvisioningKeyResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id := state.ID.ValueString()
	if err := r.client.UpdateSiteProvisioningKey(ctx, id, &client.UpdateSiteProvisioningKeyRequest{
		MaxBatchSize:    int64PtrToIntPtr(plan.MaxBatchSize),
		ValidUntil:      plan.ValidUntil.ValueString(),
		ApproveNewSites: plan.ApproveNewSites.ValueBool(),
	}); err != nil {
		resp.Diagnostics.AddError("Failed to update site provisioning key", err.Error())
		return
	}
	got, err := r.client.GetSiteProvisioningKey(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read site provisioning key after update", err.Error())
		return
	}
	plan.Key = state.Key
	mergeSiteProvisioningKey(&plan, got)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *SiteProvisioningKeyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state SiteProvisioningKeyResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DeleteSiteProvisioningKey(ctx, state.ID.ValueString()); err != nil && !errors.Is(err, client.ErrNotFound) {
		resp.Diagnostics.AddError("Failed to delete site provisioning key", err.Error())
	}
}

func (r *SiteProvisioningKeyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
