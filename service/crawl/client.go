// Package crawl is a client for the Zenrows Crawl API. Give it one start URL, and it walks the
// links behind it and returns the URLs it keeps — optionally with each page's HTML. A crawl
// stays on the start URL's registrable domain; subdomains count.
//
// Crawl is in Beta: this package is v0, and its API can change before v1.
//
// A crawl is a long-running job: Create starts it and returns at once, while it runs. Wait
// blocks until it ends; Get reads its status, coverage and one page of results; Results
// reads every result. Content and Download read the pages of a crawl created with
// OutputFormatHTML. Stop ends a running crawl early. List lists the account's crawls.
//
// The main entry point is Client (via NewClient).
package crawl

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-resty/resty/v2"
)

const apiKeyHeader = "X-API-Key" //nolint:gosec // header name, not a credential value

// A download line holds a URL plus its page's HTML: read with a 64 KiB buffer that may grow
// to maxDownloadLineBytes for one line.
const (
	downloadBufferBytes  = 64 * 1024
	maxDownloadLineBytes = 64 * 1024 * 1024
)

// Client is the Zenrows Crawl API client.
type Client struct {
	cfg  options
	http *resty.Client
}

// NewClient creates and returns a new Zenrows Crawl API client.
func NewClient(opts ...Option) *Client {
	client := &Client{cfg: defaultOptions()}
	for _, opt := range opts {
		opt.apply(&client.cfg)
	}

	client.http = resty.New().
		SetBaseURL(client.cfg.baseURL).
		SetHeader(apiKeyHeader, client.cfg.apiKey)

	return client
}

func (c *Client) isConfigured() bool {
	return c.cfg.baseURL != "" && c.cfg.apiKey != ""
}

func (c *Client) do(ctx context.Context, req *resty.Request, method, path string) error {
	res, release, err := executeWithRetry(ctx, req, method, path, c.cfg.retries, c.cfg.timeout)
	defer release()
	if err != nil {
		return err
	}
	if res.IsError() {
		return newAPIError(res.StatusCode(), res.Header(), res.Body())
	}
	return nil
}

func crawlPath(crawlID string) string {
	return "/crawls/" + url.PathEscape(crawlID)
}

// Create starts a crawl and returns it at once, with StatusRunning. Poll it with Wait or Get.
//
// When the account has reached its limit of active jobs (3 by default), shared with its Batch
// jobs, Create returns an APIError with StatusCode 429 and Code() "too_many_crawls" at once;
// nothing is created, and APIError.RetryAfter says when to retry. The create is retried on
// other transient failures only when params.IdempotencyKey is set.
func (c *Client) Create(ctx context.Context, params CreateParams) (*Crawl, error) {
	if !c.isConfigured() {
		return nil, NotConfiguredError{}
	}

	var result Crawl
	req := c.http.R().SetResult(&result).SetBody(params)
	if params.IdempotencyKey != "" {
		req.SetHeader("Idempotency-Key", params.IdempotencyKey)
	}
	if err := c.do(ctx, req, http.MethodPost, "/crawls"); err != nil {
		return nil, err
	}
	return &result, nil
}

// Get reads a crawl's status and coverage, and one page of the URLs it has kept. For all of
// them prefer Results.
func (c *Client) Get(ctx context.Context, crawlID string, opts GetOptions) (*CrawlWithResults, error) {
	if !c.isConfigured() {
		return nil, NotConfiguredError{}
	}

	var result CrawlWithResults
	req := c.http.R().SetResult(&result)
	if opts.Cursor != "" {
		req.SetQueryParam("cursor", opts.Cursor)
	}
	if opts.Limit > 0 {
		req.SetQueryParam("limit", fmt.Sprintf("%d", opts.Limit))
	}
	if err := c.do(ctx, req, http.MethodGet, crawlPath(crawlID)); err != nil {
		return nil, err
	}
	return &result, nil
}

// Results reads every URL a crawl has kept, following the next cursor and yielding
// (Result, error) pairs. On a crawl that is still running it yields what was kept so far and
// stops at the first empty page rather than polling: call it after Wait to read them all. A
// non-nil error from the sequence should stop the range loop.
func (c *Client) Results(ctx context.Context, crawlID string, opts ResultsOptions) iter.Seq2[Result, error] {
	return func(yield func(Result, error) bool) {
		var cursor string
		for {
			page, err := c.Get(ctx, crawlID, GetOptions{Cursor: cursor, Limit: opts.Limit})
			if err != nil {
				yield(Result{}, err)
				return
			}
			for _, r := range page.Results {
				if !yield(r, nil) {
					return
				}
			}
			if page.NextCursor == nil || len(page.Results) == 0 {
				return
			}
			cursor = *page.NextCursor
		}
	}
}

// List reads one page of the account's crawls, newest first, without their results. Pass the
// page's NextCursor in ListOptions.Cursor to read the next one.
func (c *Client) List(ctx context.Context, opts ListOptions) (*ListResponse, error) {
	if !c.isConfigured() {
		return nil, NotConfiguredError{}
	}

	var result ListResponse
	req := c.http.R().SetResult(&result)
	if opts.Cursor != "" {
		req.SetQueryParam("cursor", opts.Cursor)
	}
	if opts.Limit > 0 {
		req.SetQueryParam("limit", fmt.Sprintf("%d", opts.Limit))
	}
	if err := c.do(ctx, req, http.MethodGet, "/crawls"); err != nil {
		return nil, err
	}
	return &result, nil
}

// Stop stops a running crawl. The URLs it kept stay readable; a stopped crawl cannot resume.
// Stopping a crawl that has already ended is not an error: it returns the crawl as it ended.
// Pages already in flight finish, so read the final coverage and results with Get.
func (c *Client) Stop(ctx context.Context, crawlID string) (*StopResponse, error) {
	if !c.isConfigured() {
		return nil, NotConfiguredError{}
	}

	var result StopResponse
	req := c.http.R().SetResult(&result)
	if err := c.do(ctx, req, http.MethodPost, crawlPath(crawlID)+"/stop"); err != nil {
		return nil, err
	}
	return &result, nil
}

// Content fetches one kept URL's page (HTML for OutputFormatHTML). contentID is a content id or
// a Result.ContentURL, present once the result's ContentStatus is ContentStatusFetched. The
// returned bytes are the raw page — unlike other methods here, this is not decoded as JSON.
func (c *Client) Content(ctx context.Context, crawlID, contentID string) ([]byte, error) {
	if !c.isConfigured() {
		return nil, NotConfiguredError{}
	}

	contentID = contentID[strings.LastIndex(contentID, "/")+1:]
	path := crawlPath(crawlID) + "/contents/" + url.PathEscape(contentID)
	res, release, err := executeWithRetry(ctx, c.http.R(), http.MethodGet, path, c.cfg.retries, c.cfg.timeout)
	defer release()
	if err != nil {
		return nil, err
	}
	if res.IsError() {
		return nil, newAPIError(res.StatusCode(), res.Header(), res.Body())
	}
	return res.Body(), nil
}

// Download is every result of a crawl as one NDJSON stream, one DownloadLine per line, in
// result order. Read it with Lines, or as raw bytes; Close it when done.
type Download struct {
	io.ReadCloser
	// Status is the crawl's status when the file was read. StatusRunning means it holds what
	// the crawl has kept so far, and a later download may hold more.
	Status Status

	release context.CancelFunc
}

// Close closes the stream and releases its request.
func (d *Download) Close() error {
	defer d.release()
	return d.ReadCloser.Close()
}

// Lines decodes the stream one line at a time, yielding (DownloadLine, error) pairs. A
// non-nil error from the sequence should stop the range loop. It does not close the stream.
func (d *Download) Lines() iter.Seq2[DownloadLine, error] {
	return func(yield func(DownloadLine, error) bool) {
		scanner := bufio.NewScanner(d.ReadCloser)
		scanner.Buffer(make([]byte, 0, downloadBufferBytes), maxDownloadLineBytes)
		for scanner.Scan() {
			raw := scanner.Bytes()
			if len(raw) == 0 {
				continue
			}
			var line DownloadLine
			if err := json.Unmarshal(raw, &line); err != nil {
				yield(DownloadLine{}, fmt.Errorf("download: decode line: %w", err))
				return
			}
			if !yield(line, nil) {
				return
			}
		}
		if err := scanner.Err(); err != nil {
			yield(DownloadLine{}, fmt.Errorf("download: read: %w", err))
		}
	}
}

// Download streams every result of a crawl, with each page's content when the crawl has an
// OutputFormat. The caller must Close the returned Download.
func (c *Client) Download(ctx context.Context, crawlID string) (*Download, error) {
	if !c.isConfigured() {
		return nil, NotConfiguredError{}
	}

	req := c.http.R().SetDoNotParseResponse(true)
	res, release, err := executeWithRetry(ctx, req, http.MethodGet, crawlPath(crawlID)+"/download",
		c.cfg.retries, c.cfg.timeout)
	if err != nil {
		closeRawBody(res)
		release()
		return nil, err
	}
	body := res.RawBody()
	if body == nil {
		release()
		return nil, errors.New("download: empty response")
	}
	if res.IsError() {
		defer release()
		defer body.Close()
		raw, readErr := io.ReadAll(body)
		if readErr != nil {
			return nil, readErr
		}
		return nil, newAPIError(res.StatusCode(), res.Header(), raw)
	}
	return &Download{ReadCloser: body, Status: Status(res.Header().Get("X-Crawl-Status")), release: release}, nil
}
