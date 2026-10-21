package resources

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/stackopshq/terraform-provider-pangolin/internal/client"
)

// httpResourceSchema returns the pangolin_resource schema and its
// Terraform object type.
func httpResourceSchema(t *testing.T) (resource.SchemaResponse, tftypes.Object) {
	t.Helper()
	var sr resource.SchemaResponse
	(&HTTPResource{}).Schema(context.Background(), resource.SchemaRequest{}, &sr)
	if sr.Diagnostics.HasError() {
		t.Fatalf("schema: %v", sr.Diagnostics)
	}
	return sr, sr.Schema.Type().TerraformType(context.Background()).(tftypes.Object)
}

// httpResourceConfig builds a config where every attribute is null
// except the ones in set.
func httpResourceConfig(t *testing.T, set map[string]tftypes.Value) tfsdk.Config {
	t.Helper()
	sr, typ := httpResourceSchema(t)
	vals := map[string]tftypes.Value{}
	for name, at := range typ.AttributeTypes {
		vals[name] = tftypes.NewValue(at, nil)
	}
	for name, v := range set {
		vals[name] = v
	}
	return tfsdk.Config{Schema: sr.Schema, Raw: tftypes.NewValue(typ, vals)}
}

// headerListValue returns a one-entry header list for attr.
func headerListValue(t *testing.T, attr string) tftypes.Value {
	t.Helper()
	_, typ := httpResourceSchema(t)
	lt := typ.AttributeTypes[attr].(tftypes.List)
	ot := lt.ElementType.(tftypes.Object)
	return tftypes.NewValue(lt, []tftypes.Value{
		tftypes.NewValue(ot, map[string]tftypes.Value{
			"name":  tftypes.NewValue(tftypes.String, "X-Env"),
			"value": tftypes.NewValue(tftypes.String, "prod"),
		}),
	})
}

func TestHTTPResource_ValidateConfig_Headers(t *testing.T) {
	cases := []struct {
		name    string
		mode    tftypes.Value
		attr    string
		wantErr bool
	}{
		{"http headers", tftypes.NewValue(tftypes.String, "http"), "headers", false},
		{"default mode headers", tftypes.NewValue(tftypes.String, nil), "headers", false},
		{"unknown mode headers", tftypes.NewValue(tftypes.String, tftypes.UnknownValue), "headers", false},
		{"http response_headers", tftypes.NewValue(tftypes.String, "http"), "response_headers", false},
		{"tcp headers", tftypes.NewValue(tftypes.String, "tcp"), "headers", true},
		{"udp response_headers", tftypes.NewValue(tftypes.String, "udp"), "response_headers", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := httpResourceConfig(t, map[string]tftypes.Value{
				"mode":  tc.mode,
				tc.attr: headerListValue(t, tc.attr),
			})
			var resp resource.ValidateConfigResponse
			(&HTTPResource{}).ValidateConfig(context.Background(), resource.ValidateConfigRequest{Config: cfg}, &resp)
			if resp.Diagnostics.HasError() != tc.wantErr {
				t.Errorf("HasError = %v, want %v (%v)", resp.Diagnostics.HasError(), tc.wantErr, resp.Diagnostics)
			}
		})
	}
}

func TestHTTPResource_ValidateConfig_L4WithoutHeaders(t *testing.T) {
	cfg := httpResourceConfig(t, map[string]tftypes.Value{
		"mode": tftypes.NewValue(tftypes.String, "tcp"),
	})
	var resp resource.ValidateConfigResponse
	(&HTTPResource{}).ValidateConfig(context.Background(), resource.ValidateConfigRequest{Config: cfg}, &resp)
	if resp.Diagnostics.HasError() {
		t.Errorf("unexpected error: %v", resp.Diagnostics)
	}
}

// rollBackCreateResponse runs rollBackCreate against a server answering
// DELETE /v1/resource/42 with status, and returns the response and
// whether the DELETE was received.
func rollBackCreateResponse(t *testing.T, status int) (*resource.CreateResponse, bool) {
	t.Helper()
	deleted := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/v1/resource/42" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		deleted = true
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if status >= 400 {
			_, _ = w.Write([]byte(`{"data":null,"success":false,"error":true,"message":"forbidden","status":403}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":null,"success":true,"error":false,"message":"ok","status":200}`))
	}))
	t.Cleanup(srv.Close)

	sr, typ := httpResourceSchema(t)
	resp := &resource.CreateResponse{State: tfsdk.State{Schema: sr.Schema, Raw: tftypes.NewValue(typ, nil)}}
	r := &HTTPResource{client: client.NewClient(srv.URL, "k", "org")}
	r.rollBackCreate(context.Background(), 42, resp)
	return resp, deleted
}

func TestHTTPResource_RollBackCreate_DeletesResource(t *testing.T) {
	resp, deleted := rollBackCreateResponse(t, http.StatusOK)
	if !deleted {
		t.Fatal("expected a DELETE of the created resource")
	}
	if resp.Diagnostics.HasError() {
		t.Errorf("unexpected error: %v", resp.Diagnostics)
	}
	if !resp.State.Raw.IsNull() {
		t.Errorf("state should stay empty after a successful rollback")
	}
}

func TestHTTPResource_RollBackCreate_KeepsIDWhenDeleteFails(t *testing.T) {
	resp, deleted := rollBackCreateResponse(t, http.StatusForbidden)
	if !deleted {
		t.Fatal("expected a DELETE of the created resource")
	}
	if !resp.Diagnostics.HasError() {
		t.Errorf("expected an error when the rollback delete fails")
	}
	var id types.Int64
	resp.Diagnostics.Append(resp.State.GetAttribute(context.Background(), path.Root("id"), &id)...)
	if id.ValueInt64() != 42 {
		t.Errorf("id in state = %v, want 42", id)
	}
}
