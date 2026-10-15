package scraperapi_test

import (
	"context"
	"fmt"
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

	params := &scraperapi.RequestParameters{AutoparseFallback: true}
	if _, err := client.Extract(context.Background(), "https://example.com", params); err != nil {
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

// problemResponse is one canned answer from the fake API.
type problemResponse struct {
	status int
	body   string
}

// extractAgainst runs Client.Extract against a fake API that answers each request from
// responses in order (repeating the last one) and returns the response and every query sent.
func extractAgainst(
	t *testing.T, params *scraperapi.RequestParameters, responses ...problemResponse,
) (*scraperapi.Response, []string) {
	t.Helper()
	var queries []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.RawQuery)
		next := responses[min(len(queries), len(responses))-1]
		if next.status >= http.StatusBadRequest {
			w.Header().Set("Content-Type", "application/problem+json")
		}
		w.WriteHeader(next.status)
		_, _ = w.Write([]byte(next.body))
	}))
	t.Cleanup(server.Close)

	client := scraperapi.NewClient(scraperapi.WithBaseURL(server.URL), scraperapi.WithAPIKey("test-key"))
	res, err := client.Extract(context.Background(), "https://example.com", params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return res, queries
}

var (
	reqs007 = problemResponse{http.StatusForbidden, `{"code":"REQS007","title":"Domain not prepared for Extract","status":403}`}
	auth010 = problemResponse{http.StatusPaymentRequired, `{"code":"AUTH010","title":"Domain not enabled","status":402}`}
	okBody  = problemResponse{http.StatusOK, `[{"found":"via autoparse"}]`}
)

// REQS007 means the domain isn't prepared for Extract yet. The caller has to see it, so they
// can prepare the domain; retrying with AutoParse by default would hide it and charge them for it.
func TestExtractReturnsDomainUnavailableWithoutFallbackByDefault(t *testing.T) {
	for name, notReady := range map[string]problemResponse{"REQS007": reqs007, "AUTH010": auth010} {
		t.Run(name, func(t *testing.T) {
			res, queries := extractAgainst(t, nil, notReady, okBody)

			if res.StatusCode() != notReady.status {
				t.Fatalf("expected the raw %d to be returned, got status %d", notReady.status, res.StatusCode())
			}
			if len(queries) != 1 {
				t.Fatalf("%s must not trigger an AutoParse fallback by default, got %d requests", name, len(queries))
			}
			if contains(queries[0], "autoparse") {
				t.Fatalf("expected no autoparse param, got %q", queries[0])
			}
		})
	}
}

func TestExtractAutoparseFallbackRetriesOnce(t *testing.T) {
	for name, notReady := range map[string]problemResponse{"REQS007": reqs007, "AUTH010": auth010} {
		t.Run(name, func(t *testing.T) {
			params := &scraperapi.RequestParameters{AutoparseFallback: true}
			res, queries := extractAgainst(t, params, notReady, okBody)

			if !res.IsSuccess() {
				t.Fatalf("expected the fallback response to succeed, got status %d", res.StatusCode())
			}
			if len(queries) != 2 {
				t.Fatalf("expected 2 requests (extract + autoparse fallback), got %d", len(queries))
			}
			if !contains(queries[1], "autoparse=true") {
				t.Fatalf("expected fallback request to set autoparse=true, got %q", queries[1])
			}
			if contains(queries[1], "extract=") {
				t.Fatalf("expected fallback request to drop the extract param, got %q", queries[1])
			}
		})
	}
}

func TestExtractAutoparseFallbackDoesNotRetryTwice(t *testing.T) {
	params := &scraperapi.RequestParameters{AutoparseFallback: true}
	res, queries := extractAgainst(t, params, reqs007)

	if res.StatusCode() != http.StatusForbidden {
		t.Fatalf("expected the failed fallback's 403 to be returned, got status %d", res.StatusCode())
	}
	if len(queries) != 2 {
		t.Fatalf("expected exactly one fallback (2 requests), got %d", len(queries))
	}
}

func TestExtractOther4xxNeverFallBack(t *testing.T) {
	cases := map[string]problemResponse{
		"403 bad key":         {http.StatusForbidden, `{"code":"AUTH002","title":"Invalid API key","status":403}`},
		"402 out of credits":  {http.StatusPaymentRequired, `{"code":"AUTH004","title":"No credit available","status":402}`},
		"REQS007 on a 400":    {http.StatusBadRequest, `{"code":"REQS007","status":400}`},
		"AUTH010 on a 403":    {http.StatusForbidden, `{"code":"AUTH010","status":403}`},
		"404 without problem": {http.StatusNotFound, `{}`},
	}
	for name, other := range cases {
		for _, fallback := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/AutoparseFallback=%t", name, fallback), func(t *testing.T) {
				params := &scraperapi.RequestParameters{AutoparseFallback: fallback}
				res, queries := extractAgainst(t, params, other, okBody)

				if res.StatusCode() != other.status {
					t.Fatalf("expected the raw %d to be returned, got status %d", other.status, res.StatusCode())
				}
				if len(queries) != 1 {
					t.Fatalf("expected no fallback, got %d requests", len(queries))
				}
			})
		}
	}
}

func TestExtractNoFallbackForNonAutoMode(t *testing.T) {
	params := &scraperapi.RequestParameters{Extract: scraperapi.ExtractModeNative, AutoparseFallback: true}
	res, queries := extractAgainst(t, params, auth010, okBody)

	if res.StatusCode() != http.StatusPaymentRequired {
		t.Fatalf("expected the raw 402 to be returned, got status %d", res.StatusCode())
	}
	if len(queries) != 1 {
		t.Fatalf("AUTH010 should only trigger a fallback for the auto mode, got %d requests", len(queries))
	}
}

// The deprecated DisableAutoparseFallback still compiles and still wins over AutoparseFallback.
func TestExtractDeprecatedDisableAutoparseFallbackStillWins(t *testing.T) {
	params := &scraperapi.RequestParameters{
		AutoparseFallback:        true,
		DisableAutoparseFallback: true,
	}
	res, queries := extractAgainst(t, params, auth010, okBody)

	if res.StatusCode() != http.StatusPaymentRequired {
		t.Fatalf("expected the raw 402 to be returned, got status %d", res.StatusCode())
	}
	if len(queries) != 1 {
		t.Fatalf("expected fallback to be disabled (1 request), got %d", len(queries))
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

func TestExtractSkipsAdaptiveStealthWithForcedRendering(t *testing.T) {
	cases := map[string]*scraperapi.RequestParameters{
		"JSRender":                {JSRender: true},
		"UsePremiumProxies":       {UsePremiumProxies: true},
		"js_render custom param":  {CustomParams: map[string]string{"js_render": "true"}},
		"premium_proxy as custom": {CustomParams: map[string]string{"premium_proxy": "TRUE"}},
	}
	for name, params := range cases {
		t.Run(name, func(t *testing.T) {
			var gotQuery string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotQuery = r.URL.RawQuery
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			client := scraperapi.NewClient(scraperapi.WithBaseURL(server.URL), scraperapi.WithAPIKey("test-key"))

			if _, err := client.Extract(context.Background(), "https://example.com", params); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if contains(gotQuery, "mode=") {
				t.Fatalf("expected query to omit mode, got %q", gotQuery)
			}
		})
	}
}

func TestExtractKeepsAdaptiveStealthWhenRenderingIsFalse(t *testing.T) {
	var gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := scraperapi.NewClient(scraperapi.WithBaseURL(server.URL), scraperapi.WithAPIKey("test-key"))

	params := &scraperapi.RequestParameters{CustomParams: map[string]string{"js_render": "false"}}
	if _, err := client.Extract(context.Background(), "https://example.com", params); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !contains(gotQuery, "mode=auto") {
		t.Fatalf("expected query to contain mode=auto, got %q", gotQuery)
	}
}

func TestExtractFallbackSkipsAdaptiveStealthWithPremiumProxy(t *testing.T) {
	var queries []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.RawQuery)
		if len(queries) == 1 {
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(http.StatusPaymentRequired)
			_, _ = w.Write([]byte(`{"code":"AUTH010","status":402}`))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := scraperapi.NewClient(scraperapi.WithBaseURL(server.URL), scraperapi.WithAPIKey("test-key"))

	params := &scraperapi.RequestParameters{UsePremiumProxies: true, AutoparseFallback: true}
	if _, err := client.Extract(context.Background(), "https://example.com", params); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(queries) != 2 {
		t.Fatalf("expected 2 requests, got %d", len(queries))
	}
	for i, q := range queries {
		if contains(q, "mode=") {
			t.Fatalf("expected request %d to omit mode, got %q", i+1, q)
		}
	}
	if !contains(queries[1], "autoparse=true") {
		t.Fatalf("expected the fallback request to set autoparse, got %q", queries[1])
	}
}
