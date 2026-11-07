package client

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"
)

// TestCreates_NotReplayedOn5xx pins every create call to doRequestNoRetry:
// a 502 may come after the server committed the insert, so the create is
// sent once, while a 429 (rejected before the handler ran) is retried.
func TestCreates_NotReplayedOn5xx(t *testing.T) {
	disableRetryBackoff(t)
	ctx := context.Background()
	creates := []struct {
		name string
		call func(c *Client) error
	}{
		{"CreateSite", func(c *Client) error { _, err := c.CreateSite(ctx, &CreateSiteRequest{}); return err }},
		{"CreateResource", func(c *Client) error { _, err := c.CreateResource(ctx, &CreateResourceRequest{}); return err }},
		{"CreateTarget", func(c *Client) error { _, err := c.CreateTarget(ctx, 1, &CreateTargetRequest{}); return err }},
		{"CreateSiteResource", func(c *Client) error {
			_, err := c.CreateSiteResource(ctx, &CreateSiteResourceRequest{})
			return err
		}},
		{"CreateRole", func(c *Client) error { _, err := c.CreateRole(ctx, &CreateRoleRequest{}); return err }},
		{"CreateAPIKey", func(c *Client) error { _, err := c.CreateAPIKey(ctx, &CreateAPIKeyRequest{}); return err }},
		{"CreateOLMClient", func(c *Client) error { _, err := c.CreateOLMClient(ctx, &CreateOLMClientRequest{}); return err }},
		{"CreateOrg", func(c *Client) error { _, err := c.CreateOrg(ctx, &CreateOrgRequest{}); return err }},
		{"CreateUser", func(c *Client) error { return c.CreateUser(ctx, &CreateUserRequest{}) }},
		{"ApplyBlueprint", func(c *Client) error { return c.ApplyBlueprint(ctx, "e30=") }},
		{"CreateIDP", func(c *Client) error { _, err := c.CreateIDP(ctx, &CreateIDPRequest{}); return err }},
		{"CreateOrgIDP", func(c *Client) error { _, err := c.CreateOrgIDP(ctx, "org", &CreateIDPRequest{}); return err }},
		{"CreateDomain", func(c *Client) error { _, err := c.CreateDomain(ctx, "org", &CreateDomainRequest{}); return err }},
		{"CreateAIModel", func(c *Client) error { _, err := c.CreateAIModel(ctx, 1, &AIModelRequest{}); return err }},
		{"CreateSiteProvisioningKey", func(c *Client) error {
			_, err := c.CreateSiteProvisioningKey(ctx, &CreateSiteProvisioningKeyRequest{})
			return err
		}},
		{"CreateVirtualAPIKey", func(c *Client) error { _, err := c.CreateVirtualAPIKey(ctx, &VirtualAPIKeyRequest{}); return err }},
		{"CreateAIBudget", func(c *Client) error { _, err := c.CreateAIBudget(ctx, &AIBudgetRequest{}); return err }},
		{"CreateCustomDNSRecord", func(c *Client) error {
			_, err := c.CreateCustomDNSRecord(ctx, "d", &CustomDNSRecordRequest{})
			return err
		}},
		{"CreateEventStreamingDestination", func(c *Client) error {
			_, err := c.CreateEventStreamingDestination(ctx, &EventStreamingDestinationRequest{})
			return err
		}},
		{"CreateRedirect", func(c *Client) error { _, err := c.CreateRedirect(ctx, &RedirectRequest{}); return err }},
		{"CreateMTLSCertificate", func(c *Client) error {
			_, err := c.CreateMTLSCertificate(ctx, MTLSOwner{ResourceID: 1}, nil, "pem")
			return err
		}},
		{"CreateResourceRule", func(c *Client) error {
			_, err := c.CreateResourceRule(ctx, 1, &SetResourceRuleRequest{})
			return err
		}},
	}
	statuses := []struct {
		status int
		want   int32
	}{
		{http.StatusBadGateway, 1},
		{http.StatusTooManyRequests, int32(maxAttempts)},
	}
	for _, tc := range creates {
		for _, st := range statuses {
			t.Run(tc.name+"/"+http.StatusText(st.status), func(t *testing.T) {
				var calls int32
				c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
					atomic.AddInt32(&calls, 1)
					if r.Method != http.MethodPut {
						t.Errorf("method = %s, want PUT", r.Method)
					}
					writeEnvelope(t, w, st.status, nil)
				})
				if err := tc.call(c); err == nil {
					t.Fatal("expected an error")
				}
				if got := atomic.LoadInt32(&calls); got != st.want {
					t.Errorf("calls = %d, want %d", got, st.want)
				}
			})
		}
	}
}
