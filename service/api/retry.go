package scraperapi

import (
	"context"
	"errors"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
)

const (
	minRetryWait      = 250 * time.Millisecond
	maxRetryAfterWait = 30 * time.Second
	dialOp            = "dial"
)

// retryableStatusCodes are retried on a GET. A 422 is retried too, unless it is Extract's no_data_extracted.
var retryableStatusCodes = []int{
	http.StatusTooManyRequests,
	http.StatusInternalServerError,
	http.StatusBadGateway,
	http.StatusServiceUnavailable,
	http.StatusGatewayTimeout,
}

// execute sends req, and retries it as the shared retry policy says when retries are on. Only a GET is retried, and
// only when its body is not an io.Reader, which the first attempt reads. The API does not dedupe a request, so a
// retried POST or PUT could run, and be charged, twice.
func (c *Client) execute(ctx context.Context, req *resty.Request, method string, body any, query url.Values) (*resty.Response, error) {
	opts := c.cfg.retryOptions
	retries := opts.maxRetryCount
	if _, isReader := body.(io.Reader); method != http.MethodGet || isReader {
		retries = 0
	}

	start := time.Now()
	for retry := 0; ; retry++ {
		res, err := req.Execute(method, "/")
		if retry >= retries || !shouldRetry(ctx, res, err, query) {
			return res, err
		}
		wait := opts.wait(res.Header().Get("Retry-After"), retry)
		if time.Since(start)+wait > opts.retryBudget {
			return res, err
		}
		timer := time.NewTimer(wait)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return res, ctx.Err()
		}
	}
}

// shouldRetry reports whether another try can fix a failed attempt: a connection that failed before the request was
// sent, or a retryable status that comes from the API rather than from the target site.
func shouldRetry(ctx context.Context, res *resty.Response, err error, query url.Values) bool {
	if err != nil {
		return isConnectFailure(ctx, err)
	}
	status := res.StatusCode()
	if isTargetStatus(query, status) {
		return false
	}
	if status == http.StatusUnprocessableEntity {
		return !isNoDataExtracted(status, res.Body())
	}
	return slices.Contains(retryableStatusCodes, status)
}

// isConnectFailure reports whether err is a failed dial: the connection was refused or timed out, the network was
// unreachable, or the name did not resolve. Any other error (a reset, an EOF, a read timeout) may come after the API
// got the request, and a canceled or expired context is the caller's choice, so neither is retried.
func isConnectFailure(ctx context.Context, err error) bool {
	var opErr *net.OpError
	return ctx.Err() == nil && errors.As(err, &opErr) && opErr.Op == dialOp
}

// isTargetStatus reports whether the API passed status through from the target site, so another try would get the
// same answer: the request sets original_status, or lists status in allowed_status_codes.
func isTargetStatus(query url.Values, status int) bool {
	if original, err := strconv.ParseBool(query.Get("original_status")); err == nil && original {
		return true
	}
	for _, code := range strings.Split(query.Get("allowed_status_codes"), ",") {
		if strings.TrimSpace(code) == strconv.Itoa(status) {
			return true
		}
	}
	return false
}

// wait returns the wait before retry n (from 0): the Retry-After when it is valid, else a uniform random time
// between minRetryWait and backoffCeiling.
func (o retryOptions) wait(retryAfter string, n int) time.Duration {
	if wait, ok := parseRetryAfter(retryAfter); ok {
		return wait
	}
	ceiling := backoffCeiling(o.retryWaitTime, o.retryMaxWaitTime, n)
	return minRetryWait + rand.N(ceiling-minRetryWait+1) //nolint:gosec // jitter needs no cryptographic randomness
}

// backoffCeiling returns max(minRetryWait, min(maxWait, base * 2^n)), without overflow.
func backoffCeiling(base, maxWait time.Duration, n int) time.Duration {
	ceiling := maxWait
	if base <= maxWait>>n {
		ceiling = base << n
	}
	return max(ceiling, minRetryWait)
}

// parseRetryAfter reads a Retry-After in whole seconds, clamped to [minRetryWait, maxRetryAfterWait]. It reports
// false for any other form (a date, a fraction, a sign, text).
func parseRetryAfter(header string) (time.Duration, bool) {
	secs, err := strconv.ParseUint(strings.TrimSpace(header), 10, 64)
	// On ErrRange, ParseUint returns the largest uint64, which the clamp below handles.
	if err != nil && !errors.Is(err, strconv.ErrRange) {
		return 0, false
	}
	secs = min(secs, uint64(maxRetryAfterWait/time.Second))
	return max(time.Duration(secs)*time.Second, minRetryWait), true
}
