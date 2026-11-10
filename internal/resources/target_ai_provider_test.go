package resources

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stackopshq/terraform-provider-pangolin/internal/client"
)

// TestTargetCreate_Owner checks that the owner attribute picks the create
// route, and that the owner the server leaves null stays null in state.
// The fake server answers like createTarget: 201 envelope with the raw row
// merged with its health check, the other owner column null.
func TestTargetCreate_Owner(t *testing.T) {
	for _, tc := range []struct {
		name         string
		resourceID   types.Int64
		aiProviderID types.Int64
		wantPath     string
		row          string
	}{
		{"resource", types.Int64Value(2), types.Int64Null(), "/v1/resource/2/target",
			`"resourceId":2,"providerId":null`},
		{"ai provider", types.Int64Null(), types.Int64Value(7), "/v1/ai-provider/7/target",
			`"resourceId":null,"providerId":7`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var gotPath string
			var gotBody map[string]any
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPut {
					t.Errorf("method = %s, want PUT", r.Method)
				}
				gotPath = r.URL.Path
				_ = json.NewDecoder(r.Body).Decode(&gotBody)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte(`{"success":true,"error":false,"status":201,"message":"Target created successfully",` +
					`"data":{"targetId":9,` + tc.row + `,"siteId":5,"ip":"llm.lan","method":"http","port":11434,` +
					`"enabled":true,"internalPort":21001,"hcEnabled":false,"hcHealth":"unknown","priority":100}}`))
			}))
			t.Cleanup(srv.Close)
			r := &TargetResource{client: client.NewClient(srv.URL, "k", "org")}

			ctx := context.Background()
			var sresp resource.SchemaResponse
			r.Schema(ctx, resource.SchemaRequest{}, &sresp)
			plan := tfsdk.Plan{Schema: sresp.Schema}
			m := TargetResourceModel{
				ID: types.Int64Unknown(), ResourceID: tc.resourceID, AIProviderID: tc.aiProviderID,
				SiteID: types.Int64Value(5), IP: types.StringValue("llm.lan"), Port: types.Int64Value(11434),
				Method: types.StringValue("http"), Enabled: types.BoolValue(true),
			}
			if d := plan.Set(ctx, &m); d.HasError() {
				t.Fatalf("set plan: %v", d)
			}
			resp := resource.CreateResponse{State: tfsdk.State{Schema: sresp.Schema}}
			r.Create(ctx, resource.CreateRequest{Plan: plan}, &resp)
			if resp.Diagnostics.HasError() {
				t.Fatalf("create: %v", resp.Diagnostics)
			}
			if gotPath != tc.wantPath {
				t.Errorf("path = %s, want %s", gotPath, tc.wantPath)
			}
			if gotBody["siteId"] != float64(5) || gotBody["ip"] != "llm.lan" || gotBody["method"] != "http" {
				t.Errorf("body = %v", gotBody)
			}
			var got TargetResourceModel
			resp.State.Get(ctx, &got)
			if got.ID.ValueInt64() != 9 || !got.ResourceID.Equal(tc.resourceID) || !got.AIProviderID.Equal(tc.aiProviderID) {
				t.Errorf("state id=%v resource_id=%v ai_provider_id=%v", got.ID, got.ResourceID, got.AIProviderID)
			}
		})
	}
}

// targetTestConfig builds a framework Config from a model (tfsdk.Config
// has no Set: go through a State).
func targetTestConfig(t *testing.T, m TargetResourceModel) tfsdk.Config {
	t.Helper()
	ctx := context.Background()
	var sr resource.SchemaResponse
	(&TargetResource{}).Schema(ctx, resource.SchemaRequest{}, &sr)
	st := tfsdk.State{Schema: sr.Schema}
	if d := st.Set(ctx, &m); d.HasError() {
		t.Fatalf("config: %v", d)
	}
	return tfsdk.Config{Schema: sr.Schema, Raw: st.Raw}
}

// TestTargetValidateConfig_AIIgnoredFields: the routing fields the AI
// gateway does not read are refused on an AI provider target only, and
// an unknown value is never refused.
func TestTargetValidateConfig_AIIgnoredFields(t *testing.T) {
	base := func(resourceID, aiProviderID types.Int64) TargetResourceModel {
		return TargetResourceModel{
			ID: types.Int64Null(), ResourceID: resourceID, AIProviderID: aiProviderID,
			SiteID: types.Int64Value(5), IP: types.StringValue("llm.lan"), Port: types.Int64Value(11434),
			Method: types.StringNull(), Enabled: types.BoolNull(), HCHealth: types.StringNull(),
		}
	}
	ai := func() TargetResourceModel { return base(types.Int64Null(), types.Int64Unknown()) }
	res := func() TargetResourceModel { return base(types.Int64Value(2), types.Int64Null()) }
	for _, tc := range []struct {
		name    string
		m       TargetResourceModel
		wantErr string
	}{
		{"ai: path", func() TargetResourceModel { m := ai(); m.Path = types.StringValue("/v1"); return m }(), "path"},
		{"ai: path_match_type", func() TargetResourceModel {
			m := ai()
			m.PathMatchType = types.StringValue("prefix")
			return m
		}(), "path_match_type"},
		{"ai: rewrite_path", func() TargetResourceModel { m := ai(); m.RewritePath = types.StringValue("/"); return m }(), "rewrite_path"},
		{"ai: rewrite_path_type", func() TargetResourceModel {
			m := ai()
			m.RewritePathType = types.StringValue("stripPrefix")
			return m
		}(), "rewrite_path_type"},
		{"ai: priority", func() TargetResourceModel { m := ai(); m.Priority = types.Int64Value(10); return m }(), "priority"},
		{"ai: unknown path", func() TargetResourceModel { m := ai(); m.Path = types.StringUnknown(); return m }(), ""},
		{"ai: health check", func() TargetResourceModel {
			m := ai()
			m.HCEnabled, m.HCHostname, m.HCMode = types.BoolValue(true), types.StringValue("llm.lan"), types.StringValue("http")
			return m
		}(), ""},
		{"resource: path and priority", func() TargetResourceModel {
			m := res()
			m.Path, m.Priority = types.StringValue("/v1"), types.Int64Value(10)
			return m
		}(), ""},
		{"unknown resource_id: path", func() TargetResourceModel {
			m := base(types.Int64Unknown(), types.Int64Null())
			m.Path = types.StringValue("/v1")
			return m
		}(), ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := resource.ValidateConfigResponse{}
			(&TargetResource{}).ValidateConfig(context.Background(),
				resource.ValidateConfigRequest{Config: targetTestConfig(t, tc.m)}, &resp)
			errs := resp.Diagnostics.Errors()
			if tc.wantErr == "" {
				if len(errs) > 0 {
					t.Fatalf("unexpected errors: %v", errs)
				}
				return
			}
			if len(errs) != 1 {
				t.Fatalf("errors = %v, want one on %s", errs, tc.wantErr)
			}
			if d, ok := errs[0].(interface{ Path() path.Path }); !ok || !d.Path().Equal(path.Root(tc.wantErr)) {
				t.Errorf("error %v, want it on %s", errs[0], tc.wantErr)
			}
		})
	}
}

// TestTargetResourceBodies_Unchanged pins the create and update bodies of
// a resource target with every hc_* and routing field set: adding
// ai_provider_id must not change what an existing target sends.
func TestTargetResourceBodies_Unchanged(t *testing.T) {
	const want = `{"enabled":true,"hcEnabled":true,"hcFollowRedirects":false,"hcHeaders":[{"name":"X-Probe","value":"yes"}],` +
		`"hcHealthyThreshold":2,"hcHostname":"app.lan","hcInterval":30,"hcMethod":"GET","hcMode":"http","hcPath":"/health",` +
		`"hcPort":8081,"hcScheme":"http","hcStatus":200,"hcTimeout":5,"hcTlsServerName":"app.svc","hcUnhealthyInterval":10,` +
		`"hcUnhealthyThreshold":3,"ip":"app.lan","method":"https","path":"/api","pathMatchType":"prefix","port":8080,` +
		`"priority":10,"rewritePath":"/","rewritePathType":"stripPrefix","siteId":5}`
	bodies := map[string]string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b map[string]any
		_ = json.NewDecoder(r.Body).Decode(&b)
		norm, _ := json.Marshal(b) // map keys come out sorted
		bodies[r.Method+" "+r.URL.Path] = string(norm)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"error":false,"status":200,"message":"ok","data":{"targetId":9,` +
			`"resourceId":2,"providerId":null,"siteId":5,"ip":"app.lan","method":"https","port":8080,"enabled":true,` +
			`"hcEnabled":true,"hcHealth":"unhealthy"}}`))
	}))
	t.Cleanup(srv.Close)
	r := &TargetResource{client: client.NewClient(srv.URL, "k", "org")}

	ctx := context.Background()
	var sr resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &sr)
	m := TargetResourceModel{
		ID: types.Int64Value(9), ResourceID: types.Int64Value(2), AIProviderID: types.Int64Null(),
		SiteID: types.Int64Value(5), IP: types.StringValue("app.lan"), Port: types.Int64Value(8080),
		Method: types.StringValue("https"), Enabled: types.BoolValue(true),
		HCEnabled: types.BoolValue(true), HCPath: types.StringValue("/health"), HCScheme: types.StringValue("http"),
		HCMode: types.StringValue("http"), HCHostname: types.StringValue("app.lan"), HCPort: types.Int64Value(8081),
		HCInterval: types.Int64Value(30), HCUnhealthyInterval: types.Int64Value(10), HCTimeout: types.Int64Value(5),
		HCHeaders:         []TargetHCHeaderModel{{Name: types.StringValue("X-Probe"), Value: types.StringValue("yes")}},
		HCFollowRedirects: types.BoolValue(false), HCMethod: types.StringValue("GET"), HCStatus: types.Int64Value(200),
		HCTLSServerName: types.StringValue("app.svc"), HCHealthyThreshold: types.Int64Value(2),
		HCUnhealthyThreshold: types.Int64Value(3), HCHealth: types.StringUnknown(),
		Path: types.StringValue("/api"), PathMatchType: types.StringValue("prefix"), RewritePath: types.StringValue("/"),
		RewritePathType: types.StringValue("stripPrefix"), Priority: types.Int64Value(10),
	}
	plan := tfsdk.Plan{Schema: sr.Schema}
	if d := plan.Set(ctx, &m); d.HasError() {
		t.Fatalf("plan: %v", d)
	}
	cresp := resource.CreateResponse{State: tfsdk.State{Schema: sr.Schema}}
	r.Create(ctx, resource.CreateRequest{Plan: plan}, &cresp)
	uresp := resource.UpdateResponse{State: tfsdk.State{Schema: sr.Schema}}
	r.Update(ctx, resource.UpdateRequest{Plan: plan}, &uresp)
	if cresp.Diagnostics.HasError() || uresp.Diagnostics.HasError() {
		t.Fatalf("create: %v update: %v", cresp.Diagnostics, uresp.Diagnostics)
	}
	for _, key := range []string{"PUT /v1/resource/2/target", "POST /v1/target/9"} {
		if bodies[key] != want {
			t.Errorf("%s body\n got %s\nwant %s", key, bodies[key], want)
		}
	}
	if len(bodies) != 2 {
		t.Errorf("calls = %v", bodies)
	}
}

// TestTargetToModel_Owner: the owner comes from the wire row, never both,
// never the wrong one.
func TestTargetToModel_Owner(t *testing.T) {
	for _, tc := range []struct {
		row                    string
		wantResource, wantAIPr types.Int64
	}{
		{`{"targetId":1,"resourceId":2,"providerId":null}`, types.Int64Value(2), types.Int64Null()},
		{`{"targetId":1,"resourceId":2}`, types.Int64Value(2), types.Int64Null()},
		{`{"targetId":1,"resourceId":null,"providerId":7}`, types.Int64Null(), types.Int64Value(7)},
		{`{"targetId":1,"providerId":7}`, types.Int64Null(), types.Int64Value(7)},
	} {
		var tgt client.Target
		if err := json.Unmarshal([]byte(tc.row), &tgt); err != nil {
			t.Fatalf("%s: %v", tc.row, err)
		}
		m := targetToModel(&tgt, TargetResourceModel{})
		if !m.ResourceID.Equal(tc.wantResource) || !m.AIProviderID.Equal(tc.wantAIPr) {
			t.Errorf("%s: resource_id=%v ai_provider_id=%v", tc.row, m.ResourceID, m.AIProviderID)
		}
	}
}
