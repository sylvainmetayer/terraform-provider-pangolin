package resources

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestCustomDNSValueError(t *testing.T) {
	ok := map[string][]string{
		"A":     {"192.0.2.1"},
		"AAAA":  {"2001:db8::1"},
		"CNAME": {"target.example.com"},
		"MX":    {"mail.example.com"},
		"NS":    {"ns1.example.net"},
		"SRV":   {"sip.example.com"},
		"TXT":   {"v=spf1 -all", `a "quoted" word`},
	}
	bad := map[string][]string{
		"A":     {"2001:db8::1", " 192.0.2.1", "192.0.2"},
		"AAAA":  {"192.0.2.1", "2001:DB8::1"},
		"CNAME": {"Target.example.com", "target.example.com.", "target"},
		"MX":    {"mail.example.com."},
		"TXT":   {`"v=spf1 -all"`, "", "café"},
	}
	for typ, vs := range ok {
		for _, v := range vs {
			if msg := customDNSValueError(typ, v); msg != "" {
				t.Errorf("%s %q refused: %s", typ, v, msg)
			}
		}
	}
	for typ, vs := range bad {
		for _, v := range vs {
			if customDNSValueError(typ, v) == "" {
				t.Errorf("%s %q accepted", typ, v)
			}
		}
	}
}

func TestCustomDNSNameRe(t *testing.T) {
	for n, want := range map[string]bool{
		"@": true, "www": true, "_dmarc": true, "a.b": true, "*": true, "*.apps": true, "sel._domainkey": true,
		"WWW": false, "www.": false, "": false, "a.*": false, "-a": false,
	} {
		if customDNSNameRe.MatchString(n) != want {
			t.Errorf("%q: match=%v, want %v", n, !want, want)
		}
	}
}

func TestValidateCustomDNSRecordExtraFields(t *testing.T) {
	values := types.ListValueMust(types.StringType, []attr.Value{types.StringValue("mail.example.com")})
	m := CustomDNSRecordResourceModel{Type: types.StringValue("MX"), Values: values, Priority: types.Int64Null(), Weight: types.Int64Null(), Port: types.Int64Null()}
	if !validateCustomDNSRecord(context.Background(), m).HasError() {
		t.Fatal("MX without priority accepted")
	}
	m.Priority = types.Int64Value(10)
	if d := validateCustomDNSRecord(context.Background(), m); d.HasError() {
		t.Fatalf("valid MX refused: %v", d)
	}
	m.Port = types.Int64Value(25)
	if !validateCustomDNSRecord(context.Background(), m).HasError() {
		t.Fatal("MX with a port accepted")
	}
	two := types.ListValueMust(types.StringType, []attr.Value{types.StringValue("a.example.com"), types.StringValue("b.example.com")})
	c := CustomDNSRecordResourceModel{Type: types.StringValue("CNAME"), Values: two, Priority: types.Int64Null(), Weight: types.Int64Null(), Port: types.Int64Null()}
	if !validateCustomDNSRecord(context.Background(), c).HasError() {
		t.Fatal("CNAME with two values accepted")
	}
}
