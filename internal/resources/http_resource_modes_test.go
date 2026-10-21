package resources

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/stackopshq/terraform-provider-pangolin/internal/client"
)

func httpResourceSchema(t *testing.T) schema.Schema {
	t.Helper()
	resp := &resource.SchemaResponse{}
	NewHTTPResource().Schema(context.Background(), resource.SchemaRequest{}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("schema: %v", resp.Diagnostics)
	}
	return resp.Schema
}

// httpResourceConfig builds a framework config of pangolin_resource
// where every attribute not in vals is null.
func httpResourceConfig(t *testing.T, vals map[string]tftypes.Value) tfsdk.Config {
	t.Helper()
	s := httpResourceSchema(t)
	typ := s.Type().TerraformType(context.Background()).(tftypes.Object)
	attrs := make(map[string]tftypes.Value, len(typ.AttributeTypes))
	for name, at := range typ.AttributeTypes {
		if v, ok := vals[name]; ok {
			attrs[name] = v
		} else {
			attrs[name] = tftypes.NewValue(at, nil)
		}
	}
	return tfsdk.Config{Schema: s, Raw: tftypes.NewValue(typ, attrs)}
}

func tfString(v string) tftypes.Value { return tftypes.NewValue(tftypes.String, v) }

func tfNumber(v int64) tftypes.Value { return tftypes.NewValue(tftypes.Number, v) }

func tfNumberSet(vs ...int64) tftypes.Value {
	elems := make([]tftypes.Value, len(vs))
	for i, v := range vs {
		elems[i] = tfNumber(v)
	}
	return tftypes.NewValue(tftypes.Set{ElementType: tftypes.Number}, elems)
}

func TestHTTPResourceSchema_AcceptsEveryMode(t *testing.T) {
	ctx := context.Background()
	s := httpResourceSchema(t)
	if err := s.ValidateImplementation(ctx); err != nil {
		t.Fatalf("ValidateImplementation: %v", err)
	}
	modeAttr := s.Attributes["mode"].(schema.StringAttribute)
	for _, m := range []string{"http", "ssh", "rdp", "vnc", "inference", "tcp", "udp"} {
		if errs := validateStringValue(ctx, modeAttr, "mode", m); errs != "" {
			t.Errorf("mode %q rejected: %s", m, errs)
		}
	}
	if validateStringValue(ctx, modeAttr, "mode", "ftp") == "" {
		t.Errorf("mode \"ftp\" should be rejected")
	}
	daemonAttr := s.Attributes["auth_daemon_mode"].(schema.StringAttribute)
	for _, m := range []string{"site", "remote", "native"} {
		if errs := validateStringValue(ctx, daemonAttr, "auth_daemon_mode", m); errs != "" {
			t.Errorf("auth_daemon_mode %q rejected: %s", m, errs)
		}
	}
	if validateStringValue(ctx, daemonAttr, "auth_daemon_mode", "sidecar") == "" {
		t.Errorf("auth_daemon_mode \"sidecar\" should be rejected")
	}
}

// validateStringValue runs the attribute's validators on v and returns
// the error summaries, empty when v is accepted.
func validateStringValue(ctx context.Context, a schema.StringAttribute, name, v string) string {
	out := ""
	for _, val := range a.Validators {
		resp := &validator.StringResponse{}
		val.ValidateString(ctx, validator.StringRequest{Path: path.Root(name), ConfigValue: types.StringValue(v)}, resp)
		for _, d := range resp.Diagnostics.Errors() {
			out += d.Summary() + ";"
		}
	}
	return out
}

func TestResourceModeConfigValidator(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name    string
		vals    map[string]tftypes.Value
		wantErr string
	}{
		{"ssh with domain", map[string]tftypes.Value{"mode": tfString("ssh"), "domain_id": tfString("d")}, ""},
		{"rdp without domain", map[string]tftypes.Value{"mode": tfString("rdp")}, "Missing domain_id"},
		{"vnc with proxy_port", map[string]tftypes.Value{"mode": tfString("vnc"), "domain_id": tfString("d"), "proxy_port": tfNumber(5900)}, "Unsupported proxy_port"},
		{"inference with providers", map[string]tftypes.Value{"mode": tfString("inference"), "domain_id": tfString("d"), "ai_provider_ids": tfNumberSet(1, 2)}, ""},
		{"http with providers", map[string]tftypes.Value{"mode": tfString("http"), "domain_id": tfString("d"), "ai_provider_ids": tfNumberSet(1)}, "Unsupported ai_provider_ids"},
		{"default mode with providers", map[string]tftypes.Value{"domain_id": tfString("d"), "ai_provider_ids": tfNumberSet(1)}, "Unsupported ai_provider_ids"},
		{"http with empty providers", map[string]tftypes.Value{"mode": tfString("http"), "ai_provider_ids": tfNumberSet()}, "Unsupported ai_provider_ids"},
		{"inference with empty providers", map[string]tftypes.Value{"mode": tfString("inference"), "domain_id": tfString("d"), "ai_provider_ids": tfNumberSet()}, ""},
		{"tcp unchanged", map[string]tftypes.Value{"mode": tfString("tcp"), "proxy_port": tfNumber(2222)}, ""},
		{"http without domain unchanged", map[string]tftypes.Value{"mode": tfString("http")}, ""},
		{"unknown mode skipped", map[string]tftypes.Value{"mode": tftypes.NewValue(tftypes.String, tftypes.UnknownValue)}, ""},
		{"unknown domain is fine", map[string]tftypes.Value{"mode": tfString("ssh"), "domain_id": tftypes.NewValue(tftypes.String, tftypes.UnknownValue)}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := &resource.ValidateConfigResponse{}
			resourceModeConfigValidator{}.ValidateResource(ctx, resource.ValidateConfigRequest{Config: httpResourceConfig(t, tc.vals)}, resp)
			got := ""
			for _, d := range resp.Diagnostics.Errors() {
				got += d.Summary() + ";"
			}
			if tc.wantErr == "" && got != "" {
				t.Errorf("unexpected errors: %s", got)
			}
			if tc.wantErr != "" && !strings.Contains(got, tc.wantErr) {
				t.Errorf("errors = %q, want %q", got, tc.wantErr)
			}
		})
	}
}

func TestBuildHTTPResourceCreateRequest_PerMode(t *testing.T) {
	ctx := context.Background()
	base := func(mode string) HTTPResourceModel {
		return HTTPResourceModel{
			Name:          types.StringValue("r"),
			Mode:          types.StringValue(mode),
			DomainID:      types.StringValue("dom"),
			Protocol:      types.StringValue("tcp"),
			Subdomain:     types.StringValue("app"),
			ProxyPort:     types.Int64Null(),
			AIProviderIDs: types.SetNull(types.Int64Type),
		}
	}
	for _, tc := range []struct {
		mode     string
		wantHTTP bool
		wantMode string
	}{
		{"http", true, ""},
		{"ssh", true, "ssh"},
		{"rdp", true, "rdp"},
		{"vnc", true, "vnc"},
		{"inference", true, "inference"},
		{"tcp", false, "tcp"},
		{"udp", false, "udp"},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			req, diags := buildHTTPResourceCreateRequest(ctx, base(tc.mode))
			if diags.HasError() {
				t.Fatal(diags)
			}
			if req.HTTP != tc.wantHTTP || req.Mode != tc.wantMode {
				t.Errorf("http=%v mode=%q, want http=%v mode=%q", req.HTTP, req.Mode, tc.wantHTTP, tc.wantMode)
			}
			if req.DomainID != "dom" || req.Subdomain == nil || *req.Subdomain != "app" {
				t.Errorf("domain fields = %q / %v", req.DomainID, req.Subdomain)
			}
			if req.AIProviders != nil {
				t.Errorf("AIProviders = %v, want nil", req.AIProviders)
			}
		})
	}

	t.Run("inference sends sorted providers", func(t *testing.T) {
		plan := base("inference")
		plan.AIProviderIDs = types.SetValueMust(types.Int64Type, []attr.Value{types.Int64Value(7), types.Int64Value(3)})
		req, diags := buildHTTPResourceCreateRequest(ctx, plan)
		if diags.HasError() {
			t.Fatal(diags)
		}
		if len(req.AIProviders) != 2 || req.AIProviders[0].ProviderID != 3 || req.AIProviders[1].ProviderID != 7 {
			t.Errorf("AIProviders = %+v", req.AIProviders)
		}
	})

	t.Run("tcp keeps proxy port", func(t *testing.T) {
		plan := base("tcp")
		plan.ProxyPort = types.Int64Value(2222)
		req, _ := buildHTTPResourceCreateRequest(ctx, plan)
		if req.ProxyPort == nil || *req.ProxyPort != 2222 {
			t.Errorf("ProxyPort = %v", req.ProxyPort)
		}
	})
}

func TestResourceModeUsesDomain(t *testing.T) {
	for mode, want := range map[string]bool{
		"": true, "http": true, "ssh": true, "rdp": true, "vnc": true, "inference": true,
		"tcp": false, "udp": false,
	} {
		if got := resourceModeUsesDomain(mode); got != want {
			t.Errorf("resourceModeUsesDomain(%q) = %v, want %v", mode, got, want)
		}
	}
}

func TestDiffAIProviderIDs(t *testing.T) {
	current := []client.ResourceAIProvider{{ProviderID: 1}, {ProviderID: 2}, {ProviderID: 5}}
	add, remove := diffAIProviderIDs([]int{2, 4, 3}, current)
	if len(add) != 2 || add[0] != 3 || add[1] != 4 {
		t.Errorf("add = %v", add)
	}
	if len(remove) != 2 || remove[0] != 1 || remove[1] != 5 {
		t.Errorf("remove = %v", remove)
	}
	add, remove = diffAIProviderIDs(nil, nil)
	if add != nil || remove != nil {
		t.Errorf("empty diff = %v / %v", add, remove)
	}
}

// aiProviderStub serves GET/POST /v1/resource/{id}/ai-providers[/add|/remove]
// against an in-memory list and records every call. listStatus, when
// set, is returned on the list call instead of the list.
type aiProviderStub struct {
	mu         sync.Mutex
	attached   map[int]bool
	calls      []string
	listStatus int
}

func (s *aiProviderStub) handler(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		write := func(data any) {
			raw, _ := json.Marshal(data)
			_ = json.NewEncoder(w).Encode(client.APIResponse{Data: raw, Success: true, Status: 200})
		}
		switch {
		case r.Method == "GET" && r.URL.Path == "/v1/resource/9/ai-providers":
			s.calls = append(s.calls, "list")
			if s.listStatus != 0 {
				w.WriteHeader(s.listStatus)
				_ = json.NewEncoder(w).Encode(client.APIResponse{Error: true, Message: "denied", Status: s.listStatus})
				return
			}
			ids := make([]int, 0, len(s.attached))
			for id := range s.attached {
				ids = append(ids, id)
			}
			sort.Ints(ids)
			providers := make([]map[string]any, len(ids))
			for i, id := range ids {
				providers[i] = map[string]any{"providerId": id, "accessMode": "inherit", "enabled": true}
			}
			write(map[string]any{"providers": providers})
		case r.Method == "POST" && strings.HasPrefix(r.URL.Path, "/v1/resource/9/ai-providers/"):
			raw, _ := io.ReadAll(r.Body)
			var body struct {
				ProviderID int `json:"providerId"`
			}
			_ = json.Unmarshal(raw, &body)
			op := strings.TrimPrefix(r.URL.Path, "/v1/resource/9/ai-providers/")
			s.calls = append(s.calls, op+":"+strconv.Itoa(body.ProviderID))
			if op == "add" {
				s.attached[body.ProviderID] = true
			} else {
				delete(s.attached, body.ProviderID)
			}
			write(map[string]any{})
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}
}

func newAIProviderStubResource(t *testing.T, attached ...int) (*HTTPResource, *aiProviderStub) {
	t.Helper()
	stub := &aiProviderStub{attached: map[int]bool{}}
	for _, id := range attached {
		stub.attached[id] = true
	}
	srv := httptest.NewServer(stub.handler(t))
	t.Cleanup(srv.Close)
	c := &client.Client{BaseURL: srv.URL, APIKey: "k", OrgID: "o", HTTPClient: srv.Client()}
	return &HTTPResource{client: c}, stub
}

func int64Set(vs ...int64) types.Set {
	elems := make([]attr.Value, len(vs))
	for i, v := range vs {
		elems[i] = types.Int64Value(v)
	}
	return types.SetValueMust(types.Int64Type, elems)
}

func inferenceModel(providers types.Set) HTTPResourceModel {
	return HTTPResourceModel{ID: types.Int64Value(9), Mode: types.StringValue("inference"), AIProviderIDs: providers}
}

func TestHTTPResource_ApplyAIProviders_Configured(t *testing.T) {
	r, stub := newAIProviderStubResource(t, 1, 2)
	m := inferenceModel(int64Set(2, 3))
	if diags := r.applyAIProviders(context.Background(), &m, int64Set(1, 2), true); diags.HasError() {
		t.Fatal(diags)
	}
	// One list, then the writes; no second list after a successful sync.
	if got := strings.Join(stub.calls, ","); got != "list,add:3,remove:1" {
		t.Errorf("calls = %v", got)
	}
	if !m.AIProviderIDs.Equal(int64Set(2, 3)) {
		t.Errorf("ai_provider_ids = %v", m.AIProviderIDs)
	}
}

func TestHTTPResource_ApplyAIProviders_AfterCreateAttachesMissing(t *testing.T) {
	// The server attached only provider 1 of the create body [1, 2].
	r, stub := newAIProviderStubResource(t, 1)
	m := inferenceModel(int64Set(1, 2))
	if diags := r.applyAIProviders(context.Background(), &m, int64Set(1, 2), true); diags.HasError() {
		t.Fatal(diags)
	}
	if got := strings.Join(stub.calls, ","); got != "list,add:2" {
		t.Errorf("calls = %v", got)
	}
}

func TestHTTPResource_ApplyAIProviders_UnsetNeverTouchesServer(t *testing.T) {
	// Provider 2 was attached out of band; the attribute is not in the
	// configuration, so the planned (prior) value is kept and nothing is
	// called, even the list.
	r, stub := newAIProviderStubResource(t, 1, 2)
	m := inferenceModel(int64Set(1))
	if diags := r.applyAIProviders(context.Background(), &m, int64Set(1), false); diags.HasError() {
		t.Fatal(diags)
	}
	if len(stub.calls) != 0 {
		t.Errorf("calls = %v, want none", stub.calls)
	}
	if !m.AIProviderIDs.Equal(int64Set(1)) {
		t.Errorf("ai_provider_ids = %v, want the planned [1]", m.AIProviderIDs)
	}
}

func TestHTTPResource_ApplyAIProviders_UnsetOnCreateReadsServer(t *testing.T) {
	r, stub := newAIProviderStubResource(t, 4)
	m := inferenceModel(types.SetUnknown(types.Int64Type))
	if diags := r.applyAIProviders(context.Background(), &m, types.SetNull(types.Int64Type), false); diags.HasError() {
		t.Fatal(diags)
	}
	if got := strings.Join(stub.calls, ","); got != "list" {
		t.Errorf("calls = %v", got)
	}
	if !m.AIProviderIDs.Equal(int64Set(4)) {
		t.Errorf("ai_provider_ids = %v", m.AIProviderIDs)
	}
}

func TestHTTPResource_ApplyAIProviders_ForbiddenListUsesPrior(t *testing.T) {
	r, stub := newAIProviderStubResource(t, 1, 5)
	stub.listStatus = http.StatusForbidden
	m := inferenceModel(int64Set(1, 3))
	diags := r.applyAIProviders(context.Background(), &m, int64Set(1, 2), true)
	if diags.HasError() {
		t.Fatal(diags)
	}
	if diags.WarningsCount() != 1 {
		t.Errorf("warnings = %d, want 1", diags.WarningsCount())
	}
	// Diffed against the prior [1, 2], not the (unreadable) server list.
	if got := strings.Join(stub.calls, ","); got != "list,add:3,remove:2" {
		t.Errorf("calls = %v", got)
	}
}

func TestHTTPResource_ApplyAIProviders_NonInferenceIsNull(t *testing.T) {
	// No server: a non-inference resource must not call the API.
	r := &HTTPResource{client: &client.Client{BaseURL: "http://127.0.0.1:1", HTTPClient: http.DefaultClient}}
	m := HTTPResourceModel{ID: types.Int64Value(9), Mode: types.StringValue("ssh"), AIProviderIDs: types.SetUnknown(types.Int64Type)}
	if diags := r.applyAIProviders(context.Background(), &m, types.SetNull(types.Int64Type), false); diags.HasError() {
		t.Fatal(diags)
	}
	if !m.AIProviderIDs.IsNull() || !m.AIProviderIDs.ElementType(context.Background()).Equal(types.Int64Type) {
		t.Errorf("ai_provider_ids = %v, want typed null", m.AIProviderIDs)
	}
}

func TestHTTPResource_RefreshAIProviders(t *testing.T) {
	t.Run("forbidden keeps the value with a warning", func(t *testing.T) {
		r, stub := newAIProviderStubResource(t, 1)
		stub.listStatus = http.StatusForbidden
		m := inferenceModel(int64Set(1))
		diags := r.refreshAIProviders(context.Background(), &m)
		if diags.HasError() || diags.WarningsCount() != 1 {
			t.Fatalf("diags = %v", diags)
		}
		if !m.AIProviderIDs.Equal(int64Set(1)) {
			t.Errorf("ai_provider_ids = %v", m.AIProviderIDs)
		}
	})
	t.Run("other errors fail and never leave unknown", func(t *testing.T) {
		r, stub := newAIProviderStubResource(t)
		stub.listStatus = http.StatusBadRequest
		m := inferenceModel(types.SetUnknown(types.Int64Type))
		if diags := r.refreshAIProviders(context.Background(), &m); !diags.HasError() {
			t.Fatal("expected an error diagnostic")
		}
		if !m.AIProviderIDs.IsNull() {
			t.Errorf("ai_provider_ids = %v, want null after a failed read", m.AIProviderIDs)
		}
	})
	t.Run("reads the server list", func(t *testing.T) {
		r, _ := newAIProviderStubResource(t, 2, 1)
		m := inferenceModel(int64Set(1))
		if diags := r.refreshAIProviders(context.Background(), &m); diags.HasError() {
			t.Fatal(diags)
		}
		if !m.AIProviderIDs.Equal(int64Set(1, 2)) {
			t.Errorf("ai_provider_ids = %v", m.AIProviderIDs)
		}
	})
}

func TestApplyHTTPResourceResponse_NewModes(t *testing.T) {
	for _, mode := range []string{"ssh", "rdp", "vnc", "inference"} {
		res := &client.Resource{ResourceID: 9, Name: "r", Mode: mode, DomainID: "dom", FullDomain: "app.example.com"}
		m := applyHTTPResourceResponse(HTTPResourceModel{ID: types.Int64Value(9)}, res)
		if m.Mode.ValueString() != mode || m.FullDomain.ValueString() != "app.example.com" {
			t.Errorf("mode %q: got mode=%q full_domain=%q", mode, m.Mode.ValueString(), m.FullDomain.ValueString())
		}
	}
}

func TestHTTPResource_CreateKeepsIDWhenFollowUpFails(t *testing.T) {
	ctx := context.Background()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "PUT" && r.URL.Path == "/v1/org/o/resource":
			raw, _ := json.Marshal(map[string]any{"resourceId": 3, "niceId": "n", "name": "r", "mode": "rdp"})
			_ = json.NewEncoder(w).Encode(client.APIResponse{Data: raw, Success: true, Status: 201})
		case r.Method == "POST" && r.URL.Path == "/v1/resource/3":
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(client.APIResponse{Error: true, Message: "bad field", Status: 400})
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	r := &HTTPResource{client: &client.Client{BaseURL: srv.URL, APIKey: "k", OrgID: "o", HTTPClient: srv.Client()}}

	cfg := httpResourceConfig(t, map[string]tftypes.Value{
		"name": tfString("r"), "mode": tfString("rdp"), "domain_id": tfString("d"),
		"subdomain": tfString("app"), "protocol": tfString("tcp"),
	})
	s := httpResourceSchema(t)
	resp := &resource.CreateResponse{State: tfsdk.State{Schema: s, Raw: tftypes.NewValue(s.Type().TerraformType(ctx), nil)}}
	r.Create(ctx, resource.CreateRequest{Config: cfg, Plan: tfsdk.Plan{Schema: s, Raw: cfg.Raw}}, resp)

	if !resp.Diagnostics.HasError() {
		t.Fatal("expected the follow-up update error")
	}
	var id types.Int64
	resp.Diagnostics.Append(resp.State.GetAttribute(ctx, path.Root("id"), &id)...)
	if id.ValueInt64() != 3 {
		t.Errorf("id in state = %v, want 3 so Terraform taints instead of orphaning", id)
	}
}
