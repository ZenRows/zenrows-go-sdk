//go:build integration

package crawl_test

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/zenrows/zenrows-go-sdk/service/crawl"
)

// The end-to-end test runs a real crawl against a live API. It is compiled only with
// `-tags integration` and skips unless ZENROWS_API_KEY (a key with Crawl access),
// ZENROWS_CRAWL_BASE_URL (e.g. https://api.zenrows.com/v1) and ZENROWS_E2E_CRAWL_URL (the start
// URL) are set. ZENROWS_E2E_CRAWL_INCLUDE optionally sets an include pattern that every result
// must match. See CONTRIBUTING.md.

const (
	e2eCreateBudget = 5 * time.Minute
	e2eWaitTimeout  = 10 * time.Minute
)

func e2eSetup(t *testing.T) (client *crawl.Client, startURL, include string) {
	t.Helper()
	baseURL, startURL := os.Getenv("ZENROWS_CRAWL_BASE_URL"), os.Getenv("ZENROWS_E2E_CRAWL_URL")
	if os.Getenv("ZENROWS_API_KEY") == "" || baseURL == "" || startURL == "" {
		t.Skip("set ZENROWS_API_KEY, ZENROWS_CRAWL_BASE_URL and ZENROWS_E2E_CRAWL_URL to run the Crawl end-to-end test")
	}
	return crawl.NewClient(crawl.WithBaseURL(baseURL)), startURL, os.Getenv("ZENROWS_E2E_CRAWL_INCLUDE")
}

// createWithSlot creates the crawl, waiting out 429 too_many_crawls: other runs on the same
// account share its active-crawl slots.
func createWithSlot(ctx context.Context, t *testing.T, client *crawl.Client, params crawl.CreateParams) *crawl.Crawl {
	t.Helper()
	deadline := time.Now().Add(e2eCreateBudget)
	for {
		created, err := client.Create(ctx, params)
		var apiErr crawl.APIError
		if err == nil || !errors.As(err, &apiErr) || apiErr.Code() != crawl.CodeTooManyCrawls || time.Now().After(deadline) {
			if err != nil {
				t.Fatalf("create: %v", err)
			}
			return created
		}
		wait := apiErr.RetryAfter
		if wait <= 0 {
			wait = 30 * time.Second
		}
		t.Logf("create: %s, retrying in %s", apiErr.Code(), wait)
		select {
		case <-ctx.Done():
			t.Fatalf("create: %v", ctx.Err())
		case <-time.After(wait):
		}
	}
}

func TestE2ECrawl(t *testing.T) {
	client, startURL, include := e2eSetup(t)
	ctx, cancel := context.WithTimeout(context.Background(), e2eCreateBudget+e2eWaitTimeout+time.Minute)
	defer cancel()

	params := crawl.CreateParams{URL: startURL, Depth: 1, MaxItems: 3, MaxPages: 5, OutputFormat: crawl.OutputFormatHTML}
	if include != "" {
		params.IncludePatterns = []string{include}
	}
	created := createWithSlot(ctx, t, client, params)
	t.Logf("created %s (status %s)", created.CrawlID, created.Status)

	done, err := client.Wait(ctx, created.CrawlID, crawl.WaitOptions{Timeout: e2eWaitTimeout})
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	t.Logf("ended %s: status=%s stop_reason=%s pages_fetched=%d items_found=%d",
		done.CrawlID, done.Status, done.StopReason, done.Coverage.PagesFetched, done.Coverage.ItemsFound)
	if done.Status != crawl.StatusCompleted {
		t.Fatalf("status = %s (error %+v), want completed", done.Status, done.Error)
	}

	results := make([]crawl.Result, 0, done.Coverage.ItemsFound)
	for r, err := range client.IterResults(ctx, created.CrawlID, crawl.GetOptions{}) {
		if err != nil {
			t.Fatalf("results: %v", err)
		}
		if include != "" && !strings.Contains(r.URL, include) {
			t.Errorf("result %q does not match include pattern %q", r.URL, include)
		}
		results = append(results, r)
	}
	if len(results) == 0 {
		t.Fatal("no results")
	}
	t.Logf("results: %d", len(results))

	checkContent(ctx, t, client, created.CrawlID, results)
	checkDownload(ctx, t, client, created.CrawlID, len(results))
	checkListed(ctx, t, client, created.CrawlID)

	stopped, err := client.Stop(ctx, created.CrawlID)
	if err != nil {
		t.Fatalf("stop on an ended crawl: %v", err)
	}
	if stopped.Status != done.Status {
		t.Fatalf("stop status = %s, want %s", stopped.Status, done.Status)
	}
	t.Logf("stop on ended crawl: status=%s", stopped.Status)

	_, err = client.Get(ctx, "c_does_not_exist", crawl.GetOptions{})
	var apiErr crawl.APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusNotFound || apiErr.Code() != crawl.CodeCrawlNotFound {
		t.Fatalf("get unknown crawl: err = %v, want 404 %s", err, crawl.CodeCrawlNotFound)
	}
	t.Logf("get unknown crawl: %d %s", apiErr.StatusCode, apiErr.Code())
}

func checkContent(ctx context.Context, t *testing.T, client *crawl.Client, crawlID string, results []crawl.Result) {
	t.Helper()
	for _, r := range results {
		if r.ContentStatus != crawl.ContentStatusFetched {
			continue
		}
		page, err := client.GetContent(ctx, crawlID, r.ContentID())
		if err != nil {
			t.Fatalf("content of %s: %v", r.URL, err)
		}
		if !strings.Contains(strings.ToLower(string(page)), "<html") {
			t.Fatalf("content of %s is not HTML (%d bytes)", r.URL, len(page))
		}
		t.Logf("content: %d bytes of HTML", len(page))
		return
	}
	t.Fatal("no result was fetched")
}

func checkDownload(ctx context.Context, t *testing.T, client *crawl.Client, crawlID string, want int) {
	t.Helper()
	dl, err := client.Download(ctx, crawlID)
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	defer dl.Close()
	lines := 0
	for _, err := range dl.Lines() {
		if err != nil {
			t.Fatalf("download: %v", err)
		}
		lines++
	}
	if lines != want {
		t.Fatalf("download has %d lines, want %d", lines, want)
	}
	t.Logf("download: %d lines (X-Crawl-Status %s)", lines, dl.Status)
}

func checkListed(ctx context.Context, t *testing.T, client *crawl.Client, crawlID string) {
	t.Helper()
	for c, err := range client.IterCrawls(ctx, crawl.ListOptions{Limit: 100}) {
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if c.CrawlID == crawlID {
			t.Logf("list: found %s", crawlID)
			return
		}
	}
	t.Fatalf("list: %s not found", crawlID)
}
