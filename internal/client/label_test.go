package client

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestGetLabel_WalksPages(t *testing.T) {
	pages := 0
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		pages++
		if r.URL.Path != "/v1/org/test-org/labels" {
			t.Errorf("path = %s", r.URL.Path)
		}
		id := 1
		if r.URL.Query().Get("page") == "2" {
			id = 2
		}
		writeEnvelope(t, w, http.StatusOK, map[string]any{
			"labels":     []map[string]any{{"labelId": id, "name": fmt.Sprint("l", id), "color": "#fff"}},
			"pagination": map[string]any{"total": 2},
		})
	})
	got, err := c.GetLabel(context.Background(), 2)
	if err != nil {
		t.Fatalf("GetLabel: %v", err)
	}
	if got.Name != "l2" || pages != 2 {
		t.Errorf("got %+v after %d pages", got, pages)
	}
	if _, err := c.GetLabel(context.Background(), 9); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestAttachLabel_PathAndBody(t *testing.T) {
	var raw []byte
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/v1/org/test-org/label/4/attach" {
			t.Errorf("got %s %s", r.Method, r.URL.Path)
		}
		raw, _ = io.ReadAll(r.Body)
		writeEnvelope(t, w, http.StatusOK, nil)
	})
	id := 3
	if err := c.AttachLabel(context.Background(), 4, LabelTarget{SiteResourceID: &id}); err != nil {
		t.Fatalf("AttachLabel: %v", err)
	}
	if strings.TrimSpace(string(raw)) != `{"siteResourceId":3}` {
		t.Errorf("body = %s", raw)
	}
}

func TestLabelAttached(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/org/test-org/site-resources" {
			t.Errorf("path = %s", r.URL.Path)
		}
		items := []map[string]any{{"siteResourceId": 1, "labels": []map[string]any{{"labelId": 9}}}}
		if r.URL.Query().Get("page") == "2" {
			items = []map[string]any{{"siteResourceId": 2, "labels": []map[string]any{{"labelId": 4}}}, {"siteResourceId": 3}}
		}
		writeEnvelope(t, w, http.StatusOK, map[string]any{"siteResources": items, "pagination": map[string]any{"total": 3}})
	})
	for _, tc := range []struct {
		item, label int
		want        bool
	}{
		{2, 4, true},  // on the second page
		{1, 4, false}, // the item has another label
		{3, 4, false}, // the item has no labels key
		{7, 4, false}, // the item is gone
	} {
		id := tc.item
		got, err := c.LabelAttached(context.Background(), tc.label, LabelTarget{SiteResourceID: &id})
		if err != nil {
			t.Fatalf("LabelAttached(%d): %v", tc.item, err)
		}
		if got != tc.want {
			t.Errorf("LabelAttached(item %d) = %v, want %v", tc.item, got, tc.want)
		}
	}
}

// A blocked or archived client still bears its labels, but the clients list
// only returns the active ones unless asked otherwise.
func TestLabelAttached_ClientOfAnyStatus(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/org/test-org/clients" {
			t.Errorf("path = %s", r.URL.Path)
		}
		items := []map[string]any{}
		if strings.Contains(r.URL.Query().Get("status"), "blocked") {
			items = append(items, map[string]any{"clientId": 5, "labels": []map[string]any{{"labelId": 4}}})
		}
		writeEnvelope(t, w, http.StatusOK, map[string]any{"clients": items, "pagination": map[string]any{"total": len(items)}})
	})
	id := 5
	got, err := c.LabelAttached(context.Background(), 4, LabelTarget{ClientID: &id})
	if err != nil || !got {
		t.Errorf("LabelAttached = %v, %v, want true", got, err)
	}
}

// A response of an unexpected shape must fail the read: a miss removes the
// object from the state.
func TestLabelLists_RefuseUnexpectedShape(t *testing.T) {
	pagination := map[string]any{"total": 1}
	for name, payload := range map[string]map[string]any{
		"no list":       {"pagination": pagination},
		"no pagination": {"labels": []map[string]any{{"labelId": 9}}, "sites": []map[string]any{{"siteId": 2}}},
		"no item id":    {"labels": []map[string]any{{"labelId": 9}}, "sites": []map[string]any{{"id": 1}}, "pagination": pagination},
	} {
		t.Run(name, func(t *testing.T) {
			c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
				writeEnvelope(t, w, http.StatusOK, payload)
			})
			id := 1
			if _, err := c.LabelAttached(context.Background(), 4, LabelTarget{SiteID: &id}); err == nil {
				t.Error("LabelAttached: want an error")
			}
			if name == "no item id" {
				return // a label without its ID is just not the one asked for
			}
			if _, err := c.GetLabel(context.Background(), 4); err == nil || errors.Is(err, ErrNotFound) {
				t.Errorf("GetLabel: err = %v, want a non-ErrNotFound error", err)
			}
		})
	}
}
