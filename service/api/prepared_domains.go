package scraperapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/zenrows/zenrows-go-sdk/service/api/version"
)

const preparedDomainsPath = "/extract/prepared-domains"

// PreparedDomainStatus is where a domain's Extract preparation stands. Use Status to decide
// whether the domain is usable; State is for progress display and may gain values later.
type PreparedDomainStatus struct {
	Domain string `json:"domain"`
	// Status is "preparing", "ready" or "failed".
	Status string `json:"status"`
	// State is "received", "discovering", "preparing", "verifying", "ready" or "failed".
	State string `json:"state"`
	// Roles is the per-page-kind progress. Only returned to the account that submitted the domain.
	Roles []PreparedDomainRole `json:"roles,omitempty"`
	// Failure is set when Status is "failed".
	Failure     *PreparedDomainFailure `json:"failure,omitempty"`
	SubmittedAt time.Time              `json:"submitted_at"`
	UpdatedAt   time.Time              `json:"updated_at"`
}

// PreparedDomainRole is the preparation progress of one kind of page on a domain.
type PreparedDomainRole struct {
	Role   string `json:"role"`
	Status string `json:"status"`
	URL    string `json:"url,omitempty"`
	Detail string `json:"detail,omitempty"`
}

// PreparedDomainFailure says why a preparation failed and whether it used one of the
// account's monthly preparations.
type PreparedDomainFailure struct {
	Code         string `json:"code"`
	Detail       string `json:"detail,omitempty"`
	SlotConsumed bool   `json:"slot_consumed"`
}

// PreparedDomainError is a non-2xx answer from PrepareDomain or PreparedDomainStatus. Branch on
// StatusCode and Code; display Detail, don't parse it. Code is the problem's code, for example
// "invalid_argument" (400), "not_found" (404), "in_progress" (409: a preparation is already
// running, read its status instead of resubmitting), "out_of_allowance" (429: no preparations
// left this month), or "overloaded" / "disabled" (503); empty when the body has none.
type PreparedDomainError struct {
	StatusCode int
	Code       string
	Title      string
	Detail     string
	Reason     string
	// RetryAfter is the wait the server asked for in a Retry-After header, or 0.
	RetryAfter time.Duration
	// Body is the raw response body.
	Body []byte
}

func (e *PreparedDomainError) Error() string {
	title := e.Title
	if title == "" {
		title = "prepared-domains request failed"
	}
	msg := fmt.Sprintf("%s [HTTP %d]", title, e.StatusCode)
	switch {
	case e.Detail != "":
		msg += ": " + e.Detail
	case e.Code != "":
		msg += ": " + e.Code
	}
	return msg
}

// PrepareDomain submits domain for Extract preparation (POST /v1/extract/prepared-domains) and
// returns the status the API answers with 202. domain is the domain without a scheme;
// exampleURLs are 1 to 5 URLs on that domain.
//
// One example URL works; we recommend 3 or more of the same page type (for example 3 product
// pages), because Extract compares pages of one type to check the fields it found and fill in
// fields one page is missing. Several page types can share the 5 (for example 3 product pages
// and 2 search-result pages).
//
// Submitting a domain that is already prepared returns its current status instead of starting
// a new run.
//
// Preparation takes minutes. PrepareDomain does not wait for it: call PreparedDomainStatus
// until Status is no longer "preparing". These endpoints don't fetch pages and cost no credits,
// and the client's retry settings don't apply to them.
//
// An empty or whitespace-only domain returns an InvalidParameterError before any request. A
// non-2xx answer returns a *PreparedDomainError, for example 400 invalid_argument,
// 409 in_progress, 429 out_of_allowance or 503 overloaded / disabled.
func (c *Client) PrepareDomain(ctx context.Context, domain string, exampleURLs []string) (*PreparedDomainStatus, error) {
	if err := checkDomain(domain); err != nil {
		return nil, err
	}
	if exampleURLs == nil {
		exampleURLs = []string{}
	}
	body, err := json.Marshal(struct {
		Domain      string   `json:"domain"`
		ExampleURLs []string `json:"example_urls"`
	}{domain, exampleURLs})
	if err != nil {
		return nil, err
	}
	return c.preparedDomainsRequest(ctx, http.MethodPost, preparedDomainsPath, body)
}

// PreparedDomainStatus reads the preparation status of domain
// (GET /v1/extract/prepared-domains/{domain}). Status is "preparing", "ready" or "failed"; a
// failed preparation carries a Failure with its code, detail and whether it used a slot.
//
// An empty or whitespace-only domain returns an InvalidParameterError before any request. A
// non-2xx answer returns a *PreparedDomainError, for example 404 not_found when your account
// never submitted domain.
func (c *Client) PreparedDomainStatus(ctx context.Context, domain string) (*PreparedDomainStatus, error) {
	if err := checkDomain(domain); err != nil {
		return nil, err
	}
	return c.preparedDomainsRequest(ctx, http.MethodGet, preparedDomainsPath+"/"+url.PathEscape(domain), nil)
}

// checkDomain rejects an empty or whitespace-only domain: it would request the list endpoint and
// read its answer as a status.
func checkDomain(domain string) error {
	if strings.TrimSpace(domain) == "" {
		return InvalidParameterError{Msg: "domain cannot be empty"}
	}
	return nil
}

func (c *Client) preparedDomainsRequest(ctx context.Context, method, path string, body []byte) (*PreparedDomainStatus, error) {
	if !c.isConfigured() {
		return nil, NotConfiguredError{}
	}

	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.cfg.baseURL, "/")+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-API-Key", c.cfg.apiKey)
	req.Header.Set("User-Agent", "zenrows-go/"+version.Version)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	res, err := c.http.GetClient().Do(req) //nolint:gosec // host is the configured API base URL; the domain is path-escaped
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}

	if res.StatusCode < http.StatusOK || res.StatusCode >= http.StatusMultipleChoices {
		return nil, newPreparedDomainError(res, raw)
	}

	var status PreparedDomainStatus
	if err := json.Unmarshal(raw, &status); err != nil {
		return nil, fmt.Errorf("decode prepared-domain status: %w", err)
	}
	return &status, nil
}

func newPreparedDomainError(res *http.Response, raw []byte) *PreparedDomainError {
	perr := &PreparedDomainError{StatusCode: res.StatusCode, Body: raw}

	var prob struct {
		Code   string `json:"code"`
		Title  string `json:"title"`
		Detail string `json:"detail"`
		Reason string `json:"reason"`
	}
	if json.Unmarshal(raw, &prob) == nil {
		perr.Code, perr.Title, perr.Detail, perr.Reason = prob.Code, prob.Title, prob.Detail, prob.Reason
	}

	if secs, err := strconv.Atoi(strings.TrimSpace(res.Header.Get("Retry-After"))); err == nil && secs >= 0 {
		perr.RetryAfter = time.Duration(secs) * time.Second
	}
	return perr
}
