package crawl

import (
	"context"
	"fmt"
	"math/rand"
	"net/http"
	"strconv"
	"time"

	"github.com/go-resty/resty/v2"
)

// Retry tuning: ~250ms * 2^attempt, +/-20% jitter, capped at 10s.
const (
	backoffBaseMs = 250
	backoffCapMs  = 10_000
	backoffJitter = 0.2
)

var retryableStatuses = map[int]bool{
	http.StatusTooManyRequests:    true,
	http.StatusBadGateway:         true,
	http.StatusServiceUnavailable: true,
	http.StatusGatewayTimeout:     true,
}

var idempotentMethods = map[string]bool{
	http.MethodGet: true, http.MethodPut: true, http.MethodDelete: true,
	http.MethodHead: true, http.MethodOptions: true,
}

// isRetryableStatus reports whether a response status is transient. A 429 on POST is
// too_many_crawls: the slot frees only when one of the account's jobs ends, so it is not retried.
func isRetryableStatus(method string, status int) bool {
	if status == http.StatusTooManyRequests && method == http.MethodPost {
		return false
	}
	return retryableStatuses[status]
}

func hasIdempotencyKey(req *resty.Request) bool {
	for k := range req.Header {
		if http.CanonicalHeaderKey(k) == "Idempotency-Key" {
			return true
		}
	}
	return false
}

func backoffDuration(attempt int) time.Duration {
	base := float64(backoffBaseMs) * float64(int64(1)<<attempt)
	if base > backoffCapMs {
		base = backoffCapMs
	}
	jittered := base * (1 + (rand.Float64()*2-1)*backoffJitter) //nolint:gosec // timing jitter, not security-sensitive
	return time.Duration(jittered) * time.Millisecond
}

func retryAfterDuration(res *resty.Response) (time.Duration, bool) {
	return parseRetryAfter(res.Header().Get("Retry-After"))
}

// parseRetryAfter reads a Retry-After header given in seconds.
func parseRetryAfter(raw string) (time.Duration, bool) {
	if raw == "" {
		return 0, false
	}
	secs, err := strconv.ParseFloat(raw, 64)
	if err != nil || secs < 0 {
		return 0, false
	}
	return time.Duration(secs * float64(time.Second)), true
}

// executeWithRetry sends req via method+path, retrying transient failures (429 except on
// POST, 502/503/504, or a network error) up to maxRetries times with jittered exponential
// backoff (honoring Retry-After when present). Only idempotent requests are replayed: GET/PUT/DELETE/HEAD/
// OPTIONS, plus POST when the caller supplied an Idempotency-Key header. Cancellation of ctx is
// never retried: the caller set that budget.
//
// Each attempt must receive its response within timeout. A request sent with
// SetDoNotParseResponse (Download) is bounded only until its headers arrive, so its body stays
// readable until the caller calls the returned release. The caller must always call release.
func executeWithRetry(
	ctx context.Context, req *resty.Request, method, path string, maxRetries int, timeout time.Duration,
) (res *resty.Response, release context.CancelFunc, err error) {
	idempotent := idempotentMethods[method] || (method == http.MethodPost && hasIdempotencyKey(req))

	for attempt := 0; ; attempt++ {
		attemptCtx, cancel := context.WithCancel(ctx)
		timer := time.AfterFunc(timeout, cancel)
		res, err = req.SetContext(attemptCtx).Execute(method, path)
		timedOut := !timer.Stop()

		var wait time.Duration
		switch {
		case err != nil:
			cancel()
			if ctx.Err() != nil {
				return res, cancel, err
			}
			if timedOut {
				err = fmt.Errorf("%s %s: no response within %s: %w", method, path, timeout, context.DeadlineExceeded)
			}
			if !idempotent || attempt >= maxRetries {
				return res, cancel, err
			}
			wait = backoffDuration(attempt)
		case idempotent && attempt < maxRetries && isRetryableStatus(method, res.StatusCode()):
			closeRawBody(res)
			cancel()
			var ok bool
			if wait, ok = retryAfterDuration(res); !ok {
				wait = backoffDuration(attempt)
			}
		default:
			return res, cancel, nil
		}

		if !sleepCtx(ctx, wait) {
			return res, cancel, ctx.Err()
		}
	}
}

// closeRawBody releases the body of a response that is about to be retried. A request sent
// with SetDoNotParseResponse (Download) leaves it open; for any other request resty has
// already read and closed it, and closing it again is a no-op.
func closeRawBody(res *resty.Response) {
	if res != nil && res.RawResponse != nil && res.RawResponse.Body != nil {
		_ = res.RawResponse.Body.Close()
	}
}

// sleepCtx sleeps for d or returns false early if ctx is done.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return true
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
