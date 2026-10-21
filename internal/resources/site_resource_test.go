package resources

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/stackopshq/terraform-provider-pangolin/internal/client"
)

// -----------------------------------------------------------------------------
// hydrateSiteResourceState: wire -> state
// -----------------------------------------------------------------------------

func TestHydrateSiteResourceState_HTTPMode_ListResponse(t *testing.T) {
	// GET /org/{org}/site-resources (LIST) shape: carries SiteIDs, the
	// scalars are all populated. HTTP mode also fills scheme /
	// domainId / subdomain / fullDomain / destinationPort.
	siteRes := &client.SiteResource{
		SiteResourceID:   17,
		OrgID:            "org1",
		NiceID:           "nice-17",
		Name:             "http-svc",
		Mode:             "http",
		Destination:      "backend.local",
		Alias:            "",
		TCPPortRange:     "443,80",
		UDPPortRange:     "",
		DisableICMP:      true,
		AuthDaemonPort:   0,
		AuthDaemonMode:   "site",
		PamMode:          "",
		Enabled:          true,
		SSL:              true,
		NetworkID:        3,
		Scheme:           strPtr("https"),
		ProxyPort:        intPtr(8443),
		DestinationPort:  intPtr(8080),
		AliasAddress:     nil,
		DomainID:         strPtr("dom-1"),
		Subdomain:        strPtr("api"),
		FullDomain:       strPtr("api.example.com"),
		DefaultNetworkID: intPtr(2),
		SiteIDs:          []int{5, 6},
	}
	m := SitePrivateResourceModel{}
	hydrateSiteResourceState(&m, siteRes)

	if got := m.NiceID.ValueString(); got != "nice-17" {
		t.Errorf("NiceID = %q", got)
	}
	if got := m.Name.ValueString(); got != "http-svc" {
		t.Errorf("Name = %q", got)
	}
	if got := m.Mode.ValueString(); got != "http" {
		t.Errorf("Mode = %q", got)
	}
	if got := m.Destination.ValueString(); got != "backend.local" {
		t.Errorf("Destination = %q", got)
	}
	if got := m.Alias.ValueString(); got != "" {
		t.Errorf("Alias = %q, want empty", got)
	}
	if got := m.TCPPortRange.ValueString(); got != "443,80" {
		t.Errorf("TCPPortRange = %q", got)
	}
	if !m.DisableICMP.ValueBool() {
		t.Errorf("DisableICMP expected true")
	}
	if !m.Enabled.ValueBool() || !m.SSL.ValueBool() {
		t.Errorf("Enabled/SSL expected true")
	}
	if got := m.NetworkID.ValueInt64(); got != 3 {
		t.Errorf("NetworkID = %d", got)
	}
	if got := m.DefaultNetworkID.ValueInt64(); got != 2 {
		t.Errorf("DefaultNetworkID = %d", got)
	}
	// SiteID derived from SiteIDs[0].
	if got := m.SiteID.ValueInt64(); got != 5 {
		t.Errorf("SiteID = %d, want 5 (SiteIDs[0])", got)
	}
	if got := m.Scheme.ValueString(); got != "https" {
		t.Errorf("Scheme = %q", got)
	}
	if !m.AliasAddress.IsNull() {
		t.Errorf("AliasAddress expected null")
	}
	if got := m.DomainID.ValueString(); got != "dom-1" {
		t.Errorf("DomainID = %q", got)
	}
	if got := m.Subdomain.ValueString(); got != "api" {
		t.Errorf("Subdomain = %q", got)
	}
	if got := m.FullDomain.ValueString(); got != "api.example.com" {
		t.Errorf("FullDomain = %q", got)
	}
	if got := m.ProxyPort.ValueInt64(); got != 8443 {
		t.Errorf("ProxyPort = %d", got)
	}
	if got := m.DestinationPort.ValueInt64(); got != 8080 {
		t.Errorf("DestinationPort = %d", got)
	}
	if !m.PamMode.IsNull() {
		t.Errorf("PamMode expected null (empty on wire)")
	}
}

func TestHydrateSiteResourceState_L4Mode_CreateResponse(t *testing.T) {
	// CREATE / UPDATE shape: no SiteIDs slice, so state.SiteID stays
	// at its plan value (we simulate a plan-populated value here).
	siteRes := &client.SiteResource{
		SiteResourceID: 42,
		OrgID:          "org1",
		NiceID:         "nice-42",
		Name:           "tcp-svc",
		Mode:           "cidr",
		Destination:    "10.0.0.0/24",
		Alias:          "svc.internal",
		TCPPortRange:   "8000-8100",
		UDPPortRange:   "",
		DisableICMP:    false,
		AuthDaemonPort: 9000,
		AuthDaemonMode: "remote",
		PamMode:        "push",
		Enabled:        true,
		SSL:            false,
		NetworkID:      3,
	}
	// SiteID = 77 comes from the plan; hydrate must NOT overwrite it
	// when the response has no SiteIDs.
	m := SitePrivateResourceModel{SiteID: types.Int64Value(77)}
	hydrateSiteResourceState(&m, siteRes)

	if got := m.SiteID.ValueInt64(); got != 77 {
		t.Errorf("SiteID overwritten: got %d, want 77 (preserved from plan)", got)
	}
	if got := m.Mode.ValueString(); got != "cidr" {
		t.Errorf("Mode = %q", got)
	}
	if got := m.Alias.ValueString(); got != "svc.internal" {
		t.Errorf("Alias = %q", got)
	}
	if got := m.TCPPortRange.ValueString(); got != "8000-8100" {
		t.Errorf("TCPPortRange = %q", got)
	}
	if got := m.PamMode.ValueString(); got != "push" {
		t.Errorf("PamMode = %q", got)
	}
	if got := m.AuthDaemonMode.ValueString(); got != "remote" {
		t.Errorf("AuthDaemonMode = %q", got)
	}
	if got := m.AuthDaemonPort.ValueInt64(); got != 9000 {
		t.Errorf("AuthDaemonPort = %d", got)
	}
	// L4 mode leaves http-specific fields null.
	if !m.Scheme.IsNull() {
		t.Errorf("Scheme expected null for cidr mode")
	}
	if !m.DomainID.IsNull() || !m.Subdomain.IsNull() || !m.FullDomain.IsNull() {
		t.Errorf("HTTP-only fields expected null for cidr mode")
	}
	if !m.ProxyPort.IsNull() {
		t.Errorf("ProxyPort expected null")
	}
	if !m.DestinationPort.IsNull() {
		t.Errorf("DestinationPort expected null")
	}
	if !m.DefaultNetworkID.IsNull() {
		t.Errorf("DefaultNetworkID expected null")
	}
	if !m.AliasAddress.IsNull() {
		t.Errorf("AliasAddress expected null")
	}
}

func TestHydrateSiteResourceState_HostMode_AllNullPointers(t *testing.T) {
	// Pre-1.19 or minimal `host` mode payload: every optional pointer
	// is nil on the wire. State must surface those as TF null.
	siteRes := &client.SiteResource{
		SiteResourceID: 3,
		NiceID:         "nice-3",
		Name:           "host-svc",
		Mode:           "host",
		Destination:    "host.internal",
		Alias:          "svc",
		Enabled:        true,
	}
	m := SitePrivateResourceModel{}
	hydrateSiteResourceState(&m, siteRes)
	for name, v := range map[string]bool{
		"Scheme":           m.Scheme.IsNull(),
		"AliasAddress":     m.AliasAddress.IsNull(),
		"DomainID":         m.DomainID.IsNull(),
		"Subdomain":        m.Subdomain.IsNull(),
		"FullDomain":       m.FullDomain.IsNull(),
		"ProxyPort":        m.ProxyPort.IsNull(),
		"DestinationPort":  m.DestinationPort.IsNull(),
		"DefaultNetworkID": m.DefaultNetworkID.IsNull(),
		"PamMode":          m.PamMode.IsNull(),
	} {
		if !v {
			t.Errorf("%s expected null", name)
		}
	}
}

// -----------------------------------------------------------------------------
// Round-trip via JSON to catch struct-tag drift between wire and Go.
// -----------------------------------------------------------------------------

func TestSiteResource_JSONRoundTrip(t *testing.T) {
	// Emulate a full LIST payload the way Pangolin emits it: notice
	// tcpPortRangeString / udpPortRangeString / disableIcmp / etc.
	raw := `{
		"siteResourceId": 17,
		"orgId": "org1",
		"niceId": "nice-17",
		"name": "http-svc",
		"mode": "http",
		"destination": "backend.local",
		"alias": "",
		"tcpPortRangeString": "443,80",
		"udpPortRangeString": "",
		"disableIcmp": true,
		"authDaemonPort": 0,
		"authDaemonMode": "site",
		"pamMode": "",
		"enabled": true,
		"ssl": true,
		"networkId": 3,
		"scheme": "https",
		"proxyPort": 8443,
		"destinationPort": 8080,
		"aliasAddress": null,
		"domainId": "dom-1",
		"subdomain": "api",
		"fullDomain": "api.example.com",
		"defaultNetworkId": 2,
		"siteIds": [5,6],
		"siteNames": ["a","b"],
		"siteNiceIds": ["s-a","s-b"],
		"siteAddresses": ["1.1.1.1","2.2.2.2"],
		"siteOnlines": [true,false]
	}`
	var sr client.SiteResource
	if err := json.Unmarshal([]byte(raw), &sr); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if sr.TCPPortRange != "443,80" {
		t.Errorf("TCPPortRange tag broken: %q", sr.TCPPortRange)
	}
	if sr.UDPPortRange != "" {
		t.Errorf("UDPPortRange tag broken: %q", sr.UDPPortRange)
	}
	if !sr.DisableICMP {
		t.Errorf("DisableICMP tag broken")
	}
	if len(sr.SiteIDs) != 2 || sr.SiteIDs[0] != 5 {
		t.Errorf("SiteIDs tag broken: %v", sr.SiteIDs)
	}

	m := SitePrivateResourceModel{}
	hydrateSiteResourceState(&m, &sr)
	if m.SiteID.ValueInt64() != 5 {
		t.Errorf("SiteID from list not derived: %d", m.SiteID.ValueInt64())
	}
	if m.Scheme.ValueString() != "https" {
		t.Errorf("Scheme lost across round-trip")
	}
	if m.FullDomain.ValueString() != "api.example.com" {
		t.Errorf("FullDomain lost across round-trip")
	}
}

// -----------------------------------------------------------------------------
// gateway mode (Pangolin 1.24+)
// -----------------------------------------------------------------------------

// siteResourceSchema returns the resource schema, for the tests that
// build a tfsdk.Config / tfsdk.Plan by hand.
func siteResourceSchema(t *testing.T) schema.Schema {
	t.Helper()
	var resp resource.SchemaResponse
	(&SitePrivateResource{}).Schema(context.Background(), resource.SchemaRequest{}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("schema: %v", resp.Diagnostics)
	}
	return resp.Schema
}

// siteResourceRaw builds an object value of the resource type where
// every attribute is null except the given ones.
func siteResourceRaw(t *testing.T, s schema.Schema, set map[string]tftypes.Value) tftypes.Value {
	t.Helper()
	objType, ok := s.Type().TerraformType(context.Background()).(tftypes.Object)
	if !ok {
		t.Fatalf("schema type is not an object")
	}
	vals := make(map[string]tftypes.Value, len(objType.AttributeTypes))
	for name, typ := range objType.AttributeTypes {
		if v, ok := set[name]; ok {
			vals[name] = v
			continue
		}
		vals[name] = tftypes.NewValue(typ, nil)
	}
	return tftypes.NewValue(objType, vals)
}

func TestBuildCreateSiteResourceRequest_GatewayOmitsForcedFields(t *testing.T) {
	plan := SitePrivateResourceModel{
		Name:         types.StringValue("exit"),
		SiteID:       types.Int64Value(4),
		Mode:         types.StringValue("gateway"),
		Destination:  types.StringValue("0.0.0.0/0"),
		TCPPortRange: types.StringValue("*"),
		UDPPortRange: types.StringValue("*"),
		DisableICMP:  types.BoolValue(false),
	}
	raw, err := json.Marshal(buildCreateSiteResourceRequest(plan))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if string(body["mode"]) != `"gateway"` {
		t.Errorf("mode = %s", body["mode"])
	}
	for _, k := range []string{"destination", "tcpPortRangeString", "udpPortRangeString", "disableIcmp", "alias"} {
		if v, ok := body[k]; ok {
			t.Errorf("%s must be omitted in gateway mode, got %s", k, v)
		}
	}
	if string(body["siteId"]) != "4" {
		t.Errorf("siteId = %s", body["siteId"])
	}
}

func TestBuildCreateSiteResourceRequest_HostModeUnchanged(t *testing.T) {
	// Non-gateway modes keep the historical create payload: mode,
	// destination, alias, port ranges and ICMP from the plan.
	plan := SitePrivateResourceModel{
		Name:         types.StringValue("db"),
		SiteID:       types.Int64Value(4),
		Mode:         types.StringValue("host"),
		Destination:  types.StringValue("10.0.0.5"),
		Alias:        types.StringValue("db.internal"),
		TCPPortRange: types.StringValue("5432"),
		UDPPortRange: types.StringValue("53"),
		DisableICMP:  types.BoolValue(true),
	}
	raw, err := json.Marshal(buildCreateSiteResourceRequest(plan))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	want := map[string]string{
		"mode":               `"host"`,
		"destination":        `"10.0.0.5"`,
		"alias":              `"db.internal"`,
		"tcpPortRangeString": `"5432"`,
		"udpPortRangeString": `"53"`,
		"disableIcmp":        "true",
	}
	for k, v := range want {
		if string(body[k]) != v {
			t.Errorf("%s = %s, want %s (payload %s)", k, body[k], v, raw)
		}
	}
}

func TestBuildUpdateSiteResourceRequest_GatewaySendsMode(t *testing.T) {
	plan := SitePrivateResourceModel{
		ID:           types.Int64Value(9),
		Name:         types.StringValue("exit-renamed"),
		SiteID:       types.Int64Value(4),
		Mode:         types.StringValue("gateway"),
		Destination:  types.StringValue("0.0.0.0/0"),
		TCPPortRange: types.StringValue("*"),
		UDPPortRange: types.StringValue("*"),
	}
	got := buildUpdateSiteResourceRequest(plan)
	if got.Mode != "gateway" {
		t.Errorf("Mode = %q, want gateway", got.Mode)
	}
	raw, _ := json.Marshal(got)
	if strings.Contains(string(raw), `"destination"`) || strings.Contains(string(raw), "PortRangeString") {
		t.Errorf("payload %s must leave the forced fields to the server", raw)
	}
}

func TestBuildUpdateSiteResourceRequest_HostModeUnchanged(t *testing.T) {
	// Non-gateway modes keep the historical payload: no mode key,
	// destination and port ranges from the plan.
	plan := SitePrivateResourceModel{
		Name:         types.StringValue("db"),
		SiteID:       types.Int64Value(4),
		Mode:         types.StringValue("host"),
		Destination:  types.StringValue("10.0.0.5"),
		Alias:        types.StringValue("db.internal"),
		TCPPortRange: types.StringValue("5432"),
		UDPPortRange: types.StringValue(""),
	}
	raw, _ := json.Marshal(buildUpdateSiteResourceRequest(plan))
	var body map[string]json.RawMessage
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := body["mode"]; ok {
		t.Errorf("mode must not be sent outside gateway mode: %s", raw)
	}
	if string(body["destination"]) != `"10.0.0.5"` || string(body["tcpPortRangeString"]) != `"5432"` {
		t.Errorf("payload = %s", raw)
	}
}

func TestHydrateSiteResourceState_GatewayListResponse(t *testing.T) {
	// LIST shape of a gateway resource as stored by a 1.24 server.
	siteRes := &client.SiteResource{
		SiteResourceID: 30,
		NiceID:         "exit-node",
		Name:           "exit",
		Mode:           "gateway",
		Destination:    "0.0.0.0/0",
		TCPPortRange:   "*",
		UDPPortRange:   "*",
		DisableICMP:    false,
		AuthDaemonMode: "site",
		Enabled:        true,
		NetworkID:      12,
		SiteIDs:        []int{4},
	}
	m := SitePrivateResourceModel{}
	hydrateSiteResourceState(&m, siteRes)
	if m.Mode.ValueString() != "gateway" || m.Destination.ValueString() != "0.0.0.0/0" {
		t.Errorf("Mode/Destination = %q / %q", m.Mode.ValueString(), m.Destination.ValueString())
	}
	if m.TCPPortRange.ValueString() != "*" || m.UDPPortRange.ValueString() != "*" || m.DisableICMP.ValueBool() {
		t.Errorf("forced fields = %q %q %v", m.TCPPortRange.ValueString(), m.UDPPortRange.ValueString(), m.DisableICMP.ValueBool())
	}
	if m.SiteID.ValueInt64() != 4 {
		t.Errorf("SiteID = %d", m.SiteID.ValueInt64())
	}
}

func TestSiteResourceValidateConfig(t *testing.T) {
	s := siteResourceSchema(t)
	str := func(v string) tftypes.Value { return tftypes.NewValue(tftypes.String, v) }
	cases := []struct {
		name      string
		set       map[string]tftypes.Value
		wantError string
	}{
		{"gateway without destination", map[string]tftypes.Value{"mode": str("gateway")}, ""},
		{"gateway with the forced values spelled out", map[string]tftypes.Value{
			"mode": str("gateway"), "destination": str("0.0.0.0/0"),
			"tcp_port_range": str("*"), "udp_port_range": str("*"),
			"disable_icmp": tftypes.NewValue(tftypes.Bool, false),
		}, ""},
		{"gateway with another destination", map[string]tftypes.Value{"mode": str("gateway"), "destination": str("10.0.0.0/8")}, "destination"},
		{"gateway with a port range", map[string]tftypes.Value{"mode": str("gateway"), "tcp_port_range": str("443")}, "tcp_port_range"},
		{"gateway with icmp disabled", map[string]tftypes.Value{"mode": str("gateway"), "disable_icmp": tftypes.NewValue(tftypes.Bool, true)}, "disable_icmp"},
		{"host without destination", map[string]tftypes.Value{"mode": str("host")}, "destination"},
		{"cidr with destination", map[string]tftypes.Value{"mode": str("cidr"), "destination": str("10.0.0.0/24")}, ""},
		{"unknown mode is not checked", map[string]tftypes.Value{"mode": tftypes.NewValue(tftypes.String, tftypes.UnknownValue)}, ""},
		{"gateway with an unknown destination is checked at apply", map[string]tftypes.Value{
			"mode": str("gateway"), "destination": tftypes.NewValue(tftypes.String, tftypes.UnknownValue),
		}, ""},
		{"unsupported mode is left to the OneOf validator", map[string]tftypes.Value{"mode": str("ssh")}, ""},
		{"gateway with an alias", map[string]tftypes.Value{"mode": str("gateway"), "alias": str("exit.internal")}, "alias"},
		{"gateway with a domain", map[string]tftypes.Value{"mode": str("gateway"), "domain_id": str("dom")}, "domain_id"},
		{"gateway with a subdomain", map[string]tftypes.Value{"mode": str("gateway"), "subdomain": str("exit")}, "subdomain"},
		{"gateway with a scheme", map[string]tftypes.Value{"mode": str("gateway"), "scheme": str("https")}, "scheme"},
		{"gateway with a destination port", map[string]tftypes.Value{"mode": str("gateway"), "destination_port": tftypes.NewValue(tftypes.Number, 443)}, "destination_port"},
		{"gateway with unknown unused attributes is checked at apply", map[string]tftypes.Value{
			"mode": str("gateway"), "alias": tftypes.NewValue(tftypes.String, tftypes.UnknownValue),
			"destination_port": tftypes.NewValue(tftypes.Number, tftypes.UnknownValue),
		}, ""},
		{"host keeps its alias", map[string]tftypes.Value{"mode": str("host"), "destination": str("10.0.0.5"), "alias": str("db.internal")}, ""},
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
			if !resp.Diagnostics.HasError() {
				t.Fatalf("expected an error on %s", tc.wantError)
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

func TestGatewayForcedPlanModifiers(t *testing.T) {
	s := siteResourceSchema(t)
	ctx := context.Background()
	plan := func(mode tftypes.Value) tfsdk.Plan {
		return tfsdk.Plan{Schema: s, Raw: siteResourceRaw(t, s, map[string]tftypes.Value{"mode": mode})}
	}
	gateway := tftypes.NewValue(tftypes.String, "gateway")

	// Gateway, attribute unset: the forced value is planned, even over
	// a drifted value UseStateForUnknown copied from the state.
	req := planmodifier.StringRequest{
		Plan:        plan(gateway),
		ConfigValue: types.StringNull(),
		PlanValue:   types.StringValue("10.0.0.5"),
	}
	resp := &planmodifier.StringResponse{PlanValue: req.PlanValue}
	gatewayForcedString(gatewayDestination).PlanModifyString(ctx, req, resp)
	if resp.Diagnostics.HasError() || resp.PlanValue.ValueString() != "0.0.0.0/0" {
		t.Errorf("gateway destination plan = %v (%v)", resp.PlanValue, resp.Diagnostics)
	}

	boolReq := planmodifier.BoolRequest{Plan: plan(gateway), ConfigValue: types.BoolNull(), PlanValue: types.BoolUnknown()}
	boolResp := &planmodifier.BoolResponse{PlanValue: boolReq.PlanValue}
	gatewayForcedBool(gatewayDisableICMP).PlanModifyBool(ctx, boolReq, boolResp)
	if boolResp.PlanValue.IsUnknown() || boolResp.PlanValue.ValueBool() {
		t.Errorf("gateway disable_icmp plan = %v", boolResp.PlanValue)
	}

	// Gateway with the attribute set (known or unknown) in the config:
	// the config value stands, ValidateConfig polices it.
	for _, cfg := range []types.String{types.StringValue("*"), types.StringUnknown()} {
		req := planmodifier.StringRequest{Plan: plan(gateway), ConfigValue: cfg, PlanValue: cfg}
		resp := &planmodifier.StringResponse{PlanValue: req.PlanValue}
		gatewayForcedString(gatewayPortRange).PlanModifyString(ctx, req, resp)
		if !resp.PlanValue.Equal(cfg) {
			t.Errorf("config %v: plan = %v, want the config value", cfg, resp.PlanValue)
		}
	}

	// Destroy: a null plan is left alone.
	destroy := planmodifier.StringRequest{
		Plan:        tfsdk.Plan{Schema: s, Raw: tftypes.NewValue(s.Type().TerraformType(ctx), nil)},
		ConfigValue: types.StringNull(),
		PlanValue:   types.StringNull(),
	}
	destroyResp := &planmodifier.StringResponse{PlanValue: destroy.PlanValue}
	gatewayForcedString(gatewayDestination).PlanModifyString(ctx, destroy, destroyResp)
	if destroyResp.Diagnostics.HasError() || !destroyResp.PlanValue.IsNull() {
		t.Errorf("destroy plan = %v (%v), want null", destroyResp.PlanValue, destroyResp.Diagnostics)
	}

	// Other modes, or an unknown mode: untouched.
	for _, mode := range []tftypes.Value{
		tftypes.NewValue(tftypes.String, "host"),
		tftypes.NewValue(tftypes.String, tftypes.UnknownValue),
	} {
		req := planmodifier.StringRequest{Plan: plan(mode), ConfigValue: types.StringNull(), PlanValue: types.StringUnknown()}
		resp := &planmodifier.StringResponse{PlanValue: req.PlanValue}
		gatewayForcedString(gatewayPortRange).PlanModifyString(ctx, req, resp)
		if !resp.PlanValue.IsUnknown() {
			t.Errorf("mode %v: plan = %v, want unknown", mode, resp.PlanValue)
		}
	}
}

func TestCreateSiteResourceErrorDetail(t *testing.T) {
	// The 1.23 body schema refuses the mode with a generic 400.
	old := errors.New(`API error (status 400): Validation error: Invalid option: expected one of "host"|"cidr"|"http"|"ssh"|"inference" at "mode"`)
	if got := createSiteResourceErrorDetail("gateway", old); !strings.Contains(got, "Pangolin 1.24") || !strings.HasPrefix(got, old.Error()) {
		t.Errorf("gateway 400: %q", got)
	}
	// No hint for another mode, or for a non-validation failure.
	if got := createSiteResourceErrorDetail("host", old); got != old.Error() {
		t.Errorf("host 400: %q", got)
	}
	// A 1.24 server refusing another field: no upgrade hint.
	otherField := errors.New(`API error (status 400): Validation error: Invalid input: expected number, received string at "siteIds[0]"`)
	if got := createSiteResourceErrorDetail("gateway", otherField); got != otherField.Error() {
		t.Errorf("gateway 400 on another field: %q", got)
	}
	conflict := errors.New("API error (status 409): Alias already in use by another site resource")
	if got := createSiteResourceErrorDetail("gateway", conflict); got != conflict.Error() {
		t.Errorf("gateway 409: %q", got)
	}
}
