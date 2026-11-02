package datasources

import (
	"testing"

	"github.com/stackopshq/terraform-provider-pangolin/internal/client"
)

func TestResourcePolicyDataModel_SortsLists(t *testing.T) {
	p := &client.ResourcePolicy{
		ResourcePolicyID: 1,
		Roles:            []client.ResourcePolicyRole{{RoleID: 9}, {RoleID: 3}},
		Users:            []client.ResourcePolicyUser{{UserID: "u2"}, {UserID: "u1"}},
		EmailWhitelist:   []client.ResourcePolicyWhitelistEntry{{Email: "b@x.io"}, {Email: "a@x.io"}},
		Rules: []client.ResourcePolicyRule{
			{RuleID: 7, Priority: 2},
			{RuleID: 5, Priority: 1},
			{RuleID: 4, Priority: 2},
		},
	}
	m := resourcePolicyDataModel(p)
	if m.RoleIDs[0].ValueInt64() != 3 || m.UserIDs[0].ValueString() != "u1" || m.WhitelistEmails[0].ValueString() != "a@x.io" {
		t.Errorf("unsorted ids/emails: %v %v %v", m.RoleIDs, m.UserIDs, m.WhitelistEmails)
	}
	var got []int64
	for _, r := range m.Rules {
		got = append(got, r.RuleID.ValueInt64())
	}
	if len(got) != 3 || got[0] != 5 || got[1] != 4 || got[2] != 7 {
		t.Errorf("rules order = %v, want [5 4 7]", got)
	}
	// The payload itself is left untouched.
	if p.Roles[0].RoleID != 9 {
		t.Errorf("input mutated")
	}
}
