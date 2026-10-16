package scraperapi

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"syscall"
	"testing"
	"time"

	"github.com/go-resty/resty/v2"
)

func TestJitterWaitStaysWithinFullJitterBounds(t *testing.T) {
	const samples = 2000
	for retry := range 7 {
		upper := min(30*time.Second, time.Second<<retry)
		lowest, highest := upper, time.Duration(0)
		for range samples {
			wait := jitterWait(time.Second, 30*time.Second, retry)
			if wait < minRetryWait || wait > upper {
				t.Fatalf("retry %d: wait %v outside [%v, %v]", retry, wait, minRetryWait, upper)
			}
			lowest, highest = min(lowest, wait), max(highest, wait)
		}
		// Full jitter spreads over the whole range, not around its middle.
		tenth := (upper - minRetryWait) / 10
		if lowest > minRetryWait+tenth || highest < upper-tenth {
			t.Fatalf("retry %d: waits spanned only [%v, %v] of [%v, %v]", retry, lowest, highest, minRetryWait, upper)
		}
	}
}

func TestJitterWaitWithSmallBaseNeverExceedsTheBackoff(t *testing.T) {
	for retry := range 5 {
		upper := min(5*time.Millisecond, time.Millisecond<<retry)
		for range 100 {
			if wait := jitterWait(time.Millisecond, 5*time.Millisecond, retry); wait > upper {
				t.Fatalf("retry %d: wait %v above %v", retry, wait, upper)
			}
		}
	}
}

func TestJitterWaitDoesNotOverflow(t *testing.T) {
	for _, retry := range []int{40, 62, 63, 100} {
		if wait := jitterWait(time.Second, 30*time.Second, retry); wait < minRetryWait || wait > 30*time.Second {
			t.Fatalf("retry %d: wait %v outside [%v, 30s]", retry, wait, minRetryWait)
		}
	}
}

func TestRetryWaitHonorsRetryAfterUpTo30s(t *testing.T) {
	opts := retryOptions{retryWaitTime: time.Second, retryMaxWaitTime: 5 * time.Second}
	cases := map[string]time.Duration{
		"2":     2 * time.Second,
		"1.5":   1500 * time.Millisecond,
		"20":    20 * time.Second, // above retryMaxWaitTime, still honored
		"120":   maxRetryAfterWait,
		"1e300": maxRetryAfterWait,
		"0":     minRetryWait,
	}
	for header, want := range cases {
		if got := retryWait(opts, header, 0); got != want {
			t.Fatalf("Retry-After %q: wait %v, want %v", header, got, want)
		}
	}
	for _, header := range []string{"", "-1", "soon", "Wed, 21 Oct 2026 07:28:00 GMT"} {
		if got := retryWait(opts, header, 0); got < minRetryWait || got > time.Second {
			t.Fatalf("Retry-After %q: expected the backoff wait in [%v, 1s], got %v", header, minRetryWait, got)
		}
	}
}

type timeoutError struct{}

func (timeoutError) Error() string   { return "i/o timeout" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }

func TestIsRetryableFailure(t *testing.T) {
	noResponse := &resty.Response{}
	answered := &resty.Response{RawResponse: &http.Response{StatusCode: http.StatusServiceUnavailable}}
	cases := []struct {
		name string
		resp *resty.Response
		err  error
		want bool
	}{
		{"connection refused", noResponse, &url.Error{Op: http.MethodGet, Err: syscall.ECONNREFUSED}, true},
		{"connection reset", noResponse, &url.Error{Op: http.MethodGet, Err: syscall.ECONNRESET}, true},
		{"client timeout", noResponse, &url.Error{Op: http.MethodGet, Err: timeoutError{}}, false},
		{"deadline", noResponse, &url.Error{Op: http.MethodGet, Err: context.DeadlineExceeded}, false},
		{"canceled", noResponse, context.Canceled, false},
		{"error after a response arrived", answered, errors.New("body read failed"), false},
		{"503", answered, nil, true},
	}
	for _, tc := range cases {
		if got := isRetryableFailure(tc.resp, tc.err); got != tc.want {
			t.Fatalf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}
