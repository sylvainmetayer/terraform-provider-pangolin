package client

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"testing"
)

func TestMTLSCertificate_PathsAndBody(t *testing.T) {
	name := "ca"
	for _, tc := range []struct {
		owner MTLSOwner
		base  string
	}{
		{MTLSOwner{ResourceID: 7}, "/v1/resource/7"},
		{MTLSOwner{ResourcePolicyID: 8}, "/v1/resource-policy/8"},
	} {
		c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodPut:
				if r.URL.Path != tc.base+"/mtls/certificate" {
					t.Errorf("create path = %s", r.URL.Path)
				}
				raw, _ := io.ReadAll(r.Body)
				var body map[string]any
				_ = json.Unmarshal(raw, &body)
				if len(body) != 2 || body["name"] != "ca" || body["certificate"] != "pem" {
					t.Errorf("body = %s", raw)
				}
				writeEnvelope(t, w, http.StatusCreated, map[string]any{"certificates": []map[string]any{{"mtlsCertificateId": 3, "name": "ca", "fingerprint": "AA"}}})
			case http.MethodGet:
				if r.URL.Path != tc.base+"/mtls/certificates" {
					t.Errorf("list path = %s", r.URL.Path)
				}
				writeEnvelope(t, w, http.StatusOK, map[string]any{"certificates": []map[string]any{{"mtlsCertificateId": 3}}})
			case http.MethodDelete:
				if r.URL.Path != tc.base+"/mtls/certificate/3" {
					t.Errorf("delete path = %s", r.URL.Path)
				}
				writeEnvelope(t, w, http.StatusOK, map[string]any{"mtlsDisabled": false})
			}
		})
		ctx := context.Background()
		cert, err := c.CreateMTLSCertificate(ctx, tc.owner, &name, "pem")
		if err != nil || cert.ID != 3 || *cert.Fingerprint != "AA" {
			t.Fatalf("create: %+v, %v", cert, err)
		}
		if _, err := c.GetMTLSCertificate(ctx, tc.owner, 3); err != nil {
			t.Errorf("get: %v", err)
		}
		if _, err := c.GetMTLSCertificate(ctx, tc.owner, 4); !errors.Is(err, ErrNotFound) {
			t.Errorf("get missing: %v", err)
		}
		if err := c.DeleteMTLSCertificate(ctx, tc.owner, 3); err != nil {
			t.Errorf("delete: %v", err)
		}
	}
}

func TestCreateMTLSCertificate_RejectsBundleAnswer(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeEnvelope(t, w, http.StatusCreated, map[string]any{"certificates": []map[string]any{{"mtlsCertificateId": 1}, {"mtlsCertificateId": 2}}})
	})
	if _, err := c.CreateMTLSCertificate(context.Background(), MTLSOwner{ResourceID: 1}, nil, "pem"); err == nil {
		t.Fatal("two certificates accepted")
	}
}
