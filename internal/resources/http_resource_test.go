package resources

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stackopshq/terraform-provider-pangolin/internal/client"
)

// strPtr / intPtr / boolPtr are tiny helpers so the test tables read
// like specs rather than pointer-taking incantations. Kept file-local
// (as unexported plain functions) so they don't leak into other tests.
func strPtr(s string) *string { return &s }
func intPtr(i int) *int       { return &i }
func boolPtr(b bool) *bool    { return &b }

// -----------------------------------------------------------------------------
// applyHTTPResourceResponse: wire -> state
// -----------------------------------------------------------------------------

func TestApplyHTTPResourceResponse_NominalHTTP(t *testing.T) {
	res := &client.Resource{
		ResourceID:               12,
		ResourceGuid:             "guid-abc",
		NiceID:                   "nice-abc",
		Name:                     "web",
		Subdomain:                "app",
		FullDomain:               "app.example.com",
		DomainID:                 "dom-1",
		Wildcard:                 true,
		Health:                   "healthy",
		Mode:                     "http",
		SSO:                      true,
		SSL:                      true,
		Enabled:                  true,
		BlockAccess:              false,
		EmailWhitelistEnabled:    true,
		ApplyRules:               true,
		StickySession:            true,
		TLSServerName:            strPtr("backend.svc"),
		SetHostHeader:            strPtr("app.internal"),
		EnableProxy:              boolPtr(true),
		SkipToIdpID:              intPtr(7),
		PostAuthPath:             strPtr("/dashboard"),
		ProxyProtocol:            boolPtr(true),
		ProxyProtocolVersion:     intPtr(2),
		MaintenanceModeEnabled:   boolPtr(true),
		MaintenanceModeType:      "planned",
		MaintenanceTitle:         strPtr("Down"),
		MaintenanceMessage:       strPtr("Back soon"),
		MaintenanceEstimatedTime: strPtr("2026-08-01T00:00:00Z"),
		ResourcePolicyID:         intPtr(42),
		DefaultResourcePolicyID:  intPtr(1),
		Headers: []client.ResourceHeader{
			{Name: "X-Env", Value: "prod"},
			{Name: "X-Team", Value: "core"},
		},
		HasHeadersKey: true,
	}

	m := applyHTTPResourceResponse(HTTPResourceModel{ID: types.Int64Value(12)}, res)

	if got := m.NiceID.ValueString(); got != "nice-abc" {
		t.Errorf("NiceID = %q, want nice-abc", got)
	}
	if got := m.Name.ValueString(); got != "web" {
		t.Errorf("Name = %q, want web", got)
	}
	if got := m.Subdomain.ValueString(); got != "app" {
		t.Errorf("Subdomain = %q, want app", got)
	}
	if got := m.FullDomain.ValueString(); got != "app.example.com" {
		t.Errorf("FullDomain = %q", got)
	}
	if got := m.DomainID.ValueString(); got != "dom-1" {
		t.Errorf("DomainID = %q", got)
	}
	if !m.SSO.ValueBool() || !m.SSL.ValueBool() || !m.Enabled.ValueBool() {
		t.Errorf("SSO/SSL/Enabled expected true, got %v/%v/%v",
			m.SSO.ValueBool(), m.SSL.ValueBool(), m.Enabled.ValueBool())
	}
	if m.BlockAccess.ValueBool() {
		t.Errorf("BlockAccess expected false")
	}
	if !m.EmailWhitelistEnabled.ValueBool() || !m.ApplyRules.ValueBool() || !m.StickySession.ValueBool() {
		t.Errorf("email/apply/sticky expected true")
	}
	if got := m.TLSServerName.ValueString(); got != "backend.svc" {
		t.Errorf("TLSServerName = %q", got)
	}
	if got := m.Mode.ValueString(); got != "http" {
		t.Errorf("Mode = %q", got)
	}
	if got := m.ResourceGuid.ValueString(); got != "guid-abc" {
		t.Errorf("ResourceGuid = %q", got)
	}
	if !m.Wildcard.ValueBool() {
		t.Errorf("Wildcard expected true")
	}
	if got := m.Health.ValueString(); got != "healthy" {
		t.Errorf("Health = %q", got)
	}
	if got := m.SetHostHeader.ValueString(); got != "app.internal" {
		t.Errorf("SetHostHeader = %q", got)
	}
	if !m.EnableProxy.ValueBool() {
		t.Errorf("EnableProxy expected true")
	}
	if got := m.SkipToIdpID.ValueInt64(); got != 7 {
		t.Errorf("SkipToIdpID = %d", got)
	}
	if got := m.PostAuthPath.ValueString(); got != "/dashboard" {
		t.Errorf("PostAuthPath = %q", got)
	}
	if !m.ProxyProtocol.ValueBool() {
		t.Errorf("ProxyProtocol expected true")
	}
	if got := m.ProxyProtocolVersion.ValueInt64(); got != 2 {
		t.Errorf("ProxyProtocolVersion = %d", got)
	}
	if !m.MaintenanceModeEnabled.ValueBool() {
		t.Errorf("MaintenanceModeEnabled expected true")
	}
	if got := m.MaintenanceModeType.ValueString(); got != "planned" {
		t.Errorf("MaintenanceModeType = %q", got)
	}
	if got := m.MaintenanceTitle.ValueString(); got != "Down" {
		t.Errorf("MaintenanceTitle = %q", got)
	}
	if got := m.MaintenanceMessage.ValueString(); got != "Back soon" {
		t.Errorf("MaintenanceMessage = %q", got)
	}
	if got := m.MaintenanceEstimatedTime.ValueString(); got != "2026-08-01T00:00:00Z" {
		t.Errorf("MaintenanceEstimatedTime = %q", got)
	}
	if got := m.ResourcePolicyID.ValueInt64(); got != 42 {
		t.Errorf("ResourcePolicyID = %d", got)
	}
	if got := m.DefaultResourcePolicyID.ValueInt64(); got != 1 {
		t.Errorf("DefaultResourcePolicyID = %d", got)
	}
	if len(m.Headers) != 2 {
		t.Fatalf("Headers len = %d, want 2", len(m.Headers))
	}
	if m.Headers[0].Name.ValueString() != "X-Env" || m.Headers[0].Value.ValueString() != "prod" {
		t.Errorf("Headers[0] = %+v", m.Headers[0])
	}
	if m.Headers[1].Name.ValueString() != "X-Team" || m.Headers[1].Value.ValueString() != "core" {
		t.Errorf("Headers[1] = %+v", m.Headers[1])
	}
}

func TestApplyHTTPResourceResponse_NilPointersAreNull(t *testing.T) {
	// A minimal Resource from a pre-1.19 server: no mode, no 1.19
	// scalars, no headers. The mapper should surface the nulls
	// consistently rather than crash or emit zero values.
	res := &client.Resource{
		ResourceID: 1,
		NiceID:     "nice-1",
		Name:       "min",
		DomainID:   "dom-1",
	}
	m := applyHTTPResourceResponse(HTTPResourceModel{ID: types.Int64Value(1)}, res)

	// Subdomain absent -> null
	if !m.Subdomain.IsNull() {
		t.Errorf("Subdomain expected null, got %v", m.Subdomain)
	}
	if !m.TLSServerName.IsNull() {
		t.Errorf("TLSServerName expected null")
	}
	// Mode defaults to "http" when server omits it (pre-1.19).
	if got := m.Mode.ValueString(); got != "http" {
		t.Errorf("Mode default = %q, want http", got)
	}
	if !m.PamMode.IsNull() {
		t.Errorf("PamMode expected null")
	}
	if !m.AuthDaemonMode.IsNull() {
		t.Errorf("AuthDaemonMode expected null")
	}
	if !m.ProxyPort.IsNull() {
		t.Errorf("ProxyPort expected null")
	}
	if !m.AuthDaemonPort.IsNull() {
		t.Errorf("AuthDaemonPort expected null")
	}
	if !m.SetHostHeader.IsNull() {
		t.Errorf("SetHostHeader expected null")
	}
	if !m.EnableProxy.IsNull() {
		t.Errorf("EnableProxy expected null")
	}
	if !m.SkipToIdpID.IsNull() {
		t.Errorf("SkipToIdpID expected null")
	}
	if !m.PostAuthPath.IsNull() {
		t.Errorf("PostAuthPath expected null")
	}
	if !m.ProxyProtocol.IsNull() {
		t.Errorf("ProxyProtocol expected null")
	}
	if !m.ProxyProtocolVersion.IsNull() {
		t.Errorf("ProxyProtocolVersion expected null")
	}
	if !m.MaintenanceModeEnabled.IsNull() {
		t.Errorf("MaintenanceModeEnabled expected null")
	}
	if !m.MaintenanceModeType.IsNull() {
		t.Errorf("MaintenanceModeType expected null")
	}
	if !m.MaintenanceTitle.IsNull() {
		t.Errorf("MaintenanceTitle expected null")
	}
	if !m.MaintenanceMessage.IsNull() {
		t.Errorf("MaintenanceMessage expected null")
	}
	if !m.MaintenanceEstimatedTime.IsNull() {
		t.Errorf("MaintenanceEstimatedTime expected null")
	}
	if !m.ResourcePolicyID.IsNull() {
		t.Errorf("ResourcePolicyID expected null")
	}
	if !m.DefaultResourcePolicyID.IsNull() {
		t.Errorf("DefaultResourcePolicyID expected null")
	}
	if !m.Health.IsNull() {
		t.Errorf("Health expected null")
	}
	// Headers: nothing on the wire -> nil slice (round-trips as null).
	if m.Headers != nil {
		t.Errorf("Headers expected nil, got %v", m.Headers)
	}
}

func TestApplyHTTPResourceResponse_L4TCPMode(t *testing.T) {
	res := &client.Resource{
		ResourceID:     55,
		NiceID:         "tcp-x",
		Name:           "bastion",
		Mode:           "tcp",
		ProxyPort:      intPtr(2222),
		PamMode:        "passthrough",
		AuthDaemonMode: "sidecar",
		AuthDaemonPort: intPtr(9000),
	}
	m := applyHTTPResourceResponse(HTTPResourceModel{ID: types.Int64Value(55)}, res)
	if got := m.Mode.ValueString(); got != "tcp" {
		t.Errorf("Mode = %q", got)
	}
	if got := m.ProxyPort.ValueInt64(); got != 2222 {
		t.Errorf("ProxyPort = %d", got)
	}
	if got := m.PamMode.ValueString(); got != "passthrough" {
		t.Errorf("PamMode = %q", got)
	}
	if got := m.AuthDaemonMode.ValueString(); got != "sidecar" {
		t.Errorf("AuthDaemonMode = %q", got)
	}
	if got := m.AuthDaemonPort.ValueInt64(); got != 9000 {
		t.Errorf("AuthDaemonPort = %d", got)
	}
}

// -----------------------------------------------------------------------------
// buildHTTPResourceUpdateRequest: state -> wire
// -----------------------------------------------------------------------------

func TestBuildHTTPResourceUpdateRequest_UnknownsOmitted(t *testing.T) {
	// A brand-new plan where every Optional+Computed field is
	// Unknown: the request must contain only the required Name and
	// leave every optional pointer nil so the server-side JSON stays
	// free of noise.
	plan := HTTPResourceModel{
		Name:                     types.StringValue("res"),
		Subdomain:                types.StringUnknown(),
		SSO:                      types.BoolUnknown(),
		SSL:                      types.BoolUnknown(),
		Enabled:                  types.BoolUnknown(),
		BlockAccess:              types.BoolUnknown(),
		EmailWhitelistEnabled:    types.BoolUnknown(),
		ApplyRules:               types.BoolUnknown(),
		StickySession:            types.BoolUnknown(),
		TLSServerName:            types.StringUnknown(),
		SetHostHeader:            types.StringUnknown(),
		SkipToIdpID:              types.Int64Unknown(),
		PostAuthPath:             types.StringUnknown(),
		MaintenanceModeEnabled:   types.BoolUnknown(),
		MaintenanceModeType:      types.StringUnknown(),
		MaintenanceTitle:         types.StringUnknown(),
		MaintenanceMessage:       types.StringUnknown(),
		MaintenanceEstimatedTime: types.StringUnknown(),
		PamMode:                  types.StringUnknown(),
		AuthDaemonMode:           types.StringUnknown(),
		AuthDaemonPort:           types.Int64Unknown(),
	}
	got := buildHTTPResourceUpdateRequest(plan)
	if got.Name != "res" {
		t.Errorf("Name = %q", got.Name)
	}
	if got.Subdomain != nil || got.SSO != nil || got.SSL != nil ||
		got.Enabled != nil || got.BlockAccess != nil ||
		got.EmailWhitelistEnabled != nil || got.ApplyRules != nil ||
		got.StickySession != nil || got.TLSServerName != nil {
		t.Errorf("legacy scalars must be nil when unknown, got %+v", got)
	}
	if got.SetHostHeader != nil || got.SkipToIdpID != nil ||
		got.PostAuthPath != nil || got.MaintenanceModeEnabled != nil ||
		got.MaintenanceTitle != nil || got.MaintenanceMessage != nil ||
		got.MaintenanceEstimatedTime != nil {
		t.Errorf("1.19+ pointer fields must be nil when unknown, got %+v", got)
	}
	if got.MaintenanceModeType != "" {
		t.Errorf("MaintenanceModeType = %q, want empty", got.MaintenanceModeType)
	}
	if got.PamMode != "" || got.AuthDaemonMode != "" || got.AuthDaemonPort != nil {
		t.Errorf("PAM/AuthDaemon fields must stay empty/nil when unknown")
	}
	if got.Headers != nil {
		t.Errorf("Headers must be nil when the plan slice is nil")
	}

	// Also confirm the JSON payload emits only `name`.
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(raw) != `{"name":"res"}` {
		t.Errorf("json payload = %s", raw)
	}
}

func TestBuildHTTPResourceUpdateRequest_NullClearsField(t *testing.T) {
	// A null tls_server_name / set_host_header must serialize as
	// the pointer being nil (= omit from body). buildHTTP treats
	// null explicitly as "clear the pointer".
	plan := HTTPResourceModel{
		Name:                     types.StringValue("res"),
		TLSServerName:            types.StringNull(),
		SetHostHeader:            types.StringNull(),
		SkipToIdpID:              types.Int64Null(),
		PostAuthPath:             types.StringNull(),
		MaintenanceTitle:         types.StringNull(),
		MaintenanceMessage:       types.StringNull(),
		MaintenanceEstimatedTime: types.StringNull(),
	}
	got := buildHTTPResourceUpdateRequest(plan)
	if got.TLSServerName != nil || got.SetHostHeader != nil || got.SkipToIdpID != nil ||
		got.PostAuthPath != nil || got.MaintenanceTitle != nil ||
		got.MaintenanceMessage != nil || got.MaintenanceEstimatedTime != nil {
		t.Errorf("null fields must map to nil pointer, got %+v", got)
	}
}

func TestBuildHTTPResourceUpdateRequest_FullPayload(t *testing.T) {
	plan := HTTPResourceModel{
		Name:                     types.StringValue("web"),
		Subdomain:                types.StringValue("app"),
		SSO:                      types.BoolValue(true),
		SSL:                      types.BoolValue(true),
		Enabled:                  types.BoolValue(true),
		BlockAccess:              types.BoolValue(false),
		EmailWhitelistEnabled:    types.BoolValue(true),
		ApplyRules:               types.BoolValue(false),
		StickySession:            types.BoolValue(true),
		TLSServerName:            types.StringValue("backend"),
		SetHostHeader:            types.StringValue("upstream.local"),
		SkipToIdpID:              types.Int64Value(3),
		PostAuthPath:             types.StringValue("/home"),
		MaintenanceModeEnabled:   types.BoolValue(true),
		MaintenanceModeType:      types.StringValue("planned"),
		MaintenanceTitle:         types.StringValue("down"),
		MaintenanceMessage:       types.StringValue("brb"),
		MaintenanceEstimatedTime: types.StringValue("later"),
		PamMode:                  types.StringValue("push"),
		AuthDaemonMode:           types.StringValue("remote"),
		AuthDaemonPort:           types.Int64Value(9001),
		Headers: []ResourceHeaderModel{
			{Name: types.StringValue("X-A"), Value: types.StringValue("1")},
		},
	}
	got := buildHTTPResourceUpdateRequest(plan)
	if got.Subdomain == nil || *got.Subdomain != "app" {
		t.Errorf("Subdomain = %v", got.Subdomain)
	}
	if got.SSO == nil || *got.SSO != true {
		t.Errorf("SSO = %v", got.SSO)
	}
	if got.TLSServerName == nil || *got.TLSServerName != "backend" {
		t.Errorf("TLSServerName = %v", got.TLSServerName)
	}
	if got.SetHostHeader == nil || *got.SetHostHeader != "upstream.local" {
		t.Errorf("SetHostHeader = %v", got.SetHostHeader)
	}
	if got.SkipToIdpID == nil || *got.SkipToIdpID != 3 {
		t.Errorf("SkipToIdpID = %v", got.SkipToIdpID)
	}
	if got.PostAuthPath == nil || *got.PostAuthPath != "/home" {
		t.Errorf("PostAuthPath = %v", got.PostAuthPath)
	}
	if got.MaintenanceModeEnabled == nil || *got.MaintenanceModeEnabled != true {
		t.Errorf("MaintenanceModeEnabled = %v", got.MaintenanceModeEnabled)
	}
	if got.MaintenanceModeType != "planned" {
		t.Errorf("MaintenanceModeType = %q", got.MaintenanceModeType)
	}
	if got.MaintenanceTitle == nil || *got.MaintenanceTitle != "down" {
		t.Errorf("MaintenanceTitle = %v", got.MaintenanceTitle)
	}
	if got.PamMode != "push" || got.AuthDaemonMode != "remote" {
		t.Errorf("PAM/AuthDaemon = %q / %q", got.PamMode, got.AuthDaemonMode)
	}
	if got.AuthDaemonPort == nil || *got.AuthDaemonPort != 9001 {
		t.Errorf("AuthDaemonPort = %v", got.AuthDaemonPort)
	}
	if got.Headers == nil || len(*got.Headers) != 1 || (*got.Headers)[0].Name != "X-A" ||
		(*got.Headers)[0].Value != "1" {
		t.Errorf("Headers = %+v", got.Headers)
	}
}

func TestBuildHTTPResourceUpdateRequest_EmptyHeadersListSent(t *testing.T) {
	// A non-nil-but-empty Headers slice from the plan must produce a
	// non-nil empty payload slice: it means "clear all injected
	// headers", not "leave alone". isNullList uses "slice is nil"
	// as the "leave alone" signal.
	plan := HTTPResourceModel{
		Name:    types.StringValue("res"),
		Headers: []ResourceHeaderModel{},
	}
	got := buildHTTPResourceUpdateRequest(plan)
	if got.Headers == nil {
		t.Fatalf("Headers pointer expected non-nil for empty list")
	}
	if len(*got.Headers) != 0 {
		t.Errorf("Headers slice len = %d, want 0", len(*got.Headers))
	}
}

// -----------------------------------------------------------------------------
// Round-trip: model -> request -> JSON -> Resource -> model
// -----------------------------------------------------------------------------

func TestHTTPResource_RoundTrip(t *testing.T) {
	initial := HTTPResourceModel{
		ID:                       types.Int64Value(99),
		Name:                     types.StringValue("web"),
		Subdomain:                types.StringValue("app"),
		SSO:                      types.BoolValue(true),
		SSL:                      types.BoolValue(false),
		Enabled:                  types.BoolValue(true),
		BlockAccess:              types.BoolValue(false),
		EmailWhitelistEnabled:    types.BoolValue(true),
		ApplyRules:               types.BoolValue(true),
		StickySession:            types.BoolValue(false),
		TLSServerName:            types.StringValue("svc"),
		SetHostHeader:            types.StringValue("upstream"),
		SkipToIdpID:              types.Int64Value(2),
		PostAuthPath:             types.StringValue("/dash"),
		MaintenanceModeEnabled:   types.BoolValue(false),
		MaintenanceModeType:      types.StringValue("planned"),
		MaintenanceTitle:         types.StringValue("t"),
		MaintenanceMessage:       types.StringValue("m"),
		MaintenanceEstimatedTime: types.StringValue("later"),
		Headers: []ResourceHeaderModel{
			{Name: types.StringValue("X-Env"), Value: types.StringValue("prod")},
		},
	}
	upd := buildHTTPResourceUpdateRequest(initial)
	// Emulate server: turn the update request into a Resource by
	// carrying the fields verbatim through JSON.
	raw, err := json.Marshal(upd)
	if err != nil {
		t.Fatalf("marshal update: %v", err)
	}
	var res client.Resource
	if err := json.Unmarshal(raw, &res); err != nil {
		t.Fatalf("unmarshal resource: %v", err)
	}
	// The server also always echoes the immutable id.
	res.ResourceID = 99
	res.NiceID = "nice"
	res.FullDomain = "app.example.com"
	res.DomainID = "dom"

	back := applyHTTPResourceResponse(HTTPResourceModel{ID: initial.ID}, &res)

	// Writable fields we care about must survive the round-trip.
	if back.Name.ValueString() != "web" {
		t.Errorf("Name lost")
	}
	if back.Subdomain.ValueString() != "app" {
		t.Errorf("Subdomain lost")
	}
	if !back.SSO.ValueBool() || back.SSL.ValueBool() {
		t.Errorf("SSO/SSL wrong: %v %v", back.SSO.ValueBool(), back.SSL.ValueBool())
	}
	if back.TLSServerName.ValueString() != "svc" {
		t.Errorf("TLSServerName lost")
	}
	if back.SetHostHeader.ValueString() != "upstream" {
		t.Errorf("SetHostHeader lost")
	}
	if back.SkipToIdpID.ValueInt64() != 2 {
		t.Errorf("SkipToIdpID lost")
	}
	if back.PostAuthPath.ValueString() != "/dash" {
		t.Errorf("PostAuthPath lost")
	}
	if back.MaintenanceModeType.ValueString() != "planned" {
		t.Errorf("MaintenanceModeType lost")
	}
	if len(back.Headers) != 1 || back.Headers[0].Name.ValueString() != "X-Env" {
		t.Errorf("Headers lost: %+v", back.Headers)
	}
}

// -----------------------------------------------------------------------------
// isNullList
// -----------------------------------------------------------------------------

func TestIsUnknownList(t *testing.T) {
	if !isNullList(nil) {
		t.Errorf("nil slice must count as unknown")
	}
	if isNullList([]ResourceHeaderModel{}) {
		t.Errorf("empty slice is not unknown - user explicitly cleared")
	}
	if isNullList([]ResourceHeaderModel{{Name: types.StringValue("a"), Value: types.StringValue("b")}}) {
		t.Errorf("populated slice is not unknown")
	}
}

// -----------------------------------------------------------------------------
// headers: removal and 1.24 `requestHeaders`
// -----------------------------------------------------------------------------

func TestClearRemovedHeaders(t *testing.T) {
	withHeaders := []ResourceHeaderModel{{Name: types.StringValue("X-A"), Value: types.StringValue("1")}}
	cases := []struct {
		name      string
		plan      []ResourceHeaderModel
		state     []ResourceHeaderModel
		wantSent  bool
		wantCount int
	}{
		{"removed from config clears", nil, withHeaders, true, 0},
		// Sent even with no prior headers, so headers added outside
		// Terraform are removed without a refresh (-refresh=false).
		{"absent with empty state still clears", nil, nil, true, 0},
		{"explicit empty list is sent as is", []ResourceHeaderModel{}, withHeaders, true, 0},
		{"configured list is sent as is", withHeaders, nil, true, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan := HTTPResourceModel{Name: types.StringValue("res"), Headers: tc.plan}
			state := HTTPResourceModel{Name: types.StringValue("res"), Headers: tc.state}
			req := buildHTTPResourceUpdateRequest(plan)
			clearRemovedHeaders(req, plan, state)
			if (req.Headers != nil) != tc.wantSent {
				t.Fatalf("Headers sent = %v, want %v", req.Headers != nil, tc.wantSent)
			}
			if tc.wantSent && len(*req.Headers) != tc.wantCount {
				t.Errorf("len(Headers) = %d, want %d", len(*req.Headers), tc.wantCount)
			}
			raw, err := json.Marshal(req)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if tc.wantSent && tc.wantCount == 0 && !strings.Contains(string(raw), `"headers":[]`) {
				t.Errorf("payload %s must carry an explicit empty headers list", raw)
			}
		})
	}
}

func TestApplyHTTPResourceResponse_ReadsRequestHeaders124(t *testing.T) {
	res := &client.Resource{
		ResourceID:           1,
		Name:                 "web",
		RequestHeaders:       client.ResourceHeaders{{Name: "X-Env", Value: "prod"}},
		HasRequestHeadersKey: true,
	}
	m := applyHTTPResourceResponse(HTTPResourceModel{}, res)
	if len(m.Headers) != 1 || m.Headers[0].Name.ValueString() != "X-Env" {
		t.Errorf("Headers = %+v, want the 1.24 requestHeaders", m.Headers)
	}
}

func TestApplyHTTPResourceResponse_NoHeaderKeyKeepsKnownValue(t *testing.T) {
	// A response with neither `headers` nor `requestHeaders` says
	// nothing about them: keep the planned / prior value instead of
	// reading it as "no headers" (inconsistent result after apply).
	prior := []ResourceHeaderModel{{Name: types.StringValue("X-A"), Value: types.StringValue("1")}}
	m := applyHTTPResourceResponse(HTTPResourceModel{Headers: prior}, &client.Resource{ResourceID: 1, Name: "web"})
	if len(m.Headers) != 1 || m.Headers[0].Name.ValueString() != "X-A" {
		t.Errorf("Headers = %+v, want the prior value kept", m.Headers)
	}
}

func TestApplyHTTPResourceResponse_EmptyHeadersKeepsNullOrEmpty(t *testing.T) {
	res := &client.Resource{ResourceID: 1, Name: "web", HasRequestHeadersKey: true}

	// Config without the attribute: null stays null.
	if m := applyHTTPResourceResponse(HTTPResourceModel{}, res); m.Headers != nil {
		t.Errorf("null prior: Headers = %#v, want nil", m.Headers)
	}
	// Config with `headers = []`: the empty list must survive, or the
	// framework reports an inconsistent result after apply.
	m := applyHTTPResourceResponse(HTTPResourceModel{Headers: []ResourceHeaderModel{}}, res)
	if m.Headers == nil || len(m.Headers) != 0 {
		t.Errorf("empty prior: Headers = %#v, want empty non-nil", m.Headers)
	}
	// State that had headers, server now reports none (cleared out
	// of band): null, so a config without the attribute sees no diff.
	prior := []ResourceHeaderModel{{Name: types.StringValue("X-A"), Value: types.StringValue("1")}}
	if m := applyHTTPResourceResponse(HTTPResourceModel{Headers: prior}, res); m.Headers != nil {
		t.Errorf("populated prior: Headers = %#v, want nil", m.Headers)
	}
}

// -----------------------------------------------------------------------------
// response_headers (1.24+)
// -----------------------------------------------------------------------------

func TestBuildHTTPResourceUpdateRequest_ResponseHeaders(t *testing.T) {
	// Unset: the key must stay off the payload, pre-1.24 servers
	// reject it.
	plan := HTTPResourceModel{Name: types.StringValue("res")}
	raw, err := json.Marshal(buildHTTPResourceUpdateRequest(plan))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(raw), "responseHeaders") {
		t.Errorf("unset response_headers leaked into the payload: %s", raw)
	}

	plan.ResponseHeaders = []ResourceHeaderModel{
		{Name: types.StringValue("Strict-Transport-Security"), Value: types.StringValue("max-age=63072000")},
	}
	raw, err = json.Marshal(buildHTTPResourceUpdateRequest(plan))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(raw), `"responseHeaders":[{"name":"Strict-Transport-Security","value":"max-age=63072000"}]`) {
		t.Errorf("payload = %s", raw)
	}
}

func TestClearRemovedHeaders_ResponseHeadersSendsNull(t *testing.T) {
	prior := []ResourceHeaderModel{{Name: types.StringValue("X-Frame-Options"), Value: types.StringValue("DENY")}}
	plan := HTTPResourceModel{Name: types.StringValue("res")}
	state := HTTPResourceModel{Name: types.StringValue("res"), ResponseHeaders: prior}
	req := buildHTTPResourceUpdateRequest(plan)
	clearRemovedHeaders(req, plan, state)
	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(raw), `"responseHeaders":null`) {
		t.Errorf("payload %s must clear responseHeaders with null", raw)
	}

	// No prior response headers (e.g. a pre-1.24 server): nothing sent.
	req = buildHTTPResourceUpdateRequest(plan)
	clearRemovedHeaders(req, plan, HTTPResourceModel{Name: types.StringValue("res")})
	if req.ResponseHeaders != nil {
		t.Errorf("ResponseHeaders = %+v, want nil", req.ResponseHeaders)
	}
}

func TestApplyHTTPResourceResponse_ResponseHeaders(t *testing.T) {
	res := &client.Resource{
		ResourceID:      1,
		Name:            "web",
		ResponseHeaders: client.ResourceHeaders{{Name: "X-Frame-Options", Value: "DENY"}},
	}
	m := applyHTTPResourceResponse(HTTPResourceModel{}, res)
	if len(m.ResponseHeaders) != 1 || m.ResponseHeaders[0].Value.ValueString() != "DENY" {
		t.Errorf("ResponseHeaders = %+v", m.ResponseHeaders)
	}
	if m.Headers != nil {
		t.Errorf("Headers = %+v, want nil", m.Headers)
	}
	// Pre-1.24 server: no key on the wire, attribute stays null.
	if m := applyHTTPResourceResponse(HTTPResourceModel{}, &client.Resource{ResourceID: 1}); m.ResponseHeaders != nil {
		t.Errorf("ResponseHeaders = %+v, want nil", m.ResponseHeaders)
	}
}

// -----------------------------------------------------------------------------
// headers: always written under `headers`
// -----------------------------------------------------------------------------

func TestUpdatePayload_HeadersKey(t *testing.T) {
	// Every 1.19+ server reads `headers`; 1.24 keeps it as an alias of
	// `requestHeaders`, a key pre-1.24 servers refuse. So the payload
	// never carries `requestHeaders`.
	withHeaders := []ResourceHeaderModel{{Name: types.StringValue("X-A"), Value: types.StringValue("1")}}
	cases := []struct {
		name  string
		plan  []ResourceHeaderModel
		state []ResourceHeaderModel
		want  string
	}{
		{"set", withHeaders, nil, `"headers":[{"name":"X-A","value":"1"}]`},
		{"removed clears", nil, withHeaders, `"headers":[]`},
		{"explicit empty", []ResourceHeaderModel{}, nil, `"headers":[]`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan := HTTPResourceModel{Name: types.StringValue("res"), Headers: tc.plan}
			state := HTTPResourceModel{Name: types.StringValue("res"), Headers: tc.state}
			req := buildHTTPResourceUpdateRequest(plan)
			clearRemovedHeaders(req, plan, state)
			raw, err := json.Marshal(req)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if !strings.Contains(string(raw), tc.want) {
				t.Errorf("payload %s, want %s", raw, tc.want)
			}
			if strings.Contains(string(raw), `"requestHeaders"`) {
				t.Errorf("payload %s must not carry requestHeaders", raw)
			}
		})
	}
}
