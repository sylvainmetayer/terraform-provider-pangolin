package resources

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
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
	_ resource.Resource                = &SiteResource{}
	_ resource.ResourceWithImportState = &SiteResource{}
	_ resource.ResourceWithModifyPlan  = &SiteResource{}
)

// SiteResource defines the resource implementation.
type SiteResource struct {
	client *client.Client
}

// SiteResourceModel describes the resource data model.
type SiteResourceModel struct {
	ID                  types.Int64  `tfsdk:"id"`
	NiceID              types.String `tfsdk:"nice_id"`
	Name                types.String `tfsdk:"name"`
	Type                types.String `tfsdk:"type"`
	Online              types.Bool   `tfsdk:"online"`
	Address             types.String `tfsdk:"address"`
	NewtID              types.String `tfsdk:"newt_id"`
	NewtSecret          types.String `tfsdk:"newt_secret"`
	DockerSocketEnabled types.Bool   `tfsdk:"docker_socket_enabled"`
	NewtAutoUpdate      types.Bool   `tfsdk:"newt_auto_update"`
	RotationTrigger     types.String `tfsdk:"rotation_trigger"`
	RotationDisconnect  types.Bool   `tfsdk:"rotation_disconnect"`
}

// NewSiteResource returns a new resource factory.
func NewSiteResource() resource.Resource {
	return &SiteResource{}
}

func (r *SiteResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_site"
}

func (r *SiteResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a Pangolin site (tunnel connector).",
		Attributes: map[string]schema.Attribute{
			"id": schema.Int64Attribute{
				Description: "The numeric ID of the site.",
				Computed:    true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"nice_id": schema.StringAttribute{
				Description: "The human-readable ID of the site.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Description: "The name of the site.",
				Required:    true,
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
			"type": schema.StringAttribute{
				Description: "The type of the site (e.g. 'newt').",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"online": schema.BoolAttribute{
				Description: "Whether the site is online.",
				Computed:    true,
			},
			"address": schema.StringAttribute{
				Description: "The WireGuard address of the site.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"newt_id": schema.StringAttribute{
				Description: "The Newt client ID assigned to this site. Set at creation time.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"newt_secret": schema.StringAttribute{
				Description: "The Newt client secret assigned to this site. Set at creation time and not retrievable from the API afterwards. Not available after `terraform import`.",
				Computed:    true,
				Sensitive:   true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"rotation_trigger":    rotationTriggerAttribute("newt_secret", "Newt"),
			"rotation_disconnect": rotationDisconnectAttribute("Newt"),
			"docker_socket_enabled": schema.BoolAttribute{
				Description: "Enable Docker socket access on this site. Defaults to `true` to match the Pangolin API default.",
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(true),
			},
			"newt_auto_update": schema.BoolAttribute{
				Description: "Whether this site's Newt updates itself, overriding the organization's `newt_auto_update` (`pangolin_org`). Unset, the site follows the organization. Enterprise: a server without a license forces it off, so `true` fails with an inconsistent result.",
				Optional:    true,
			},
		},
	}
}

func (r *SiteResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	markSecretUnknownOnRotation(ctx, req, resp, "newt_secret")
}

func (r *SiteResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *SiteResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan SiteResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Get site defaults (newtId, secret, address)
	r.client.AddressMu.Lock()
	defaults, err := r.client.GetSiteDefaults(ctx)
	if err != nil {
		r.client.AddressMu.Unlock()
		resp.Diagnostics.AddError("Failed to get site defaults", err.Error())
		return
	}

	site, err := r.client.CreateSite(ctx, &client.CreateSiteRequest{
		Name:    plan.Name.ValueString(),
		Type:    "newt",
		NewtID:  defaults.NewtID,
		Secret:  defaults.NewtSecret,
		Address: defaults.Address,
	})
	r.client.AddressMu.Unlock() // address is taken (or the create failed); the update below needs no lock
	if err != nil {
		resp.Diagnostics.AddError("Failed to create site", err.Error())
		return
	}

	plan.ID = types.Int64Value(int64(site.SiteID))
	plan.NiceID = types.StringValue(site.NiceID)
	plan.Type = types.StringValue(site.Type)
	plan.Online = types.BoolValue(site.Online)
	plan.Address = types.StringValue(site.Address)
	plan.NewtID = types.StringValue(defaults.NewtID)
	plan.NewtSecret = types.StringValue(defaults.NewtSecret)

	// Create takes no settings: apply them when they differ from the
	// server defaults (docker socket on, follow the org).
	if !plan.NewtAutoUpdate.IsNull() || (!plan.DockerSocketEnabled.IsUnknown() && plan.DockerSocketEnabled.ValueBool() != site.DockerSocketEnabled) {
		site, err = r.client.UpdateSite(ctx, site.SiteID, siteUpdateRequest(plan))
		if err != nil {
			// Keep the site in the state: Terraform taints it, so it is destroyed
			// on the next apply instead of leaking on the server.
			resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
			resp.Diagnostics.AddError("Failed to apply site settings after creation", err.Error())
			return
		}
	}
	plan.DockerSocketEnabled = types.BoolValue(site.DockerSocketEnabled)
	plan.NewtAutoUpdate = siteNewtAutoUpdate(site)

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// siteUpdateRequest builds POST /site/{id}. An unset newt_auto_update
// clears the override, so the site follows the org setting again.
func siteUpdateRequest(m SiteResourceModel) *client.UpdateSiteRequest {
	return &client.UpdateSiteRequest{
		Name:                  m.Name.ValueString(),
		DockerSocketEnabled:   m.DockerSocketEnabled.ValueBool(),
		AutoUpdateEnabled:     m.NewtAutoUpdate.ValueBool(),
		AutoUpdateOverrideOrg: !m.NewtAutoUpdate.IsNull() && !m.NewtAutoUpdate.IsUnknown(),
	}
}

// siteNewtAutoUpdate reads the override back: null when the site follows
// the org (or the server does not report it).
func siteNewtAutoUpdate(s *client.Site) types.Bool {
	if s.AutoUpdateOverrideOrg == nil || !*s.AutoUpdateOverrideOrg {
		return types.BoolNull()
	}
	return tfconv.BoolFromPtr(s.AutoUpdateEnabled)
}

func (r *SiteResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state SiteResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	site, err := r.client.GetSite(ctx, int(state.ID.ValueInt64()))
	if err != nil {
		if errors.Is(err, client.ErrNotFound) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read site", err.Error())
		return
	}

	state.NiceID = types.StringValue(site.NiceID)
	state.Name = types.StringValue(site.Name)
	state.Type = types.StringValue(site.Type)
	state.Online = types.BoolValue(site.Online)
	state.Address = types.StringValue(site.Address)
	state.DockerSocketEnabled = types.BoolValue(site.DockerSocketEnabled)
	state.NewtAutoUpdate = siteNewtAutoUpdate(site)
	state.RotationDisconnect = rotationDisconnectOrDefault(state.RotationDisconnect)

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *SiteResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan SiteResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state SiteResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	site, err := r.client.UpdateSite(ctx, int(plan.ID.ValueInt64()), siteUpdateRequest(plan))
	if err != nil {
		resp.Diagnostics.AddError("Failed to update site", err.Error())
		return
	}

	plan.NiceID = types.StringValue(site.NiceID)
	plan.Name = types.StringValue(site.Name)
	plan.Type = types.StringValue(site.Type)
	plan.Online = types.BoolValue(site.Online)
	plan.Address = types.StringValue(site.Address)
	plan.DockerSocketEnabled = types.BoolValue(site.DockerSocketEnabled)
	plan.NewtAutoUpdate = siteNewtAutoUpdate(site)
	// newt_id is write-once; newt_secret only changes through rotation_trigger.
	if rotationRequested(plan.RotationTrigger, state.RotationTrigger) {
		// The update above is already applied: if the rotation fails, persist it
		// with the old secret and trigger so the next apply retries only the rotation.
		failedRotation := func(summary string, err error) {
			plan.NewtSecret, plan.RotationTrigger = state.NewtSecret, state.RotationTrigger
			resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
			resp.Diagnostics.AddError(summary, err.Error())
		}
		defaults, err := r.client.GetSiteDefaults(ctx)
		if err != nil {
			failedRotation("Failed to generate a new site secret", err)
			return
		}
		if err := r.client.ReKeySite(ctx, int(plan.ID.ValueInt64()), defaults.NewtSecret, plan.RotationDisconnect.ValueBool()); err != nil {
			failedRotation("Failed to rotate site secret", err)
			return
		}
		plan.NewtSecret = types.StringValue(defaults.NewtSecret)
		if !plan.RotationDisconnect.ValueBool() {
			resp.Diagnostics.AddWarning("Secret rotated without disconnecting",
				"The running Newt keeps its session with the old secret, which is refused at its next reconnect: restart it with the new secret now.")
		}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *SiteResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state SiteResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteSite(ctx, int(state.ID.ValueInt64()))
	if err != nil && !errors.Is(err, client.ErrNotFound) {
		resp.Diagnostics.AddError("Failed to delete site", err.Error())
		return
	}
}

func (r *SiteResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	id, err := strconv.ParseInt(req.ID, 10, 64)
	if err != nil {
		resp.Diagnostics.AddError("Invalid ID", fmt.Sprintf("Cannot parse site ID %q as integer", req.ID))
		return
	}

	site, err := r.client.GetSite(ctx, int(id))
	if err != nil {
		resp.Diagnostics.AddError("Failed to import site", err.Error())
		return
	}

	state := SiteResourceModel{
		ID:                  types.Int64Value(int64(site.SiteID)),
		NiceID:              types.StringValue(site.NiceID),
		Name:                types.StringValue(site.Name),
		Type:                types.StringValue(site.Type),
		Online:              types.BoolValue(site.Online),
		Address:             types.StringValue(site.Address),
		NewtID:              types.StringNull(),
		NewtSecret:          types.StringNull(),
		DockerSocketEnabled: types.BoolValue(site.DockerSocketEnabled),
		NewtAutoUpdate:      siteNewtAutoUpdate(site),
		RotationDisconnect:  types.BoolValue(true),
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
