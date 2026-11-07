package client

import (
	"context"
	"net/http"
	"testing"
)

func TestClientRoleRoutes(t *testing.T) {
	var calls []string
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		if r.Method == "GET" {
			writeEnvelope(t, w, http.StatusOK, map[string]any{"clientId": 7, "roles": []map[string]any{{"roleId": 3, "name": "ci"}}})
			return
		}
		writeEnvelope(t, w, http.StatusOK, nil)
	})
	ctx := context.Background()
	if err := c.AddRoleToClient(ctx, 7, 3); err != nil {
		t.Fatal(err)
	}
	if has, err := c.ClientHasRole(ctx, 7, 3); err != nil || !has {
		t.Errorf("ClientHasRole(3) = %v, %v", has, err)
	}
	if has, _ := c.ClientHasRole(ctx, 7, 4); has {
		t.Error("ClientHasRole(4) must be false")
	}
	if err := c.RemoveRoleFromClient(ctx, 7, 3); err != nil {
		t.Fatal(err)
	}
	want := []string{"POST /v1/client/7/add-role/3", "GET /v1/client/7", "GET /v1/client/7", "DELETE /v1/client/7/remove-role/3"}
	if len(calls) != len(want) {
		t.Fatalf("calls = %v", calls)
	}
	for i := range want {
		if calls[i] != want[i] {
			t.Errorf("call %d = %s, want %s", i, calls[i], want[i])
		}
	}
}
