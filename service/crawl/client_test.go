package crawl_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zenrows/zenrows-go-sdk/service/crawl"
)

const testCrawlID = "c_123"

func newTestClient(t *testing.T, handler http.HandlerFunc, opts ...crawl.Option) *crawl.Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	opts = append([]crawl.Option{crawl.WithBaseURL(server.URL), crawl.WithAPIKey("test-key")}, opts...)
	return crawl.NewClient(opts...)
}

func writeJSON(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}

func writeProblem(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, `{"code":"`+code+`","title":"t","detail":"d `+code+`","status":`+
		jsonInt(status)+`,"instance":"urn:zenrows:request:x"}`)
}

func jsonInt(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func TestAllMethodsRejectWhenNotConfigured(t *testing.T) {
	client := crawl.NewClient(crawl.WithBaseURL("http://127.0.0.1:0"), crawl.WithAPIKey(""))
	ctx := context.Background()

	cases := map[string]func() error{
		"Create":   func() error { _, err := client.Create(ctx, crawl.CreateParams{}); return err },
		"Get":      func() error { _, err := client.Get(ctx, testCrawlID, crawl.GetOptions{}); return err },
		"List":     func() error { _, err := client.List(ctx, crawl.ListOptions{}); return err },
		"Stop":     func() error { _, err := client.Stop(ctx, testCrawlID); return err },
		"Content":  func() error { _, err := client.Content(ctx, testCrawlID, "ct_1"); return err },
		"Download": func() error { _, err := client.Download(ctx, testCrawlID); return err },
		"Wait":     func() error { _, err := client.Wait(ctx, testCrawlID, crawl.WaitOptions{}); return err },
	}
	for name, call := range cases {
		var notConfigured crawl.NotConfiguredError
		if err := call(); !errors.As(err, &notConfigured) {
			t.Errorf("%s: expected NotConfiguredError, got %v (%T)", name, err, err)
		}
	}
}

func TestCreateSendsOnlySetFields(t *testing.T) {
	var gotKey, gotIdem, gotMethod, gotPath string
	var gotBody map[string]any
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotKey, gotIdem = r.Header.Get("X-API-Key"), r.Header.Get("Idempotency-Key")
		gotMethod, gotPath = r.Method, r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Location", "/v1/crawls/"+testCrawlID)
		writeJSON(w, http.StatusAccepted, `{"crawl_id":"c_123","status":"running","url":"https://example.com/",
			"depth":1,"max_items":10,"max_pages":10,
			"coverage":{"pages_fetched":0,"pages_failed":0,"items_found":0},"created_at":"2026-10-08T10:00:00Z"}`)
	})

	got, err := client.Create(context.Background(), crawl.CreateParams{
		URL: "https://example.com/", Depth: 1, IncludePatterns: []string{"/product/"},
		OutputFormat: crawl.OutputFormatHTML, IdempotencyKey: "k1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/crawls" {
		t.Fatalf("request = %s %s, want POST /crawls", gotMethod, gotPath)
	}
	if gotKey != "test-key" || gotIdem != "k1" {
		t.Fatalf("headers: X-API-Key=%q Idempotency-Key=%q", gotKey, gotIdem)
	}
	want := map[string]bool{"url": true, "depth": true, "include_patterns": true, "output_format": true}
	for k := range gotBody {
		if !want[k] {
			t.Errorf("body carries unset or unsupported field %q: %v", k, gotBody)
		}
	}
	if len(gotBody) != len(want) || gotBody["output_format"] != "html" {
		t.Fatalf("body = %v", gotBody)
	}
	if got.CrawlID != testCrawlID || got.Status != crawl.StatusRunning || got.MaxItems != 10 {
		t.Fatalf("crawl = %+v", got)
	}
}

func TestGetParsesResultsAndForwardsPaging(t *testing.T) {
	var gotQuery string
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/crawls/"+testCrawlID {
			t.Errorf("path = %q", r.URL.Path)
		}
		gotQuery = r.URL.RawQuery
		writeJSON(w, http.StatusOK, `{"crawl_id":"c_123","status":"completed","stop_reason":"max_items",
			"url":"https://example.com/","depth":1,"max_items":2,"max_pages":5,"output_format":"html",
			"a_field_added_later":true,
			"coverage":{"pages_fetched":3,"pages_failed":0,"items_found":2},"duplicates_removed":1,
			"created_at":"2026-10-08T10:00:00Z","finished_at":"2026-10-08T10:01:00Z",
			"results":[{"url":"https://example.com/product/a","content_status":"fetched",
			"content_url":"/v1/crawls/c_123/contents/ct_a"},{"url":"https://example.com/product/b","content_status":"failed"}],
			"next_cursor":null}`)
	})

	got, err := client.Get(context.Background(), testCrawlID, crawl.GetOptions{Cursor: "cur", Limit: 50})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotQuery != "cursor=cur&limit=50" {
		t.Fatalf("query = %q", gotQuery)
	}
	if got.Status != crawl.StatusCompleted || got.StopReason != crawl.StopReasonMaxItems || got.NextCursor != nil {
		t.Fatalf("crawl = %+v", got)
	}
	if got.Coverage.ItemsFound != 2 || got.DuplicatesRemoved != 1 || got.FinishedAt == "" {
		t.Fatalf("crawl = %+v", got)
	}
	if len(got.Results) != 2 || got.Results[0].ContentID() != "ct_a" || got.Results[1].ContentID() != "" {
		t.Fatalf("results = %+v", got.Results)
	}
}

func TestResultsFollowsCursorUntilNull(t *testing.T) {
	var calls atomic.Int32
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Query().Get("limit") != "2" {
			t.Errorf("limit = %q, want 2", r.URL.Query().Get("limit"))
		}
		switch r.URL.Query().Get("cursor") {
		case "":
			writeJSON(w, http.StatusOK, `{"crawl_id":"c_123","status":"completed","results":[{"url":"u1"},{"url":"u2"}],
				"next_cursor":"p2"}`)
		case "p2":
			writeJSON(w, http.StatusOK, `{"crawl_id":"c_123","status":"completed","results":[{"url":"u3"}],"next_cursor":null}`)
		default:
			t.Errorf("unexpected cursor %q", r.URL.Query().Get("cursor"))
		}
	})

	urls := make([]string, 0, 3)
	for r, err := range client.Results(context.Background(), testCrawlID, crawl.ResultsOptions{Limit: 2}) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		urls = append(urls, r.URL)
	}
	if strings.Join(urls, ",") != "u1,u2,u3" || calls.Load() != 2 {
		t.Fatalf("urls = %v after %d calls", urls, calls.Load())
	}
}

func TestResultsStopsWhenARunningCrawlIsCaughtUp(t *testing.T) {
	var calls atomic.Int32
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Query().Get("cursor") == "" {
			writeJSON(w, http.StatusOK, `{"crawl_id":"c_123","status":"running","results":[{"url":"u1"}],"next_cursor":"p2"}`)
			return
		}
		writeJSON(w, http.StatusOK, `{"crawl_id":"c_123","status":"running","results":[],"next_cursor":"p2"}`)
	})

	n := 0
	for _, err := range client.Results(context.Background(), testCrawlID, crawl.ResultsOptions{}) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		n++
	}
	if n != 1 || calls.Load() != 2 {
		t.Fatalf("yielded %d results after %d calls, want 1 after 2", n, calls.Load())
	}
}

func TestListForwardsPaging(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/crawls" || r.URL.RawQuery != "cursor=n&limit=1" {
			t.Errorf("request = %s %s", r.URL.Path, r.URL.RawQuery)
		}
		writeJSON(w, http.StatusOK, `{"crawls":[{"crawl_id":"c_1","status":"completed"}]}`)
	})

	got, err := client.List(context.Background(), crawl.ListOptions{Cursor: "n", Limit: 1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Crawls) != 1 || got.Crawls[0].CrawlID != "c_1" || got.NextCursor != "" {
		t.Fatalf("list = %+v", got)
	}
}

func TestStop(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/crawls/c_123/stop" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if b, _ := io.ReadAll(r.Body); len(b) != 0 {
			t.Errorf("stop sent a body: %q", b)
		}
		writeJSON(w, http.StatusOK, `{"crawl_id":"c_123","status":"stopped","stop_reason":"user","finished_at":"2026-10-08T10:01:00Z"}`)
	})

	got, err := client.Stop(context.Background(), testCrawlID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Status != crawl.StatusStopped || got.StopReason != crawl.StopReasonUser {
		t.Fatalf("stop = %+v", got)
	}
}

func TestContentReturnsRawBody(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/crawls/c_123/contents/ct_a" {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, "<html>a</html>")
	})

	got, err := client.Content(context.Background(), testCrawlID, "ct_a")
	if err != nil || string(got) != "<html>a</html>" {
		t.Fatalf("content = %q, err = %v", got, err)
	}
}

func TestDownloadStreamsLines(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/crawls/c_123/download" {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.Header().Set("X-Crawl-Status", "running")
		_, _ = io.WriteString(w, `{"url":"u1","content_status":"fetched","content":"<p>1</p>"}`+"\n"+
			`{"url":"u2","content_status":"pending"}`+"\n"+
			`{"url":"u3","content_status":"fetched","content":{"title":"t"}}`+"\n")
	})

	dl, err := client.Download(context.Background(), testCrawlID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer dl.Close()
	if dl.Status != crawl.StatusRunning {
		t.Fatalf("status = %q", dl.Status)
	}
	lines := make([]crawl.DownloadLine, 0, 3)
	for line, err := range dl.Lines() {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		lines = append(lines, line)
	}
	if len(lines) != 3 || lines[0].HTML() != "<p>1</p>" || lines[1].HTML() != "" ||
		lines[1].ContentStatus != crawl.ContentStatusPending ||
		string(lines[2].Content) != `{"title":"t"}` || lines[2].HTML() != "" {
		t.Fatalf("lines = %+v", lines)
	}
}

func TestDownloadError(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeProblem(w, http.StatusNotFound, crawl.CodeCrawlNotFound)
	})

	_, err := client.Download(context.Background(), testCrawlID)
	var apiErr crawl.APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusNotFound || apiErr.Code() != crawl.CodeCrawlNotFound {
		t.Fatalf("err = %v", err)
	}
}

func TestErrorMapping(t *testing.T) {
	cases := []struct {
		status int
		code   string
	}{
		{http.StatusNotFound, crawl.CodeCrawlNotFound},
		{http.StatusUnprocessableEntity, crawl.CodeInvalidStartURL},
		{http.StatusTooManyRequests, crawl.CodeTooManyCrawls},
		{http.StatusForbidden, crawl.CodeNotEnabled},
	}
	for _, c := range cases {
		client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			if c.status == http.StatusTooManyRequests {
				w.Header().Set("Retry-After", "30")
			}
			writeProblem(w, c.status, c.code)
		})
		_, err := client.Create(context.Background(), crawl.CreateParams{URL: "https://example.com/", Depth: 1})
		var apiErr crawl.APIError
		if !errors.As(err, &apiErr) {
			t.Fatalf("%s: expected APIError, got %v (%T)", c.code, err, err)
		}
		if apiErr.StatusCode != c.status || apiErr.Code() != c.code {
			t.Errorf("%s: got status %d code %q", c.code, apiErr.StatusCode, apiErr.Code())
		}
		if c.status == http.StatusTooManyRequests && apiErr.RetryAfter != 30*time.Second {
			t.Errorf("RetryAfter = %s, want 30s", apiErr.RetryAfter)
		}
		if apiErr.NotEnabled() != (c.code == crawl.CodeNotEnabled) {
			t.Errorf("%s: NotEnabled() = %v", c.code, apiErr.NotEnabled())
		}
	}
}

func TestNotEnabledErrorMessage(t *testing.T) {
	err := crawl.APIError{StatusCode: http.StatusForbidden, Detail: &crawl.Problem{Code: "REQS008", Detail: "Crawl is not enabled for this account."}}
	want := "zenrows crawl api request failed with status 403: Crawl is not enabled for this account (REQS008)"
	if err.Error() != want {
		t.Fatalf("got %q, want %q", err.Error(), want)
	}
}

func TestAPIErrorWithoutProblemBody(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, "<html>upstream error</html>")
	}, crawl.WithRetries(0))

	_, err := client.Get(context.Background(), testCrawlID, crawl.GetOptions{})
	var apiErr crawl.APIError
	if !errors.As(err, &apiErr) || apiErr.Code() != "" || apiErr.Detail != nil {
		t.Fatalf("err = %v", err)
	}
	if err.Error() != "zenrows crawl api request failed with status 502" {
		t.Fatalf("message = %q", err.Error())
	}
}

func TestProblemWithoutCodeHasEmptyCode(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"title":"t","status":500}`)
	})

	_, err := client.Get(context.Background(), testCrawlID, crawl.GetOptions{})
	var apiErr crawl.APIError
	if !errors.As(err, &apiErr) || apiErr.Detail == nil || apiErr.Code() != "" {
		t.Fatalf("err = %v", err)
	}
}

func TestGetRetriesTransientFailures(t *testing.T) {
	var calls atomic.Int32
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		writeJSON(w, http.StatusOK, `{"crawl_id":"c_123","status":"running","results":[],"next_cursor":"x"}`)
	})

	if _, err := client.Get(context.Background(), testCrawlID, crawl.GetOptions{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("calls = %d, want 2", calls.Load())
	}
}

func TestCreateNeverRetriesTooManyCrawls(t *testing.T) {
	for _, key := range []string{"", "k1"} {
		var calls atomic.Int32
		client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			w.Header().Set("Retry-After", "0")
			writeProblem(w, http.StatusTooManyRequests, crawl.CodeTooManyCrawls)
		})

		params := crawl.CreateParams{URL: "https://example.com/", Depth: 1, IdempotencyKey: key}
		if _, err := client.Create(context.Background(), params); err == nil {
			t.Fatalf("key %q: expected an error", key)
		}
		if calls.Load() != 1 {
			t.Fatalf("key %q: calls = %d, want 1", key, calls.Load())
		}
	}
}

func TestCreateWithIdempotencyKeyRetriesTransientFailures(t *testing.T) {
	var calls atomic.Int32
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		writeJSON(w, http.StatusAccepted, `{"crawl_id":"c_123","status":"running"}`)
	})

	params := crawl.CreateParams{URL: "https://example.com/", Depth: 1, IdempotencyKey: "k1"}
	if _, err := client.Create(context.Background(), params); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("calls = %d, want 2", calls.Load())
	}
}

func fastWait(t *testing.T) {
	t.Helper()
	t.Cleanup(crawl.SetWaitPollInterval(time.Millisecond))
}

func TestWaitPollsUntilTerminal(t *testing.T) {
	fastWait(t)
	var calls atomic.Int32
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("limit") != "1" {
			t.Errorf("wait polled with limit %q, want 1", r.URL.Query().Get("limit"))
		}
		if calls.Add(1) < 3 {
			writeJSON(w, http.StatusOK, `{"crawl_id":"c_123","status":"running","results":[],"next_cursor":"x"}`)
			return
		}
		writeJSON(w, http.StatusOK, `{"crawl_id":"c_123","status":"failed","error":{"code":"seed_unreachable","detail":"d"},
			"results":[],"next_cursor":null}`)
	})

	got, err := client.Wait(context.Background(), testCrawlID, crawl.WaitOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Status != crawl.StatusFailed || got.Error == nil || got.Error.Code != crawl.RunErrorSeedUnreachable || calls.Load() != 3 {
		t.Fatalf("crawl = %+v after %d calls", got, calls.Load())
	}
}

func TestWaitReturnsTheRunningCrawlOnTimeout(t *testing.T) {
	fastWait(t)
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, `{"crawl_id":"c_123","status":"running","results":[],"next_cursor":"x"}`)
	})

	got, err := client.Wait(context.Background(), testCrawlID, crawl.WaitOptions{Timeout: 30 * time.Millisecond})
	if err != nil || got.CrawlID != testCrawlID || got.Status != crawl.StatusRunning {
		t.Fatalf("crawl = %+v, err = %v; want the running crawl and no error", got, err)
	}
}

func TestWaitBoundsAHungPollByItsTimeout(t *testing.T) {
	fastWait(t)
	var calls atomic.Int32
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) > 1 {
			<-r.Context().Done()
			return
		}
		writeJSON(w, http.StatusOK, `{"crawl_id":"c_123","status":"running","results":[],"next_cursor":"x"}`)
	})

	start := time.Now()
	got, err := client.Wait(context.Background(), testCrawlID, crawl.WaitOptions{Timeout: 50 * time.Millisecond})
	if err != nil || got.Status != crawl.StatusRunning {
		t.Fatalf("crawl = %+v, err = %v; want the running crawl and no error", got, err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("Wait returned after %s, want about 50ms", elapsed)
	}
}

func TestWaitReturnsCallerCancellation(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := client.Wait(ctx, testCrawlID, crawl.WaitOptions{Timeout: time.Minute})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v (%T), want the caller's context.DeadlineExceeded", err, err)
	}
}

func TestRequestTimeout(t *testing.T) {
	var calls atomic.Int32
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		<-r.Context().Done()
	}, crawl.WithTimeout(20*time.Millisecond), crawl.WithRetries(1))

	start := time.Now()
	_, err := client.Get(context.Background(), testCrawlID, crawl.GetOptions{})
	if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "no response within 20ms") {
		t.Fatalf("err = %v, want a request timeout", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("calls = %d, want 2: a timed out GET is retried", calls.Load())
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("Get returned after %s", elapsed)
	}
}

func TestDownloadTimeoutCoversHeadersOnly(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Crawl-Status", "completed")
		_, _ = io.WriteString(w, `{"url":"u1"}`+"\n")
		w.(http.Flusher).Flush()
		time.Sleep(60 * time.Millisecond)
		_, _ = io.WriteString(w, `{"url":"u2"}`+"\n")
	}, crawl.WithTimeout(20*time.Millisecond))

	dl, err := client.Download(context.Background(), testCrawlID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer dl.Close()
	n := 0
	for _, err := range dl.Lines() {
		if err != nil {
			t.Fatalf("read past the request timeout: %v", err)
		}
		n++
	}
	if n != 2 {
		t.Fatalf("lines = %d, want 2", n)
	}
}

func TestResponseEnumsIsKnown(t *testing.T) {
	cases := []struct {
		name  string
		known interface{ IsKnown() bool }
		other interface{ IsKnown() bool }
	}{
		{"Status", crawl.StatusStopped, crawl.Status("x_added_later")},
		{"StopReason", crawl.StopReasonUser, crawl.StopReason("x_added_later")},
		{"RunErrorCode", crawl.RunErrorNoItemsFound, crawl.RunErrorCode("x_added_later")},
		{"ContentStatus", crawl.ContentStatusFetched, crawl.ContentStatus("x_added_later")},
	}
	for _, c := range cases {
		if !c.known.IsKnown() {
			t.Errorf("%s: defined constant reported unknown", c.name)
		}
		if c.other.IsKnown() {
			t.Errorf("%s: undefined value reported known", c.name)
		}
	}
	if crawl.StatusRunning.IsTerminal() || !crawl.Status("x_added_later").IsTerminal() || !crawl.StatusCompleted.IsTerminal() {
		t.Fatal("IsTerminal: only running is non-terminal")
	}
}
