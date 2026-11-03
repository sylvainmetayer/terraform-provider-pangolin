package resources

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stackopshq/terraform-provider-pangolin/internal/client"
)

var (
	_ resource.Resource                = &LabelAttachmentResource{}
	_ resource.ResourceWithImportState = &LabelAttachmentResource{}
)

// LabelAttachmentResource attaches a label to one site, resource, private
// resource or machine client.
type LabelAttachmentResource struct {
	client *client.Client
}

// LabelAttachmentModel describes the resource data model.
type LabelAttachmentModel struct {
	ID             types.String `tfsdk:"id"`
	LabelID        types.Int64  `tfsdk:"label_id"`
	SiteID         types.Int64  `tfsdk:"site_id"`
	ResourceID     types.Int64  `tfsdk:"resource_id"`
	SiteResourceID types.Int64  `tfsdk:"site_resource_id"`
	ClientID       types.Int64  `tfsdk:"client_id"`
}

// NewLabelAttachmentResource returns a new resource factory.
func NewLabelAttachmentResource() resource.Resource {
	return &LabelAttachmentResource{}
}

func (r *LabelAttachmentResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_label_attachment"
}

func labelTargetAttribute(description string, validators ...validator.Int64) schema.Int64Attribute {
	return schema.Int64Attribute{
		Description:   description + " Exactly one of `site_id`, `resource_id`, `site_resource_id` and `client_id` must be set. Changing it forces a new attachment.",
		Optional:      true,
		PlanModifiers: []planmodifier.Int64{int64planmodifier.RequiresReplace()},
		Validators:    validators,
	}
}

func (r *LabelAttachmentResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Attaches a label to a site, a public resource, a private resource or a machine client.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description:   "Composite ID: `{label_id}/{kind}/{target_id}`, where kind is `site`, `resource`, `site_resource` or `client`.",
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"label_id": schema.Int64Attribute{
				Description:   "The label ID. Changing it forces a new attachment.",
				Required:      true,
				PlanModifiers: []planmodifier.Int64{int64planmodifier.RequiresReplace()},
			},
			"site_id": labelTargetAttribute("The site.", int64validator.ExactlyOneOf(
				path.MatchRoot("resource_id"), path.MatchRoot("site_resource_id"), path.MatchRoot("client_id"),
			)),
			"resource_id":      labelTargetAttribute("The public resource."),
			"site_resource_id": labelTargetAttribute("The private resource."),
			"client_id":        labelTargetAttribute("The machine client. User devices are not supported: the API does not list their labels."),
		},
	}
}

func (r *LabelAttachmentResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// labelAttachmentTarget returns the API target of a model and the kind and ID
// its composite ID uses.
func labelAttachmentTarget(m LabelAttachmentModel) (client.LabelTarget, string, int64) {
	set := func(v types.Int64) *int {
		if v.IsNull() || v.IsUnknown() {
			return nil
		}
		id := int(v.ValueInt64())
		return &id
	}
	switch {
	case set(m.SiteID) != nil:
		return client.LabelTarget{SiteID: set(m.SiteID)}, "site", m.SiteID.ValueInt64()
	case set(m.ResourceID) != nil:
		return client.LabelTarget{ResourceID: set(m.ResourceID)}, "resource", m.ResourceID.ValueInt64()
	case set(m.SiteResourceID) != nil:
		return client.LabelTarget{SiteResourceID: set(m.SiteResourceID)}, "site_resource", m.SiteResourceID.ValueInt64()
	default:
		return client.LabelTarget{ClientID: set(m.ClientID)}, "client", m.ClientID.ValueInt64()
	}
}

func parseLabelAttachmentID(id string) (LabelAttachmentModel, error) {
	invalid := fmt.Errorf("expected {label_id}/{site|resource|site_resource|client}/{target_id}, got: %q", id)
	parts := strings.Split(id, "/")
	if len(parts) != 3 {
		return LabelAttachmentModel{}, invalid
	}
	labelID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return LabelAttachmentModel{}, invalid
	}
	targetID, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return LabelAttachmentModel{}, invalid
	}
	m := LabelAttachmentModel{
		ID:             types.StringValue(id),
		LabelID:        types.Int64Value(labelID),
		SiteID:         types.Int64Null(),
		ResourceID:     types.Int64Null(),
		SiteResourceID: types.Int64Null(),
		ClientID:       types.Int64Null(),
	}
	switch parts[1] {
	case "site":
		m.SiteID = types.Int64Value(targetID)
	case "resource":
		m.ResourceID = types.Int64Value(targetID)
	case "site_resource":
		m.SiteResourceID = types.Int64Value(targetID)
	case "client":
		m.ClientID = types.Int64Value(targetID)
	default:
		return LabelAttachmentModel{}, invalid
	}
	return m, nil
}

func (r *LabelAttachmentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan LabelAttachmentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	target, kind, targetID := labelAttachmentTarget(plan)
	if err := r.client.AttachLabel(ctx, int(plan.LabelID.ValueInt64()), target); err != nil {
		resp.Diagnostics.AddError("Failed to attach label", err.Error())
		return
	}
	plan.ID = types.StringValue(fmt.Sprintf("%d/%s/%d", plan.LabelID.ValueInt64(), kind, targetID))
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *LabelAttachmentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state LabelAttachmentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	target, _, _ := labelAttachmentTarget(state)
	attached, err := r.client.LabelAttached(ctx, int(state.LabelID.ValueInt64()), target)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read label attachment", err.Error())
		return
	}
	if !attached {
		resp.State.RemoveResource(ctx)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *LabelAttachmentResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError("Update not supported", "Every attribute forces a new attachment.")
}

func (r *LabelAttachmentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state LabelAttachmentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	target, _, _ := labelAttachmentTarget(state)
	if err := r.client.DetachLabel(ctx, int(state.LabelID.ValueInt64()), target); err != nil && !errors.Is(err, client.ErrNotFound) {
		resp.Diagnostics.AddError("Failed to detach label", err.Error())
	}
}

func (r *LabelAttachmentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	m, err := parseLabelAttachmentID(req.ID)
	if err != nil {
		resp.Diagnostics.AddError("Invalid import ID", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
