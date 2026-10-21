package resources

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/stackopshq/terraform-provider-pangolin/internal/client"
)

// -----------------------------------------------------------------------------
// healthCheckToModel: wire -> state
// -----------------------------------------------------------------------------

func TestHealthCheckToModel_FullPayload(t *testing.T) {
	headers := `[{"name":"X-Probe","value":"yes"}]`
	hc := &client.HealthCheck{
		HealthCheckID:        42,
		Name:                 "db",
		SiteID:               intPtr(4),
		HCEnabled:            true,
		HCHealth:             "healthy",
		HCMode:               strPtr("http"),
		HCHostname:           strPtr("10.0.0.5"),
		HCPort:               intPtr(8080),
		HCPath:               strPtr("/health"),
		HCScheme:             strPtr("https"),
		HCMethod:             strPtr("HEAD"),
		HCInterval:           intPtr(30),
		HCUnhealthyInterval:  intPtr(10),
		HCTimeout:            intPtr(5),
		HCHeaders:            &headers,
		HCFollowRedirects:    boolPtr(false),
		HCStatus:             intPtr(204),
		HCTLSServerName:      strPtr("db.internal"),
		HCHealthyThreshold:   intPtr(2),
		HCUnhealthyThreshold: intPtr(3),
	}

	m := healthCheckToModel(hc, HealthCheckResourceModel{})

	if m.ID.ValueInt64() != 42 || m.Name.ValueString() != "db" || m.SiteID.ValueInt64() != 4 {
		t.Errorf("identity wrong: %+v", m)
	}
	if !m.Enabled.ValueBool() || m.Health.ValueString() != "healthy" || m.Mode.ValueString() != "http" {
		t.Errorf("enabled/health/mode wrong: %+v", m)
	}
	if m.Hostname.ValueString() != "10.0.0.5" || m.Port.ValueInt64() != 8080 || m.Path.ValueString() != "/health" {
		t.Errorf("target fields wrong: %+v", m)
	}
	if m.Scheme.ValueString() != "https" || m.Method.ValueString() != "HEAD" || m.TLSServerName.ValueString() != "db.internal" {
		t.Errorf("http fields wrong: %+v", m)
	}
	if m.Interval.ValueInt64() != 30 || m.UnhealthyInterval.ValueInt64() != 10 || m.Timeout.ValueInt64() != 5 {
		t.Errorf("timing fields wrong: %+v", m)
	}
	if m.FollowRedirects.ValueBool() || m.Status.ValueInt64() != 204 {
		t.Errorf("follow_redirects/status wrong: %+v", m)
	}
	if m.HealthyThreshold.ValueInt64() != 2 || m.UnhealthyThreshold.ValueInt64() != 3 {
		t.Errorf("thresholds wrong: %+v", m)
	}
	if len(m.Headers) != 1 || m.Headers[0].Name.ValueString() != "X-Probe" || m.Headers[0].Value.ValueString() != "yes" {
		t.Errorf("headers = %+v", m.Headers)
	}
}

func TestHealthCheckToModel_EmptyStringsReadAsNull(t *testing.T) {
	// The update path clears optional strings by sending "", which the
	// server stores and echoes back.
	hc := &client.HealthCheck{
		HealthCheckID:   1,
		Name:            "x",
		HCHostname:      strPtr(""),
		HCPath:          strPtr(""),
		HCScheme:        strPtr(""),
		HCTLSServerName: nil,
	}
	m := healthCheckToModel(hc, HealthCheckResourceModel{})
	for name, v := range map[string]types.String{
		"hostname": m.Hostname, "path": m.Path, "scheme": m.Scheme, "tls_server_name": m.TLSServerName,
	} {
		if !v.IsNull() {
			t.Errorf("%s = %q, want null", name, v.ValueString())
		}
	}
	if !m.Status.IsNull() || !m.Port.IsNull() {
		t.Errorf("status/port should be null, got %v/%v", m.Status, m.Port)
	}
	if m.Health.ValueString() != "unknown" {
		t.Errorf("empty health should read as unknown, got %q", m.Health.ValueString())
	}
}

func TestHealthCheckToModel_HeadersKeepPriorEmptyList(t *testing.T) {
	hc := &client.HealthCheck{HealthCheckID: 1, Name: "x"}

	// Unset in config -> stays null.
	if m := healthCheckToModel(hc, HealthCheckResourceModel{}); m.Headers != nil {
		t.Errorf("headers = %+v, want nil", m.Headers)
	}
	// `headers = []` in config -> stays [] (not null).
	prior := HealthCheckResourceModel{Headers: []TargetHCHeaderModel{}}
	if m := healthCheckToModel(hc, prior); m.Headers == nil || len(m.Headers) != 0 {
		t.Errorf("headers = %#v, want empty non-nil slice", m.Headers)
	}
	// Malformed server value -> no headers, no panic.
	bad := "not-json"
	hc.HCHeaders = &bad
	if m := healthCheckToModel(hc, HealthCheckResourceModel{}); m.Headers != nil {
		t.Errorf("headers = %+v, want nil on malformed input", m.Headers)
	}
}

// -----------------------------------------------------------------------------
// buildHealthCheckRequest: plan -> wire
// -----------------------------------------------------------------------------

func healthCheckPlan() HealthCheckResourceModel {
	return HealthCheckResourceModel{
		ID:                 types.Int64Value(42),
		Name:               types.StringValue("db"),
		SiteID:             types.Int64Value(4),
		Enabled:            types.BoolValue(true),
		Mode:               types.StringValue("http"),
		Hostname:           types.StringNull(),
		Port:               types.Int64Unknown(),
		Path:               types.StringNull(),
		Scheme:             types.StringNull(),
		Method:             types.StringValue("GET"),
		Interval:           types.Int64Value(30),
		UnhealthyInterval:  types.Int64Value(30),
		Timeout:            types.Int64Value(5),
		FollowRedirects:    types.BoolValue(true),
		Status:             types.Int64Null(),
		TLSServerName:      types.StringNull(),
		HealthyThreshold:   types.Int64Value(1),
		UnhealthyThreshold: types.Int64Value(1),
	}
}

func TestBuildHealthCheckRequest_CreateOmitsUnsetStrings(t *testing.T) {
	body, err := buildHealthCheckRequest(healthCheckPlan(), false)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if body.HCHostname != nil || body.HCPath != nil || body.HCScheme != nil || body.HCTLSServerName != nil {
		t.Errorf("unset strings should be omitted on create: %+v", body)
	}
	if body.HCPort != nil || body.HCStatus != nil || body.HCHeaders != nil {
		t.Errorf("unknown port / null status / no headers should be nil: %+v", body)
	}
	if *body.Name != "db" || *body.SiteID != 4 || !*body.HCEnabled || *body.HCMode != "http" || *body.HCTimeout != 5 {
		t.Errorf("defaulted fields wrong: %+v", body)
	}
}

func TestBuildHealthCheckRequest_UpdateClearsUnsetStrings(t *testing.T) {
	body, err := buildHealthCheckRequest(healthCheckPlan(), true)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	for name, p := range map[string]*string{
		"hcHostname": body.HCHostname, "hcPath": body.HCPath,
		"hcScheme": body.HCScheme, "hcTlsServerName": body.HCTLSServerName,
	} {
		if p == nil || *p != "" {
			t.Errorf("%s = %v, want empty string to clear it", name, p)
		}
	}
	// Port cannot be cleared: an unknown / null port is still omitted.
	if body.HCPort != nil {
		t.Errorf("hcPort = %v, want nil", *body.HCPort)
	}
}

func TestBuildHealthCheckRequest_HeadersAndValues(t *testing.T) {
	plan := healthCheckPlan()
	plan.Hostname = types.StringValue("10.0.0.5")
	plan.Port = types.Int64Value(8080)
	plan.Status = types.Int64Value(200)
	plan.Headers = []TargetHCHeaderModel{
		{Name: types.StringValue("X-Probe"), Value: types.StringValue("yes")},
		{Name: types.StringValue("Authorization"), Value: types.StringValue(`Bearer "x"`)},
	}
	body, err := buildHealthCheckRequest(plan, true)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if *body.HCHostname != "10.0.0.5" || *body.HCPort != 8080 || *body.HCStatus != 200 {
		t.Errorf("values wrong: %+v", body)
	}
	want := `[{"name":"X-Probe","value":"yes"},{"name":"Authorization","value":"Bearer \"x\""}]`
	if body.HCHeaders == nil || *body.HCHeaders != want {
		t.Errorf("hcHeaders = %v, want %s", body.HCHeaders, want)
	}
	// Round trip through the read path.
	got, err := client.ParseTargetHCHeaders(body.HCHeaders)
	if err != nil || len(got) != 2 || got[1].Value != `Bearer "x"` {
		t.Errorf("round trip = %+v err=%v", got, err)
	}
}

func TestBuildHealthCheckRequest_EmptyHeadersSendNull(t *testing.T) {
	plan := healthCheckPlan()
	plan.Headers = []TargetHCHeaderModel{}
	body, err := buildHealthCheckRequest(plan, true)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if body.HCHeaders != nil {
		t.Errorf("hcHeaders = %q, want nil (null on the wire)", *body.HCHeaders)
	}
}

// -----------------------------------------------------------------------------
// Schema
// -----------------------------------------------------------------------------

func TestHealthCheckSchema_Valid(t *testing.T) {
	var resp resource.SchemaResponse
	NewHealthCheckResource().Schema(context.Background(), resource.SchemaRequest{}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("schema diagnostics: %v", resp.Diagnostics)
	}
	if diags := resp.Schema.ValidateImplementation(context.Background()); diags.HasError() {
		t.Fatalf("schema implementation: %v", diags)
	}
	for _, name := range []string{"name", "site_id", "enabled", "mode", "hostname", "port", "headers", "health"} {
		if _, ok := resp.Schema.Attributes[name]; !ok {
			t.Errorf("missing attribute %q", name)
		}
	}
}

// -----------------------------------------------------------------------------
// ValidateConfig
// -----------------------------------------------------------------------------

// healthCheckConfig builds a tfsdk.Config from the resource schema,
// with every attribute null except the given overrides.
func healthCheckConfig(t *testing.T, overrides map[string]tftypes.Value) tfsdk.Config {
	t.Helper()
	ctx := context.Background()
	var sresp resource.SchemaResponse
	NewHealthCheckResource().Schema(ctx, resource.SchemaRequest{}, &sresp)
	objType, ok := sresp.Schema.Type().TerraformType(ctx).(tftypes.Object)
	if !ok {
		t.Fatalf("schema type is not an object")
	}
	vals := map[string]tftypes.Value{}
	for name, typ := range objType.AttributeTypes {
		if v, ok := overrides[name]; ok {
			vals[name] = v
			continue
		}
		vals[name] = tftypes.NewValue(typ, nil)
	}
	return tfsdk.Config{Schema: sresp.Schema, Raw: tftypes.NewValue(objType, vals)}
}

func validateHealthCheck(t *testing.T, overrides map[string]tftypes.Value) resource.ValidateConfigResponse {
	t.Helper()
	var resp resource.ValidateConfigResponse
	r := &HealthCheckResource{}
	r.ValidateConfig(context.Background(), resource.ValidateConfigRequest{Config: healthCheckConfig(t, overrides)}, &resp)
	return resp
}

func TestHealthCheckValidateConfig(t *testing.T) {
	enabled := tftypes.NewValue(tftypes.Bool, true)
	str := func(s string) tftypes.Value { return tftypes.NewValue(tftypes.String, s) }
	num := func(n int64) tftypes.Value { return tftypes.NewValue(tftypes.Number, n) }

	// Disabled probe: nothing to check.
	if resp := validateHealthCheck(t, nil); resp.Diagnostics.HasError() || resp.Diagnostics.WarningsCount() != 0 {
		t.Errorf("disabled: diags = %v", resp.Diagnostics)
	}

	// Enabled without hostname: server rule, hard error.
	resp := validateHealthCheck(t, map[string]tftypes.Value{"enabled": enabled, "port": num(80), "path": str("/")})
	if resp.Diagnostics.ErrorsCount() != 1 {
		t.Errorf("missing hostname: diags = %v", resp.Diagnostics)
	}

	// Enabled http probe without port and path: two warnings.
	resp = validateHealthCheck(t, map[string]tftypes.Value{"enabled": enabled, "hostname": str("db")})
	if resp.Diagnostics.HasError() || resp.Diagnostics.WarningsCount() != 2 {
		t.Errorf("http without port/path: diags = %v", resp.Diagnostics)
	}

	// Enabled tcp probe with port: path is not needed.
	resp = validateHealthCheck(t, map[string]tftypes.Value{
		"enabled": enabled, "hostname": str("db"), "port": num(5432), "mode": str("tcp"),
	})
	if resp.Diagnostics.HasError() || resp.Diagnostics.WarningsCount() != 0 {
		t.Errorf("tcp: diags = %v", resp.Diagnostics)
	}
}
