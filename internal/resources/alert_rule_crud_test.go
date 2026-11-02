package resources

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stackopshq/terraform-provider-pangolin/internal/client"
)

// alertRuleTestServer answers the rule GET / DELETE with ruleStatus and
// the route probe (GET /alert-rules) with probeStatus.
func alertRuleTestServer(t *testing.T, ruleStatus, probeStatus int) *AlertRuleResource {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		status := ruleStatus
		if r.URL.Path == "/v1/org/org/alert-rules" {
			status = probeStatus
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if status == http.StatusOK {
			_, _ = w.Write([]byte(`{"success":true,"data":{"alertRules":[],"pagination":{"total":0}}}`))
			return
		}
		_, _ = w.Write([]byte(`{"success":false,"message":"nope","data":null}`))
	}))
	t.Cleanup(srv.Close)
	return &AlertRuleResource{client: client.NewClient(srv.URL, "k", "org")}
}

// alertRuleTestState builds a framework state holding rule 17.
func alertRuleTestState(t *testing.T, r *AlertRuleResource) tfsdk.State {
	t.Helper()
	ctx := context.Background()
	var sresp resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &sresp)
	m := alertRuleTestPlan(t)
	m.ID = types.Int64Value(17)
	m.CreatedAt = types.Int64Value(1)
	state := tfsdk.State{Schema: sresp.Schema}
	if d := state.Set(ctx, &m); d.HasError() {
		t.Fatalf("set state: %v", d)
	}
	return state
}

func TestAlertRuleRead_404(t *testing.T) {
	cases := []struct {
		name        string
		probe       int
		wantRemoved bool
		wantError   bool
		wantWarning bool
	}{
		{"rule gone, routes answer", http.StatusOK, true, false, false},
		{"routes missing (CE / wrong URL)", http.StatusNotFound, false, true, false},
		{"probe refused (key without list permission)", http.StatusForbidden, true, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := alertRuleTestServer(t, http.StatusNotFound, tc.probe)
			state := alertRuleTestState(t, r)
			resp := resource.ReadResponse{State: state}
			r.Read(context.Background(), resource.ReadRequest{State: state}, &resp)

			if removed := resp.State.Raw.IsNull(); removed != tc.wantRemoved {
				t.Errorf("removed = %v, want %v", removed, tc.wantRemoved)
			}
			if got := resp.Diagnostics.HasError(); got != tc.wantError {
				t.Errorf("error = %v, want %v (%v)", got, tc.wantError, resp.Diagnostics)
			}
			if got := len(resp.Diagnostics.Warnings()) > 0; got != tc.wantWarning {
				t.Errorf("warning = %v, want %v (%v)", got, tc.wantWarning, resp.Diagnostics)
			}
		})
	}
}

func TestAlertRuleDelete_404(t *testing.T) {
	for _, tc := range []struct {
		name      string
		probe     int
		wantError bool
	}{
		{"rule already gone", http.StatusOK, false},
		{"routes missing (CE / wrong URL)", http.StatusNotFound, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := alertRuleTestServer(t, http.StatusNotFound, tc.probe)
			state := alertRuleTestState(t, r)
			resp := resource.DeleteResponse{State: state}
			r.Delete(context.Background(), resource.DeleteRequest{State: state}, &resp)
			if got := resp.Diagnostics.HasError(); got != tc.wantError {
				t.Errorf("error = %v, want %v (%v)", got, tc.wantError, resp.Diagnostics)
			}
		})
	}
}
