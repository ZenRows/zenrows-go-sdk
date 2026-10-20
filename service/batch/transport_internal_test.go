package batch

import (
	"net/http"
	"testing"
	"time"

	"github.com/go-resty/resty/v2"
)

func TestRetryAfterIsCapped(t *testing.T) {
	cases := map[string]time.Duration{
		"2":     2 * time.Second,
		"0.5":   500 * time.Millisecond,
		"30":    maxRetryAfter,
		"3600":  maxRetryAfter,
		"1e300": maxRetryAfter,
	}
	for raw, want := range cases {
		res := &resty.Response{RawResponse: &http.Response{Header: http.Header{"Retry-After": []string{raw}}}}
		got, ok := retryAfterDuration(res)
		if !ok || got != want {
			t.Errorf("Retry-After %q: got %s (%v), want %s", raw, got, ok, want)
		}
	}
}
