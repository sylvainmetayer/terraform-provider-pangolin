package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
)

func TestGetEventStreamingDestination_FindsInListAndNotFound(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/org/test-org/event-streaming-destinations" || r.URL.Query().Get("limit") != "1000" {
			t.Errorf("path = %s?%s", r.URL.Path, r.URL.RawQuery)
		}
		writeEnvelope(t, w, http.StatusOK, map[string]any{"destinations": []map[string]any{
			{"destinationId": 3, "type": "http", "config": `{"url":"https://x"}`, "enabled": true, "sendAccessLogs": true, "lastError": nil},
		}})
	})
	got, err := c.GetEventStreamingDestination(context.Background(), 3)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Config != `{"url":"https://x"}` || !got.SendAccessLogs || got.LastError != nil {
		t.Errorf("got %+v", got)
	}
	if _, err := c.GetEventStreamingDestination(context.Background(), 9); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing id: err = %v, want ErrNotFound", err)
	}
}

func TestUpdateEventStreamingDestination_SendsFalseFlags(t *testing.T) {
	var body map[string]json.RawMessage
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/v1/org/test-org/event-streaming-destination/3" {
			t.Errorf("method/path = %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		writeEnvelope(t, w, http.StatusOK, map[string]any{"destinationId": 3})
	})
	if err := c.UpdateEventStreamingDestination(context.Background(), 3, &EventStreamingDestinationRequest{Type: "http", Config: "{}"}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if string(body["sendRequestLogs"]) != "false" || string(body["enabled"]) != "false" {
		t.Errorf("false flags must be sent, body = %v", body)
	}
}
