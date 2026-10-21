package resources

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/stackopshq/terraform-provider-pangolin/internal/client"
)

func mustCreateSiteResourceReq(t *testing.T, plan SitePrivateResourceModel) *client.CreateSiteResourceRequest {
	t.Helper()
	req, diags := buildCreateSiteResourceRequest(context.Background(), plan)
	if diags.HasError() {
		t.Fatalf("build create request: %v", diags)
	}
	return req
}

func mustUpdateSiteResourceReq(t *testing.T, plan *SitePrivateResourceModel) *client.UpdateSiteResourceRequest {
	t.Helper()
	req, diags := buildUpdateSiteResourceRequest(context.Background(), plan)
	if diags.HasError() {
		t.Fatalf("build update request: %v", diags)
	}
	return req
}

func int64Set(ids ...int64) types.Set {
	elems := make([]attr.Value, 0, len(ids))
	for _, id := range ids {
		elems = append(elems, types.Int64Value(id))
	}
	return types.SetValueMust(types.Int64Type, elems)
}

func bodyOf(t *testing.T, v any) map[string]json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return body
}

// -----------------------------------------------------------------------------
// site attachment on the wire
// -----------------------------------------------------------------------------

func TestSiteResourceRequests_SiteAttachment(t *testing.T) {
	base := func() SitePrivateResourceModel {
		return SitePrivateResourceModel{
			Name: types.StringValue("db"), Mode: types.StringValue("host"),
			Destination: types.StringValue("10.0.0.5"), Alias: types.StringValue("db.internal"),
			SiteID: types.Int64Null(), SiteIDs: types.SetNull(types.Int64Type),
		}
	}
	cases := []struct {
		name        string
		mutate      func(*SitePrivateResourceModel)
		wantSiteID  string
		wantSiteIDs string
	}{
		{"one site in site_ids goes in siteId", func(m *SitePrivateResourceModel) { m.SiteIDs = int64Set(7) }, "7", ""},
		{"legacy site_id", func(m *SitePrivateResourceModel) { m.SiteID = types.Int64Value(4) }, "4", ""},
		{"several sites go in siteIds, sorted", func(m *SitePrivateResourceModel) { m.SiteIDs = int64Set(9, 2) }, "", "[2,9]"},
		{"inference has no site", func(m *SitePrivateResourceModel) {
			m.Mode = types.StringValue("inference")
			m.SiteIDs = int64Set()
		}, "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan := base()
			tc.mutate(&plan)
			for _, body := range []map[string]json.RawMessage{
				bodyOf(t, mustCreateSiteResourceReq(t, plan)),
				bodyOf(t, mustUpdateSiteResourceReq(t, &plan)),
			} {
				if got := string(body["siteId"]); got != tc.wantSiteID {
					t.Errorf("siteId = %q, want %q", got, tc.wantSiteID)
				}
				if got := string(body["siteIds"]); got != tc.wantSiteIDs {
					t.Errorf("siteIds = %q, want %q", got, tc.wantSiteIDs)
				}
			}
		})
	}
}

func TestSiteResourceRequests_NewFields(t *testing.T) {
	plan := SitePrivateResourceModel{
		Name: types.StringValue("llm"), Mode: types.StringValue("inference"),
		NiceID: types.StringValue("llm-gw"), SSL: types.BoolValue(true),
		Enabled: types.BoolValue(false), AIProviderIDs: int64Set(3, 1),
		SiteID: types.Int64Null(), SiteIDs: int64Set(),
	}
	create := bodyOf(t, mustCreateSiteResourceReq(t, plan))
	if string(create["niceId"]) != `"llm-gw"` || string(create["ssl"]) != "true" {
		t.Errorf("create = %v", create)
	}
	if string(create["aiProviders"]) != `[{"providerId":1},{"providerId":3}]` {
		t.Errorf("aiProviders = %s", create["aiProviders"])
	}
	if _, ok := create["enabled"]; ok {
		t.Errorf("the create schema has no enabled field: %s", create["enabled"])
	}

	update := bodyOf(t, mustUpdateSiteResourceReq(t, &plan))
	if string(update["mode"]) != `"inference"` || string(update["enabled"]) != "false" || string(update["ssl"]) != "true" {
		t.Errorf("update = %v", update)
	}
	if _, ok := update["aiProviders"]; ok {
		t.Errorf("the update schema has no aiProviders field")
	}

	plan.Mode = types.StringValue("ssh")
	if got := string(bodyOf(t, mustUpdateSiteResourceReq(t, &plan))["mode"]); got != `"ssh"` {
		t.Errorf("ssh update mode = %s", got)
	}
	plan.Mode = types.StringValue("host")
	if _, ok := bodyOf(t, mustUpdateSiteResourceReq(t, &plan))["mode"]; ok {
		t.Errorf("host update must not send mode")
	}
	plan.Mode = types.StringValue("host")
	if _, ok := bodyOf(t, mustCreateSiteResourceReq(t, plan))["aiProviders"]; ok {
		t.Errorf("aiProviders is inference-only")
	}
}

// -----------------------------------------------------------------------------
// hydrate: siteIds
// -----------------------------------------------------------------------------

func TestHydrateSiteResourceState_SiteIDs(t *testing.T) {
	cases := []struct {
		name     string
		prior    SitePrivateResourceModel
		siteIDs  []int
		wantIDs  types.Set
		wantSite types.Int64
	}{
		{"list with several sites", SitePrivateResourceModel{}, []int{5, 2}, int64Set(2, 5), types.Int64Null()},
		{"list with one site", SitePrivateResourceModel{}, []int{5}, int64Set(5), types.Int64Value(5)},
		{"inference list (no site)", SitePrivateResourceModel{}, []int{}, int64Set(), types.Int64Null()},
		{"create response keeps the plan", SitePrivateResourceModel{SiteIDs: int64Set(1, 2), SiteID: types.Int64Null()}, nil, int64Set(1, 2), types.Int64Null()},
		{"legacy state without site_ids", SitePrivateResourceModel{SiteID: types.Int64Value(3), SiteIDs: types.SetNull(types.Int64Type)}, nil, int64Set(3), types.Int64Value(3)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := tc.prior
			hydrateSiteResourceState(&m, &client.SiteResource{SiteResourceID: 1, Mode: "host", SiteIDs: tc.siteIDs})
			if !m.SiteIDs.Equal(tc.wantIDs) {
				t.Errorf("site_ids = %v, want %v", m.SiteIDs, tc.wantIDs)
			}
			if !m.SiteID.Equal(tc.wantSite) {
				t.Errorf("site_id = %v, want %v", m.SiteID, tc.wantSite)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// ValidateConfig: ssh / inference / sites
// -----------------------------------------------------------------------------

func TestSiteResourceValidateConfig_NewModes(t *testing.T) {
	s := siteResourceSchema(t)
	str := func(v string) tftypes.Value { return tftypes.NewValue(tftypes.String, v) }
	num := func(v int64) tftypes.Value { return tftypes.NewValue(tftypes.Number, v) }
	numSet := func(ids ...int64) tftypes.Value {
		vals := make([]tftypes.Value, 0, len(ids))
		for _, id := range ids {
			vals = append(vals, num(id))
		}
		return tftypes.NewValue(tftypes.Set{ElementType: tftypes.Number}, vals)
	}
	site := numSet(1)

	cases := []struct {
		name      string
		set       map[string]tftypes.Value
		wantError string
	}{
		{"ssh with an IPv4 destination", map[string]tftypes.Value{"mode": str("ssh"), "site_ids": site, "destination": str("10.0.0.5")}, ""},
		{"ssh with a hostname and an alias", map[string]tftypes.Value{"mode": str("ssh"), "site_ids": site, "destination": str("bastion"), "alias": str("bastion.internal")}, ""},
		{"ssh with a hostname and no alias", map[string]tftypes.Value{"mode": str("ssh"), "site_ids": site, "destination": str("bastion")}, "alias"},
		{"ssh without destination", map[string]tftypes.Value{"mode": str("ssh"), "site_ids": site}, "destination"},
		{"ssh native without destination", map[string]tftypes.Value{"mode": str("ssh"), "site_ids": site, "auth_daemon_mode": str("native")}, ""},
		{"ssh tcp range is the destination port", map[string]tftypes.Value{
			"mode": str("ssh"), "site_ids": site, "destination": str("10.0.0.5"),
			"destination_port": num(2222), "tcp_port_range": str("2222"),
		}, ""},
		{"ssh with another tcp range", map[string]tftypes.Value{"mode": str("ssh"), "site_ids": site, "destination": str("10.0.0.5"), "tcp_port_range": str("2222")}, "tcp_port_range"},
		{"ssh with icmp enabled", map[string]tftypes.Value{"mode": str("ssh"), "site_ids": site, "destination": str("10.0.0.5"), "disable_icmp": tftypes.NewValue(tftypes.Bool, false)}, "disable_icmp"},
		{"ssh native on several sites", map[string]tftypes.Value{"mode": str("ssh"), "site_ids": numSet(1, 2), "auth_daemon_mode": str("native")}, "site_ids"},
		{"ssh push with the site daemon on several sites", map[string]tftypes.Value{"mode": str("ssh"), "site_ids": numSet(1, 2), "destination": str("10.0.0.5"), "pam_mode": str("push")}, "site_ids"},
		{"ssh remote daemon on several sites", map[string]tftypes.Value{
			"mode": str("ssh"), "site_ids": numSet(1, 2), "destination": str("10.0.0.5"),
			"pam_mode": str("push"), "auth_daemon_mode": str("remote"),
		}, ""},
		{"inference without site", map[string]tftypes.Value{"mode": str("inference"), "ai_provider_ids": numSet(4)}, ""},
		{"inference with a site", map[string]tftypes.Value{"mode": str("inference"), "site_ids": site}, "site_ids"},
		{"inference with another udp range", map[string]tftypes.Value{"mode": str("inference"), "udp_port_range": str("53")}, "udp_port_range"},
		{"ai providers outside inference", map[string]tftypes.Value{"mode": str("host"), "site_ids": site, "destination": str("10.0.0.5"), "ai_provider_ids": numSet(4)}, "ai_provider_ids"},
		{"host without site", map[string]tftypes.Value{"mode": str("host"), "destination": str("10.0.0.5")}, "site_ids"},
		{"host on several sites", map[string]tftypes.Value{"mode": str("host"), "site_ids": numSet(1, 2), "destination": str("10.0.0.5")}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := resource.ValidateConfigRequest{Config: tfsdk.Config{Schema: s, Raw: siteResourceRaw(t, s, tc.set)}}
			var resp resource.ValidateConfigResponse
			(&SitePrivateResource{}).ValidateConfig(context.Background(), req, &resp)
			if tc.wantError == "" {
				if resp.Diagnostics.HasError() {
					t.Errorf("unexpected error: %v", resp.Diagnostics)
				}
				return
			}
			found := false
			for _, d := range resp.Diagnostics.Errors() {
				if dp, ok := d.(diag.DiagnosticWithPath); ok && dp.Path().Equal(path.Root(tc.wantError)) {
					found = true
				}
			}
			if !found {
				t.Errorf("no error on %s: %v", tc.wantError, resp.Diagnostics)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// ModifyPlan
// -----------------------------------------------------------------------------

func TestSiteResourceModifyPlan(t *testing.T) {
	s := siteResourceSchema(t)
	ctx := context.Background()
	str := func(v string) tftypes.Value { return tftypes.NewValue(tftypes.String, v) }
	num := func(v int64) tftypes.Value { return tftypes.NewValue(tftypes.Number, v) }
	unknown := func(typ tftypes.Type) tftypes.Value { return tftypes.NewValue(typ, tftypes.UnknownValue) }
	setType := tftypes.Set{ElementType: tftypes.Number}
	numSet := func(ids ...int64) tftypes.Value {
		vals := make([]tftypes.Value, 0, len(ids))
		for _, id := range ids {
			vals = append(vals, num(id))
		}
		return tftypes.NewValue(setType, vals)
	}

	run := func(t *testing.T, config, plan map[string]tftypes.Value) SitePrivateResourceModel {
		t.Helper()
		// The proposed plan holds the config values, with the computed
		// attributes left unset in config unknown (create).
		proposed := map[string]tftypes.Value{
			"site_id": unknown(tftypes.Number), "site_ids": unknown(setType),
			"tcp_port_range": unknown(tftypes.String), "udp_port_range": unknown(tftypes.String),
			"disable_icmp": unknown(tftypes.Bool), "destination_port": unknown(tftypes.Number),
			"ai_provider_ids": unknown(setType),
		}
		for k, v := range config {
			proposed[k] = v
		}
		for k, v := range plan {
			proposed[k] = v
		}
		req := resource.ModifyPlanRequest{
			Config: tfsdk.Config{Schema: s, Raw: siteResourceRaw(t, s, config)},
			Plan:   tfsdk.Plan{Schema: s, Raw: siteResourceRaw(t, s, proposed)},
		}
		resp := resource.ModifyPlanResponse{Plan: req.Plan}
		(&SitePrivateResource{}).ModifyPlan(ctx, req, &resp)
		if resp.Diagnostics.HasError() {
			t.Fatalf("ModifyPlan: %v", resp.Diagnostics)
		}
		var got SitePrivateResourceModel
		if d := resp.Plan.Get(ctx, &got); d.HasError() {
			t.Fatalf("get plan: %v", d)
		}
		return got
	}

	t.Run("site_ids drives site_id", func(t *testing.T) {
		got := run(t, map[string]tftypes.Value{"mode": str("host"), "site_ids": numSet(3)}, nil)
		if !got.SiteID.Equal(types.Int64Value(3)) {
			t.Errorf("site_id = %v", got.SiteID)
		}
		got = run(t, map[string]tftypes.Value{"mode": str("host"), "site_ids": numSet(3, 4)}, nil)
		if !got.SiteID.IsNull() {
			t.Errorf("site_id with two sites = %v", got.SiteID)
		}
	})
	t.Run("site_id drives site_ids", func(t *testing.T) {
		got := run(t, map[string]tftypes.Value{"mode": str("host"), "site_id": num(8)}, nil)
		if !got.SiteIDs.Equal(int64Set(8)) {
			t.Errorf("site_ids = %v", got.SiteIDs)
		}
	})
	t.Run("ssh forced values", func(t *testing.T) {
		got := run(t, map[string]tftypes.Value{"mode": str("ssh"), "site_ids": numSet(1), "destination_port": num(2222)}, nil)
		if got.TCPPortRange.ValueString() != "2222" || got.UDPPortRange.ValueString() != "" || !got.DisableICMP.ValueBool() {
			t.Errorf("plan = tcp %v udp %v icmp %v", got.TCPPortRange, got.UDPPortRange, got.DisableICMP)
		}
		got = run(t, map[string]tftypes.Value{"mode": str("ssh"), "site_ids": numSet(1)}, nil)
		if got.TCPPortRange.ValueString() != "22" {
			t.Errorf("tcp without destination_port = %v", got.TCPPortRange)
		}
		if !got.AIProviderIDs.IsNull() {
			t.Errorf("ai_provider_ids outside inference = %v", got.AIProviderIDs)
		}
	})
	t.Run("ssh after destination_port is removed from config keeps the planned port", func(t *testing.T) {
		got := run(t, map[string]tftypes.Value{"mode": str("ssh"), "site_ids": numSet(1)},
			map[string]tftypes.Value{"destination_port": num(2200)})
		if got.TCPPortRange.ValueString() != "2200" {
			t.Errorf("tcp = %v", got.TCPPortRange)
		}
	})
	t.Run("inference", func(t *testing.T) {
		got := run(t, map[string]tftypes.Value{"mode": str("inference")}, nil)
		if !got.SiteIDs.Equal(int64Set()) || !got.SiteID.IsNull() {
			t.Errorf("sites = %v / %v", got.SiteIDs, got.SiteID)
		}
		if got.TCPPortRange.ValueString() != httpTCPPortRange || !got.DisableICMP.ValueBool() {
			t.Errorf("forced = %v / %v", got.TCPPortRange, got.DisableICMP)
		}
		if !got.AIProviderIDs.IsUnknown() {
			t.Errorf("ai_provider_ids should stay unknown until read: %v", got.AIProviderIDs)
		}
	})
	t.Run("host keeps unknown port ranges", func(t *testing.T) {
		got := run(t, map[string]tftypes.Value{"mode": str("host"), "site_ids": numSet(1)}, nil)
		if !got.TCPPortRange.IsUnknown() {
			t.Errorf("tcp = %v", got.TCPPortRange)
		}
	})
}

// -----------------------------------------------------------------------------
// Create against a stub server
// -----------------------------------------------------------------------------

type stubCall struct {
	method, path string
	body         map[string]json.RawMessage
}

func newSiteResourceStub(t *testing.T, siteResource map[string]any, providers []map[string]any) (*client.Client, *[]stubCall) {
	t.Helper()
	var calls []stubCall
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := stubCall{method: r.Method, path: r.URL.Path}
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&c.body)
		}
		calls = append(calls, c)
		var data any = siteResource
		switch {
		case strings.HasSuffix(r.URL.Path, "/ai-providers") && r.Method == http.MethodGet:
			data = map[string]any{"providers": providers}
		case strings.HasSuffix(r.URL.Path, "/ai-providers"):
			data = map[string]any{}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data, "success": true, "error": false, "message": "ok", "status": 200})
	}))
	t.Cleanup(srv.Close)
	return client.NewClient(srv.URL, "k", "org"), &calls
}

func siteResourceEmptyState(t *testing.T) tfsdk.State {
	t.Helper()
	s := siteResourceSchema(t)
	return tfsdk.State{Schema: s, Raw: tftypes.NewValue(s.Type().TerraformType(context.Background()), nil)}
}

func TestSiteResourceCreate_DisabledAndInference(t *testing.T) {
	ctx := context.Background()
	s := siteResourceSchema(t)
	c, calls := newSiteResourceStub(t,
		map[string]any{"siteResourceId": 12, "niceId": "llm", "name": "llm", "mode": "inference", "enabled": false,
			"tcpPortRangeString": "443,80", "udpPortRangeString": "", "disableIcmp": true},
		[]map[string]any{{"providerId": 4, "accessMode": "inherit", "enabled": true}})

	plan := SitePrivateResourceModel{
		ID: types.Int64Unknown(), NiceID: types.StringUnknown(), Name: types.StringValue("llm"),
		Mode: types.StringValue("inference"), SiteID: types.Int64Null(), SiteIDs: int64Set(),
		AIProviderIDs: int64Set(4), Enabled: types.BoolValue(false), SSL: types.BoolUnknown(),
		Destination: types.StringUnknown(), Alias: types.StringUnknown(),
		TCPPortRange: types.StringValue("443,80"), UDPPortRange: types.StringValue(""), DisableICMP: types.BoolValue(true),
		AuthDaemonMode: types.StringValue("site"), AuthDaemonPort: types.Int64Unknown(), PamMode: types.StringUnknown(),
		NetworkID: types.Int64Unknown(), DefaultNetworkID: types.Int64Unknown(), Scheme: types.StringUnknown(),
		ProxyPort: types.Int64Unknown(), DestinationPort: types.Int64Unknown(), AliasAddress: types.StringUnknown(),
		DomainID: types.StringUnknown(), Subdomain: types.StringUnknown(), FullDomain: types.StringUnknown(),
	}
	tfPlan := tfsdk.Plan{Schema: s}
	if d := tfPlan.Set(ctx, &plan); d.HasError() {
		t.Fatalf("plan: %v", d)
	}
	resp := resource.CreateResponse{State: siteResourceEmptyState(t)}
	(&SitePrivateResource{client: c}).Create(ctx, resource.CreateRequest{Plan: tfPlan, Config: tfsdk.Config{Schema: s, Raw: tfPlan.Raw}}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Create: %v", resp.Diagnostics)
	}

	if len(*calls) != 3 {
		t.Fatalf("calls = %+v", *calls)
	}
	create, update, list := (*calls)[0], (*calls)[1], (*calls)[2]
	if create.method != http.MethodPut || string(create.body["aiProviders"]) != `[{"providerId":4}]` {
		t.Errorf("create = %+v", create)
	}
	if _, ok := create.body["siteId"]; ok {
		t.Errorf("inference create must not send a site")
	}
	if update.method != http.MethodPost || update.path != "/v1/site-resource/12" || string(update.body["enabled"]) != "false" {
		t.Errorf("update = %+v", update)
	}
	if list.method != http.MethodGet || list.path != "/v1/site-resource/12/ai-providers" {
		t.Errorf("list = %+v", list)
	}

	var got SitePrivateResourceModel
	if d := resp.State.Get(ctx, &got); d.HasError() {
		t.Fatalf("state: %v", d)
	}
	if got.Enabled.ValueBool() || !got.AIProviderIDs.Equal(int64Set(4)) || !got.SiteIDs.Equal(int64Set()) || !got.SiteID.IsNull() {
		t.Errorf("state = enabled %v providers %v sites %v / %v", got.Enabled, got.AIProviderIDs, got.SiteIDs, got.SiteID)
	}
}

func TestSiteResourceUpdate_AIProvidersKeepAccessMode(t *testing.T) {
	ctx := context.Background()
	s := siteResourceSchema(t)
	c, calls := newSiteResourceStub(t,
		map[string]any{"siteResourceId": 12, "name": "llm", "mode": "inference", "enabled": true},
		[]map[string]any{{"providerId": 4, "accessMode": "select", "enabled": false}})

	model := func(providers types.Set) SitePrivateResourceModel {
		return SitePrivateResourceModel{
			ID: types.Int64Value(12), NiceID: types.StringValue("llm"), Name: types.StringValue("llm"),
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
	prior, planned := model(int64Set(4)), model(int64Set(4, 6))
	tfState, tfPlan := tfsdk.State{Schema: s}, tfsdk.Plan{Schema: s}
	tfState.Set(ctx, &prior)
	tfPlan.Set(ctx, &planned)
	resp := resource.UpdateResponse{State: tfState}
	(&SitePrivateResource{client: c}).Update(ctx, resource.UpdateRequest{Plan: tfPlan, State: tfState, Config: tfsdk.Config{Schema: s, Raw: tfPlan.Raw}}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Update: %v", resp.Diagnostics)
	}
	var set *stubCall
	for i := range *calls {
		if (*calls)[i].method == http.MethodPost && (*calls)[i].path == "/v1/site-resource/12/ai-providers" {
			set = &(*calls)[i]
		}
	}
	if set == nil {
		t.Fatalf("no set call: %+v", *calls)
	}
	if got := string(set.body["providers"]); got != `[{"providerId":4,"accessMode":"select","enabled":false},{"providerId":6}]` {
		t.Errorf("providers = %s", got)
	}
}
