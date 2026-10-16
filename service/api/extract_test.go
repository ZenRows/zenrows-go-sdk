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

	reqs007WithReason = problemResponse{
		http.StatusForbidden,
		`{"code":"REQS007","reason":"domain_not_prepared","title":"Domain not prepared for Extract","status":403}`,
	}
)

// REQS007 means the domain isn't prepared for Extract yet. The caller has to see it, so they
// can prepare the domain; retrying with AutoParse by default would hide it and charge them for it.

// The deprecated DisableAutoparseFallback still compiles and still wins over AutoparseFallback.

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

// Extract never retries with AutoParse: REQS007 and AUTH010 come back as is, in one request,
// whatever the deprecated DisableAutoparseFallback says.
func TestExtractNeverFallsBackToAutoparse(t *testing.T) {
	cases := map[string]problemResponse{"REQS007": reqs007, "AUTH010": auth010}
	for name, notReady := range cases {
		for _, disable := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/DisableAutoparseFallback=%t", name, disable), func(t *testing.T) {
				params := &scraperapi.RequestParameters{DisableAutoparseFallback: disable}
				res, queries := extractAgainst(t, params, notReady, okBody)

				if res.StatusCode() != notReady.status {
					t.Fatalf("expected the raw %d to be returned, got status %d", notReady.status, res.StatusCode())
				}
				if len(queries) != 1 {
					t.Fatalf("%s must not trigger a second request, got %d", name, len(queries))
				}
				if contains(queries[0], "autoparse") {
					t.Fatalf("expected no autoparse param, got %q", queries[0])
				}
			})
		}
	}
}

func TestExtractReqs007ExposesReason(t *testing.T) {
	res, _ := extractAgainst(t, nil, reqs007WithReason)

	prob := res.Problem()
	if prob == nil || prob.Code != "REQS007" || prob.Reason != "domain_not_prepared" {
		t.Fatalf("expected REQS007 with reason domain_not_prepared, got %+v", prob)
	}
}
