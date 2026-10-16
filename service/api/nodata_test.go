package scraperapi_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	scraperapi "github.com/zenrows/zenrows-go-sdk/service/api"
)

func newRetryingServer(t *testing.T, contentType, body string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", contentType)
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server, &hits
}

func newRetryingClient(baseURL string) *scraperapi.Client {
	return scraperapi.NewClient(
		scraperapi.WithBaseURL(baseURL),
		scraperapi.WithAPIKey("k"),
		scraperapi.WithMaxRetryCount(2),
		scraperapi.WithRetryWaitTime(time.Millisecond),
		scraperapi.WithRetryMaxWaitTime(time.Millisecond),
	)
}

func TestExtractDoesNotRetryNoDataExtracted(t *testing.T) {
	for _, contentType := range []string{"application/problem+json", "application/json"} {
		t.Run(contentType, func(t *testing.T) {
			server, hits := newRetryingServer(t, contentType, `{"code":"no_data_extracted","title":"No data extracted"}`)

			response, err := newRetryingClient(server.URL).Extract(context.Background(), "https://example.com", nil)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := hits.Load(); got != 1 {
				t.Fatalf("expected 1 request, got %d", got)
			}
			if !response.IsNoDataExtracted() {
				t.Fatal("expected IsNoDataExtracted to be true")
			}
		})
	}
}

// No 422 is retried: the API no longer sends a 422 that another try could fix.
func TestOther422IsNotRetried(t *testing.T) {
	server, hits := newRetryingServer(t, "application/problem+json", `{"code":"RESP001","title":"Could not get content"}`)

	response, err := newRetryingClient(server.URL).Get(context.Background(), "https://example.com", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := hits.Load(); got != 1 {
		t.Fatalf("expected 1 request, got %d", got)
	}
	if response.IsNoDataExtracted() {
		t.Fatal("expected IsNoDataExtracted to be false for RESP001")
	}
}
