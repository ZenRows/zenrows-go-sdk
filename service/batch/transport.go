package batch

import (
	"context"
	"errors"
	"math/rand"
	"net/http"
	"strconv"
	"time"

	"github.com/go-resty/resty/v2"
)

// Retry tuning: ~250ms * 2^attempt, +/-20% jitter, capped at 10s. Mirrors the Python SDK's
// batch transport so behavior is consistent across languages.
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

func hasIdempotencyKey(req *resty.Request) bool {
	for k := range req.Header {
		if http.CanonicalHeaderKey(k) == "Idempotency-Key" {
			return true
		}
	}
	return false
}

func backoffDuration(attempt int) time.Duration {
	base := float64(backoffBaseMs) * float64(int64(1)<<uint(attempt))
	if base > backoffCapMs {
		base = backoffCapMs
	}
	jittered := base * (1 + (rand.Float64()*2-1)*backoffJitter)
	return time.Duration(jittered) * time.Millisecond
}

func retryAfterDuration(res *resty.Response) (time.Duration, bool) {
	raw := res.Header().Get("Retry-After")
	if raw == "" {
		return 0, false
	}
	secs, err := strconv.ParseFloat(raw, 64)
	if err != nil || secs < 0 {
		return 0, false
	}
	return time.Duration(secs * float64(time.Second)), true
}

// executeWithRetry sends req via method+path, retrying transient failures (429/502/503/504,
// or a network error) up to maxRetries times with jittered exponential backoff (honoring
// Retry-After when present). Only idempotent requests are replayed: GET/PUT/DELETE/HEAD/
// OPTIONS, plus POST when the caller supplied an Idempotency-Key header. Context
// cancellation/timeout is never retried — the caller set that budget.
func executeWithRetry(ctx context.Context, req *resty.Request, method, path string, maxRetries int) (*resty.Response, error) {
	idempotent := idempotentMethods[method] || (method == http.MethodPost && hasIdempotencyKey(req))

	attempt := 0
	for {
		res, err := req.Execute(method, path)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
				return res, err
			}
			if idempotent && attempt < maxRetries {
				if !sleepCtx(ctx, backoffDuration(attempt)) {
					return res, ctx.Err()
				}
				attempt++
				continue
			}
			return res, err
		}

		if idempotent && attempt < maxRetries && retryableStatuses[res.StatusCode()] {
			wait, ok := retryAfterDuration(res)
			if !ok {
				wait = backoffDuration(attempt)
			}
			if !sleepCtx(ctx, wait) {
				return res, ctx.Err()
			}
			attempt++
			continue
		}

		return res, nil
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
