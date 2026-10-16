package scraperapi_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	scraperapi "github.com/zenrows/zenrows-go-sdk/service/api"
)

// arrivals records when each request reached a test server.
type arrivals struct {
	mu    sync.Mutex
	times []time.Time
}

func (a *arrivals) add() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.times = append(a.times, time.Now())
	return len(a.times)
}

func (a *arrivals) count() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.times)
}

func (a *arrivals) gaps() []time.Duration {
	a.mu.Lock()
	defer a.mu.Unlock()
	var gaps []time.Duration
	for i := 1; i < len(a.times); i++ {
		gaps = append(gaps, a.times[i].Sub(a.times[i-1]))
	}
	return gaps
}

// newStatusServer answers each request with statuses[n] (the last one repeats), setting
// Retry-After when retryAfter is not empty.
func newStatusServer(t *testing.T, retryAfter string, statuses ...int) (*httptest.Server, *arrivals) {
	t.Helper()
	got := &arrivals{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := got.add()
		status := statuses[min(n, len(statuses))-1]
		if retryAfter != "" && status != http.StatusOK {
			w.Header().Set("Retry-After", retryAfter)
		}
		w.WriteHeader(status)
	}))
	t.Cleanup(server.Close)
	return server, got
}

func fastRetryClient(baseURL string, opts ...scraperapi.Option) *scraperapi.Client {
	return scraperapi.NewClient(append([]scraperapi.Option{
		scraperapi.WithBaseURL(baseURL),
		scraperapi.WithAPIKey("k"),
		scraperapi.WithRetryWaitTime(time.Millisecond),
		scraperapi.WithRetryMaxWaitTime(time.Millisecond),
	}, opts...)...)
}

func TestDefaultClientRetriesTwice(t *testing.T) {
	// Retry-After: 0 keeps each wait at the 250ms floor, so the test stays fast.
	server, got := newStatusServer(t, "0", http.StatusServiceUnavailable)
	client := scraperapi.NewClient(scraperapi.WithBaseURL(server.URL), scraperapi.WithAPIKey("k"))

	res, err := client.Get(context.Background(), "https://example.com", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.StatusCode() != http.StatusServiceUnavailable {
		t.Fatalf("expected the last 503 to be returned, got %d", res.StatusCode())
	}
	if got.count() != 3 {
		t.Fatalf("expected 3 attempts (1 + 2 default retries), got %d", got.count())
	}
}

func TestRetriesEachRetryableStatus(t *testing.T) {
	for _, status := range []int{
		http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout,
	} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			server, got := newStatusServer(t, "", status, http.StatusOK)

			res, err := fastRetryClient(server.URL).Get(context.Background(), "https://example.com", nil)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !res.IsSuccess() || got.count() != 2 {
				t.Fatalf("expected a retry to succeed after %d, got status %d after %d attempts", status, res.StatusCode(), got.count())
			}
		})
	}
}

func TestDoesNotRetryOtherStatuses(t *testing.T) {
	for _, status := range []int{
		http.StatusBadRequest, http.StatusUnauthorized, http.StatusPaymentRequired, http.StatusForbidden,
		http.StatusNotFound, http.StatusUnprocessableEntity, http.StatusInternalServerError,
	} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			server, got := newStatusServer(t, "", status)

			res, err := fastRetryClient(server.URL).Get(context.Background(), "https://example.com", nil)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if res.StatusCode() != status || got.count() != 1 {
				t.Fatalf("expected %d once with no retry, got status %d after %d attempts", status, res.StatusCode(), got.count())
			}
		})
	}
}

func TestRetryWaitIsJitteredWithinBackoffBounds(t *testing.T) {
	server, got := newStatusServer(t, "", http.StatusServiceUnavailable, http.StatusServiceUnavailable, http.StatusOK)
	client := scraperapi.NewClient(
		scraperapi.WithBaseURL(server.URL),
		scraperapi.WithAPIKey("k"),
		scraperapi.WithRetryWaitTime(300*time.Millisecond),
		scraperapi.WithRetryMaxWaitTime(400*time.Millisecond),
	)

	res, err := client.Get(context.Background(), "https://example.com", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.IsSuccess() {
		t.Fatalf("expected the third attempt to succeed, got %d", res.StatusCode())
	}
	// Retry 0 waits in [250ms, 300ms], retry 1 in [250ms, min(400ms, 600ms)]. The slack covers the
	// round trip, which the race detector slows down.
	const slack = 200 * time.Millisecond
	uppers := []time.Duration{300 * time.Millisecond, 400 * time.Millisecond}
	gaps := got.gaps()
	if len(gaps) != len(uppers) {
		t.Fatalf("expected %d retries, got %d", len(uppers), len(gaps))
	}
	for i, gap := range gaps {
		if gap < 250*time.Millisecond || gap > uppers[i]+slack {
			t.Fatalf("retry %d waited %v, want between 250ms and %v", i, gap, uppers[i])
		}
	}
}

func TestRetryAfterIsHonored(t *testing.T) {
	server, got := newStatusServer(t, "1", http.StatusServiceUnavailable, http.StatusOK)

	res, err := fastRetryClient(server.URL).Get(context.Background(), "https://example.com", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.IsSuccess() {
		t.Fatalf("expected the retry to succeed, got %d", res.StatusCode())
	}
	if gaps := got.gaps(); len(gaps) != 1 || gaps[0] < time.Second {
		t.Fatalf("expected one retry after at least the 1s Retry-After, got gaps %v", gaps)
	}
}

func TestRetryBudgetStopsRetries(t *testing.T) {
	t.Run("backoff", func(t *testing.T) {
		// Every jittered wait is at least 250ms, so a 100ms budget fits no retry.
		server, got := newStatusServer(t, "", http.StatusServiceUnavailable)
		client := scraperapi.NewClient(
			scraperapi.WithBaseURL(server.URL),
			scraperapi.WithAPIKey("k"),
			scraperapi.WithRetryBudget(100*time.Millisecond),
		)
		start := time.Now()
		res, err := client.Get(context.Background(), "https://example.com", nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.StatusCode() != http.StatusServiceUnavailable || got.count() != 1 {
			t.Fatalf("expected one 503 with no retry, got status %d after %d attempts", res.StatusCode(), got.count())
		}
		if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
			t.Fatalf("expected the call to return without waiting, took %v", elapsed)
		}
	})
	t.Run("retry-after", func(t *testing.T) {
		server, got := newStatusServer(t, "5", http.StatusServiceUnavailable)
		start := time.Now()
		res, err := fastRetryClient(server.URL, scraperapi.WithRetryBudget(time.Second)).
			Get(context.Background(), "https://example.com", nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.StatusCode() != http.StatusServiceUnavailable || got.count() != 1 {
			t.Fatalf("expected one 503 with no retry, got status %d after %d attempts", res.StatusCode(), got.count())
		}
		if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
			t.Fatalf("expected the call to return without waiting, took %v", elapsed)
		}
	})
}

func TestTimeoutIsNotRetried(t *testing.T) {
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		select {
		case <-r.Context().Done():
		case <-time.After(time.Second):
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := fastRetryClient(server.URL).Get(ctx, "https://example.com", nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected a deadline error, got %v", err)
	}
	if hits.Load() != 1 {
		t.Fatalf("expected 1 attempt with no retry after a timeout, got %d", hits.Load())
	}
}

func TestConnectionErrorBeforeResponseIsRetried(t *testing.T) {
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits.Add(1) == 1 {
			// Drop the connection without answering.
			conn, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				_ = conn.Close()
			}
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	res, err := fastRetryClient(server.URL).Get(context.Background(), "https://example.com", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.IsSuccess() || hits.Load() != 2 {
		t.Fatalf("expected the retry to succeed, got status %d after %d attempts", res.StatusCode(), hits.Load())
	}
}

// A retryable failure on the extract attempt is retried, and the AUTH010 fallback still follows.
func TestExtractRetriesThenFallsBackOnAuth010(t *testing.T) {
	var calls atomic.Int32
	var fallbackQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch calls.Add(1) {
		case 1:
			w.WriteHeader(http.StatusServiceUnavailable)
		case 2:
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(http.StatusPaymentRequired)
			_, _ = w.Write([]byte(`{"code":"AUTH010","title":"Domain not enabled","status":402}`))
		default:
			fallbackQuery = r.URL.RawQuery
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	res, err := fastRetryClient(server.URL).Extract(context.Background(), "https://example.com", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.IsSuccess() || calls.Load() != 3 {
		t.Fatalf("expected 503, AUTH010, then a successful fallback; got status %d after %d calls", res.StatusCode(), calls.Load())
	}
	if !contains(fallbackQuery, "autoparse=true") || contains(fallbackQuery, "extract=") {
		t.Fatalf("expected the fallback to set autoparse and drop extract, got %q", fallbackQuery)
	}
}
