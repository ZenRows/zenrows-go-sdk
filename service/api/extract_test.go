package scraperapi_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	scraperapi "github.com/zenrows/zenrows-go-sdk/service/api"
)

func TestFetchIsAnAliasForGet(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path + "?" + r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := scraperapi.NewClient(scraperapi.WithBaseURL(server.URL), scraperapi.WithAPIKey("test-key"))

	res, err := client.Fetch(context.Background(), "https://example.com", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.IsSuccess() {
		t.Fatalf("expected success, got status %d", res.StatusCode())
	}
	if gotPath == "" {
		t.Fatal("expected request to reach the server")
	}
}

func TestExtractDefaultsToAutoMode(t *testing.T) {
	var gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := scraperapi.NewClient(scraperapi.WithBaseURL(server.URL), scraperapi.WithAPIKey("test-key"))

	if _, err := client.Extract(context.Background(), "https://example.com", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !contains(gotQuery, "extract=auto") {
		t.Fatalf("expected query to contain extract=auto, got %q", gotQuery)
	}
}

func TestExtractSendsAdaptiveStealthByDefault(t *testing.T) {
	var gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := scraperapi.NewClient(scraperapi.WithBaseURL(server.URL), scraperapi.WithAPIKey("test-key"))

	if _, err := client.Extract(context.Background(), "https://example.com", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !contains(gotQuery, "mode=auto") {
		t.Fatalf("expected query to contain mode=auto, got %q", gotQuery)
	}
}

func TestExtractOmitsModeWhenAdaptiveStealthDisabled(t *testing.T) {
	var gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := scraperapi.NewClient(scraperapi.WithBaseURL(server.URL), scraperapi.WithAPIKey("test-key"))

	params := &scraperapi.RequestParameters{DisableAdaptiveStealth: true}
	if _, err := client.Extract(context.Background(), "https://example.com", params); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if contains(gotQuery, "mode=") {
		t.Fatalf("expected query to omit mode, got %q", gotQuery)
	}
}

func TestExtractFallbackRequestAlsoCarriesAdaptiveStealth(t *testing.T) {
	var calls int
	var secondQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(http.StatusPaymentRequired)
			_, _ = w.Write([]byte(`{"code":"AUTH010","status":402}`))
			return
		}
		secondQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := scraperapi.NewClient(scraperapi.WithBaseURL(server.URL), scraperapi.WithAPIKey("test-key"))

	if _, err := client.Extract(context.Background(), "https://example.com", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !contains(secondQuery, "mode=auto") {
		t.Fatalf("expected fallback request to also contain mode=auto, got %q", secondQuery)
	}
}

func TestExtractRespectsExplicitMode(t *testing.T) {
	var gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := scraperapi.NewClient(scraperapi.WithBaseURL(server.URL), scraperapi.WithAPIKey("test-key"))

	params := &scraperapi.RequestParameters{Extract: scraperapi.ExtractModeNative}
	if _, err := client.Extract(context.Background(), "https://example.com", params); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !contains(gotQuery, "extract=native") {
		t.Fatalf("expected query to contain extract=native, got %q", gotQuery)
	}
}

func TestValidateRejectsInvalidExtractMode(t *testing.T) {
	params := &scraperapi.RequestParameters{Extract: "bogus"}
	if err := params.Validate(); err == nil {
		t.Fatal("expected an error for an invalid extract mode")
	}
}

func TestExtractFallsBackToAutoparseOnAuth010(t *testing.T) {
	var calls int
	var secondQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(http.StatusPaymentRequired)
			_, _ = w.Write([]byte(`{"code":"AUTH010","title":"Domain not enabled","status":402}`))
			return
		}
		secondQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := scraperapi.NewClient(scraperapi.WithBaseURL(server.URL), scraperapi.WithAPIKey("test-key"))

	res, err := client.Extract(context.Background(), "https://example.com", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.IsSuccess() {
		t.Fatalf("expected the fallback response to succeed, got status %d", res.StatusCode())
	}
	if calls != 2 {
		t.Fatalf("expected 2 requests (extract + autoparse fallback), got %d", calls)
	}
	if !contains(secondQuery, "autoparse=true") {
		t.Fatalf("expected fallback request to set autoparse=true, got %q", secondQuery)
	}
	if contains(secondQuery, "extract=") {
		t.Fatalf("expected fallback request to drop the extract param, got %q", secondQuery)
	}
}

func TestExtractFallbackDisabledReturnsAuth010Response(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusPaymentRequired)
		_, _ = w.Write([]byte(`{"code":"AUTH010","status":402}`))
	}))
	defer server.Close()

	client := scraperapi.NewClient(scraperapi.WithBaseURL(server.URL), scraperapi.WithAPIKey("test-key"))

	params := &scraperapi.RequestParameters{DisableAutoparseFallback: true}
	res, err := client.Extract(context.Background(), "https://example.com", params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.StatusCode() != http.StatusPaymentRequired {
		t.Fatalf("expected the raw 402 to be returned, got status %d", res.StatusCode())
	}
	if calls != 1 {
		t.Fatalf("expected fallback to be disabled (1 request), got %d", calls)
	}
}

func TestExtract402WithoutAuth010DoesNotFallBack(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusPaymentRequired)
		_, _ = w.Write([]byte(`{"code":"AUTH004","title":"No credit available","status":402}`))
	}))
	defer server.Close()

	client := scraperapi.NewClient(scraperapi.WithBaseURL(server.URL), scraperapi.WithAPIKey("test-key"))

	res, err := client.Extract(context.Background(), "https://example.com", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.StatusCode() != http.StatusPaymentRequired {
		t.Fatalf("expected the raw 402 to be returned, got status %d", res.StatusCode())
	}
	if calls != 1 {
		t.Fatalf("a non-AUTH010 402 (e.g. out of credits) must not trigger a fallback, got %d requests", calls)
	}
}

func TestExtractNoFallbackForNonAutoMode(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusPaymentRequired)
		_, _ = w.Write([]byte(`{"code":"AUTH010","status":402}`))
	}))
	defer server.Close()

	client := scraperapi.NewClient(scraperapi.WithBaseURL(server.URL), scraperapi.WithAPIKey("test-key"))

	params := &scraperapi.RequestParameters{Extract: scraperapi.ExtractModeNative}
	res, err := client.Extract(context.Background(), "https://example.com", params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.StatusCode() != http.StatusPaymentRequired {
		t.Fatalf("expected the raw 402 to be returned, got status %d", res.StatusCode())
	}
	if calls != 1 {
		t.Fatalf("AUTH010 should only trigger a fallback for the auto mode, got %d requests", calls)
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
