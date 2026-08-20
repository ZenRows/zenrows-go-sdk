package scraperapi_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	scraperapi "github.com/zenrows/zenrows-go-sdk/service/api"
)

func TestGetRejectsWhenClientNotConfigured(t *testing.T) {
	// Force both to empty explicitly rather than relying on defaults, since defaultOptions()
	// reads ZENROWS_API_KEY from the environment and this test must be deterministic regardless
	// of what's set there.
	client := scraperapi.NewClient(scraperapi.WithBaseURL(""), scraperapi.WithAPIKey(""))
	_, err := client.Get(context.Background(), "https://example.com", nil)

	var notConfigured scraperapi.NotConfiguredError
	if !errors.As(err, &notConfigured) {
		t.Fatalf("expected NotConfiguredError, got %v", err)
	}
}

func TestGetRejectsEmptyTargetURL(t *testing.T) {
	client := scraperapi.NewClient(scraperapi.WithBaseURL("https://api.zenrows.com/v1"), scraperapi.WithAPIKey("k"))

	_, err := client.Get(context.Background(), "", nil)

	var invalidURL scraperapi.InvalidTargetURLError
	if !errors.As(err, &invalidURL) {
		t.Fatalf("expected InvalidTargetURLError, got %v", err)
	}
}

func TestGetRejectsMalformedTargetURL(t *testing.T) {
	client := scraperapi.NewClient(scraperapi.WithBaseURL("https://api.zenrows.com/v1"), scraperapi.WithAPIKey("k"))

	// A control character makes url.Parse fail.
	_, err := client.Get(context.Background(), "https://example.com/\x7f", nil)

	var invalidURL scraperapi.InvalidTargetURLError
	if !errors.As(err, &invalidURL) {
		t.Fatalf("expected InvalidTargetURLError, got %v", err)
	}
}

func TestGetPropagatesParameterValidationErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("server should not be called when parameter validation fails")
	}))
	defer server.Close()

	client := scraperapi.NewClient(scraperapi.WithBaseURL(server.URL), scraperapi.WithAPIKey("k"))
	_, err := client.Get(context.Background(), "https://example.com", &scraperapi.RequestParameters{Extract: "bogus"})

	var invalidParam scraperapi.InvalidParameterError
	if !errors.As(err, &invalidParam) {
		t.Fatalf("expected InvalidParameterError, got %v", err)
	}
}

func TestPostSendsBodyAndMethod(t *testing.T) {
	var gotMethod, gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		buf := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(buf)
		gotBody = string(buf)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := scraperapi.NewClient(scraperapi.WithBaseURL(server.URL), scraperapi.WithAPIKey("k"))
	_, err := client.Post(context.Background(), "https://example.com", nil, map[string]string{"a": "b"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Fatalf("expected POST, got %s", gotMethod)
	}
	if gotBody == "" {
		t.Fatal("expected a non-empty request body")
	}
}

func TestPutSendsMethod(t *testing.T) {
	var gotMethod string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := scraperapi.NewClient(scraperapi.WithBaseURL(server.URL), scraperapi.WithAPIKey("k"))
	_, err := client.Put(context.Background(), "https://example.com", nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotMethod != http.MethodPut {
		t.Fatalf("expected PUT, got %s", gotMethod)
	}
}

func TestFetchIsEquivalentToGet(t *testing.T) {
	var getPath, fetchPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if getPath == "" {
			getPath = r.URL.RawQuery
		} else {
			fetchPath = r.URL.RawQuery
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := scraperapi.NewClient(scraperapi.WithBaseURL(server.URL), scraperapi.WithAPIKey("k"))
	if _, err := client.Get(context.Background(), "https://example.com", nil); err != nil {
		t.Fatalf("Get: unexpected error: %v", err)
	}
	if _, err := client.Fetch(context.Background(), "https://example.com", nil); err != nil {
		t.Fatalf("Fetch: unexpected error: %v", err)
	}
	if getPath != fetchPath {
		t.Fatalf("expected Fetch to produce the same request as Get: %q vs %q", getPath, fetchPath)
	}
}

func TestCustomHeadersAreForwardedToTargetRequest(t *testing.T) {
	var gotReferer string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotReferer = r.Header.Get("Referer")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := scraperapi.NewClient(scraperapi.WithBaseURL(server.URL), scraperapi.WithAPIKey("k"))
	params := &scraperapi.RequestParameters{CustomHeaders: http.Header{"Referer": []string{"https://google.com"}}}
	if _, err := client.Get(context.Background(), "https://example.com", params); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotReferer != "https://google.com" {
		t.Fatalf("expected custom Referer header to reach the request, got %q", gotReferer)
	}
}

func TestWithMaxRetryCountRetriesRetryableStatusCodes(t *testing.T) {
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits.Add(1) <= 2 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := scraperapi.NewClient(
		scraperapi.WithBaseURL(server.URL),
		scraperapi.WithAPIKey("k"),
		scraperapi.WithMaxRetryCount(3),
		scraperapi.WithRetryWaitTime(1*time.Millisecond),
		scraperapi.WithRetryMaxWaitTime(5*time.Millisecond),
	)

	res, err := client.Get(context.Background(), "https://example.com", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.IsSuccess() {
		t.Fatalf("expected the retried request to eventually succeed, got status %d", res.StatusCode())
	}
	if hits.Load() != 3 {
		t.Fatalf("expected 3 total attempts (2 failures + 1 success), got %d", hits.Load())
	}
}

func TestWithoutRetryCountDoesNotRetry(t *testing.T) {
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	client := scraperapi.NewClient(scraperapi.WithBaseURL(server.URL), scraperapi.WithAPIKey("k"))
	res, err := client.Get(context.Background(), "https://example.com", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.StatusCode() != http.StatusTooManyRequests {
		t.Fatalf("expected the 429 to be returned as-is, got %d", res.StatusCode())
	}
	if hits.Load() != 1 {
		t.Fatalf("expected exactly 1 attempt with no retry configured, got %d", hits.Load())
	}
}

func TestWithMaxConcurrentRequestsLimitsInFlightRequests(t *testing.T) {
	const maxConcurrent = 2
	var (
		current     atomic.Int32
		maxObserved atomic.Int32
	)
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := current.Add(1)
		defer current.Add(-1)
		for {
			observed := maxObserved.Load()
			if n <= observed || maxObserved.CompareAndSwap(observed, n) {
				break
			}
		}
		<-release
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := scraperapi.NewClient(
		scraperapi.WithBaseURL(server.URL),
		scraperapi.WithAPIKey("k"),
		scraperapi.WithMaxConcurrentRequests(maxConcurrent),
	)

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = client.Get(context.Background(), "https://example.com", nil)
		}()
	}

	// Let requests queue up against the semaphore, then release them all at once.
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()

	if maxObserved.Load() > int32(maxConcurrent) {
		t.Fatalf("expected at most %d concurrent requests, observed %d", maxConcurrent, maxObserved.Load())
	}
}
