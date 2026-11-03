package client

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"testing"
)

func TestCreateRedirect_PathAndBody(t *testing.T) {
	var body map[string]any
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/v1/org/test-org/redirect" {
			t.Errorf("got %s %s", r.Method, r.URL.Path)
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		writeEnvelope(t, w, http.StatusCreated, map[string]any{"redirect": map[string]any{"redirectId": 7, "niceId": "n"}})
	})
	domain := "dom"
	got, err := c.CreateRedirect(context.Background(), &RedirectRequest{Name: "r", DomainID: &domain, DestinationHost: "https://example.com", PathMatchType: "prefix", Priority: 100, SSL: true, Enabled: true})
	if err != nil {
		t.Fatalf("CreateRedirect: %v", err)
	}
	if got.RedirectID != 7 {
		t.Errorf("id = %d, want 7", got.RedirectID)
	}
	if body["domainId"] != "dom" || body["pathMatchType"] != "prefix" || body["priority"] != float64(100) {
		t.Errorf("body = %v", body)
	}
}

func TestUpdateRedirect_SendsNullsToClear(t *testing.T) {
	var body map[string]any
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/org/test-org/redirects/7" {
			t.Errorf("got %s %s", r.Method, r.URL.Path)
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		writeEnvelope(t, w, http.StatusOK, map[string]any{"redirect": map[string]any{"redirectId": 7}})
	})
	resourceID := 3
	if _, err := c.UpdateRedirect(context.Background(), 7, &RedirectRequest{Name: "r", ResourceID: &resourceID, DestinationHost: "https://example.com", PathMatchType: "regex", Priority: 100}); err != nil {
		t.Fatalf("UpdateRedirect: %v", err)
	}
	for _, key := range []string{"domainId", "subdomain", "matchPath", "rewritePath", "rewritePathType"} {
		v, sent := body[key]
		if !sent || v != nil {
			t.Errorf("body[%q] = %v (sent %v), want an explicit null", key, v, sent)
		}
	}
}

func TestGetRedirect_NotFound(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		writeEnvelope(t, w, http.StatusNotFound, nil)
	})
	if _, err := c.GetRedirect(context.Background(), 7); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestGetRedirect_MissingIDIsAnError(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		writeEnvelope(t, w, http.StatusOK, map[string]any{"redirect": nil})
	})
	if _, err := c.GetRedirect(context.Background(), 7); err == nil {
		t.Error("want an error on a response without a redirect")
	}
}
