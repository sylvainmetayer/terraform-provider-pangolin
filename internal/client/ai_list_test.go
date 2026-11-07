package client

import (
	"context"
	"net/http"
	"reflect"
	"testing"
)

func TestListAIProviders_PagesAndQuery(t *testing.T) {
	calls := 0
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/v1/org/test-org/ai-providers" || r.URL.Query().Get("query") != "oa" {
			t.Errorf("got %s", r.URL.String())
		}
		writeEnvelope(t, w, http.StatusOK, map[string]any{
			"providers":  []map[string]any{{"providerId": calls, "name": "p", "type": "openai"}},
			"pagination": map[string]any{"total": 2, "page": calls, "pageSize": 100},
		})
	})
	got, err := c.ListAIProviders(context.Background(), "oa")
	if err != nil {
		t.Fatalf("ListAIProviders: %v", err)
	}
	if len(got) != 2 || got[1].ProviderID != 2 || calls != 2 {
		t.Errorf("got %+v after %d calls", got, calls)
	}
}

func TestListAIModels_Path(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/ai-provider/5/models" {
			t.Errorf("got %s", r.URL.Path)
		}
		writeEnvelope(t, w, http.StatusOK, map[string]any{
			"models":     []map[string]any{{"modelId": 9, "providerId": 5, "modelKey": "gpt-4o", "listType": "allow", "enabled": true}},
			"pagination": map[string]any{"total": 1},
		})
	})
	got, err := c.ListAIModels(context.Background(), 5, "")
	if err != nil {
		t.Fatalf("ListAIModels: %v", err)
	}
	if len(got) != 1 || got[0].ModelKey != "gpt-4o" || got[0].ProviderID != 5 {
		t.Errorf("got %+v", got)
	}
}

func TestListAICatalogModels_TypeAndKeys(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/org/test-org/ai-catalog-models" || r.URL.Query().Get("type") != "anthropic" {
			t.Errorf("got %s", r.URL.String())
		}
		writeEnvelope(t, w, http.StatusOK, map[string]any{"models": []map[string]any{{"model": "a"}, {"model": "b"}}})
	})
	got, err := c.ListAICatalogModels(context.Background(), "anthropic", "")
	if err != nil {
		t.Fatalf("ListAICatalogModels: %v", err)
	}
	if !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Errorf("got %v", got)
	}
}

func TestListAIBudgets_NullScopes(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/org/test-org/ai-budgets" {
			t.Errorf("got %s", r.URL.Path)
		}
		writeEnvelope(t, w, http.StatusOK, map[string]any{
			"budgets":    []map[string]any{{"budgetId": 1, "providerId": 4, "modelId": nil, "amount": 12.5, "unit": "usd", "period": "monthly", "enforcement": "block", "enabled": true}},
			"pagination": map[string]any{"total": 1},
		})
	})
	got, err := c.ListAIBudgets(context.Background())
	if err != nil {
		t.Fatalf("ListAIBudgets: %v", err)
	}
	if len(got) != 1 || got[0].ProviderID == nil || *got[0].ProviderID != 4 || got[0].ModelID != nil || got[0].Amount != 12.5 {
		t.Errorf("got %+v", got)
	}
}

func TestListVirtualAPIKeys_FiltersAndNoSecret(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if r.URL.Path != "/v1/org/test-org/virtual-api-keys" || q.Get("search") != "ci" || q.Get("resourceId") != "3" {
			t.Errorf("got %s", r.URL.String())
		}
		writeEnvelope(t, w, http.StatusOK, map[string]any{
			"virtualApiKeys": []map[string]any{{"virtualApiKeyId": "k1", "name": "ci", "lastChars": "ab12", "allResources": false, "resourceIds": []int{3}, "createdAt": 1700000000000}},
			"pagination":     map[string]any{"total": 1},
		})
	})
	rid := 3
	got, err := c.ListVirtualAPIKeys(context.Background(), "ci", "", &rid)
	if err != nil {
		t.Fatalf("ListVirtualAPIKeys: %v", err)
	}
	if len(got) != 1 || got[0].LastChars != "ab12" || got[0].Secret != "" || !reflect.DeepEqual(got[0].ResourceIDs, []int{3}) {
		t.Errorf("got %+v", got)
	}
}
