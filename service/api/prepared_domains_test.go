package scraperapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	scraperapi "github.com/zenrows/zenrows-go-sdk/service/api"
)

const preparingJSON = `{"domain":"www.example.com","status":"preparing","state":"received",` +
	`"submitted_at":"2026-10-15T10:00:00Z","updated_at":"2026-10-15T10:00:00Z"}`

type seenRequest struct {
	method, path, rawQuery, apiKey string
	body                           []byte
}

// preparedDomainsStub answers every request with status, body and headers, and records what was sent.
func preparedDomainsStub(t *testing.T, status int, body string, headers map[string]string) (*scraperapi.Client, *[]seenRequest) {
	t.Helper()
	var seen []seenRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		seen = append(seen, seenRequest{r.Method, r.URL.EscapedPath(), r.URL.RawQuery, r.Header.Get("X-API-Key"), raw})
		for k, v := range headers {
			w.Header().Set(k, v)
		}
		if status >= http.StatusBadRequest {
			w.Header().Set("Content-Type", "application/problem+json")
		} else {
			w.Header().Set("Content-Type", "application/json")
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)

	client := scraperapi.NewClient(
		scraperapi.WithBaseURL(server.URL+"/v1"),
		scraperapi.WithAPIKey("test-key"),
		// Retries must not apply to these endpoints.
		scraperapi.WithMaxRetryCount(3),
		scraperapi.WithRetryWaitTime(time.Millisecond),
		scraperapi.WithRetryMaxWaitTime(time.Millisecond),
	)
	return client, &seen
}

func TestPrepareDomainPostsDomainAndExampleURLs(t *testing.T) {
	client, seen := preparedDomainsStub(t, http.StatusAccepted, preparingJSON, nil)

	status, err := client.PrepareDomain(context.Background(), "www.example.com",
		[]string{"https://www.example.com/a", "https://www.example.com/b"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status.Domain != "www.example.com" || status.Status != "preparing" || status.State != "received" {
		t.Fatalf("unexpected status: %+v", status)
	}
	if !status.SubmittedAt.Equal(time.Date(2026, 10, 15, 10, 0, 0, 0, time.UTC)) {
		t.Fatalf("unexpected submitted_at: %v", status.SubmittedAt)
	}
	if len(*seen) != 1 {
		t.Fatalf("expected 1 request (no polling), got %d", len(*seen))
	}
	req := (*seen)[0]
	if req.method != http.MethodPost || req.path != "/v1/extract/prepared-domains" {
		t.Fatalf("unexpected request %s %s", req.method, req.path)
	}
	if req.apiKey != "test-key" || req.rawQuery != "" {
		t.Fatalf("expected the key in X-API-Key and no query, got key %q query %q", req.apiKey, req.rawQuery)
	}
	var sent map[string]any
	if err := json.Unmarshal(req.body, &sent); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	urls, _ := sent["example_urls"].([]any)
	if sent["domain"] != "www.example.com" || len(urls) != 2 || urls[0] != "https://www.example.com/a" {
		t.Fatalf("unexpected body: %s", req.body)
	}
}

func TestPreparedDomainStatusReturnsTheStatus(t *testing.T) {
	body := `{"domain":"www.example.com","status":"failed","state":"failed",` +
		`"roles":[{"role":"listing","status":"failed","url":"https://www.example.com/a","detail":"x"}],` +
		`"failure":{"code":"unsupported","detail":"y","slot_consumed":true},` +
		`"submitted_at":"2026-10-15T10:00:00Z","updated_at":"2026-10-15T10:05:00Z"}`
	client, seen := preparedDomainsStub(t, http.StatusOK, body, nil)

	status, err := client.PreparedDomainStatus(context.Background(), "www.example.com")
	if err != nil {
		t.Fatalf("a failed preparation is a status, not an error: %v", err)
	}
	if status.Status != "failed" || status.Failure == nil || status.Failure.Code != "unsupported" || !status.Failure.SlotConsumed {
		t.Fatalf("unexpected status: %+v", status)
	}
	if len(status.Roles) != 1 || status.Roles[0].Role != "listing" {
		t.Fatalf("unexpected roles: %+v", status.Roles)
	}
	req := (*seen)[0]
	if req.method != http.MethodGet || req.path != "/v1/extract/prepared-domains/www.example.com" || req.apiKey != "test-key" {
		t.Fatalf("unexpected request %s %s key %q", req.method, req.path, req.apiKey)
	}
}

func TestPreparedDomainStatusEscapesTheDomain(t *testing.T) {
	client, seen := preparedDomainsStub(t, http.StatusOK, preparingJSON, nil)

	if _, err := client.PreparedDomainStatus(context.Background(), "bad/domain?x"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := (*seen)[0].path; got != "/v1/extract/prepared-domains/bad%2Fdomain%3Fx" {
		t.Fatalf("expected an escaped domain, got %q", got)
	}
}

func TestPreparedDomainsRejectAnEmptyDomain(t *testing.T) {
	client, seen := preparedDomainsStub(t, http.StatusOK, preparingJSON, nil)

	for _, domain := range []string{"", "   ", "\t\n"} {
		var invalid scraperapi.InvalidParameterError
		if _, err := client.PreparedDomainStatus(context.Background(), domain); !errors.As(err, &invalid) {
			t.Fatalf("PreparedDomainStatus(%q): expected InvalidParameterError, got %v", domain, err)
		}
		_, err := client.PrepareDomain(context.Background(), domain, []string{"https://www.example.com/"})
		if !errors.As(err, &invalid) {
			t.Fatalf("PrepareDomain(%q): expected InvalidParameterError, got %v", domain, err)
		}
	}
	if len(*seen) != 0 {
		t.Fatalf("expected no request, got %d", len(*seen))
	}
}

func TestPreparedDomainsErrorAnswers(t *testing.T) {
	cases := []struct {
		status       int
		code, reason string
	}{
		{http.StatusBadRequest, "invalid_argument", "invalid_domain"},
		{http.StatusConflict, "in_progress", ""},
		{http.StatusTooManyRequests, "out_of_allowance", ""},
		{http.StatusServiceUnavailable, "overloaded", ""},
		{http.StatusServiceUnavailable, "disabled", ""},
	}
	for _, tc := range cases {
		t.Run(tc.code, func(t *testing.T) {
			body, _ := json.Marshal(map[string]any{
				"code": tc.code, "detail": "detail for " + tc.code, "reason": tc.reason,
				"status": tc.status, "title": "Title", "type": "https://docs.zenrows.com/api-error-codes#" + tc.code,
			})
			client, seen := preparedDomainsStub(t, tc.status, string(body), map[string]string{"Retry-After": "7"})

			_, err := client.PrepareDomain(context.Background(), "www.example.com", []string{"https://www.example.com/"})

			var perr *scraperapi.PreparedDomainError
			if !errors.As(err, &perr) {
				t.Fatalf("expected *PreparedDomainError, got %v", err)
			}
			if perr.StatusCode != tc.status || perr.Code != tc.code || perr.Reason != tc.reason {
				t.Fatalf("unexpected error fields: %+v", perr)
			}
			if perr.Detail != "detail for "+tc.code || perr.RetryAfter != 7*time.Second {
				t.Fatalf("unexpected detail / retry-after: %+v", perr)
			}
			if len(*seen) != 1 {
				t.Fatalf("expected no retry, got %d requests", len(*seen))
			}
		})
	}
}

func TestPreparedDomainStatusNotFound(t *testing.T) {
	client, _ := preparedDomainsStub(t, http.StatusNotFound,
		`{"code":"not_found","detail":"never submitted","status":404,"title":"Not found"}`, nil)

	_, err := client.PreparedDomainStatus(context.Background(), "www.example.com")

	var perr *scraperapi.PreparedDomainError
	if !errors.As(err, &perr) || perr.Code != "not_found" || perr.StatusCode != http.StatusNotFound {
		t.Fatalf("expected a not_found PreparedDomainError, got %v", err)
	}
	if perr.Error() != "Not found [HTTP 404]: never submitted" {
		t.Fatalf("unexpected message %q", perr.Error())
	}
}

func TestPreparedDomainsErrorWithoutJSONBody(t *testing.T) {
	client, _ := preparedDomainsStub(t, http.StatusBadGateway, "bad gateway", nil)

	_, err := client.PreparedDomainStatus(context.Background(), "www.example.com")

	var perr *scraperapi.PreparedDomainError
	if !errors.As(err, &perr) || perr.StatusCode != http.StatusBadGateway || perr.Code != "" {
		t.Fatalf("expected a 502 PreparedDomainError without code, got %v", err)
	}
	if string(perr.Body) != "bad gateway" {
		t.Fatalf("expected the raw body, got %q", perr.Body)
	}
}

func TestPreparedDomainsNeedAConfiguredClient(t *testing.T) {
	client := scraperapi.NewClient(scraperapi.WithAPIKey(""))

	_, err := client.PrepareDomain(context.Background(), "www.example.com", nil)
	if !errors.As(err, &scraperapi.NotConfiguredError{}) {
		t.Fatalf("expected NotConfiguredError, got %v", err)
	}
}
