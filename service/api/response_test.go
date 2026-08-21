package scraperapi_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	scraperapi "github.com/zenrows/zenrows-go-sdk/service/api"
)

func doGet(t *testing.T, handler http.HandlerFunc) *scraperapi.Response {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client := scraperapi.NewClient(scraperapi.WithBaseURL(server.URL), scraperapi.WithAPIKey("test-key"))
	res, err := client.Get(context.Background(), "https://example.com", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return res
}

func TestResponseBasicAccessors(t *testing.T) {
	res := doGet(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Custom", "hello")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("body content"))
	})

	if res.StatusCode() != http.StatusOK {
		t.Fatalf("StatusCode: got %d", res.StatusCode())
	}
	if res.Status() == "" {
		t.Fatal("Status: expected a non-empty status string")
	}
	if string(res.Body()) != "body content" {
		t.Fatalf("Body: got %q", res.Body())
	}
	if res.String() != "body content" {
		t.Fatalf("String: got %q", res.String())
	}
	if res.Header().Get("X-Custom") != "hello" {
		t.Fatalf("Header: expected X-Custom to be echoed, got %q", res.Header().Get("X-Custom"))
	}
	if res.Size() <= 0 {
		t.Fatal("Size: expected a positive body size")
	}
	if res.ReceivedAt().IsZero() {
		t.Fatal("ReceivedAt: expected a non-zero timestamp")
	}
}

func TestResponseIsSuccessAndIsErrorBoundaries(t *testing.T) {
	cases := []struct {
		status      int
		wantSuccess bool
	}{
		{http.StatusOK, true},
		{http.StatusCreated, true},
		{http.StatusNoContent, true},
		{http.StatusMovedPermanently, false},
		{http.StatusBadRequest, false},
		{http.StatusInternalServerError, false},
	}

	for _, c := range cases {
		res := doGet(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(c.status) })
		if res.IsSuccess() != c.wantSuccess {
			t.Errorf("status %d: IsSuccess() = %v, want %v", c.status, res.IsSuccess(), c.wantSuccess)
		}
		wantIsError := c.status >= 400
		if res.IsError() != wantIsError {
			t.Errorf("status %d: IsError() = %v, want %v", c.status, res.IsError(), wantIsError)
		}
	}
}

func TestResponseProblemParsesRFC7807BodyOnError(t *testing.T) {
	res := doGet(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"title":"Service Unavailable","status":503,"detail":"upstream is down"}`))
	})

	prob := res.Problem()
	if prob == nil {
		t.Fatal("expected a parsed Problem, got nil")
	}
	if prob.Title != "Service Unavailable" || prob.Status != 503 || prob.Detail != "upstream is down" {
		t.Fatalf("unexpected problem: %+v", prob)
	}

	if res.Error() == nil {
		t.Fatal("expected Error() to surface the problem as an error")
	}
	if res.Error().Error() != prob.Error() {
		t.Fatalf("Error() message mismatch: %q vs %q", res.Error().Error(), prob.Error())
	}
}

func TestResponseProblemIsNilWhenContentTypeIsNotProblemJSON(t *testing.T) {
	res := doGet(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal error"))
	})

	if res.Problem() != nil {
		t.Fatal("expected no Problem for a non-problem+json error body")
	}
	if res.Error() != nil {
		t.Fatal("expected Error() to be nil when there is no parseable problem")
	}
}

func TestResponseProblemIsNilOnSuccessEvenWithProblemContentType(t *testing.T) {
	res := doGet(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"title":"should be ignored","status":200}`))
	})

	if res.Problem() != nil {
		t.Fatal("expected Problem() to be nil on a successful response regardless of content type")
	}
}

func TestResponseProblemIsNilOnMalformedJSON(t *testing.T) {
	res := doGet(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`not valid json`))
	})

	if res.Problem() != nil {
		t.Fatal("expected Problem() to be nil when the body fails to unmarshal")
	}
}

func TestResponseTargetHeadersFiltersZPrefixOnly(t *testing.T) {
	res := doGet(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Z-Content-Type", "text/html")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
	})

	targetHeaders := res.TargetHeaders()
	if targetHeaders.Get("Z-Content-Type") != "text/html" {
		t.Fatalf("expected Z-Content-Type to be included, got %v", targetHeaders)
	}
	if _, ok := targetHeaders["Content-Type"]; ok {
		t.Fatalf("expected non Z- prefixed headers to be excluded, got %v", targetHeaders)
	}
}

func TestResponseTargetHeadersEmptyWhenNoneSet(t *testing.T) {
	res := doGet(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

	if len(res.TargetHeaders()) != 0 {
		t.Fatalf("expected no target headers, got %v", res.TargetHeaders())
	}
}

func TestResponseTargetCookiesParsesSetCookieLines(t *testing.T) {
	res := doGet(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Z-Set-Cookie", "session=abc123; Path=/")
		w.Header().Add("Z-Set-Cookie", "theme=dark; Path=/")
		w.WriteHeader(http.StatusOK)
	})

	cookies := res.TargetCookies()
	if len(cookies) != 2 {
		t.Fatalf("expected 2 cookies, got %d: %v", len(cookies), cookies)
	}
	names := map[string]bool{cookies[0].Name: true, cookies[1].Name: true}
	if !names["session"] || !names["theme"] {
		t.Fatalf("expected session and theme cookies, got %v", names)
	}
}

func TestResponseTargetCookiesEmptyWhenNoneSet(t *testing.T) {
	res := doGet(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

	cookies := res.TargetCookies()
	if len(cookies) != 0 {
		t.Fatalf("expected no cookies, got %v", cookies)
	}
}

func TestResponseTargetCookiesSkipsUnparseableLines(t *testing.T) {
	res := doGet(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Z-Set-Cookie", "session=abc123; Path=/")
		// A space in the cookie name is wire-valid (won't break the HTTP transport) but
		// fails RFC 6265 name validation, so http.ParseSetCookie rejects it.
		w.Header().Add("Z-Set-Cookie", "invalid name=value")
		w.WriteHeader(http.StatusOK)
	})

	cookies := res.TargetCookies()
	for _, c := range cookies {
		if c.Name != "session" {
			t.Fatalf("expected only the valid cookie to survive parsing, got %v", cookies)
		}
	}
}
