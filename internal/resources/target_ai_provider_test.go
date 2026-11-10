package resources

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

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
