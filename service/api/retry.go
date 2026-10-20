package scraperapi

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
)

const (
	// minRetryWait is the shortest wait before any retry, whatever the options or the Retry-After say.
	minRetryWait = 250 * time.Millisecond
	// maxRetryAfterWait bounds the wait a Retry-After can ask for, so a large server value cannot
	// hang a call. It is fixed: RetryMaxWaitTime does not change it.
	maxRetryAfterWait = 30 * time.Second
	// dialOp is the net.OpError operation of a failed connection attempt.
	dialOp = "dial"
)

// retryableStatusCodes are the responses worth another try: the API is busy (429), failed (500), or
// a gateway failed (502, 503, 504). A 422 is retried too, unless it is Extract's no_data_extracted
// (see isRetryableStatus). Every other status is a final answer.
var retryableStatusCodes = []int{
	http.StatusTooManyRequests,
	http.StatusInternalServerError,
	http.StatusBadGateway,
	http.StatusServiceUnavailable,
	http.StatusGatewayTimeout,
}

type retryCallKey struct{}

// retryCall follows one request across its attempts. resty runs the attempts of a request one after
// another, so it needs no lock.
type retryCall struct {
	// start is when the first attempt began. The retry budget counts from it.
	start time.Time
	// replayable is false for a request that must be sent at most once (see isReplayable).
	replayable bool
	// nextWait is the wait shouldRetry chose for the coming retry, read back by retryAfter.
	nextWait time.Duration
}

// withRetryCall returns ctx carrying the retry state of one request. Its retry budget starts now.
func (c *Client) withRetryCall(ctx context.Context, replayable bool) context.Context {
	return context.WithValue(ctx, retryCallKey{}, &retryCall{start: c.now(), replayable: replayable})
}

func retryCallFrom(r *resty.Response) (*retryCall, bool) {
	if r == nil || r.Request == nil {
		return nil, false
	}
	call, ok := r.Request.Context().Value(retryCallKey{}).(*retryCall)
	return call, ok
}

// isReplayable reports whether a request may be sent more than once. Only GET, HEAD and OPTIONS may.
// Every other method, POST and PUT included, is sent once: the API does not dedupe a request on its
// Idempotency-Key header, so a retry could run, and charge, the same request twice. The body must also
// be one the client can send again: none, a string, a []byte, or a struct or map that it encodes
// before sending. An io.Reader is read by the first attempt, so a retry would send an empty body: a
// request with one is never retried, whatever its method.
func isReplayable(method string, body any) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
	default:
		return false
	}
	_, oneShot := body.(io.Reader)
	return !oneShot
}

// shouldRetry is the client's resty retry condition. It retries a retryable failure of a replayable
// request only when the wait before the retry still fits in the retry budget, and records that wait
// for retryAfter.
func (c *Client) shouldRetry(r *resty.Response, err error) bool {
	call, ok := retryCallFrom(r)
	if !ok || !call.replayable || !isRetryableFailure(r, err) {
		return false
	}
	// resty counts attempts from 1, so the first retry follows attempt 1 and uses exponent 0.
	wait := c.retryWait(r.Header().Get("Retry-After"), r.Request.Attempt-1)
	if c.now().Sub(call.start)+wait > c.cfg.retryOptions.retryBudget {
		return false
	}
	call.nextWait = wait
	return true
}

// retryAfter is the client's resty RetryAfter function: it returns the wait shouldRetry chose.
func retryAfter(_ *resty.Client, r *resty.Response) (time.Duration, error) {
	if call, ok := retryCallFrom(r); ok {
		return call.nextWait, nil
	}
	return 0, nil
}

// isRetryableFailure reports whether an attempt failed in a way another try can fix: a retryable
// status, or a connection that failed before the request was sent.
func isRetryableFailure(r *resty.Response, err error) bool {
	if err != nil {
		return r.RawResponse == nil && isConnectFailure(r.Request.Context(), err)
	}
	return isRetryableStatus(r.StatusCode(), r.Body())
}

// isRetryableStatus reports whether a response status is worth another try.
func isRetryableStatus(status int, body []byte) bool {
	if status == http.StatusUnprocessableEntity {
		return !isNoDataExtracted(status, body)
	}
	return slices.Contains(retryableStatusCodes, status)
}

// isConnectFailure reports whether err means the request never reached the API: the connection was
// refused or timed out while connecting, the network was unreachable, or the name did not resolve.
// Every other error may come after the request was sent (a reset, an EOF, a read timeout), and the
// API may then still run and charge it, so it is not retried. A canceled or expired caller context
// is never retried either.
func isConnectFailure(ctx context.Context, err error) bool {
	if ctx.Err() != nil || errors.Is(err, context.Canceled) {
		return false
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return true
	}
	var opErr *net.OpError
	return errors.As(err, &opErr) && opErr.Op == dialOp
}

// retryWait is how long to wait before the retry that follows attempt (0 for the first retry). A
// valid Retry-After wins. Otherwise the wait is full jitter (see jitterWait).
func (c *Client) retryWait(retryAfterHeader string, attempt int) time.Duration {
	if wait, ok := parseRetryAfter(retryAfterHeader); ok {
		return wait
	}
	return jitterWait(c.cfg.retryOptions.retryWaitTime, c.cfg.retryOptions.retryMaxWaitTime, attempt, c.randN)
}

// jitterWait returns a uniform wait in [minRetryWait, ceiling], where
// ceiling = max(minRetryWait, min(maxWait, base * 2^attempt)). randN(n) returns a value in [0, n).
func jitterWait(base, maxWait time.Duration, attempt int, randN func(time.Duration) time.Duration) time.Duration {
	ceiling := maxWait
	// base * 2^attempt, when it is below maxWait, without overflowing.
	if attempt >= 0 && attempt < 63 && base > 0 && base <= maxWait>>attempt {
		ceiling = base << attempt
	}
	ceiling = max(ceiling, minRetryWait)
	return minRetryWait + randN(ceiling-minRetryWait+1)
}

// parseRetryAfter reads a Retry-After header given in whole seconds, and returns that wait clamped
// to [minRetryWait, maxRetryAfterWait]. It ignores every other form (an HTTP date, a fraction, a
// sign, an exponent, text), and the caller then uses the jittered wait.
func parseRetryAfter(raw string) (time.Duration, bool) {
	value := strings.TrimSpace(raw)
	if value == "" || strings.IndexFunc(value, func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
		return 0, false
	}
	secs, err := strconv.ParseUint(value, 10, 64)
	// The value is all digits, so the only possible error is a value too large for uint64.
	if err != nil || secs > uint64(maxRetryAfterWait/time.Second) {
		return maxRetryAfterWait, true
	}
	return max(time.Duration(secs)*time.Second, minRetryWait), true
}
