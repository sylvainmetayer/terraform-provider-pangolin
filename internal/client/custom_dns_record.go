package client

import (
	"context"
	"encoding/json"
	"fmt"
)

// CustomDNSRecord is a record that Pangolin's authoritative DNS server
// serves for a domain delegated to it (type "ns"), Pangolin 1.26.0+,
// Enterprise. Each entry of Values is served as its own resource record.
type CustomDNSRecord struct {
	CustomDNSRecordID int      `json:"customDnsRecordId"`
	DomainID          string   `json:"domainId"`
	Name              string   `json:"name"`
	FullDomain        string   `json:"fullDomain"`
	Type              string   `json:"type"`
	Values            []string `json:"values"`
	TTL               int      `json:"ttl"`
	Priority          *int     `json:"priority"`
	Weight            *int     `json:"weight"`
	Port              *int     `json:"port"`
	Enabled           bool     `json:"enabled"`
}

// CustomDNSRecordRequest is the body of create (PUT) and update (POST).
// Every field is sent, so an update replaces the whole record; the server
// stores null priority / weight / port for the types without them.
type CustomDNSRecordRequest struct {
	Name     string   `json:"name"`
	Type     string   `json:"type"`
	Values   []string `json:"values"`
	TTL      int      `json:"ttl"`
	Priority *int     `json:"priority"`
	Weight   *int     `json:"weight"`
	Port     *int     `json:"port"`
	Enabled  bool     `json:"enabled"`
}

func customDNSRecordPath(orgID, domainID string) string {
	return fmt.Sprintf("/org/%s/domain/%s/custom-dns-record", orgID, domainID)
}

func parseCustomDNSRecord(data []byte) (*CustomDNSRecord, error) {
	var r CustomDNSRecord
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("failed to parse custom DNS record: %w", err)
	}
	if r.CustomDNSRecordID == 0 {
		return nil, fmt.Errorf("custom DNS record response has no customDnsRecordId: %s", truncateForDiag(data))
	}
	return &r, nil
}

// CreateCustomDNSRecord creates a record. Not retried: a repeated PUT
// after a lost response would hit the server's one-record-per-name-and-
// type rule, or add a second MX / SRV record.
func (c *Client) CreateCustomDNSRecord(ctx context.Context, domainID string, req *CustomDNSRecordRequest) (*CustomDNSRecord, error) {
	resp, err := c.doRequestNoRetry(ctx, "PUT", customDNSRecordPath(c.OrgID, domainID), req)
	if err != nil {
		return nil, err
	}
	return parseCustomDNSRecord(resp.Data)
}

// GetCustomDNSRecord reads a record. The server has no single GET: it
// lists the domain's records (at most 100 per domain, no pagination).
func (c *Client) GetCustomDNSRecord(ctx context.Context, domainID string, id int) (*CustomDNSRecord, error) {
	resp, err := c.doRequest(ctx, "GET", fmt.Sprintf("/org/%s/domain/%s/custom-dns-records", c.OrgID, domainID), nil)
	if err != nil {
		return nil, err
	}
	var out struct {
		Records []CustomDNSRecord `json:"records"`
	}
	if err := json.Unmarshal(resp.Data, &out); err != nil {
		return nil, fmt.Errorf("failed to parse custom DNS records: %w", err)
	}
	for i := range out.Records {
		if out.Records[i].CustomDNSRecordID == id {
			return &out.Records[i], nil
		}
	}
	return nil, ErrNotFound
}

// UpdateCustomDNSRecord replaces a record's settings.
func (c *Client) UpdateCustomDNSRecord(ctx context.Context, domainID string, id int, req *CustomDNSRecordRequest) (*CustomDNSRecord, error) {
	resp, err := c.doRequest(ctx, "POST", fmt.Sprintf("%s/%d", customDNSRecordPath(c.OrgID, domainID), id), req)
	if err != nil {
		return nil, err
	}
	return parseCustomDNSRecord(resp.Data)
}

// DeleteCustomDNSRecord deletes a record.
func (c *Client) DeleteCustomDNSRecord(ctx context.Context, domainID string, id int) error {
	_, err := c.doRequest(ctx, "DELETE", fmt.Sprintf("%s/%d", customDNSRecordPath(c.OrgID, domainID), id), nil)
	return err
}
