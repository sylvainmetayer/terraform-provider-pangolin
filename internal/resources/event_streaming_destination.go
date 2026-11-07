package resources

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stackopshq/terraform-provider-pangolin/internal/client"
	"github.com/stackopshq/terraform-provider-pangolin/internal/tfconv"
)

var (
	_ resource.Resource                = &EventStreamingDestinationResource{}
	_ resource.ResourceWithImportState = &EventStreamingDestinationResource{}
)

// EventStreamingDestinationResource manages a log streaming destination.
type EventStreamingDestinationResource struct {
	client *client.Client
}

// EventStreamingDestinationResourceModel describes the resource data model.
type EventStreamingDestinationResourceModel struct {
	ID                 types.Int64  `tfsdk:"id"`
	Type               types.String `tfsdk:"type"`
	Config             types.String `tfsdk:"config"`
	Enabled            types.Bool   `tfsdk:"enabled"`
	SendRequestLogs    types.Bool   `tfsdk:"send_request_logs"`
	SendActionLogs     types.Bool   `tfsdk:"send_action_logs"`
	SendAccessLogs     types.Bool   `tfsdk:"send_access_logs"`
	SendConnectionLogs types.Bool   `tfsdk:"send_connection_logs"`
	SendAISessionLogs  types.Bool   `tfsdk:"send_ai_session_logs"`
	LastError          types.String `tfsdk:"last_error"`
}

// NewEventStreamingDestinationResource returns a new resource factory.
func NewEventStreamingDestinationResource() resource.Resource {
	return &EventStreamingDestinationResource{}
}

func (r *EventStreamingDestinationResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_event_streaming_destination"
}

// jsonObjectValidator checks that a string holds a JSON object.
type jsonObjectValidator struct{}

func (jsonObjectValidator) Description(context.Context) string { return "must be a JSON object" }
func (v jsonObjectValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}
func (jsonObjectValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(req.ConfigValue.ValueString()), &obj); err != nil || obj == nil {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid JSON object",
			"The value must be a JSON object, for example built with jsonencode({...}).")
	}
}

// jsonEqual reports whether a and b are the same JSON document, so a server
// that re-serializes the config (key order, spacing) does not cause an
// inconsistent-result error or a perpetual diff.
func jsonEqual(a, b string) bool {
	var x, y any
	if json.Unmarshal([]byte(a), &x) != nil || json.Unmarshal([]byte(b), &y) != nil {
		return false
	}
	return reflect.DeepEqual(x, y)
}

func logFlag(desc string) schema.BoolAttribute {
	return schema.BoolAttribute{
		Description: desc + " Defaults to `false`.",
		Optional:    true,
		Computed:    true,
		Default:     booldefault.StaticBool(false),
	}
}

func (r *EventStreamingDestinationResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages an event streaming destination of the org (Pangolin 1.24+, Enterprise): Pangolin pushes " +
			"the selected logs to it in batches every 30 seconds. Only logs written after the destination is created are sent.",
		Attributes: map[string]schema.Attribute{
			"id": schema.Int64Attribute{
				Description:   "The numeric destination ID.",
				Computed:      true,
				PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
			},
			"type": schema.StringAttribute{
				Description: "`http` (POST to a URL) or `s3` (objects in an S3-compatible bucket).",
				Required:    true,
				Validators:  []validator.String{stringvalidator.OneOf("http", "s3")},
			},
			"config": schema.StringAttribute{
				Description: "The destination settings as a JSON object; build it with `jsonencode`. Stored encrypted, sensitive " +
					"because it holds credentials.\n\n" +
					"- `http`: `name`, `url`, `authType` (`none`, `bearer`, `basic`, `custom`), `bearerToken`, " +
					"`basicCredentials` (`user:password`), `customHeaderName`, `customHeaderValue`, `headers` " +
					"(list of `{key, value}`), `format` (`json_array` default, `ndjson`, `json_single`), `useBodyTemplate`, " +
					"`bodyTemplate` (placeholders `{{event}}`, `{{timestamp}}`, `{{data}}`).\n" +
					"- `s3`: `name`, `accessKeyId`, `secretAccessKey`, `region`, `bucket`, `prefix`, `endpoint` " +
					"(S3-compatible storage), `format`, `gzip`.",
				Required:   true,
				Sensitive:  true,
				Validators: []validator.String{jsonObjectValidator{}},
			},
			"enabled": schema.BoolAttribute{
				Description: "Whether logs are sent. Defaults to `true`.",
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(true),
			},
			"send_request_logs":    logFlag("Send the request logs (every proxied HTTP request)."),
			"send_action_logs":     logFlag("Send the action logs (changes made in the org)."),
			"send_access_logs":     logFlag("Send the access logs (authentication decisions on resources)."),
			"send_connection_logs": logFlag("Send the connection logs (client and site tunnel connections)."),
			"send_ai_session_logs": logFlag("Send the AI session logs."),
			"last_error": schema.StringAttribute{
				Description: "The last send error, null while the destination is healthy.",
				Computed:    true,
			},
		},
	}
}

func (r *EventStreamingDestinationResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func eventStreamingRequest(m EventStreamingDestinationResourceModel) *client.EventStreamingDestinationRequest {
	return &client.EventStreamingDestinationRequest{
		Type:               m.Type.ValueString(),
		Config:             m.Config.ValueString(),
		Enabled:            m.Enabled.ValueBool(),
		SendConnectionLogs: m.SendConnectionLogs.ValueBool(),
		SendRequestLogs:    m.SendRequestLogs.ValueBool(),
		SendActionLogs:     m.SendActionLogs.ValueBool(),
		SendAccessLogs:     m.SendAccessLogs.ValueBool(),
		SendAISessionLogs:  m.SendAISessionLogs.ValueBool(),
	}
}

// eventStreamingToModel maps the server row. An empty config means the
// server could not decrypt it: the prior value is kept.
func eventStreamingToModel(prior types.String, in *client.EventStreamingDestination) EventStreamingDestinationResourceModel {
	cfg := prior
	if in.Config != "" && !jsonEqual(prior.ValueString(), in.Config) {
		cfg = types.StringValue(in.Config)
	}
	return EventStreamingDestinationResourceModel{
		ID:                 types.Int64Value(int64(in.DestinationID)),
		Type:               types.StringValue(in.Type),
		Config:             cfg,
		Enabled:            types.BoolValue(in.Enabled),
		SendRequestLogs:    types.BoolValue(in.SendRequestLogs),
		SendActionLogs:     types.BoolValue(in.SendActionLogs),
		SendAccessLogs:     types.BoolValue(in.SendAccessLogs),
		SendConnectionLogs: types.BoolValue(in.SendConnectionLogs),
		SendAISessionLogs:  types.BoolValue(in.SendAISessionLogs),
		LastError:          tfconv.StringFromPtr(in.LastError),
	}
}

func (r *EventStreamingDestinationResource) readInto(ctx context.Context, id int, prior types.String) (*EventStreamingDestinationResourceModel, error) {
	got, err := r.client.GetEventStreamingDestination(ctx, id)
	if err != nil {
		return nil, err
	}
	m := eventStreamingToModel(prior, got)
	return &m, nil
}

func (r *EventStreamingDestinationResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan EventStreamingDestinationResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id, err := r.client.CreateEventStreamingDestination(ctx, eventStreamingRequest(plan))
	if err != nil {
		resp.Diagnostics.AddError("Failed to create event streaming destination", err.Error())
		return
	}
	state, err := r.readInto(ctx, id, plan.Config)
	if err != nil {
		// Keep the ID in state so the destination is not orphaned.
		plan.ID = types.Int64Value(int64(id))
		plan.LastError = types.StringNull()
		resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
		resp.Diagnostics.AddError("Failed to read event streaming destination after create", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *EventStreamingDestinationResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state EventStreamingDestinationResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	got, err := r.readInto(ctx, int(state.ID.ValueInt64()), state.Config)
	if err != nil {
		if errors.Is(err, client.ErrNotFound) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read event streaming destination", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, got)...)
}

func (r *EventStreamingDestinationResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan EventStreamingDestinationResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id := int(plan.ID.ValueInt64())
	if err := r.client.UpdateEventStreamingDestination(ctx, id, eventStreamingRequest(plan)); err != nil {
		resp.Diagnostics.AddError("Failed to update event streaming destination", err.Error())
		return
	}
	state, err := r.readInto(ctx, id, plan.Config)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read event streaming destination after update", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *EventStreamingDestinationResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state EventStreamingDestinationResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DeleteEventStreamingDestination(ctx, int(state.ID.ValueInt64())); err != nil && !errors.Is(err, client.ErrNotFound) {
		resp.Diagnostics.AddError("Failed to delete event streaming destination", err.Error())
	}
}

func (r *EventStreamingDestinationResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	id, err := strconv.ParseInt(req.ID, 10, 64)
	if err != nil {
		resp.Diagnostics.AddError("Invalid import ID", fmt.Sprintf("Expected a numeric destination ID, got: %q", req.ID))
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
}
