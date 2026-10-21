package client

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestCreateDomain_PathBodyAndID(t *testing.T) {
	var body map[string]any
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/v1/org/test-org/domain" {
			t.Errorf("got %s %s", r.Method, r.URL.Path)
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		writeEnvelope(t, w, http.StatusCreated, map[string]any{
			"domainId":     "dom-1",
			"cnameRecords": []map[string]any{{"baseDomain": "portal.example.com", "value": "x.pangolin.net"}},
		})
	})
	resolver := "letsencrypt"
	prefer := true
	id, err := c.CreateDomain(context.Background(), &CreateDomainRequest{
		Type: "wildcard", BaseDomain: "apps.example.com", CertResolver: &resolver, PreferWildcardCert: &prefer,
	})
	if err != nil {
		t.Fatalf("CreateDomain: %v", err)
	}
	if id != "dom-1" {
		t.Errorf("id = %q, want dom-1", id)
	}
	want := map[string]any{"type": "wildcard", "baseDomain": "apps.example.com", "certResolver": "letsencrypt", "preferWildcardCert": true}
	for k, v := range want {
		if body[k] != v {
			t.Errorf("body[%q] = %v, want %v", k, body[k], v)
		}
	}
}

func TestCreateDomain_OmitsUnsetCertSettings(t *testing.T) {
	var raw []byte
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		raw, _ = io.ReadAll(r.Body)
		writeEnvelope(t, w, http.StatusCreated, map[string]any{"domainId": "dom-2"})
	})
	if _, err := c.CreateDomain(context.Background(), &CreateDomainRequest{Type: "cname", BaseDomain: "portal.example.com"}); err != nil {
		t.Fatalf("CreateDomain: %v", err)
	}
	// The server schema is strict: unset keys must not be sent at all.
	if strings.Contains(string(raw), "certResolver") || strings.Contains(string(raw), "preferWildcardCert") {
		t.Errorf("body = %s, want no cert keys", raw)
	}
}

func TestCreateDomain_MissingIDIsAnError(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeEnvelope(t, w, http.StatusCreated, map[string]any{})
	})
	if _, err := c.CreateDomain(context.Background(), &CreateDomainRequest{Type: "wildcard", BaseDomain: "a.example.com"}); err == nil {
		t.Fatal("expected an error for a response without domainId")
	}
}

func TestUpdateDomain_SendsNullResolverToClear(t *testing.T) {
	var raw []byte
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/org/test-org/domain/dom-1" {
			t.Errorf("got %s %s", r.Method, r.URL.Path)
		}
		raw, _ = io.ReadAll(r.Body)
		writeEnvelope(t, w, http.StatusOK, map[string]any{"domainId": "dom-1", "certResolver": nil, "preferWildcardCert": false})
	})
	prefer := false
	if err := c.UpdateDomain(context.Background(), "dom-1", &UpdateDomainRequest{PreferWildcardCert: &prefer}); err != nil {
		t.Fatalf("UpdateDomain: %v", err)
	}
	if got := string(raw); got != `{"certResolver":null,"preferWildcardCert":false}` {
		t.Errorf("body = %s", got)
	}
}

func TestDeleteDomain_Path(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/v1/org/test-org/domain/dom-1" {
			t.Errorf("got %s %s", r.Method, r.URL.Path)
		}
		writeEnvelope(t, w, http.StatusOK, map[string]any{"success": true})
	})
	if err := c.DeleteDomain(context.Background(), "dom-1"); err != nil {
		t.Fatalf("DeleteDomain: %v", err)
	}
}
