package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
)

func TestSiteProvisioningKey_GetAndUpdateBody(t *testing.T) {
	var body map[string]json.RawMessage
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "GET":
			writeEnvelope(t, w, http.StatusOK, map[string]any{"siteProvisioningKeys": []map[string]any{
				{"siteProvisioningKeyId": "k1", "name": "n", "maxBatchSize": nil, "validUntil": nil, "approveNewSites": true},
			}})
		case "PATCH":
			if r.URL.Path != "/v1/org/test-org/site-provisioning-key/k1" {
				t.Errorf("path = %s", r.URL.Path)
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			writeEnvelope(t, w, http.StatusOK, nil)
		}
	})
	ctx := context.Background()
	got, err := c.GetSiteProvisioningKey(ctx, "k1")
	if err != nil || got.MaxBatchSize != nil || !got.ApproveNewSites {
		t.Fatalf("Get = %+v, %v", got, err)
	}
	if _, err := c.GetSiteProvisioningKey(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing key: %v", err)
	}
	if err := c.UpdateSiteProvisioningKey(ctx, "k1", &UpdateSiteProvisioningKeyRequest{}); err != nil {
		t.Fatal(err)
	}
	// Clearing must be explicit: null removes the limit, "" removes the expiry.
	if string(body["maxBatchSize"]) != "null" || string(body["validUntil"]) != `""` || string(body["approveNewSites"]) != "false" {
		t.Errorf("update body = %v", body)
	}
}
