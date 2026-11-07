package resources

import (
	"context"
	"errors"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stackopshq/terraform-provider-pangolin/internal/client"
	"github.com/stackopshq/terraform-provider-pangolin/internal/tfconv"
)

var (
	_ resource.Resource                = &ResourceAccessTokenResource{}
	_ resource.ResourceWithImportState = &ResourceAccessTokenResource{}
)

// ResourceAccessTokenResource manages a Pangolin resource access token.
type ResourceAccessTokenResource struct {
	client *client.Client
}

// ResourceAccessTokenModel describes the resource data model.
type ResourceAccessTokenModel struct {
	ID              types.String `tfsdk:"id"`
	ResourceID      types.Int64  `tfsdk:"resource_id"`
	Title           types.String `tfsdk:"title"`
	Description     types.String `tfsdk:"description"`
	ValidForSeconds types.Int64  `tfsdk:"valid_for_seconds"`
	Path            types.String `tfsdk:"path"`
	PersistSession  types.Bool   `tfsdk:"persist_session"`
	SessionLength   types.Int64  `tfsdk:"session_length"`
	ExpiresAt       types.Int64  `tfsdk:"expires_at"`
	CreatedAt       types.Int64  `tfsdk:"created_at"`
	TokenHash       types.String `tfsdk:"token_hash"`
	Token           types.String `tfsdk:"token"`
}

// NewResourceAccessTokenResource returns a new resource factory.
func NewResourceAccessTokenResource() resource.Resource {
	return &ResourceAccessTokenResource{}
}

func (r *ResourceAccessTokenResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_resource_access_token"
}

func (r *ResourceAccessTokenResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a bearer access token bound to a Pangolin HTTP resource.\n\n" +
			"> **Note:** the `token` (bearer secret) is only returned by the API at creation time. " +
			"After `terraform import`, the `token` attribute is empty by design - the upstream " +
			"only exposes a `tokenHash` afterwards. Rotate by destroying and recreating the resource.\n\n" +
			"Every meaningful attribute is `RequiresReplace`: the Pangolin API does not expose an " +
			"endpoint to update title / description / lifetime in-place.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "The access token ID (returned by the API).",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"resource_id": schema.Int64Attribute{
				Description: "The ID of the HTTP resource this token authorizes access to.",
				Required:    true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.RequiresReplace(),
				},
			},
			"title": schema.StringAttribute{
				Description: "Optional human-readable title.",
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"description": schema.StringAttribute{
				Description: "Optional description.",
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"path": schema.StringAttribute{
				Description: "Path the visitor is redirected to once authenticated with the token (its share link), for example `/albums/1`. Not read back from the API: after an import it is null.",
				Optional:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"persist_session": schema.BoolAttribute{
				Description: "Whether a visit with the token opens a browser session (cookie), so the following requests need no token. With `false` (the default), every request must carry the token, as for an API or health check client.",
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.RequiresReplace(),
				},
			},
			"valid_for_seconds": schema.Int64Attribute{
				Description: "Lifetime in seconds. When omitted, the API defaults to roughly " +
					"30 days (`session_length = 2592000000` ms) and `expires_at` is null " +
					"(token never expires).",
				Optional: true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.RequiresReplace(),
				},
			},
			"session_length": schema.Int64Attribute{
				Description: "Session lifetime in milliseconds, as returned by the API.",
				Computed:    true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"expires_at": schema.Int64Attribute{
				Description: "Expiration timestamp (epoch milliseconds). Null when the token does not expire.",
				Computed:    true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"created_at": schema.Int64Attribute{
				Description: "Creation timestamp (epoch milliseconds).",
				Computed:    true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"token_hash": schema.StringAttribute{
				Description: "SHA-256 hash of the bearer token, exposed by the list endpoints. " +
					"Not populated on the freshly-created resource; surfaced on subsequent reads.",
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"token": schema.StringAttribute{
				Description: "The bearer access token. Only available at creation time; " +
					"empty after import. Treat as a credential.",
				Computed:  true,
				Sensitive: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *ResourceAccessTokenResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *ResourceAccessTokenResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan ResourceAccessTokenModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := &client.CreateResourceAccessTokenRequest{}
	if !plan.Title.IsNull() && !plan.Title.IsUnknown() {
		v := plan.Title.ValueString()
		body.Title = &v
	}
	if !plan.Description.IsNull() && !plan.Description.IsUnknown() {
		v := plan.Description.ValueString()
		body.Description = &v
	}
	if !plan.ValidForSeconds.IsNull() && !plan.ValidForSeconds.IsUnknown() {
		v := plan.ValidForSeconds.ValueInt64()
		body.ValidForSeconds = &v
	}
	body.Path = tfconv.StringToPtr(plan.Path)
	if plan.PersistSession.ValueBool() {
		// Only true is sent: false is the server default.
		body.PersistSession = tfconv.BoolToPtr(plan.PersistSession)
	}

	tok, err := r.client.CreateResourceAccessToken(ctx, int(plan.ResourceID.ValueInt64()), body)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create resource access token", err.Error())
		return
	}

	plan.ID = types.StringValue(tok.AccessTokenID)
	plan.SessionLength = types.Int64Value(tok.SessionLength)
	plan.CreatedAt = types.Int64Value(tok.CreatedAt)
	plan.Token = types.StringValue(tok.AccessToken)
	// tokenHash is not surfaced by the CREATE response.
	plan.TokenHash = types.StringValue("")
	plan.Title = tfconv.StringFromPtr(tok.Title)
	plan.Description = tfconv.StringFromPtr(tok.Description)
	plan.ExpiresAt = tfconv.Int64FromInt64Ptr(tok.ExpiresAt)

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *ResourceAccessTokenResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state ResourceAccessTokenModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tok, err := r.client.GetResourceAccessToken(ctx, state.ID.ValueString())
	if err != nil {
		if errors.Is(err, client.ErrNotFound) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read resource access token", err.Error())
		return
	}

	state.ResourceID = types.Int64Value(int64(tok.ResourceID))
	state.SessionLength = types.Int64Value(tok.SessionLength)
	state.CreatedAt = types.Int64Value(tok.CreatedAt)
	state.TokenHash = types.StringValue(tok.TokenHash)
	state.Title = tfconv.StringFromPtr(tok.Title)
	state.Description = tfconv.StringFromPtr(tok.Description)
	state.ExpiresAt = tfconv.Int64FromInt64Ptr(tok.ExpiresAt)
	// Keep the prior value when the API omits the field: forcing false would
	// make a persist_session = true token replace (and lose its secret) on every plan.
	if tok.PersistSession != nil {
		state.PersistSession = types.BoolValue(*tok.PersistSession)
	} else if state.PersistSession.IsNull() || state.PersistSession.IsUnknown() {
		state.PersistSession = types.BoolValue(false)
	}
	// token (bearer secret) is preserved from existing state - the list endpoint never exposes it.

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *ResourceAccessTokenResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError("Update not supported", "Resource access tokens cannot be updated in-place. Every attribute is RequiresReplace.")
}

func (r *ResourceAccessTokenResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state ResourceAccessTokenModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.DeleteResourceAccessToken(ctx, state.ID.ValueString()); err != nil {
		if errors.Is(err, client.ErrNotFound) {
			return
		}
		resp.Diagnostics.AddError("Failed to delete resource access token", err.Error())
		return
	}
}

func (r *ResourceAccessTokenResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	tok, err := r.client.GetResourceAccessToken(ctx, req.ID)
	if err != nil {
		resp.Diagnostics.AddError("Failed to import resource access token", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &ResourceAccessTokenModel{
		ID:              types.StringValue(tok.AccessTokenID),
		ResourceID:      types.Int64Value(int64(tok.ResourceID)),
		Title:           tfconv.StringFromPtr(tok.Title),
		Description:     tfconv.StringFromPtr(tok.Description),
		ValidForSeconds: types.Int64Null(),
		Path:            types.StringNull(),
		PersistSession:  types.BoolValue(tok.PersistSession != nil && *tok.PersistSession),
		SessionLength:   types.Int64Value(tok.SessionLength),
		ExpiresAt:       tfconv.Int64FromInt64Ptr(tok.ExpiresAt),
		CreatedAt:       types.Int64Value(tok.CreatedAt),
		TokenHash:       types.StringValue(tok.TokenHash),
		Token:           types.StringValue(""), // not recoverable after creation
	})...)
}
