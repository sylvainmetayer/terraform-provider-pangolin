package resources

import (
	"encoding/json"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stackopshq/terraform-provider-pangolin/internal/client"
)

func budgetModel() AIBudgetResourceModel {
	return AIBudgetResourceModel{
		ProviderID: types.Int64Null(), ModelID: types.Int64Null(), ResourceID: types.Int64Null(),
		SiteResourceID: types.Int64Null(), RoleID: types.Int64Value(7), VirtualAPIKeyID: types.StringNull(),
		Amount: types.Float64Value(12.5), Unit: types.StringValue("usd"), Period: types.StringValue("monthly"),
		Enforcement: types.StringValue("hard"), Enabled: types.BoolValue(true),
	}
}

// Create refuses null scopes (omitted); update needs them to move a budget
// from one scope to another (sent as null).
func TestAIBudgetRequests_ScopeNulls(t *testing.T) {
	c, _ := json.Marshal(aiBudgetCreateRequest(budgetModel()))
	if want := `{"roleId":7,"amount":12.5,"unit":"usd","period":"monthly","enforcement":"hard","enabled":true}`; string(c) != want {
		t.Errorf("create = %s, want %s", c, want)
	}
	u, _ := json.Marshal(aiBudgetUpdateRequest(budgetModel()))
	want := `{"providerId":null,"modelId":null,"resourceId":null,"siteResourceId":null,"roleId":7,"virtualApiKeyId":null,"amount":12.5,"unit":"usd","period":"monthly","enforcement":"hard","enabled":true}`
	if string(u) != want {
		t.Errorf("update = %s, want %s", u, want)
	}
}

func TestAIBudgetToModel(t *testing.T) {
	key := "k1"
	m := aiBudgetToModel(&client.AIBudget{BudgetID: 4, VirtualAPIKeyID: &key, Amount: 3, Unit: "tokens", Period: "daily", Enforcement: "soft"})
	if m.ID.ValueInt64() != 4 || m.VirtualAPIKeyID.ValueString() != "k1" || !m.RoleID.IsNull() || m.Enabled.ValueBool() {
		t.Errorf("model = %+v", m)
	}
}
