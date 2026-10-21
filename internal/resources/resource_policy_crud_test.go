package resources

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/stackopshq/terraform-provider-pangolin/internal/client"
)

// recordedCall is one request seen by the fake Pangolin server.
type recordedCall struct {
	Method, Path string
	Body         map[string]any
}

// fakePangolin answers every request with the payload returned by
// respond and records the calls.
type fakePangolin struct {
	mu    sync.Mutex
	calls []recordedCall
}

func (f *fakePangolin) client(t *testing.T, respond func(method, path string) any) *client.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &body)
		}
		path := strings.TrimPrefix(r.URL.Path, "/v1")
		f.mu.Lock()
		f.calls = append(f.calls, recordedCall{r.Method, path, body})
		f.mu.Unlock()
		data, _ := json.Marshal(respond(r.Method, path))
		_ = json.NewEncoder(w).Encode(client.APIResponse{Data: data, Success: true, Status: 200})
	}))
	t.Cleanup(srv.Close)
	return client.NewClient(srv.URL, "test-key", "test-org")
}

func (f *fakePangolin) paths() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.calls))
	for i, c := range f.calls {
		out[i] = c.Method + " " + c.Path
	}
	return out
}

func resourceSchema(t *testing.T, r resource.Resource) (resource.SchemaResponse, tftypes.Type) {
	t.Helper()
	var resp resource.SchemaResponse
	r.Schema(context.Background(), resource.SchemaRequest{}, &resp)
	return resp, resp.Schema.Type().TerraformType(context.Background())
}

func samePaths(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func policyPayload() map[string]any {
	return map[string]any{
		"resourcePolicyId": 7, "niceId": "brave-otter", "name": "corp", "sso": true,
		"applyRules": false, "emailWhitelistEnabled": false, "idpId": nil,
		"passwordId": 1, "pincodeId": nil, "headerAuth": nil,
		"roles": []any{}, "users": []any{}, "emailWhiteList": []any{}, "rules": []any{},
	}
}

func basePolicyModel() ResourcePolicyModel {
	return ResourcePolicyModel{
		ID:                    types.Int64Value(7),
		NiceID:                types.StringValue("brave-otter"),
		Name:                  types.StringValue("corp"),
		SSO:                   types.BoolValue(true),
		SkipToIdpID:           types.Int64Null(),
		RoleIDs:               types.SetValueMust(types.Int64Type, []attr.Value{}),
		UserIDs:               types.SetValueMust(types.StringType, []attr.Value{}),
		EmailWhitelistEnabled: types.BoolValue(false),
		WhitelistEmails:       types.SetValueMust(types.StringType, []attr.Value{}),
		ApplyRules:            types.BoolValue(false),
		Rules:                 types.ListValueMust(types.ObjectType{AttrTypes: resourcePolicyRuleAttrTypes}, []attr.Value{}),
	}
}

func TestResourcePolicyCreate_CallsCreateThenGet(t *testing.T) {
	ctx := context.Background()
	fake := &fakePangolin{}
	r := &ResourcePolicyResource{client: fake.client(t, func(method, path string) any {
		if method == "POST" && path == "/org/test-org/resource-policy" {
			return map[string]any{"resourcePolicyId": 7, "niceId": "brave-otter", "name": "corp"}
		}
		return policyPayload()
	})}
	schemaResp, typ := resourceSchema(t, r)

	plan := basePolicyModel()
	plan.ID = types.Int64Unknown()
	plan.NiceID = types.StringUnknown()
	req := resource.CreateRequest{Plan: tfsdk.Plan{Schema: schemaResp.Schema, Raw: tftypes.NewValue(typ, nil)}}
	if diags := req.Plan.Set(ctx, &plan); diags.HasError() {
		t.Fatalf("plan: %v", diags)
	}
	resp := resource.CreateResponse{State: tfsdk.State{Schema: schemaResp.Schema, Raw: tftypes.NewValue(typ, nil)}}
	r.Create(ctx, req, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("create: %v", resp.Diagnostics)
	}

	want := []string{"POST /org/test-org/resource-policy", "GET /resource-policy/7"}
	if got := fake.paths(); !samePaths(got, want) {
		t.Errorf("calls = %v, want %v", got, want)
	}
	var state ResourcePolicyModel
	resp.State.Get(ctx, &state)
	if state.ID.ValueInt64() != 7 || state.NiceID.ValueString() != "brave-otter" {
		t.Errorf("state = %+v", state)
	}
}

func TestResourcePolicyCreate_RejectsEmailsOnDisabledWhitelist(t *testing.T) {
	// email_whitelist_enabled was unknown at validate time and resolved
	// to false: Create must refuse before any write.
	ctx := context.Background()
	fake := &fakePangolin{}
	r := &ResourcePolicyResource{client: fake.client(t, func(string, string) any { return policyPayload() })}
	schemaResp, typ := resourceSchema(t, r)

	plan := basePolicyModel()
	plan.ID = types.Int64Unknown()
	plan.WhitelistEmails = types.SetValueMust(types.StringType, []attr.Value{types.StringValue("a@b.com")})
	req := resource.CreateRequest{Plan: tfsdk.Plan{Schema: schemaResp.Schema, Raw: tftypes.NewValue(typ, nil)}}
	if diags := req.Plan.Set(ctx, &plan); diags.HasError() {
		t.Fatalf("plan: %v", diags)
	}
	resp := resource.CreateResponse{State: tfsdk.State{Schema: schemaResp.Schema, Raw: tftypes.NewValue(typ, nil)}}
	r.Create(ctx, req, &resp)
	if !resp.Diagnostics.HasError() {
		t.Fatal("expected an error for emails on a disabled whitelist")
	}
	if len(fake.calls) != 0 {
		t.Errorf("calls = %v, want none", fake.paths())
	}
}

func TestResourcePolicyUpdate_OnlyTouchesChangedGroups(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name   string
		change func(*ResourcePolicyModel)
		want   []string
	}{
		{"rename", func(m *ResourcePolicyModel) { m.Name = types.StringValue("corp2") }, []string{"POST /resource-policy/7"}},
		{"sso", func(m *ResourcePolicyModel) { m.SSO = types.BoolValue(false) }, []string{"POST /resource-policy/7/access-control"}},
		{"whitelist", func(m *ResourcePolicyModel) {
			m.EmailWhitelistEnabled = types.BoolValue(true)
		}, []string{"POST /resource-policy/7/whitelist"}},
		{"rules", func(m *ResourcePolicyModel) {
			m.Rules = rulesList(t, rule("DROP", "IP", "1.2.3.4", 1, true))
		}, []string{"POST /resource-policy/7/rules"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakePangolin{}
			r := &ResourcePolicyResource{client: fake.client(t, func(string, string) any { return policyPayload() })}
			schemaResp, typ := resourceSchema(t, r)

			prior := basePolicyModel()
			plan := basePolicyModel()
			tc.change(&plan)
			req := resource.UpdateRequest{
				Plan:  tfsdk.Plan{Schema: schemaResp.Schema, Raw: tftypes.NewValue(typ, nil)},
				State: tfsdk.State{Schema: schemaResp.Schema, Raw: tftypes.NewValue(typ, nil)},
			}
			req.Plan.Set(ctx, &plan)
			req.State.Set(ctx, &prior)
			resp := resource.UpdateResponse{State: tfsdk.State{Schema: schemaResp.Schema, Raw: tftypes.NewValue(typ, nil)}}
			r.Update(ctx, req, &resp)
			if resp.Diagnostics.HasError() {
				t.Fatalf("update: %v", resp.Diagnostics)
			}
			want := append(tc.want, "GET /resource-policy/7")
			if got := fake.paths(); !samePaths(got, want) {
				t.Errorf("calls = %v, want %v", got, want)
			}
		})
	}
}

func TestHTTPResourceUpdate_DetachSendsNullPolicyFirst(t *testing.T) {
	ctx := context.Background()
	fake := &fakePangolin{}
	r := &HTTPResource{client: fake.client(t, func(string, string) any {
		return map[string]any{"resourceId": 3, "name": "app", "mode": "http", "resourcePolicyId": nil}
	})}
	schemaResp, typ := resourceSchema(t, r)

	prior := HTTPResourceModel{
		ID:               types.Int64Value(3),
		Name:             types.StringValue("app"),
		Mode:             types.StringValue("http"),
		SSO:              types.BoolValue(true),
		ResourcePolicyID: types.Int64Value(5),
	}
	plan := prior
	plan.ResourcePolicyID = types.Int64Value(0)
	req := resource.UpdateRequest{
		Plan:  tfsdk.Plan{Schema: schemaResp.Schema, Raw: tftypes.NewValue(typ, nil)},
		State: tfsdk.State{Schema: schemaResp.Schema, Raw: tftypes.NewValue(typ, nil)},
	}
	if diags := req.Plan.Set(ctx, &plan); diags.HasError() {
		t.Fatalf("plan: %v", diags)
	}
	req.State.Set(ctx, &prior)
	resp := resource.UpdateResponse{State: tfsdk.State{Schema: schemaResp.Schema, Raw: tftypes.NewValue(typ, nil)}}
	r.Update(ctx, req, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("update: %v", resp.Diagnostics)
	}

	// The detach goes alone first, so the settings POST that follows
	// lands on the inline policy the server now uses.
	if !samePaths(fake.paths(), []string{"POST /resource/3", "POST /resource/3"}) {
		t.Fatalf("calls = %v", fake.paths())
	}
	v, ok := fake.calls[0].Body["resourcePolicyId"]
	if !ok || v != nil {
		t.Errorf("first call resourcePolicyId = %#v (present=%v), want explicit null", v, ok)
	}
	if _, ok := fake.calls[0].Body["sso"]; ok {
		t.Errorf("first call carries settings: %v", fake.calls[0].Body)
	}
	if _, ok := fake.calls[1].Body["resourcePolicyId"]; ok {
		t.Errorf("settings call repeats resourcePolicyId: %v", fake.calls[1].Body)
	}
	if fake.calls[1].Body["sso"] != true {
		t.Errorf("settings call sso = %v, want true", fake.calls[1].Body["sso"])
	}
	var state HTTPResourceModel
	resp.State.Get(ctx, &state)
	if state.ResourcePolicyID.IsNull() || state.ResourcePolicyID.ValueInt64() != 0 {
		t.Errorf("state ResourcePolicyID = %v, want 0 kept so the next plan is clean", state.ResourcePolicyID)
	}
}

func TestHTTPResourceUpdate_UnsetPolicyKeepsAttachment(t *testing.T) {
	ctx := context.Background()
	fake := &fakePangolin{}
	r := &HTTPResource{client: fake.client(t, func(string, string) any {
		return map[string]any{"resourceId": 3, "name": "renamed", "mode": "http", "resourcePolicyId": 5}
	})}
	schemaResp, typ := resourceSchema(t, r)

	// UseStateForUnknown carries the attachment into the plan when the
	// config leaves the attribute unset.
	prior := HTTPResourceModel{
		ID:               types.Int64Value(3),
		Name:             types.StringValue("app"),
		Mode:             types.StringValue("http"),
		ResourcePolicyID: types.Int64Value(5),
	}
	plan := prior
	plan.Name = types.StringValue("renamed")
	req := resource.UpdateRequest{
		Plan:  tfsdk.Plan{Schema: schemaResp.Schema, Raw: tftypes.NewValue(typ, nil)},
		State: tfsdk.State{Schema: schemaResp.Schema, Raw: tftypes.NewValue(typ, nil)},
	}
	req.Plan.Set(ctx, &plan)
	req.State.Set(ctx, &prior)
	resp := resource.UpdateResponse{State: tfsdk.State{Schema: schemaResp.Schema, Raw: tftypes.NewValue(typ, nil)}}
	r.Update(ctx, req, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("update: %v", resp.Diagnostics)
	}
	if len(fake.calls) != 1 {
		t.Fatalf("calls = %v, want a single settings POST", fake.paths())
	}
	if _, ok := fake.calls[0].Body["resourcePolicyId"]; ok {
		t.Errorf("unchanged attachment sent resourcePolicyId: %v", fake.calls[0].Body)
	}
}

func TestHTTPResourceCreate_AttachesPolicyBeforeSettings(t *testing.T) {
	fake := &fakePangolin{}
	r := &HTTPResource{client: fake.client(t, sharedPolicyServer(9))}
	schemaResp, typ := resourceSchema(t, r)

	resp := createHTTPResource(t, r, HTTPResourceModel{
		Name:             types.StringValue("app"),
		Mode:             types.StringValue("http"),
		Protocol:         types.StringValue("tcp"),
		Subdomain:        types.StringValue("app"),
		DomainID:         types.StringValue("dom"),
		SSO:              types.BoolValue(true),
		ResourcePolicyID: types.Int64Value(9),
	}, schemaResp, typ)
	if resp.Diagnostics.HasError() {
		t.Fatalf("create: %v", resp.Diagnostics)
	}
	paths := fake.paths()
	if len(paths) != 4 || paths[0] != "GET /org/test-org/resource-policies" || paths[2] != "POST /resource/3" || paths[3] != "POST /resource/3" {
		t.Fatalf("calls = %v, want policy check, create, attach, settings", paths)
	}
	if fake.calls[2].Body["resourcePolicyId"] != float64(9) {
		t.Errorf("attach call body = %v", fake.calls[2].Body)
	}
	if _, ok := fake.calls[2].Body["sso"]; ok {
		t.Errorf("attach call carries settings: %v", fake.calls[2].Body)
	}
	if _, ok := fake.calls[3].Body["resourcePolicyId"]; ok {
		t.Errorf("settings call repeats resourcePolicyId: %v", fake.calls[3].Body)
	}
}

func TestHTTPResourceCreate_RefusesNonSharedPolicy(t *testing.T) {
	// Policy 12 answers GET (an inline default policy) but is not in the
	// shared list: nothing must be created.
	fake := &fakePangolin{}
	r := &HTTPResource{client: fake.client(t, sharedPolicyServer(9))}
	schemaResp, typ := resourceSchema(t, r)

	resp := createHTTPResource(t, r, HTTPResourceModel{
		Name:             types.StringValue("app"),
		Mode:             types.StringValue("http"),
		Protocol:         types.StringValue("tcp"),
		ResourcePolicyID: types.Int64Value(12),
	}, schemaResp, typ)
	if !resp.Diagnostics.HasError() {
		t.Fatal("expected an error for a non-shared policy")
	}
	if !samePaths(fake.paths(), []string{"GET /org/test-org/resource-policies"}) {
		t.Errorf("calls = %v, want only the policy check", fake.paths())
	}
}

// sharedPolicyServer answers the policies list with one shared policy
// and every other call with resource 3 attached to it.
func sharedPolicyServer(policyID int) func(method, path string) any {
	return func(_ string, p string) any {
		if p == "/org/test-org/resource-policies" {
			return map[string]any{
				"policies":   []map[string]any{{"resourcePolicyId": policyID, "name": "corp", "orgId": "test-org"}},
				"pagination": map[string]any{"total": 1, "pageSize": 100, "page": 1},
			}
		}
		return map[string]any{"resourceId": 3, "niceId": "app", "name": "app", "mode": "http", "resourcePolicyId": policyID}
	}
}

func createHTTPResource(t *testing.T, r *HTTPResource, plan HTTPResourceModel, schemaResp resource.SchemaResponse, typ tftypes.Type) resource.CreateResponse {
	t.Helper()
	ctx := context.Background()
	req := resource.CreateRequest{Plan: tfsdk.Plan{Schema: schemaResp.Schema, Raw: tftypes.NewValue(typ, nil)}}
	if diags := req.Plan.Set(ctx, &plan); diags.HasError() {
		t.Fatalf("plan: %v", diags)
	}
	resp := resource.CreateResponse{State: tfsdk.State{Schema: schemaResp.Schema, Raw: tftypes.NewValue(typ, nil)}}
	r.Create(ctx, req, &resp)
	return resp
}

func TestHTTPResourceCreate_FailedAttachKeepsID(t *testing.T) {
	ctx := context.Background()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "error": true, "message": "Resource policies are not supported on your current plan", "status": 400})
			return
		}
		body := sharedPolicyServer(9)(r.Method, strings.TrimPrefix(r.URL.Path, "/v1"))
		if r.Method == http.MethodPut {
			body = map[string]any{"resourceId": 3, "niceId": "db", "name": "db", "mode": "tcp"}
		}
		data, _ := json.Marshal(body)
		_ = json.NewEncoder(w).Encode(client.APIResponse{Data: data, Success: true, Status: 200})
	}))
	t.Cleanup(srv.Close)
	r := &HTTPResource{client: client.NewClient(srv.URL, "test-key", "test-org")}
	schemaResp, typ := resourceSchema(t, r)

	plan := HTTPResourceModel{
		Name:             types.StringValue("db"),
		Mode:             types.StringValue("tcp"),
		Protocol:         types.StringValue("tcp"),
		ProxyPort:        types.Int64Value(5432),
		ResourcePolicyID: types.Int64Value(9),
	}
	req := resource.CreateRequest{Plan: tfsdk.Plan{Schema: schemaResp.Schema, Raw: tftypes.NewValue(typ, nil)}}
	if diags := req.Plan.Set(ctx, &plan); diags.HasError() {
		t.Fatalf("plan: %v", diags)
	}
	resp := resource.CreateResponse{State: tfsdk.State{Schema: schemaResp.Schema, Raw: tftypes.NewValue(typ, nil)}}
	r.Create(ctx, req, &resp)
	if !resp.Diagnostics.HasError() {
		t.Fatal("expected the attach error to surface")
	}
	var id types.Int64
	resp.State.GetAttribute(ctx, path.Root("id"), &id)
	if id.ValueInt64() != 3 {
		t.Errorf("state id = %v, want 3 so the resource is tainted, not orphaned", id)
	}
}
