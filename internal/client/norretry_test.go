package client

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"
)

func TestDoRequestNoRetry_RetriesOnlyUnsentFailures(t *testing.T) {
	disableRetryBackoff(t)
	for _, tc := range []struct {
		name   string
		status int
		want   int32
	}{
		{"429 is retried", http.StatusTooManyRequests, int32(maxAttempts)},
		{"502 is not retried", http.StatusBadGateway, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls int32
			c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				atomic.AddInt32(&calls, 1)
				writeEnvelope(t, w, tc.status, nil)
			})
			_, _ = c.doRequestNoRetry(context.Background(), http.MethodPut, "/x", nil)
			if got := atomic.LoadInt32(&calls); got != tc.want {
				t.Errorf("calls = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestDoRequestNoRetry_RetriesDialErrors(t *testing.T) {
	disableRetryBackoff(t)
	c := NewClient("http://127.0.0.1:1", "k", "o")
	_, err := c.doRequestNoRetry(context.Background(), http.MethodPut, "/x", nil)
	if err == nil || !shouldRetryUnsent(err) {
		t.Fatalf("expected a retryable dial error, got %v", err)
	}
}
