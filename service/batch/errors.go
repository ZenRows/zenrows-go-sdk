package batch

import (
	"encoding/json"
	"fmt"
)

// NotConfiguredError results when the Batch API client is used without a valid API key.
type NotConfiguredError struct{}

func (NotConfiguredError) Error() string {
	return "zenrows batch api client is not configured"
}

// ProblemDetail is the RFC 7807 Problem JSON shape the Batch API returns on errors. Kept as
// an alias of Problem for backward compatibility with earlier callers of this package.
type ProblemDetail = Problem

// standardProblemFields lists the RFC 7807 members modeled on Problem itself; anything else
// in the body lands in Problem.Extras.
var standardProblemFields = map[string]bool{
	"type": true, "title": true, "status": true, "code": true,
	"detail": true, "instance": true, "invalid_tasks": true,
}

// parseProblem decodes a Problem JSON body, tolerating non-JSON bodies (returns nil,nil in
// that case — production servers occasionally return non-JSON errors, e.g. an edge proxy
// blocking the request).
func parseProblem(body []byte) *Problem {
	if len(body) == 0 {
		return nil
	}
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil
	}

	var p Problem
	// Re-decode into the typed struct for the standard fields.
	if err := json.Unmarshal(body, &p); err != nil {
		return nil
	}
	if p.Code == "" {
		p.Code = "internal"
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

// APIError wraps a non-2xx response from the Batch API. The API returns RFC 7807 Problem
// JSON bodies; Detail carries the parsed body when it could be decoded as such (nil when the
// server returned something else, e.g. an edge-proxy HTML error page).
type APIError struct {
	StatusCode int
	Detail     *ProblemDetail
	Body       []byte
}

// Code is the RFC 7807 `code` member (e.g. "file_input_not_found", "idempotency_key_conflict"),
// or "internal" when the body couldn't be parsed as Problem JSON. Stable; safe to branch on.
func (e APIError) Code() string {
	if e.Detail != nil {
		return e.Detail.Code
	}
	return "internal"
}

func (e APIError) Error() string {
	if e.Detail != nil && e.Detail.Detail != "" {
		return fmt.Sprintf("zenrows batch api request failed with status %d: %s", e.StatusCode, e.Detail.Detail)
	}
	return fmt.Sprintf("zenrows batch api request failed with status %d", e.StatusCode)
}

func newAPIError(statusCode int, body []byte) APIError {
	return APIError{StatusCode: statusCode, Detail: parseProblem(body), Body: body}
}
