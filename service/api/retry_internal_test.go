package scraperapi

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/go-resty/resty/v2"
)

// randAtZero and randAtMax are the two ends of the random source: rand.N(n) returns a value in [0, n).
func randAtZero(time.Duration) time.Duration  { return 0 }
func randAtMax(n time.Duration) time.Duration { return n - 1 }

// fakeClock is a clock the tests move by hand. It is read and moved from different goroutines.
type fakeClock struct{ nanos atomic.Int64 }

func newFakeClock() *fakeClock {
	clock := &fakeClock{}
	clock.nanos.Store(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).UnixNano())
	return clock
}

func (f *fakeClock) now() time.Time          { return time.Unix(0, f.nanos.Load()) }
func (f *fakeClock) advance(d time.Duration) { f.nanos.Add(int64(d)) }

func TestDefaultRetryOptions(t *testing.T) {
	got := defaultOptions().retryOptions
	want := retryOptions{
		maxRetryCount:    2,
		retryWaitTime:    time.Second,
		retryMaxWaitTime: 30 * time.Second,
		retryBudget:      90 * time.Second,
	}
	if got != want {
		t.Fatalf("default retry options %+v, want %+v", got, want)
	}
	if minRetryWait != 250*time.Millisecond || maxRetryAfterWait != 30*time.Second {
		t.Fatalf("wait bounds [%v, %v], want [250ms, 30s]", minRetryWait, maxRetryAfterWait)
	}
}

// Conformance vectors: Retry-After header value -> delay.
func TestRetryAfterConformance(t *testing.T) {
	c := NewClient(WithAPIKey("k"))
	// At its maximum, the random source makes the first jittered wait exactly 1s, which no valid
	// header below produces, so a header that was wrongly honored shows.
	c.randN = randAtMax
	const jitter = time.Second

	honored := []struct {
		header string
		want   time.Duration
	}{
		{"5", 5 * time.Second},
		{"0", 250 * time.Millisecond},
		{" 7 ", 7 * time.Second},
		{"99999", 30 * time.Second},
		{"99999999999999999999999", 30 * time.Second}, // past uint64
	}
	for _, tc := range honored {
		if got, ok := parseRetryAfter(tc.header); !ok || got != tc.want {
			t.Fatalf("Retry-After %q: parsed (%v, %v), want (%v, true)", tc.header, got, ok, tc.want)
		}
		if got := c.retryWait(tc.header, 0); got != tc.want {
			t.Fatalf("Retry-After %q: wait %v, want %v", tc.header, got, tc.want)
		}
	}

	for _, header := range []string{"1.5", "-1", "1e3", "0x10", "", "abc", "+5", "Wed, 21 Oct 2026 07:28:00 GMT"} {
		if _, ok := parseRetryAfter(header); ok {
			t.Fatalf("Retry-After %q: expected it to be ignored", header)
		}
		if got := c.retryWait(header, 0); got != jitter {
			t.Fatalf("Retry-After %q: wait %v, want the jittered %v", header, got, jitter)
		}
	}
}

// The Retry-After cap is fixed at 30s: a small RetryMaxWaitTime does not lower it.
func TestRetryAfterIgnoresRetryMaxWaitTime(t *testing.T) {
	c := NewClient(WithAPIKey("k"), WithRetryMaxWaitTime(time.Second))
	if got := c.retryWait("20", 0); got != 20*time.Second {
		t.Fatalf("Retry-After 20 with RetryMaxWaitTime 1s: wait %v, want 20s", got)
	}
}

// Conformance vectors: jitter bounds, with the random source at 0 and at its maximum.
func TestJitterConformance(t *testing.T) {
	cases := []struct {
		name     string
		base     time.Duration
		maxWait  time.Duration
		attempt  int
		low, top time.Duration
	}{
		{"attempt 0", time.Second, 30 * time.Second, 0, 250 * time.Millisecond, time.Second},
		{"attempt 1", time.Second, 30 * time.Second, 1, 250 * time.Millisecond, 2 * time.Second},
		{"attempt 2", time.Second, 30 * time.Second, 2, 250 * time.Millisecond, 4 * time.Second},
		{"attempt 5", time.Second, 30 * time.Second, 5, 250 * time.Millisecond, 30 * time.Second},
		{"attempt 9", time.Second, 30 * time.Second, 9, 250 * time.Millisecond, 30 * time.Second},
		{"attempt 100", time.Second, 30 * time.Second, 100, 250 * time.Millisecond, 30 * time.Second},
		{"max wait 5s, attempt 4", time.Second, 5 * time.Second, 4, 250 * time.Millisecond, 5 * time.Second},
		{"max wait 100ms", time.Second, 100 * time.Millisecond, 0, 250 * time.Millisecond, 250 * time.Millisecond},
		{"wait time 1ms", time.Millisecond, 30 * time.Second, 0, 250 * time.Millisecond, 250 * time.Millisecond},
		{"wait time 100ms, attempt 3", 100 * time.Millisecond, 30 * time.Second, 3, 250 * time.Millisecond, 800 * time.Millisecond},
	}
	for _, tc := range cases {
		if got := jitterWait(tc.base, tc.maxWait, tc.attempt, randAtZero); got != tc.low {
			t.Fatalf("%s: random at 0 gave %v, want %v", tc.name, got, tc.low)
		}
		if got := jitterWait(tc.base, tc.maxWait, tc.attempt, randAtMax); got != tc.top {
			t.Fatalf("%s: random at max gave %v, want %v", tc.name, got, tc.top)
		}
	}
}

func TestJitterWaitSpreadsOverTheWholeRange(t *testing.T) {
	c := NewClient(WithAPIKey("k"))
	const samples = 2000
	for attempt := range 7 {
		upper := min(30*time.Second, time.Second<<attempt)
		lowest, highest := upper, time.Duration(0)
		for range samples {
			wait := jitterWait(time.Second, 30*time.Second, attempt, c.randN)
			if wait < minRetryWait || wait > upper {
				t.Fatalf("attempt %d: wait %v outside [%v, %v]", attempt, wait, minRetryWait, upper)
			}
			lowest, highest = min(lowest, wait), max(highest, wait)
		}
		tenth := (upper - minRetryWait) / 10
		if lowest > minRetryWait+tenth || highest < upper-tenth {
			t.Fatalf("attempt %d: waits spanned only [%v, %v] of [%v, %v]", attempt, lowest, highest, minRetryWait, upper)
		}
	}
}

// newRetryResponse builds the response shouldRetry sees after the given attempt of a GET.
func newRetryResponse(c *Client, ctx context.Context, attempt, status int, retryAfter string) *resty.Response {
	req := c.http.R().SetContext(ctx)
	req.Method = http.MethodGet
	req.Attempt = attempt
	header := http.Header{}
	if retryAfter != "" {
		header.Set("Retry-After", retryAfter)
	}
	return &resty.Response{Request: req, RawResponse: &http.Response{StatusCode: status, Header: header}}
}

// resty reads a 0 from retryAfter as "use your own backoff", so retryAfter never returns 0.
func TestRetryAfterNeverReturnsZero(t *testing.T) {
	c := NewClient(WithAPIKey("k"))
	withWait := func(wait time.Duration) *resty.Response {
		ctx := c.withRetryCall(context.Background(), true, nil)
		resp := newRetryResponse(c, ctx, 1, http.StatusServiceUnavailable, "")
		call, _ := retryCallFrom(resp)
		call.nextWait = wait
		return resp
	}
	cases := []struct {
		name string
		resp *resty.Response
		want time.Duration
	}{
		{"no response", nil, minRetryWait},
		{"no per-call state", newRetryResponse(c, context.Background(), 1, http.StatusServiceUnavailable, ""), minRetryWait},
		{"no chosen wait", withWait(0), minRetryWait},
		{"chosen wait", withWait(3 * time.Second), 3 * time.Second},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := retryAfter(nil, tc.resp)
			if err != nil || got != tc.want {
				t.Fatalf("retryAfter = %v, %v; want %v, nil", got, err, tc.want)
			}
		})
	}
}

// The retry after attempt n (resty counts from 1) uses exponent n-1, so the first retry waits at most 1s.
func TestShouldRetryUsesZeroBasedExponent(t *testing.T) {
	c := NewClient(WithAPIKey("k"), WithMaxRetryCount(5))
	c.randN = randAtMax
	ctx := c.withRetryCall(context.Background(), true, nil)
	for attempt, want := range map[int]time.Duration{1: time.Second, 2: 2 * time.Second, 3: 4 * time.Second} {
		resp := newRetryResponse(c, ctx, attempt, http.StatusServiceUnavailable, "")
		if !c.shouldRetry(resp, nil) {
			t.Fatalf("attempt %d: expected a retry", attempt)
		}
		if got, _ := retryAfter(nil, resp); got != want {
			t.Fatalf("attempt %d: wait %v, want %v", attempt, got, want)
		}
	}
}

func TestShouldRetryBudgetCountsTheElapsedTime(t *testing.T) {
	clock := newFakeClock()
	c := NewClient(WithAPIKey("k"), WithMaxRetryCount(5))
	c.now = clock.now
	ctx := c.withRetryCall(context.Background(), true, nil)

	clock.advance(89 * time.Second)
	// 89s + 1s fits in the 90s budget, exactly.
	if !c.shouldRetry(newRetryResponse(c, ctx, 1, http.StatusServiceUnavailable, "1"), nil) {
		t.Fatal("expected a retry whose wait ends exactly at the budget")
	}
	clock.advance(time.Millisecond)
	if c.shouldRetry(newRetryResponse(c, ctx, 2, http.StatusServiceUnavailable, "1"), nil) {
		t.Fatal("expected no retry once elapsed time plus the wait passes the budget")
	}
}

// newClockServer answers with statuses[n] (the last one repeats) and moves the clock by step on each request.
func newClockServer(t *testing.T, clock *fakeClock, step time.Duration, handle func(n int32, w http.ResponseWriter)) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		clock.advance(step)
		handle(calls.Add(1), w)
	}))
	t.Cleanup(server.Close)
	return server, &calls
}

// The budget counts from the first attempt, across every retry: it is not reset per retry, and it does
// not ignore the time already spent.
func TestRetryBudgetCountsFromTheFirstAttempt(t *testing.T) {
	clock := newFakeClock()
	// Each attempt takes 40s on the fake clock. After attempt 1, 40s + 250ms fits in 90s; after
	// attempt 2, 80s + 250ms fits; after attempt 3, 120s does not.
	server, calls := newClockServer(t, clock, 40*time.Second, func(_ int32, w http.ResponseWriter) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	c := NewClient(WithBaseURL(server.URL), WithAPIKey("k"), WithMaxRetryCount(5))
	c.now, c.randN = clock.now, randAtZero

	res, err := c.Get(context.Background(), "https://example.com", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.StatusCode() != http.StatusServiceUnavailable || calls.Load() != 3 {
		t.Fatalf("expected 3 attempts and the last 503, got status %d after %d attempts", res.StatusCode(), calls.Load())
	}
}

type timeoutError struct{}

func (timeoutError) Error() string   { return "i/o timeout" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }

// Conformance vectors: a connection failure before the request was sent is retried, through the real
// transport. The dialer fails the way the network would.
func TestConnectFailuresAreRetried(t *testing.T) {
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closedAddr := listener.Addr().String()
	_ = listener.Close()

	cases := map[string]func(ctx context.Context, network, addr string) (net.Conn, error){
		"connection refused": func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, closedAddr)
		},
		"name resolution failure": func(context.Context, string, string) (net.Conn, error) {
			return nil, &net.OpError{Op: dialOp, Err: &net.DNSError{Err: "no such host", Name: "api.invalid", IsNotFound: true}}
		},
		"connect timeout": func(context.Context, string, string) (net.Conn, error) {
			return nil, &net.OpError{Op: dialOp, Err: timeoutError{}}
		},
	}
	for name, dial := range cases {
		t.Run(name, func(t *testing.T) {
			c := NewClient(WithBaseURL("http://"+closedAddr), WithAPIKey("k"))
			c.randN = randAtZero
			var dials atomic.Int32
			c.http.SetTransport(&http.Transport{DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				dials.Add(1)
				return dial(ctx, network, addr)
			}})

			if _, err := c.Get(context.Background(), "https://example.com", nil); err == nil {
				t.Fatal("expected an error")
			}
			if got := dials.Load(); got != 3 {
				t.Fatalf("expected 3 connection attempts (1 + 2 retries), got %d", got)
			}
		})
	}
}

// Conformance vectors: which statuses are retried.
func TestIsRetryableStatus(t *testing.T) {
	cases := []struct {
		status int
		body   string
		want   bool
	}{
		{http.StatusTooManyRequests, "", true},
		{http.StatusBadGateway, "", true},
		{http.StatusServiceUnavailable, "", true},
		{http.StatusGatewayTimeout, "", true},
		{http.StatusInternalServerError, "", true},
		{http.StatusUnprocessableEntity, `{"code":"RESP001"}`, true},
		{http.StatusUnprocessableEntity, "", true},
		{http.StatusUnprocessableEntity, `{"code":"no_data_extracted"}`, false},
		{http.StatusBadRequest, "", false},
		{http.StatusUnauthorized, "", false},
		{http.StatusPaymentRequired, "", false},
		{http.StatusForbidden, "", false},
		{http.StatusNotFound, "", false},
		{http.StatusOK, "", false},
	}
	for _, tc := range cases {
		if got := isRetryableStatus(tc.status, []byte(tc.body)); got != tc.want {
			t.Fatalf("%d %s: got %v, want %v", tc.status, tc.body, got, tc.want)
		}
	}
}

// Conformance vectors: only a failure before the request was sent is retried.
func TestIsConnectFailure(t *testing.T) {
	live := context.Background()
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	dialRefused := &net.OpError{Op: dialOp, Err: syscall.ECONNREFUSED}
	cases := []struct {
		name string
		ctx  context.Context
		err  error
		want bool
	}{
		{"connection refused", live, &url.Error{Op: http.MethodGet, Err: dialRefused}, true},
		{"network unreachable", live, &url.Error{Op: http.MethodGet, Err: &net.OpError{Op: dialOp, Err: syscall.ENETUNREACH}}, true},
		{"name resolution failure", live, &url.Error{Op: http.MethodGet, Err: &net.DNSError{Err: "no such host", IsNotFound: true}}, true},
		{"connect timeout", live, &url.Error{Op: http.MethodGet, Err: &net.OpError{Op: dialOp, Err: timeoutError{}}}, true},
		{"reset after the request was sent", live, &url.Error{Op: http.MethodGet, Err: &net.OpError{Op: "read", Err: syscall.ECONNRESET}}, false},
		{"EOF after the request was sent", live, &url.Error{Op: http.MethodGet, Err: io.EOF}, false},
		{"read timeout", live, &url.Error{Op: http.MethodGet, Err: &net.OpError{Op: "read", Err: timeoutError{}}}, false},
		{"response header timeout", live, &url.Error{Op: http.MethodGet, Err: timeoutError{}}, false},
		{"caller deadline", live, &url.Error{Op: http.MethodGet, Err: context.DeadlineExceeded}, false},
		{"caller cancel", live, context.Canceled, false},
		{"dial error after the caller canceled", canceled, &url.Error{Op: http.MethodGet, Err: dialRefused}, false},
		{"bad URL", live, &url.Error{Op: "parse", Err: errors.New("invalid URL escape")}, false},
	}
	for _, tc := range cases {
		if got := isConnectFailure(tc.ctx, tc.err); got != tc.want {
			t.Fatalf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

// A response that arrived is judged by its status, even when reading it failed.
func TestIsRetryableFailureAfterAResponse(t *testing.T) {
	req := resty.New().R()
	answered := &resty.Response{Request: req, RawResponse: &http.Response{StatusCode: http.StatusServiceUnavailable}}
	// Even an error that would be retried alone is not, once a response arrived.
	for _, err := range []error{errors.New("body read failed"), &net.OpError{Op: dialOp, Err: syscall.ECONNREFUSED}} {
		if isRetryableFailure(answered, err) {
			t.Fatalf("expected no retry for %v after a response arrived", err)
		}
	}
	if !isRetryableFailure(answered, nil) {
		t.Fatal("expected a retry for a 503")
	}
}

// Conformance vectors: only GET, HEAD and OPTIONS are retried, and never with an io.Reader body. Every
// other method is sent once, with or without an Idempotency-Key, whatever its body.
func TestIsReplayable(t *testing.T) {
	cases := []struct {
		name   string
		method string
		body   any
		want   bool
	}{
		{"GET", http.MethodGet, nil, true},
		{"HEAD", http.MethodHead, nil, true},
		{"OPTIONS", http.MethodOptions, nil, true},
		{"GET with a string", http.MethodGet, "a=1", true},
		{"GET with bytes", http.MethodGet, []byte("a=1"), true},
		{"GET with a map", http.MethodGet, map[string]string{"a": "1"}, true},
		{"GET with a strings.Reader", http.MethodGet, strings.NewReader("a=1"), false},
		{"GET with a bytes.Reader", http.MethodGet, bytes.NewReader([]byte("a=1")), false},
		{"GET with a bytes.Buffer", http.MethodGet, bytes.NewBufferString("a=1"), false},
		{"HEAD with a strings.Reader", http.MethodHead, strings.NewReader("a=1"), false},
		{"OPTIONS with a strings.Reader", http.MethodOptions, strings.NewReader("a=1"), false},
		{"POST with no body", http.MethodPost, nil, false},
		{"POST with a string", http.MethodPost, "a=1", false},
		{"POST with bytes", http.MethodPost, []byte("a=1"), false},
		{"POST with a map", http.MethodPost, map[string]string{"a": "1"}, false},
		{"PUT with no body", http.MethodPut, nil, false},
		{"PUT with a struct", http.MethodPut, struct{ A string }{"1"}, false},
		{"PATCH with no body", http.MethodPatch, nil, false},
		{"DELETE with no body", http.MethodDelete, nil, false},
	}
	for _, tc := range cases {
		if got := isReplayable(tc.method, tc.body); got != tc.want {
			t.Fatalf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

// A connection that failed before the request was sent has no status, so original_status and
// allowed_status_codes do not stop its retry.
func TestConnectFailureIsRetriedWithTargetStatusParams(t *testing.T) {
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closedAddr := listener.Addr().String()
	_ = listener.Close()

	params := map[string]*RequestParameters{
		"ReturnOriginalStatus": {ReturnOriginalStatus: true},
		"AllowedStatusCodes":   {AllowedStatusCodes: []int{http.StatusServiceUnavailable}},
	}
	for name, p := range params {
		t.Run(name, func(t *testing.T) {
			c := NewClient(WithBaseURL("http://"+closedAddr), WithAPIKey("k"))
			c.randN = randAtZero
			var dials atomic.Int32
			c.http.SetTransport(&http.Transport{DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
				dials.Add(1)
				return (&net.Dialer{}).DialContext(ctx, network, closedAddr)
			}})

			if _, err := c.Get(context.Background(), "https://example.com", p); err == nil {
				t.Fatal("expected an error")
			}
			if got := dials.Load(); got != 3 {
				t.Fatalf("expected 3 connection attempts (1 + 2 retries), got %d", got)
			}
		})
	}
}

// targetStatusesFrom reads the query parameters the request sends, so a custom param counts too.
func TestTargetStatusesFrom(t *testing.T) {
	cases := []struct {
		name   string
		query  url.Values
		status int
		want   bool
	}{
		{"no params", nil, http.StatusServiceUnavailable, false},
		{"original_status true", url.Values{originalStatusParam: {"true"}}, http.StatusTooManyRequests, true},
		{"original_status true, padded", url.Values{originalStatusParam: {" true "}}, http.StatusTooManyRequests, true},
		{"original_status false", url.Values{originalStatusParam: {"false"}}, http.StatusTooManyRequests, false},
		{"original_status not a bool", url.Values{originalStatusParam: {"yes please"}}, http.StatusTooManyRequests, false},
		{"listed", url.Values{allowedStatusCodesParam: {"404,503"}}, http.StatusServiceUnavailable, true},
		{"listed with spaces", url.Values{allowedStatusCodesParam: {"404, 503"}}, http.StatusServiceUnavailable, true},
		{"not listed", url.Values{allowedStatusCodesParam: {"404,503"}}, http.StatusBadGateway, false},
		{"an entry that is not a number", url.Values{allowedStatusCodesParam: {"abc,503"}}, http.StatusServiceUnavailable, true},
	}
	for _, tc := range cases {
		if got := targetStatusesFrom(tc.query).has(tc.status); got != tc.want {
			t.Fatalf("%s, status %d: got %v, want %v", tc.name, tc.status, got, tc.want)
		}
	}
}

// A POST or a PUT that fails to connect is not retried either, with or without an Idempotency-Key,
// while a GET that fails the same way is (see TestConnectFailuresAreRetried).
func TestPostAndPutAreSentOnceOnARefusedConnection(t *testing.T) {
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closedAddr := listener.Addr().String()
	_ = listener.Close()

	headers := map[string]http.Header{
		"no Idempotency-Key": nil,
		"an Idempotency-Key": {"Idempotency-Key": []string{"key-1"}},
	}
	for _, method := range []string{http.MethodPost, http.MethodPut} {
		for name, header := range headers {
			t.Run(method+"/"+name, func(t *testing.T) {
				c := NewClient(WithBaseURL("http://"+closedAddr), WithAPIKey("k"), WithMaxRetryCount(5))
				c.randN = randAtZero
				var dials atomic.Int32
				c.http.SetTransport(&http.Transport{DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
					dials.Add(1)
					return (&net.Dialer{}).DialContext(ctx, network, closedAddr)
				}})

				params := &RequestParameters{CustomHeaders: header}
				if _, err := c.Scrape(context.Background(), method, "https://example.com", params, "a=1"); err == nil {
					t.Fatal("expected an error")
				}
				if got := dials.Load(); got != 1 {
					t.Fatalf("expected 1 connection attempt with no retry, got %d", got)
				}
			})
		}
	}
}
