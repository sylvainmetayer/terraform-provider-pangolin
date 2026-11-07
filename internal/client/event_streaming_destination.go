package client

import (
	"context"
	"encoding/json"
	"fmt"
)

// EventStreamingDestination is a log streaming target of an org
// (Pangolin 1.24+, Enterprise): Pangolin pushes its request, action,
// access, connection and AI session logs to it every 30 seconds.
type EventStreamingDestination struct {
	DestinationID      int     `json:"destinationId"`
	OrgID              string  `json:"orgId"`
	Type               string  `json:"type"`
	Config             string  `json:"config"`
	Enabled            bool    `json:"enabled"`
	SendConnectionLogs bool    `json:"sendConnectionLogs"`
	SendRequestLogs    bool    `json:"sendRequestLogs"`
	SendActionLogs     bool    `json:"sendActionLogs"`
	SendAccessLogs     bool    `json:"sendAccessLogs"`
	SendAISessionLogs  bool    `json:"sendAISessionLogs"`
	LastError          *string `json:"lastError"`
}

// EventStreamingDestinationRequest is the body of the create (PUT) and
// update (POST) routes. Every field is always sent, so a log type can be
// turned off.
type EventStreamingDestinationRequest struct {
	Type               string `json:"type"`
	Config             string `json:"config"`
	Enabled            bool   `json:"enabled"`
	SendConnectionLogs bool   `json:"sendConnectionLogs"`
	SendRequestLogs    bool   `json:"sendRequestLogs"`
	SendActionLogs     bool   `json:"sendActionLogs"`
	SendAccessLogs     bool   `json:"sendAccessLogs"`
	SendAISessionLogs  bool   `json:"sendAISessionLogs"`
}

// CreateEventStreamingDestination creates a destination and returns its
// ID. Not retried: a repeated PUT after a lost response would create a
// second destination, which would get every log twice.
func (c *Client) CreateEventStreamingDestination(ctx context.Context, req *EventStreamingDestinationRequest) (int, error) {
	resp, err := c.doRequestNoRetry(ctx, "PUT", fmt.Sprintf("/org/%s/event-streaming-destination", c.OrgID), req)
	if err != nil {
		return 0, err
	}
	var out struct {
		DestinationID int `json:"destinationId"`
	}
	if err := json.Unmarshal(resp.Data, &out); err != nil {
		return 0, fmt.Errorf("failed to parse event streaming destination: %w", err)
	}
	if out.DestinationID == 0 {
		return 0, fmt.Errorf("event streaming destination response has no destinationId: %s", truncateForDiag(resp.Data))
	}
	return out.DestinationID, nil
}

// GetEventStreamingDestination reads a destination. The server has no
// single GET: it pages through the org's list (1000 per page, its
// default). The config comes back decrypted.
func (c *Client) GetEventStreamingDestination(ctx context.Context, id int) (*EventStreamingDestination, error) {
	const limit = 1000
	for offset := 0; ; offset += limit {
		resp, err := c.doRequest(ctx, "GET", fmt.Sprintf("/org/%s/event-streaming-destinations?limit=%d&offset=%d", c.OrgID, limit, offset), nil)
		if err != nil {
			return nil, err
		}
		var out struct {
			Destinations []EventStreamingDestination `json:"destinations"`
		}
		if err := json.Unmarshal(resp.Data, &out); err != nil {
			return nil, fmt.Errorf("failed to parse event streaming destinations: %w", err)
		}
		for i := range out.Destinations {
			if out.Destinations[i].DestinationID == id {
				return &out.Destinations[i], nil
			}
		}
		if len(out.Destinations) < limit {
			return nil, ErrNotFound
		}
	}
}

// UpdateEventStreamingDestination updates a destination.
func (c *Client) UpdateEventStreamingDestination(ctx context.Context, id int, req *EventStreamingDestinationRequest) error {
	_, err := c.doRequest(ctx, "POST", fmt.Sprintf("/org/%s/event-streaming-destination/%d", c.OrgID, id), req)
	return err
}

// DeleteEventStreamingDestination deletes a destination.
func (c *Client) DeleteEventStreamingDestination(ctx context.Context, id int) error {
	_, err := c.doRequest(ctx, "DELETE", fmt.Sprintf("/org/%s/event-streaming-destination/%d", c.OrgID, id), nil)
	return err
}
