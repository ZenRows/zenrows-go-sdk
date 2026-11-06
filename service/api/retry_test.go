package scraperapi

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// The retry options must not make Client incomparable: that would break callers who compare clients.
var _ = Client{} == Client{}

const formBody = "a=1"

// newServer answers with statuses in turn (the last one repeats), adds Retry-After to each answer when it is set, and
// records the query of each request.
func newServer(t *testing.T, retryAfter string, statuses ...int) (*httptest.Server, chan string) {
	t.Helper()
	queries := make(chan string, 16)
	var n atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries <- r.URL.RawQuery
		if retryAfter != "" {
			w.Header().Set("Retry-After", retryAfter)
		}
		w.WriteHeader(statuses[min(int(n.Add(1)), len(statuses))-1])
	}))
	t.Cleanup(server.Close)
	return server, queries
}

// newRetryClient retries twice, and its 1ms maximum wait keeps every backoff wait at the 250ms floor.
func newRetryClient(baseURL string, opts ...Option) *Client {
	return NewClient(append([]Option{
		WithBaseURL(baseURL), WithAPIKey("k"), WithMaxRetryCount(2), WithRetryMaxWaitTime(time.Millisecond),
	}, opts...)...)
}

// A client that sets no retry count behaves as before, whatever other retry options it passes.
func TestRetryOptionsWithoutACountChangeNothing(t *testing.T) {
	for name, opt := range map[string]Option{
		"negative wait time":  WithRetryWaitTime(-time.Second),
		"zero max wait time":  WithRetryMaxWaitTime(0),
		"zero budget":         WithRetryBudget(0),
		"negative budget":     WithRetryBudget(-time.Second),
		"explicit zero count": WithMaxRetryCount(0),
	} {
		t.Run(name, func(t *testing.T) {
			server, queries := newServer(t, "", http.StatusOK)
			res, err := NewClient(WithBaseURL(server.URL), WithAPIKey("k"), opt).Get(context.Background(), "https://example.com", nil)
			if err != nil || res.StatusCode() != http.StatusOK || len(queries) != 1 {
				t.Fatalf("expected one request and a 200, got %v, %v after %d requests", res, err, len(queries))
			}
		})
	}
}

func TestInvalidRetryOptionsReturnAnError(t *testing.T) {
	for name, opts := range map[string][]Option{
		"negative count":     {WithMaxRetryCount(-1)},
		"negative wait time": {WithMaxRetryCount(2), WithRetryWaitTime(-time.Second)},
		"zero max wait time": {WithMaxRetryCount(2), WithRetryMaxWaitTime(0)},
		"zero budget":        {WithMaxRetryCount(2), WithRetryBudget(0)},
		"negative budget":    {WithMaxRetryCount(2), WithRetryBudget(-time.Second)},
	} {
		t.Run(name, func(t *testing.T) {
			server, queries := newServer(t, "", http.StatusOK)
			_, err := NewClient(append(opts, WithBaseURL(server.URL), WithAPIKey("k"))...).Get(context.Background(), "https://example.com", nil)
			var invalid InvalidOptionError
			if !errors.As(err, &invalid) || len(queries) != 0 {
				t.Fatalf("expected an InvalidOptionError and no request, got %v after %d requests", err, len(queries))
			}
		})
	}
}

func TestWithRetryWaitTimeZeroMeans250ms(t *testing.T) {
	opts := NewClient(WithMaxRetryCount(1), WithRetryWaitTime(0)).cfg.retryOptions
	if err := opts.validate(); err != nil || opts.retryWaitTime != minRetryWait {
		t.Fatalf("expected a valid 250ms wait time, got %v, %v", opts.retryWaitTime, err)
	}
}

// Each server answers the status, then 200, so a retry shows as a 200 after 2 requests.
func TestRetriedStatuses(t *testing.T) {
	for status, retried := range map[int]bool{
		429: true, 500: true, 502: true, 503: true, 504: true, 422: true, // a 422 that is not no_data_extracted
		400: false, 401: false, 402: false, 403: false, 404: false, 501: false,
	} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			server, queries := newServer(t, "", status, http.StatusOK)
			res, err := newRetryClient(server.URL).Get(context.Background(), "https://example.com", nil)
			if err != nil {
				t.Fatal(err)
			}
			if got := res.StatusCode() == http.StatusOK && len(queries) == 2; got != retried {
				t.Fatalf("retried = %v, want %v (status %d after %d requests)", got, retried, res.StatusCode(), len(queries))
			}
			if retried {
				if first, second := <-queries, <-queries; first != second {
					t.Fatalf("expected the retry to send the same query, got %q, then %q", first, second)
				}
			}
		})
	}
}

func TestRetryCountBoundsTheRetries(t *testing.T) {
	server, queries := newServer(t, "", http.StatusServiceUnavailable)
	res, err := newRetryClient(server.URL).Get(context.Background(), "https://example.com", nil)
	if err != nil || res.StatusCode() != http.StatusServiceUnavailable || len(queries) != 3 {
		t.Fatalf("expected the last 503 after 3 requests, got %v, %v after %d", res, err, len(queries))
	}
}

// Only a GET whose body can be sent again is retried. The Idempotency-Key header changes nothing: the API does not
// dedupe on it.
func TestOnlyAGetWithAReplayableBodyIsRetried(t *testing.T) {
	key := &RequestParameters{CustomHeaders: http.Header{"Idempotency-Key": {"key-1"}}}
	cases := []struct {
		name, method string
		body         any
		requests     int
	}{
		{"GET, no body", http.MethodGet, nil, 2},
		{"GET, string", http.MethodGet, formBody, 2},
		{"GET, bytes", http.MethodGet, []byte(formBody), 2},
		{"GET, map", http.MethodGet, map[string]string{"a": "1"}, 2},
		{"GET, io.Reader", http.MethodGet, strings.NewReader(formBody), 1},
		{"POST", http.MethodPost, formBody, 1},
		{"PUT", http.MethodPut, formBody, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server, queries := newServer(t, "", http.StatusServiceUnavailable, http.StatusOK)
			if _, err := newRetryClient(server.URL).Scrape(context.Background(), tc.method, "https://example.com", key, tc.body); err != nil {
				t.Fatal(err)
			}
			if len(queries) != tc.requests {
				t.Fatalf("expected %d requests, got %d", tc.requests, len(queries))
			}
		})
	}
}

// A status the API passed through from the target site is not retried: another try would get the same answer.
func TestTargetSiteStatusesAreNotRetried(t *testing.T) {
	cases := []struct {
		name     string
		params   RequestParameters
		status   int
		requests int
	}{
		{"ReturnOriginalStatus", RequestParameters{ReturnOriginalStatus: true}, 503, 1},
		{"original_status custom param", RequestParameters{CustomParams: map[string]string{"original_status": "true"}}, 429, 1},
		{"original_status false", RequestParameters{CustomParams: map[string]string{"original_status": "false"}}, 503, 2},
		{"AllowedStatusCodes, listed", RequestParameters{AllowedStatusCodes: []int{404, 503}}, 503, 1},
		{"AllowedStatusCodes, not listed", RequestParameters{AllowedStatusCodes: []int{404, 503}}, 502, 2},
		{"allowed_status_codes custom param", RequestParameters{CustomParams: map[string]string{"allowed_status_codes": "404, 503"}}, 503, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server, queries := newServer(t, "", tc.status, http.StatusOK)
			if _, err := newRetryClient(server.URL).Get(context.Background(), "https://example.com", &tc.params); err != nil {
				t.Fatal(err)
			}
			if len(queries) != tc.requests {
				t.Fatalf("expected %d requests, got %d", tc.requests, len(queries))
			}
		})
	}
}

// A failed dial is retried through the real transport, even when a status from the target site would not be, since
// there is no status. A POST is still sent once.
func TestConnectFailuresAreRetried(t *testing.T) {
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closedAddr := listener.Addr().String()
	_ = listener.Close()
	refused := func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, closedAddr)
	}
	failWith := func(err error) func(context.Context, string, string) (net.Conn, error) {
		return func(context.Context, string, string) (net.Conn, error) {
			return nil, &net.OpError{Op: dialOp, Err: err}
		}
	}
	cases := []struct {
		name   string
		method string
		params *RequestParameters
		dial   func(context.Context, string, string) (net.Conn, error)
		dials  int32
	}{
		{"refused", http.MethodGet, nil, refused, 3},
		{"name not resolved", http.MethodGet, nil, failWith(&net.DNSError{Err: "no such host", IsNotFound: true}), 3},
		{"connect timeout", http.MethodGet, nil, failWith(os.ErrDeadlineExceeded), 3},
		{"refused, ReturnOriginalStatus", http.MethodGet, &RequestParameters{ReturnOriginalStatus: true}, refused, 3},
		{"refused, POST", http.MethodPost, nil, refused, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newRetryClient("http://" + closedAddr)
			var dials atomic.Int32
			c.http.SetTransport(&http.Transport{DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				dials.Add(1)
				return tc.dial(ctx, network, addr)
			}})
			if _, err := c.Scrape(context.Background(), tc.method, "https://example.com", tc.params, nil); err == nil {
				t.Fatal("expected an error")
			}
			if dials.Load() != tc.dials {
				t.Fatalf("expected %d dials, got %d", tc.dials, dials.Load())
			}
		})
	}
}

// Only a failed dial is retried: any other error may come after the API got the request.
func TestIsConnectFailure(t *testing.T) {
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	wrap := func(err error) error { return &url.Error{Op: http.MethodGet, Err: err} }
	dialRefused := wrap(&net.OpError{Op: dialOp, Err: syscall.ECONNREFUSED})
	cases := []struct {
		name string
		ctx  context.Context
		err  error
		want bool
	}{
		{"refused", context.Background(), dialRefused, true},
		{"network unreachable", context.Background(), wrap(&net.OpError{Op: dialOp, Err: syscall.ENETUNREACH}), true},
		{"reset after sending", context.Background(), wrap(&net.OpError{Op: "read", Err: syscall.ECONNRESET}), false},
		{"EOF after sending", context.Background(), wrap(io.EOF), false},
		{"read timeout", context.Background(), wrap(&net.OpError{Op: "read", Err: os.ErrDeadlineExceeded}), false},
		{"caller deadline", context.Background(), wrap(context.DeadlineExceeded), false},
		{"refused after the caller canceled", canceled, dialRefused, false},
	}
	for _, tc := range cases {
		if got := isConnectFailure(tc.ctx, tc.err); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestConnectionDroppedAfterSendingIsNotRetried(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		if conn, _, err := w.(http.Hijacker).Hijack(); err == nil {
			_ = conn.Close()
		}
	}))
	defer server.Close()
	if _, err := newRetryClient(server.URL).Get(context.Background(), "https://example.com", nil); err == nil || requests.Load() != 1 {
		t.Fatalf("expected an error after 1 request, got %v after %d", err, requests.Load())
	}
}

func TestCanceledContextStopsTheCall(t *testing.T) {
	t.Run("during the attempt", func(t *testing.T) {
		var requests atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			requests.Add(1)
			<-r.Context().Done()
		}))
		defer server.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		if _, err := newRetryClient(server.URL).Get(ctx, "https://example.com", nil); !errors.Is(err, context.DeadlineExceeded) || requests.Load() != 1 {
			t.Fatalf("expected a deadline error after 1 request, got %v after %d", err, requests.Load())
		}
	})
	t.Run("during the wait", func(t *testing.T) {
		server, queries := newServer(t, "30", http.StatusServiceUnavailable)
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		defer cancel()
		start := time.Now()
		if _, err := newRetryClient(server.URL).Get(ctx, "https://example.com", nil); !errors.Is(err, context.DeadlineExceeded) || len(queries) != 1 {
			t.Fatalf("expected a deadline error after 1 request, got %v after %d", err, len(queries))
		}
		if elapsed := time.Since(start); elapsed > 2*time.Second {
			t.Fatalf("expected the wait to end with the context, took %v", elapsed)
		}
	})
}

// Conformance vectors: Retry-After header -> wait.
func TestParseRetryAfter(t *testing.T) {
	for header, want := range map[string]time.Duration{
		"5": 5 * time.Second, " 7 ": 7 * time.Second, "0": minRetryWait, "99999": maxRetryAfterWait,
		"99999999999999999999999": maxRetryAfterWait, // past uint64
	} {
		if got, ok := parseRetryAfter(header); !ok || got != want {
			t.Errorf("Retry-After %q: got %v, %v; want %v", header, got, ok, want)
		}
	}
	for _, header := range []string{"", "1.5", "-1", "+5", "1e3", "0x10", "abc", "Wed, 21 Oct 2026 07:28:00 GMT"} {
		if _, ok := parseRetryAfter(header); ok {
			t.Errorf("Retry-After %q: expected it to be ignored", header)
		}
	}
	// A Retry-After is not limited by WithRetryMaxWaitTime.
	if got := (retryOptions{retryWaitTime: time.Second, retryMaxWaitTime: time.Second}).wait("20", 0); got != 20*time.Second {
		t.Errorf("Retry-After 20 with a 1s max wait: got %v, want 20s", got)
	}
}

func TestRetryAfterSetsTheWait(t *testing.T) {
	server, queries := newServer(t, "1", http.StatusServiceUnavailable, http.StatusOK)
	start := time.Now()
	if _, err := newRetryClient(server.URL).Get(context.Background(), "https://example.com", nil); err != nil || len(queries) != 2 {
		t.Fatalf("expected a retry, got %v after %d requests", err, len(queries))
	}
	if elapsed := time.Since(start); elapsed < time.Second {
		t.Fatalf("expected the retry after the 1s Retry-After, took %v", elapsed)
	}
}

// Conformance vectors: jitter bounds. Retry n waits a uniform time in [250ms, max(250ms, min(maxWait, base*2^n))].
func TestJitterBounds(t *testing.T) {
	cases := []struct {
		base, maxWait time.Duration
		n             int
		ceiling       time.Duration
	}{
		{time.Second, 30 * time.Second, 0, time.Second},
		{time.Second, 30 * time.Second, 1, 2 * time.Second},
		{time.Second, 30 * time.Second, 2, 4 * time.Second},
		{time.Second, 30 * time.Second, 5, 30 * time.Second},
		{time.Second, 30 * time.Second, 100, 30 * time.Second},
		{time.Second, 5 * time.Second, 4, 5 * time.Second},
		{time.Second, 100 * time.Millisecond, 0, minRetryWait},
		{time.Millisecond, 30 * time.Second, 0, minRetryWait},
		{minRetryWait, 30 * time.Second, 0, minRetryWait}, // WithRetryWaitTime(0)
		{100 * time.Millisecond, 30 * time.Second, 3, 800 * time.Millisecond},
	}
	for _, tc := range cases {
		if got := backoffCeiling(tc.base, tc.maxWait, tc.n); got != tc.ceiling {
			t.Fatalf("base %v, max %v, retry %d: ceiling %v, want %v", tc.base, tc.maxWait, tc.n, got, tc.ceiling)
		}
		// The waits must stay in the bounds and spread over them.
		opts := retryOptions{retryWaitTime: tc.base, retryMaxWaitTime: tc.maxWait}
		lowest, highest := tc.ceiling, time.Duration(0)
		for range 1000 {
			wait := opts.wait("", tc.n)
			lowest, highest = min(lowest, wait), max(highest, wait)
		}
		tenth := (tc.ceiling - minRetryWait) / 10
		if lowest < minRetryWait || highest > tc.ceiling || lowest > minRetryWait+tenth || highest < tc.ceiling-tenth {
			t.Fatalf("base %v, max %v, retry %d: waits spanned [%v, %v], want [250ms, %v]", tc.base, tc.maxWait, tc.n, lowest, highest, tc.ceiling)
		}
	}
}

// No retry starts when the time since the first attempt plus the next wait would pass the budget.
func TestRetryBudgetStopsRetries(t *testing.T) {
	t.Run("a wait past the budget", func(t *testing.T) {
		server, queries := newServer(t, "5", http.StatusServiceUnavailable)
		start := time.Now()
		res, err := newRetryClient(server.URL, WithRetryBudget(time.Second)).Get(context.Background(), "https://example.com", nil)
		if err != nil || res.StatusCode() != http.StatusServiceUnavailable || len(queries) != 1 {
			t.Fatalf("expected the 503 after 1 request, got %v, %v after %d", res, err, len(queries))
		}
		if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
			t.Fatalf("expected no wait, took %v", elapsed)
		}
	})
	t.Run("counted from the first attempt", func(t *testing.T) {
		// Each wait is 250ms: the retries start at about 250ms and 500ms, and a third would start after the 650ms budget.
		server, queries := newServer(t, "", http.StatusServiceUnavailable)
		_, err := newRetryClient(server.URL, WithMaxRetryCount(5), WithRetryBudget(650*time.Millisecond)).
			Get(context.Background(), "https://example.com", nil)
		if err != nil || len(queries) != 3 {
			t.Fatalf("expected 3 requests, got %v after %d", err, len(queries))
		}
	})
}

// PrepareDomain and PreparedDomainStatus are sent once: their 429 and 503 are answers to read.
func TestPreparedDomainCallsAreNotRetried(t *testing.T) {
	calls := map[string]func(*Client) error{
		"PrepareDomain": func(c *Client) error {
			_, err := c.PrepareDomain(context.Background(), "www.example.com", nil)
			return err
		},
		"PreparedDomainStatus": func(c *Client) error {
			_, err := c.PreparedDomainStatus(context.Background(), "www.example.com")
			return err
		},
	}
	for name, call := range calls {
		for _, status := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable} {
			t.Run(name+"/"+strconv.Itoa(status), func(t *testing.T) {
				server, queries := newServer(t, "0", status, http.StatusOK)
				var perr *PreparedDomainError
				if err := call(newRetryClient(server.URL)); !errors.As(err, &perr) || len(queries) != 1 {
					t.Fatalf("expected a %d PreparedDomainError after 1 request, got %v after %d", status, err, len(queries))
				}
			})
		}
	}
}
