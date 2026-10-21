package client

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

func TestListResourceAIProviders_DecodesShape(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/v1/resource/9/ai-providers" {
			t.Errorf("got %s %s", r.Method, r.URL.Path)
		}
		writeEnvelope(t, w, http.StatusOK, map[string]any{
			"providers": []map[string]any{{
				"providerId":      4,
				"niceId":          "openai",
				"name":            "OpenAI",
				"type":            "openai",
				"enabled":         true,
				"providerEnabled": true,
				"accessMode":      "inherit",
			}},
		})
	})
	got, err := c.ListResourceAIProviders(context.Background(), 9)
	if err != nil {
		t.Fatalf("ListResourceAIProviders: %v", err)
	}
	if len(got) != 1 || got[0].ProviderID != 4 || got[0].AccessMode != "inherit" || !got[0].Enabled {
		t.Errorf("providers = %+v", got)
	}
}

func TestAddRemoveResourceAIProvider_PathAndBody(t *testing.T) {
	for _, tc := range []struct {
		name string
		path string
		call func(c *Client) error
	}{
		{"add", "/v1/resource/9/ai-providers/add", func(c *Client) error {
			return c.AddResourceAIProvider(context.Background(), 9, 4)
		}},
		{"remove", "/v1/resource/9/ai-providers/remove", func(c *Client) error {
			return c.RemoveResourceAIProvider(context.Background(), 9, 4)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" || r.URL.Path != tc.path {
					t.Errorf("got %s %s", r.Method, r.URL.Path)
				}
				raw, _ := io.ReadAll(r.Body)
				if string(raw) != `{"providerId":4}` {
					t.Errorf("body = %s", raw)
				}
				writeEnvelope(t, w, http.StatusOK, map[string]any{})
			})
			if err := tc.call(c); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCreateResource_SendsModeAndAIProviders(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if body["mode"] != "inference" || body["http"] != true {
			t.Errorf("body = %v", body)
		}
		providers, _ := body["aiProviders"].([]any)
		if len(providers) != 1 || providers[0].(map[string]any)["providerId"] != float64(4) {
			t.Errorf("aiProviders = %v", body["aiProviders"])
		}
		writeEnvelope(t, w, http.StatusCreated, map[string]any{"resourceId": 9, "mode": "inference"})
	})
	res, err := c.CreateResource(context.Background(), &CreateResourceRequest{
		Name: "llm", HTTP: true, DomainID: "d", Protocol: "tcp", Mode: "inference",
		AIProviders: []ResourceAIProviderAttachment{{ProviderID: 4}},
	})
	if err != nil || res.ResourceID != 9 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

func TestCreateResource_OmitsEmptyAIProviders(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if _, ok := body["aiProviders"]; ok {
			t.Errorf("aiProviders should be omitted, body = %v", body)
		}
		writeEnvelope(t, w, http.StatusCreated, map[string]any{"resourceId": 1})
	})
	if _, err := c.CreateResource(context.Background(), &CreateResourceRequest{Name: "x", HTTP: true, Mode: "ssh"}); err != nil {
		t.Fatal(err)
	}
}
