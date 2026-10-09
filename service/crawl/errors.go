package crawl

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// CodeNotEnabled is the problem code (403) for an account Crawl is not enabled for.
const CodeNotEnabled = "REQS008"

// NotConfiguredError results when the Crawl API client is used without a valid API key.
type NotConfiguredError struct{}

func (NotConfiguredError) Error() string {
	return "zenrows crawl api client is not configured"
}

// standardProblemFields lists the RFC 9457 members modeled on Problem itself; anything else
// in the body lands in Problem.Extras.
var standardProblemFields = map[string]bool{
	"type": true, "title": true, "status": true, "code": true, "detail": true, "instance": true,
}

// parseProblem decodes a Problem JSON body, tolerating non-JSON bodies (returns nil in that
// case — an edge proxy, for one, may answer with something else).
func parseProblem(body []byte) *Problem {
	if len(body) == 0 {
		return nil
	}
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil
	}
	var p Problem
	if err := json.Unmarshal(body, &p); err != nil {
		return nil
	}
	extras := map[string]any{}
	for k, v := range raw {
		if !standardProblemFields[k] {
			extras[k] = v
		}
	}
	if len(extras) > 0 {
		p.Extras = extras
	}
	return &p
}

// APIError wraps a non-2xx response from the Crawl API. The API returns RFC 9457 Problem JSON
// bodies; Detail carries the parsed body when it could be decoded as such (nil when the server
// returned something else).
type APIError struct {
	StatusCode int
	Detail     *Problem
	Body       []byte
	// RetryAfter is the response's Retry-After header (e.g. 30s on a 429 too_many_crawls), or 0.
	RetryAfter time.Duration
}

// Code is the problem `code` member (e.g. "crawl_not_found", "too_many_crawls", "REQS008"), or
// "" when the body has none. Stable; safe to branch on.
func (e APIError) Code() string {
	if e.Detail != nil {
		return e.Detail.Code
	}
	return ""
}

func (e APIError) Error() string {
	if e.StatusCode == http.StatusForbidden && e.Code() == CodeNotEnabled {
		return fmt.Sprintf("zenrows crawl api request failed with status %d: Crawl is not enabled for this account (%s)",
			e.StatusCode, CodeNotEnabled)
	}
	if e.Detail != nil && e.Detail.Detail != "" {
		return fmt.Sprintf("zenrows crawl api request failed with status %d: %s", e.StatusCode, e.Detail.Detail)
	}
	return fmt.Sprintf("zenrows crawl api request failed with status %d", e.StatusCode)
}

func newAPIError(statusCode int, header http.Header, body []byte) APIError {
	err := APIError{StatusCode: statusCode, Detail: parseProblem(body), Body: body}
	if wait, ok := parseRetryAfter(header.Get("Retry-After")); ok {
		err.RetryAfter = wait
	}
	return err
}
