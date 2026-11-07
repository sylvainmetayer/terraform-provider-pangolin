package resources

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/stackopshq/terraform-provider-pangolin/internal/client"
)

// An unset 1.26-only field must stay out of the body: an older server's
// strict schema refuses unknown keys.
func TestBuildUpdateOrgRequestNewSettings(t *testing.T) {
	plan := OrgResourceModel{
		Name:                       types.StringValue("o"),
		LogRetentionDaysAISessions: types.Int64Unknown(),
		LogRetentionDaysSSH:        types.Int64Null(),
		NewtAutoUpdate:             types.BoolNull(),
	}
	body, _ := json.Marshal(buildUpdateOrgRequest(plan))
	for _, k := range []string{"AISessions", "Ssh", "NewtAutoUpdate"} {
		if strings.Contains(string(body), k) {
			t.Fatalf("unset %s sent: %s", k, body)
		}
	}
	if hasOrgPolicyChanges(OrgResourceModel{NewtAutoUpdate: types.BoolValue(false)}) != true {
		t.Fatal("newt_auto_update=false must trigger the post-create update")
	}

	plan.LogRetentionDaysAISessions = types.Int64Value(0)
	plan.LogRetentionDaysSSH = types.Int64Value(30)
	plan.NewtAutoUpdate = types.BoolValue(false)
	body, _ = json.Marshal(buildUpdateOrgRequest(plan))
	for _, k := range []string{`"settingsLogRetentionDaysAISessions":0`, `"settingsLogRetentionDaysSsh":30`, `"settingsEnableGlobalNewtAutoUpdate":false`} {
		if !strings.Contains(string(body), k) {
			t.Fatalf("missing %s: %s", k, body)
		}
	}
}

func TestOrgToModelNewSettings(t *testing.T) {
	var old client.Org
	_ = json.Unmarshal([]byte(`{"orgId":"o","settingsLogRetentionDaysAISessions":7,"settingsEnableGlobalNewtAutoUpdate":true}`), &old)
	m := orgToModel(&old, OrgResourceModel{})
	if m.LogRetentionDaysAISessions.ValueInt64() != 7 || !m.NewtAutoUpdate.ValueBool() {
		t.Fatalf("read back wrong: %+v", m)
	}
	if !m.LogRetentionDaysSSH.IsNull() {
		t.Fatal("ssh retention absent from a 1.25 server must be null")
	}
}
