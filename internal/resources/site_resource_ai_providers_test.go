package resources

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stackopshq/terraform-provider-pangolin/internal/client"
)

func TestSetFullyKnown(t *testing.T) {
	partial := types.SetValueMust(types.Int64Type, []attr.Value{types.Int64Value(1), types.Int64Unknown()})
	for name, tc := range map[string]struct {
		set  types.Set
		want bool
	}{
		"known":           {int64Set(1, 2), true},
		"empty":           {int64Set(), true},
		"null":            {types.SetNull(types.Int64Type), false},
		"unknown":         {types.SetUnknown(types.Int64Type), false},
		"unknown element": {partial, false},
	} {
		if got := setFullyKnown(tc.set); got != tc.want {
			t.Errorf("%s: setFullyKnown = %v, want %v", name, got, tc.want)
		}
	}
}

func TestOmitUnchangedOptionalKeys(t *testing.T) {
	base := SitePrivateResourceModel{
		SSL: types.BoolValue(false), NiceID: types.StringValue("db"),
		Enabled: types.BoolValue(true), AuthDaemonPort: types.Int64Value(22123),
	}
	full := func() *client.UpdateSiteResourceRequest {
		f, tr := false, true
		return &client.UpdateSiteResourceRequest{SSL: &f, NiceID: "db", Enabled: &tr, AuthDaemonPort: 22123}
	}
	unset := SitePrivateResourceModel{
		SSL: types.BoolNull(), NiceID: types.StringNull(), Enabled: types.BoolNull(), AuthDaemonPort: types.Int64Null(),
	}

	// Unset in config and unchanged: the keys stay off the body, so a
	// pre-1.18 strict schema does not see `ssl` / `niceId`.
	req := full()
	omitUnchangedOptionalKeys(req, base, unset, &base)
	if req.SSL != nil || req.NiceID != "" || req.Enabled != nil || req.AuthDaemonPort != 0 {
		t.Errorf("unchanged unset keys kept: %+v", req)
	}

	// Set in config: always sent.
	req = full()
	omitUnchangedOptionalKeys(req, base, base, &base)
	if req.SSL == nil || req.NiceID != "db" || req.Enabled == nil || req.AuthDaemonPort != 22123 {
		t.Errorf("configured keys dropped: %+v", req)
	}

	// Changed from the prior state (enabled toggled): sent.
	prior := base
	prior.Enabled = types.BoolValue(false)
	req = full()
	omitUnchangedOptionalKeys(req, base, unset, &prior)
	if req.Enabled == nil || req.SSL != nil {
		t.Errorf("changed key dropped or unchanged kept: %+v", req)
	}

	// Right after a create (no prior state): only configured keys.
	req = full()
	cfg := unset
	cfg.Enabled = types.BoolValue(false)
	omitUnchangedOptionalKeys(req, base, cfg, nil)
	if req.Enabled == nil || req.SSL != nil || req.NiceID != "" {
		t.Errorf("post-create body = %+v", req)
	}
}

// newFailingAIProvidersStub answers the AI provider calls made with
// failMethod with the given status, the other AI provider calls with one
// attached provider, and everything else with the site resource.
func newFailingAIProvidersStub(t *testing.T, siteResource map[string]any, failMethod string, status int) *client.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		isProviders := strings.HasSuffix(r.URL.Path, "/ai-providers")
		if isProviders && r.Method == failMethod {
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(map[string]any{"data": nil, "success": false, "error": true, "message": "nope", "status": status})
			return
		}
		var data any = siteResource
		if isProviders {
			data = map[string]any{"providers": []map[string]any{{"providerId": 4, "accessMode": "inherit", "enabled": true}}}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data, "success": true, "error": false, "message": "ok", "status": 200})
	}))
	t.Cleanup(srv.Close)
	return client.NewClient(srv.URL, "k", "org")
}

func TestReadAIProviders_ForbiddenWarnsAndKeepsValue(t *testing.T) {
	r := &SitePrivateResource{client: newFailingAIProvidersStub(t, nil, http.MethodGet, http.StatusForbidden)}

	m := SitePrivateResourceModel{ID: types.Int64Value(12), Mode: types.StringValue("inference"), AIProviderIDs: int64Set(4)}
	diags := r.readAIProviders(context.Background(), &m, false)
	if diags.HasError() || diags.WarningsCount() != 1 {
		t.Fatalf("diags = %v", diags)
	}
	if !m.AIProviderIDs.Equal(int64Set(4)) {
		t.Errorf("providers = %v, want the last known {4}", m.AIProviderIDs)
	}

	// Nothing known yet (import): an empty set, never an unknown.
	m = SitePrivateResourceModel{ID: types.Int64Value(12), Mode: types.StringValue("inference"), AIProviderIDs: types.SetUnknown(types.Int64Type)}
	if diags := r.readAIProviders(context.Background(), &m, false); diags.HasError() {
		t.Fatalf("diags = %v", diags)
	}
	if !m.AIProviderIDs.Equal(int64Set()) {
		t.Errorf("providers = %v, want an empty set", m.AIProviderIDs)
	}
}

func TestReadAIProviders_OtherErrors(t *testing.T) {
	r := &SitePrivateResource{client: newFailingAIProvidersStub(t, nil, http.MethodGet, http.StatusBadRequest)}
	m := SitePrivateResourceModel{ID: types.Int64Value(12), Mode: types.StringValue("inference"), AIProviderIDs: int64Set(4)}
	if diags := r.readAIProviders(context.Background(), &m, false); !diags.HasError() {
		t.Errorf("a managed read error must fail, got %v", diags)
	}
	// Unmanaged (ai_provider_ids not in the config on create): a warning.
	if diags := r.readAIProviders(context.Background(), &m, true); diags.HasError() || diags.WarningsCount() != 1 {
		t.Errorf("an unmanaged read error must only warn, got %v", diags)
	}
}

func TestSiteResourceUpdate_AIProvidersFailureKeepsPriorProviders(t *testing.T) {
	ctx := context.Background()
	s := siteResourceSchema(t)
	c := newFailingAIProvidersStub(t,
		map[string]any{"siteResourceId": 12, "name": "llm-renamed", "mode": "inference", "enabled": true},
		http.MethodPost, http.StatusBadRequest)

	model := func(name string, providers types.Set) SitePrivateResourceModel {
		return SitePrivateResourceModel{
			ID: types.Int64Value(12), NiceID: types.StringValue("llm"), Name: types.StringValue(name),
			Mode: types.StringValue("inference"), SiteID: types.Int64Null(), SiteIDs: int64Set(),
			AIProviderIDs: providers, Enabled: types.BoolValue(true), SSL: types.BoolValue(false),
			Destination: types.StringValue(""), Alias: types.StringValue(""),
			TCPPortRange: types.StringValue("443,80"), UDPPortRange: types.StringValue(""), DisableICMP: types.BoolValue(true),
			AuthDaemonMode: types.StringValue("site"), AuthDaemonPort: types.Int64Value(0), PamMode: types.StringNull(),
			NetworkID: types.Int64Value(0), DefaultNetworkID: types.Int64Null(), Scheme: types.StringNull(),
			ProxyPort: types.Int64Null(), DestinationPort: types.Int64Null(), AliasAddress: types.StringNull(),
			DomainID: types.StringNull(), Subdomain: types.StringNull(), FullDomain: types.StringNull(),
		}
	}
	prior, planned := model("llm", int64Set(4)), model("llm-renamed", int64Set(4, 6))
	tfState, tfPlan := tfsdk.State{Schema: s}, tfsdk.Plan{Schema: s}
	tfState.Set(ctx, &prior)
	tfPlan.Set(ctx, &planned)
	resp := resource.UpdateResponse{State: tfState}
	(&SitePrivateResource{client: c}).Update(ctx, resource.UpdateRequest{
		Plan: tfPlan, State: tfState, Config: tfsdk.Config{Schema: s, Raw: tfPlan.Raw},
	}, &resp)
	if !resp.Diagnostics.HasError() {
		t.Fatalf("expected the AI provider error")
	}
	var got SitePrivateResourceModel
	if d := resp.State.Get(ctx, &got); d.HasError() {
		t.Fatalf("state: %v", d)
	}
	if got.Name.ValueString() != "llm-renamed" {
		t.Errorf("name = %q, want the applied rename", got.Name.ValueString())
	}
	if !got.AIProviderIDs.Equal(int64Set(4)) {
		t.Errorf("providers = %v, want the prior {4}", got.AIProviderIDs)
	}
}
