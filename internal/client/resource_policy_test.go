package client

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"testing"
)

// decodeJSONBody reads a request body into a generic map.
func decodeJSONBody(t *testing.T, r *http.Request) map[string]any {
	t.Helper()
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("decode body %s: %v", raw, err)
	}
	return body
}

func TestCreateResourcePolicy_PathAndBody(t *testing.T) {
	var body map[string]any
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/v1/org/test-org/resource-policy" {
			t.Errorf("got %s %s", r.Method, r.URL.Path)
		}
		body = decodeJSONBody(t, r)
		writeEnvelope(t, w, http.StatusCreated, map[string]any{
			"resourcePolicyId": 7, "niceId": "brave-otter", "name": "corp",
		})
	})

	enabled := false
	got, err := c.CreateResourcePolicy(context.Background(), &CreateResourcePolicyRequest{
		Name:    "corp",
		SSO:     true,
		RoleIDs: []string{"3"},
		UserIDs: []string{"u1"},
		Emails:  []string{},
		Rules: []ResourcePolicyRule{
			{Action: "ACCEPT", Match: "CIDR", Value: "10.0.0.0/8", Priority: 1, Enabled: &enabled},
		},
	})
	if err != nil {
		t.Fatalf("CreateResourcePolicy: %v", err)
	}
	if got.ResourcePolicyID != 7 || got.NiceID != "brave-otter" {
		t.Errorf("got %+v", got)
	}
	// The create schema parses role IDs as strings.
	roles, _ := body["roleIds"].([]any)
	if len(roles) != 1 || roles[0] != "3" {
		t.Errorf("roleIds = %#v, want [\"3\"]", body["roleIds"])
	}
	if v, ok := body["skipToIdpId"]; !ok || v != nil {
		t.Errorf("skipToIdpId = %#v (present=%v), want explicit null", v, ok)
	}
	for _, k := range []string{"password", "pincode", "headerAuth"} {
		if _, ok := body[k]; ok {
			t.Errorf("create body must not carry %q", k)
		}
	}
	rules, _ := body["rules"].([]any)
	if len(rules) != 1 {
		t.Fatalf("rules = %#v", body["rules"])
	}
	rule := rules[0].(map[string]any)
	if _, ok := rule["ruleId"]; ok {
		t.Errorf("new rules must not carry ruleId")
	}
	if rule["enabled"] != false {
		t.Errorf("rule.enabled = %#v", rule["enabled"])
	}
}

func TestGetResourcePolicy_DecodesFullShape(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/resource-policy/7" {
			t.Errorf("path = %q", r.URL.Path)
		}
		writeEnvelope(t, w, http.StatusOK, map[string]any{
			"resourcePolicyId":      7,
			"sso":                   true,
			"applyRules":            true,
			"emailWhitelistEnabled": true,
			"idpId":                 nil,
			"niceId":                "brave-otter",
			"name":                  "corp",
			"passwordId":            12,
			"pincodeId":             nil,
			"headerAuth":            map[string]any{"id": 4, "extendedCompability": true},
			"roles":                 []map[string]any{{"roleId": 3, "name": "devs"}},
			"users":                 []map[string]any{{"userId": "u1", "email": nil, "name": nil, "username": "alice", "type": "internal", "idpName": nil}},
			"emailWhiteList":        []map[string]any{{"whiteListId": 1, "email": "a@example.com"}},
			"rules":                 []map[string]any{{"ruleId": 9, "enabled": true, "priority": 1, "action": "DROP", "match": "COUNTRY", "value": "FR"}},
		})
	})

	p, err := c.GetResourcePolicy(context.Background(), 7)
	if err != nil {
		t.Fatalf("GetResourcePolicy: %v", err)
	}
	if p.PasswordID == nil || *p.PasswordID != 12 || p.PincodeID != nil {
		t.Errorf("password/pincode ids = %v / %v", p.PasswordID, p.PincodeID)
	}
	if !p.HasHeaderAuth() || p.HeaderAuth.ExtendedCompatibility == nil || !*p.HeaderAuth.ExtendedCompatibility {
		t.Errorf("headerAuth = %+v", p.HeaderAuth)
	}
	if len(p.Roles) != 1 || p.Roles[0].RoleID != 3 {
		t.Errorf("roles = %+v", p.Roles)
	}
	if len(p.Users) != 1 || p.Users[0].UserID != "u1" {
		t.Errorf("users = %+v", p.Users)
	}
	if len(p.EmailWhitelist) != 1 || p.EmailWhitelist[0].Email != "a@example.com" {
		t.Errorf("whitelist = %+v", p.EmailWhitelist)
	}
	if len(p.Rules) != 1 || p.Rules[0].RuleID != 9 || p.Rules[0].Enabled == nil || !*p.Rules[0].Enabled {
		t.Errorf("rules = %+v", p.Rules)
	}
}

func TestGetResourcePolicy_NullHeaderAuth(t *testing.T) {
	for _, headerAuth := range []any{nil, map[string]any{"id": nil, "extendedCompability": nil}} {
		c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			writeEnvelope(t, w, http.StatusOK, map[string]any{
				"resourcePolicyId": 7, "name": "corp", "headerAuth": headerAuth,
			})
		})
		p, err := c.GetResourcePolicy(context.Background(), 7)
		if err != nil {
			t.Fatalf("GetResourcePolicy: %v", err)
		}
		if p.HasHeaderAuth() {
			t.Errorf("headerAuth %#v reported as set", headerAuth)
		}
	}
}

func TestGetResourcePolicy_NotFound(t *testing.T) {
	disableRetryBackoff(t)
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeEnvelope(t, w, http.StatusNotFound, nil)
	})
	_, err := c.GetResourcePolicy(context.Background(), 404)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestResourcePolicySetters_PathsAndBodies(t *testing.T) {
	type call struct {
		method, path string
		body         map[string]any
	}
	var calls []call
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, call{r.Method, r.URL.Path, decodeJSONBody(t, r)})
		writeEnvelope(t, w, http.StatusOK, map[string]any{})
	})
	ctx := context.Background()

	if err := c.UpdateResourcePolicy(ctx, 7, &UpdateResourcePolicyRequest{Name: "n"}); err != nil {
		t.Fatal(err)
	}
	if err := c.SetResourcePolicyAccessControl(ctx, 7, &SetResourcePolicyAccessControlRequest{SSO: true, UserIDs: []string{}, RoleIDs: []int{3}}); err != nil {
		t.Fatal(err)
	}
	if err := c.SetResourcePolicyWhitelist(ctx, 7, true, nil); err != nil {
		t.Fatal(err)
	}
	if err := c.SetResourcePolicyRules(ctx, 7, false, nil); err != nil {
		t.Fatal(err)
	}
	if err := c.SetResourcePolicyPassword(ctx, 7, nil); err != nil {
		t.Fatal(err)
	}
	pin := "123456"
	if err := c.SetResourcePolicyPincode(ctx, 7, &pin); err != nil {
		t.Fatal(err)
	}
	if err := c.SetResourcePolicyHeaderAuth(ctx, 7, &ResourcePolicyHeaderAuthCredentials{User: "user", Password: "pass", ExtendedCompatibility: true}); err != nil {
		t.Fatal(err)
	}
	if err := c.SetResourcePolicyHeaderAuth(ctx, 7, nil); err != nil {
		t.Fatal(err)
	}

	want := []string{
		"/v1/resource-policy/7",
		"/v1/resource-policy/7/access-control",
		"/v1/resource-policy/7/whitelist",
		"/v1/resource-policy/7/rules",
		"/v1/resource-policy/7/password",
		"/v1/resource-policy/7/pincode",
		"/v1/resource-policy/7/header-auth",
		"/v1/resource-policy/7/header-auth",
	}
	if len(calls) != len(want) {
		t.Fatalf("got %d calls, want %d", len(calls), len(want))
	}
	for i, p := range want {
		if calls[i].method != "POST" || calls[i].path != p {
			t.Errorf("call %d = %s %s, want POST %s", i, calls[i].method, calls[i].path, p)
		}
	}
	if _, ok := calls[0].body["niceId"]; ok {
		t.Errorf("empty niceId must be omitted: %#v", calls[0].body)
	}
	if roles, _ := calls[1].body["roleIds"].([]any); len(roles) != 1 || roles[0] != float64(3) {
		t.Errorf("access-control roleIds = %#v, want [3]", calls[1].body["roleIds"])
	}
	if v, ok := calls[1].body["skipToIdpId"]; !ok || v != nil {
		t.Errorf("skipToIdpId must be an explicit null, got %#v", v)
	}
	if emails, ok := calls[2].body["emails"].([]any); !ok || len(emails) != 0 {
		t.Errorf("whitelist emails = %#v, want []", calls[2].body["emails"])
	}
	if rules, ok := calls[3].body["rules"].([]any); !ok || len(rules) != 0 {
		t.Errorf("rules = %#v, want []", calls[3].body["rules"])
	}
	if v, ok := calls[4].body["password"]; !ok || v != nil {
		t.Errorf("password = %#v, want explicit null", v)
	}
	if calls[5].body["pincode"] != "123456" {
		t.Errorf("pincode = %#v", calls[5].body["pincode"])
	}
	ha, _ := calls[6].body["headerAuth"].(map[string]any)
	if ha["user"] != "user" || ha["password"] != "pass" || ha["extendedCompatibility"] != true {
		t.Errorf("headerAuth = %#v", calls[6].body["headerAuth"])
	}
	if v, ok := calls[7].body["headerAuth"]; !ok || v != nil {
		t.Errorf("clearing headerAuth = %#v, want explicit null", v)
	}
}

func TestDeleteResourcePolicy_Path(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "DELETE" || r.URL.Path != "/v1/resource-policy/7" {
			t.Errorf("got %s %s", r.Method, r.URL.Path)
		}
		writeEnvelope(t, w, http.StatusOK, nil)
	})
	if err := c.DeleteResourcePolicy(context.Background(), 7); err != nil {
		t.Fatal(err)
	}
}

func TestListResourcePolicies_WalksPages(t *testing.T) {
	var pages []int
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/org/test-org/resource-policies" {
			t.Errorf("path = %q", r.URL.Path)
		}
		q := r.URL.Query()
		if q.Get("query") != "corp" || q.Get("pageSize") != strconv.Itoa(resourcePolicyListPageSize) {
			t.Errorf("query = %q", r.URL.RawQuery)
		}
		page, _ := strconv.Atoi(q.Get("page"))
		pages = append(pages, page)
		n := resourcePolicyListPageSize
		if page == 2 {
			n = 1
		}
		policies := make([]map[string]any, n)
		for i := range policies {
			policies[i] = map[string]any{
				"resourcePolicyId": page*1000 + i, "niceId": "p", "name": "corp", "orgId": "test-org",
				"resources": []map[string]any{{"resourceId": 5, "niceId": "app", "name": "App", "fullDomain": nil}},
			}
		}
		writeEnvelope(t, w, http.StatusOK, map[string]any{
			"policies":   policies,
			"pagination": map[string]any{"total": resourcePolicyListPageSize + 1, "pageSize": resourcePolicyListPageSize, "page": page},
		})
	})

	got, err := c.ListResourcePolicies(context.Background(), "corp")
	if err != nil {
		t.Fatalf("ListResourcePolicies: %v", err)
	}
	if len(got) != resourcePolicyListPageSize+1 || len(pages) != 2 || pages[0] != 1 || pages[1] != 2 {
		t.Errorf("got %d policies over pages %v", len(got), pages)
	}
	if len(got[0].Resources) != 1 || got[0].Resources[0].ResourceID != 5 || got[0].Resources[0].FullDomain != nil {
		t.Errorf("resources = %+v", got[0].Resources)
	}
}

func TestListResourcePolicies_CappedPageSizeWithoutTotal(t *testing.T) {
	// A server that caps pageSize at 2 and omits total: the walk must
	// follow the echoed page size and stop on the first short page.
	var pages []int
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		pages = append(pages, page)
		n := 2
		if page == 3 {
			n = 1
		}
		policies := make([]map[string]any, n)
		for i := range policies {
			policies[i] = map[string]any{"resourcePolicyId": page*10 + i, "name": "p", "orgId": "test-org"}
		}
		writeEnvelope(t, w, http.StatusOK, map[string]any{
			"policies":   policies,
			"pagination": map[string]any{"pageSize": 2, "page": page},
		})
	})

	got, err := c.ListResourcePolicies(context.Background(), "")
	if err != nil {
		t.Fatalf("ListResourcePolicies: %v", err)
	}
	if len(got) != 5 || len(pages) != 3 {
		t.Errorf("got %d policies over pages %v, want 5 over 3", len(got), pages)
	}
}
