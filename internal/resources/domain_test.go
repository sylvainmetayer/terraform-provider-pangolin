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

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/stackopshq/terraform-provider-pangolin/internal/client"
)

func domainStrPtr(s string) *string { return &s }

func TestApplyDomainResponse_MapsFieldsAndSortsRecords(t *testing.T) {
	domain := &client.Domain{
		DomainID: "dom-1", BaseDomain: "portal.example.com", Type: "cname",
		Verified: false, Failed: true, ErrorMessage: domainStrPtr("NXDOMAIN"), CertResolver: domainStrPtr(""),
	}
	records := []client.DomainDNSRecord{
		{ID: 9, RecordType: "TXT", BaseDomain: "_acme.portal.example.com", Value: "token"},
		{ID: 3, RecordType: "CNAME", BaseDomain: "portal.example.com", Value: "x.pangolin.net"},
	}
	m, diags := applyDomainResponse(DomainResourceModel{}, domain, records)
	if diags.HasError() {
		t.Fatalf("diags: %v", diags)
	}
	if m.ID.ValueString() != "dom-1" || m.Type.ValueString() != "cname" || m.BaseDomain.ValueString() != "portal.example.com" {
		t.Errorf("identity fields = %v %v %v", m.ID, m.Type, m.BaseDomain)
	}
	if !m.CertResolver.IsNull() {
		t.Errorf("empty cert resolver should read as null, got %v", m.CertResolver)
	}
	if m.Verified.ValueBool() || !m.Failed.ValueBool() || m.ErrorMessage.ValueString() != "NXDOMAIN" {
		t.Errorf("status fields = %v %v %v", m.Verified, m.Failed, m.ErrorMessage)
	}
	elems := m.DNSRecords.Elements()
	if len(elems) != 2 {
		t.Fatalf("records = %d, want 2", len(elems))
	}
	first := elems[0].(types.Object).Attributes()
	if first["record_type"].(types.String).ValueString() != "CNAME" {
		t.Errorf("records not sorted by ID: first = %v", first)
	}
}

func TestApplyDomainResponse_NoRecordsIsEmptyList(t *testing.T) {
	m, diags := applyDomainResponse(DomainResourceModel{}, &client.Domain{DomainID: "d", Type: "wildcard"}, nil)
	if diags.HasError() {
		t.Fatalf("diags: %v", diags)
	}
	if m.DNSRecords.IsNull() || len(m.DNSRecords.Elements()) != 0 {
		t.Errorf("dns_records = %v, want empty list", m.DNSRecords)
	}
}

func TestValidateDomainCertSettings(t *testing.T) {
	for _, tc := range []struct {
		name    string
		typ     string
		cert    types.String
		prefer  types.Bool
		wantErr bool
	}{
		{"wildcard with settings", "wildcard", types.StringValue("le"), types.BoolValue(true), false},
		{"cname without settings", "cname", types.StringNull(), types.BoolNull(), false},
		{"cname prefer false", "cname", types.StringNull(), types.BoolValue(false), false},
		{"cname with resolver", "cname", types.StringValue("le"), types.BoolNull(), true},
		{"ns prefer wildcard", "ns", types.StringNull(), types.BoolValue(true), true},
		{"unknown type", "", types.StringValue("le"), types.BoolValue(true), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			typ := types.StringValue(tc.typ)
			if tc.typ == "" {
				typ = types.StringUnknown()
			}
			diags := validateDomainCertSettings(DomainResourceModel{Type: typ, CertResolver: tc.cert, PreferWildcardCert: tc.prefer})
			if diags.HasError() != tc.wantErr {
				t.Errorf("HasError = %v, want %v (%v)", diags.HasError(), tc.wantErr, diags)
			}
		})
	}
}

func TestBaseDomainRegexp(t *testing.T) {
	for in, want := range map[string]bool{
		"example.com":      true,
		"apps.example.com": true,
		"a-b.example.com":  true,
		"Example.com":      false,
		"-a.example.com":   false,
		"https://a.com":    false,
		"":                 false,
	} {
		if got := baseDomainRegexp.MatchString(in); got != want {
			t.Errorf("%q: got %v, want %v", in, got, want)
		}
	}
}

func TestDomainResourceSchema_IsValid(t *testing.T) {
	var resp resource.SchemaResponse
	(&DomainResource{}).Schema(context.Background(), resource.SchemaRequest{}, &resp)
	if diags := resp.Schema.ValidateImplementation(context.Background()); diags.HasError() {
		t.Fatalf("schema: %v", diags)
	}
}

// domainFakeServer answers like Pangolin's domain routes and records
// the calls it receives.
type domainFakeServer struct {
	mu    sync.Mutex
	calls []string
	body  map[string]any
}

func (f *domainFakeServer) client(t *testing.T, failGet bool) *client.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		p := strings.TrimPrefix(r.URL.Path, "/v1")
		f.mu.Lock()
		f.calls = append(f.calls, r.Method+" "+p)
		if len(raw) > 0 && r.Method != http.MethodGet {
			_ = json.Unmarshal(raw, &f.body)
		}
		f.mu.Unlock()

		var data any
		status := http.StatusOK
		switch {
		case r.Method == http.MethodPut:
			data = map[string]any{"domainId": "dom-1"}
		case strings.HasSuffix(p, "/dns-records"):
			data = []map[string]any{{"id": 1, "domainId": "dom-1", "recordType": "A", "baseDomain": "*.apps.example.com", "value": "203.0.113.7", "verified": true}}
		case r.Method == http.MethodGet && failGet:
			// A 4xx is not retried, which keeps the test fast.
			status = http.StatusBadRequest
		case r.Method == http.MethodGet:
			data = map[string]any{"domainId": "dom-1", "baseDomain": "apps.example.com", "type": "wildcard", "verified": true,
				"certResolver": "letsencrypt", "preferWildcardCert": true, "configManaged": false}
		default:
			data = map[string]any{"domainId": "dom-1"}
		}
		payload, _ := json.Marshal(data)
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(client.APIResponse{Data: payload, Success: status < 400, Error: status >= 400, Status: status, Message: "x"})
	}))
	t.Cleanup(srv.Close)
	return client.NewClient(srv.URL, "test-key", "test-org")
}

func domainPlan(t *testing.T, r *DomainResource) (tfsdk.Plan, tfsdk.State) {
	t.Helper()
	var sresp resource.SchemaResponse
	r.Schema(context.Background(), resource.SchemaRequest{}, &sresp)
	plan := DomainResourceModel{
		ID:                 types.StringUnknown(),
		BaseDomain:         types.StringValue("apps.example.com"),
		Type:               types.StringValue("wildcard"),
		CertResolver:       types.StringValue("letsencrypt"),
		PreferWildcardCert: types.BoolValue(true),
		Verified:           types.BoolUnknown(),
		Failed:             types.BoolUnknown(),
		ErrorMessage:       types.StringUnknown(),
		ConfigManaged:      types.BoolUnknown(),
		DNSRecords:         types.ListUnknown(types.ObjectType{AttrTypes: domainDNSRecordAttrTypes}),
	}
	p := tfsdk.Plan{Schema: sresp.Schema}
	if diags := p.Set(context.Background(), &plan); diags.HasError() {
		t.Fatalf("plan: %v", diags)
	}
	s := tfsdk.State{Schema: sresp.Schema, Raw: tftypes.NewValue(sresp.Schema.Type().TerraformType(context.Background()), nil)}
	return p, s
}

func TestDomainCreate_CreatesThenReadsBack(t *testing.T) {
	fake := &domainFakeServer{}
	r := &DomainResource{client: fake.client(t, false)}
	plan, state := domainPlan(t, r)
	resp := &resource.CreateResponse{State: state}
	r.Create(context.Background(), resource.CreateRequest{Plan: plan}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("create: %v", resp.Diagnostics)
	}
	want := []string{"PUT /org/test-org/domain", "GET /org/test-org/domain/dom-1", "GET /org/test-org/domain/dom-1/dns-records"}
	if strings.Join(fake.calls, ",") != strings.Join(want, ",") {
		t.Errorf("calls = %v, want %v", fake.calls, want)
	}
	if fake.body["certResolver"] != "letsencrypt" || fake.body["preferWildcardCert"] != true {
		t.Errorf("create body = %v", fake.body)
	}
	var got DomainResourceModel
	resp.State.Get(context.Background(), &got)
	if got.ID.ValueString() != "dom-1" || !got.Verified.ValueBool() || len(got.DNSRecords.Elements()) != 1 {
		t.Errorf("state = %+v", got)
	}
}

func TestDomainCreate_FailedReadBackKeepsID(t *testing.T) {
	fake := &domainFakeServer{}
	r := &DomainResource{client: fake.client(t, true)}
	plan, state := domainPlan(t, r)
	resp := &resource.CreateResponse{State: state}
	r.Create(context.Background(), resource.CreateRequest{Plan: plan}, resp)
	if !resp.Diagnostics.HasError() {
		t.Fatal("expected an error when the read-back fails")
	}
	var id types.String
	resp.State.GetAttribute(context.Background(), path.Root("id"), &id)
	if id.ValueString() != "dom-1" {
		t.Errorf("id in state = %v, want dom-1 so the domain is tainted, not orphaned", id)
	}
}
