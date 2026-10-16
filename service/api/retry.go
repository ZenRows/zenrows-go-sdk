package scraperapi

import (
	"context"
	"errors"
	"math"
	"math/rand/v2"
	"net"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/go-resty/resty/v2"
)

const (
	// minRetryWait is the shortest jittered wait before a retry, so retries never hammer the API.
	minRetryWait = 250 * time.Millisecond
	// maxRetryAfterWait bounds the wait a Retry-After can ask for, so a large server value cannot
	// hang a call.
	maxRetryAfterWait = 30 * time.Second
)

// retryableStatusCodes are the responses worth another try: the API is busy (429) or a gateway
// failed (502, 503, 504). None of them is charged. Every other status is a final answer.
var retryableStatusCodes = []int{
	http.StatusTooManyRequests,
	http.StatusBadGateway,
	http.StatusServiceUnavailable,
	http.StatusGatewayTimeout,
}

type retryStateKey struct{}

// retryState follows one call across its attempts. resty runs the attempts of a call one after
// another, so it needs no lock.
type retryState struct {
	// start is when the first attempt began; the retry budget counts from it.
	start time.Time
	// nextWait is the wait shouldRetry chose for the coming retry, read back by retryAfter.
	nextWait time.Duration
}

// withRetryState returns ctx carrying a fresh retryState that starts now.
func withRetryState(ctx context.Context) context.Context {
	return context.WithValue(ctx, retryStateKey{}, &retryState{start: time.Now()})
}

func retryStateFrom(r *resty.Response) (*retryState, bool) {
	if r == nil || r.Request == nil {
		return nil, false
	}
	state, ok := r.Request.Context().Value(retryStateKey{}).(*retryState)
	return state, ok
}

// shouldRetry is the client's resty retry condition. It retries a retryable failure only when the
// wait before the retry still fits in the retry budget, and records that wait for retryAfter.
func (c *Client) shouldRetry(r *resty.Response, err error) bool {
	if r == nil || !isRetryableFailure(r, err) {
		return false
	}
	state, ok := retryStateFrom(r)
	if !ok {
		return false
	}
	// resty counts attempts from 1, so the first retry follows attempt 1.
	wait := retryWait(c.cfg.retryOptions, r.Header().Get("Retry-After"), r.Request.Attempt-1)
	if time.Since(state.start)+wait > c.cfg.retryOptions.retryBudget {
		return false
	}
	state.nextWait = wait
	return true
}

// retryAfter is the client's resty RetryAfter function: it returns the wait shouldRetry chose.
func retryAfter(_ *resty.Client, r *resty.Response) (time.Duration, error) {
	if state, ok := retryStateFrom(r); ok {
		return state.nextWait, nil
	}
	return 0, nil
}

// isRetryableFailure reports whether an attempt failed in a way another try can fix: a retryable
// status, or a connection error before any response arrived. A timeout or a canceled context is
// never retried, because the API keeps working on the request and may still charge it.
func isRetryableFailure(r *resty.Response, err error) bool {
	if err != nil {
		return r.RawResponse == nil && !isTimeoutOrCanceled(err)
	}
	return slices.Contains(retryableStatusCodes, r.StatusCode())
}

func isTimeoutOrCanceled(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

// retryWait is how long to wait before retry number retry (0 for the first one). A Retry-After
// given in seconds wins, at most maxRetryAfterWait. Otherwise the wait is full jitter: uniform in
// [minRetryWait, min(retryMaxWaitTime, retryWaitTime * 2^retry)].
func retryWait(opts retryOptions, retryAfterHeader string, retry int) time.Duration {
	if wait, ok := parseRetryAfter(retryAfterHeader); ok {
		return max(min(wait, maxRetryAfterWait), minRetryWait)
	}
	return jitterWait(opts.retryWaitTime, opts.retryMaxWaitTime, retry)
}

// jitterWait returns a uniform wait in [minRetryWait, min(ceiling, base * 2^retry)]. When that
// upper bound is below minRetryWait, the upper bound is the wait.
func jitterWait(base, ceiling time.Duration, retry int) time.Duration {
	upper := max(ceiling, 0)
	// base * 2^retry, without overflowing.
	if retry >= 0 && retry < 63 && base >= 0 && base <= upper>>retry {
		upper = base << retry
	}
	lower := min(minRetryWait, upper)
	if upper <= lower {
		return lower
	}
	return lower + rand.N(upper-lower+1) //nolint:gosec // timing jitter, not security-sensitive
}

// parseRetryAfter reads a Retry-After header given in seconds.
func parseRetryAfter(raw string) (time.Duration, bool) {
	if raw == "" {
		return 0, false
	}
	secs, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsNaN(secs) || math.IsInf(secs, 0) || secs < 0 {
		return 0, false
	}
	// Converting a value past the int64 range to a Duration is undefined and can go negative.
	if secs >= math.MaxInt64/float64(time.Second) {
		return math.MaxInt64, true
	}
	return time.Duration(secs * float64(time.Second)), true
}
