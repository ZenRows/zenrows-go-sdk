package crawl

import "encoding/json"

// Status is where a crawl stands. Every value but StatusRunning is terminal.
// The server may add values not listed here, and they decode as-is; callers must handle
// unknown values (e.g. a default branch, or IsKnown) rather than assume the constants are exhaustive.
type Status string

const (
	// StatusRunning is the only non-terminal status: the crawl is still walking pages.
	StatusRunning Status = "running"
	// StatusCompleted means the crawl did what the request asked: nothing was left to open, or
	// it reached MaxItems or MaxPages (StopReason names the limit).
	StatusCompleted Status = "completed"
	// StatusStopped means the caller stopped it with Client.Stop (StopReason is StopReasonUser).
	StatusStopped Status = "stopped"
	// StatusFailed means the run failed; Crawl.Error says why. The URLs kept so far stay readable.
	StatusFailed Status = "failed"
)

// IsKnown reports whether s is one of the Status constants this SDK version defines.
func (s Status) IsKnown() bool {
	switch s {
	case StatusRunning, StatusCompleted, StatusStopped, StatusFailed:
		return true
	default:
		return false
	}
}

// IsTerminal reports whether a crawl in status s has ended: any status but StatusRunning,
// including values this SDK version does not define.
func (s Status) IsTerminal() bool {
	return s != "" && s != StatusRunning
}

// StopReason is what ended a crawl before nothing was left to open. Absent when nothing was
// left, and on a failed crawl.
// The server may add values not listed here, and they decode as-is; callers must handle
// unknown values (e.g. a default branch, or IsKnown) rather than assume the constants are exhaustive.
type StopReason string

const (
	StopReasonMaxItems StopReason = "max_items"
	StopReasonMaxPages StopReason = "max_pages"
	StopReasonUser     StopReason = "user"
)

// IsKnown reports whether r is one of the StopReason constants this SDK version defines.
func (r StopReason) IsKnown() bool {
	switch r {
	case StopReasonMaxItems, StopReasonMaxPages, StopReasonUser:
		return true
	default:
		return false
	}
}

// RunErrorCode is why a crawl failed, carried by Crawl.Error on a StatusFailed crawl.
// The server may add values not listed here, and they decode as-is; callers must handle
// unknown values (e.g. a default branch, or IsKnown) rather than assume the constants are exhaustive.
type RunErrorCode string

const (
	RunErrorInsufficientCredits RunErrorCode = "insufficient_credits"
	RunErrorSeedUnreachable     RunErrorCode = "seed_unreachable"
	RunErrorDomainNotAllowed    RunErrorCode = "domain_not_allowed"
	RunErrorNoItemsFound        RunErrorCode = "no_items_found"
	RunErrorInternalError       RunErrorCode = "internal_error"
)

// IsKnown reports whether c is one of the RunErrorCode constants this SDK version defines.
func (c RunErrorCode) IsKnown() bool {
	switch c {
	case RunErrorInsufficientCredits, RunErrorSeedUnreachable, RunErrorDomainNotAllowed, RunErrorNoItemsFound,
		RunErrorInternalError:
		return true
	default:
		return false
	}
}

// ContentStatus is where a kept URL's page stands. Present only when the crawl has an
// OutputFormat.
// The server may add values not listed here, and they decode as-is; callers must handle
// unknown values (e.g. a default branch, or IsKnown) rather than assume the constants are exhaustive.
type ContentStatus string

const (
	ContentStatusPending ContentStatus = "pending"
	ContentStatusFetched ContentStatus = "fetched"
	ContentStatusFailed  ContentStatus = "failed"
)

// IsKnown reports whether c is one of the ContentStatus constants this SDK version defines.
func (c ContentStatus) IsKnown() bool {
	switch c {
	case ContentStatusPending, ContentStatusFetched, ContentStatusFailed:
		return true
	default:
		return false
	}
}

// OutputFormat asks the crawl to fetch the page of every URL it keeps, in this format. Leave it
// empty for URLs only.
type OutputFormat string

// OutputFormatHTML returns each kept URL's page HTML, as fetched.
const OutputFormatHTML OutputFormat = "html"

// CreateParams is the body for Client.Create. Only the fields you set are sent; the API applies
// its defaults to the rest.
type CreateParams struct {
	// URL is the page the crawl starts from: an absolute http or https URL. Required.
	URL string `json:"url"`
	// Depth is how many link hops the crawl follows from URL, 1 to 100,000. Required.
	Depth int `json:"depth"`
	// MaxItems stops the crawl once it has kept this many URLs (API default 10).
	MaxItems int `json:"max_items,omitempty"`
	// MaxPages stops the crawl once it has fetched this many pages (API default 10). It bounds
	// what the crawl costs: each fetch is one request on your account.
	MaxPages int `json:"max_pages,omitempty"`
	// IncludePatterns keeps a URL only if it contains at least one of these substrings.
	IncludePatterns []string `json:"include_patterns,omitempty"`
	// ExcludePatterns drops a URL containing any of these substrings, even if it is included.
	ExcludePatterns []string `json:"exclude_patterns,omitempty"`
	// OutputFormat fetches each kept URL's page too. Empty means URLs only.
	OutputFormat OutputFormat `json:"output_format,omitempty"`
	// IdempotencyKey (up to 255 characters) makes a retry safe: the same key with the same
	// body answers with the crawl the first request created. It also lets the client retry the
	// create on transient failures.
	IdempotencyKey string `json:"-"`
}

// Coverage is how far a crawl has got.
type Coverage struct {
	PagesFetched int `json:"pages_fetched"`
	PagesFailed  int `json:"pages_failed"`
	ItemsFound   int `json:"items_found"`
}

// RunError is why a crawl failed. Present only when Status is StatusFailed. It is part of the
// crawl, not an error response.
type RunError struct {
	Code   RunErrorCode `json:"code"`
	Detail string       `json:"detail"`
}

// Crawl is one crawl: the parameters it runs with, its status and its coverage.
type Crawl struct {
	CrawlID           string       `json:"crawl_id"`
	Status            Status       `json:"status"`
	StopReason        StopReason   `json:"stop_reason,omitempty"`
	Error             *RunError    `json:"error,omitempty"`
	URL               string       `json:"url"`
	Depth             int          `json:"depth"`
	MaxItems          int          `json:"max_items"`
	MaxPages          int          `json:"max_pages"`
	IncludePatterns   []string     `json:"include_patterns,omitempty"`
	ExcludePatterns   []string     `json:"exclude_patterns,omitempty"`
	OutputFormat      OutputFormat `json:"output_format,omitempty"`
	Coverage          Coverage     `json:"coverage"`
	DuplicatesRemoved int          `json:"duplicates_removed,omitempty"`
	CreatedAt         string       `json:"created_at"`
	FinishedAt        string       `json:"finished_at,omitempty"`
}

// Result is one URL a crawl kept.
type Result struct {
	URL string `json:"url"`
	// ContentStatus is present only when the crawl has an OutputFormat.
	ContentStatus ContentStatus `json:"content_status,omitempty"`
	// ContentURL is the page's API path (e.g. "/v1/crawls/c_x/contents/ct_y"), present only
	// when ContentStatus is ContentStatusFetched. Read it with Client.Content(ctx, crawlID,
	// result.ContentURL).
	ContentURL string `json:"content_url,omitempty"`
}

// GetOptions pages a crawl's results in Client.Get.
type GetOptions struct {
	// Cursor is the NextCursor from the previous read. Leave empty to start from the first result.
	Cursor string
	// Limit is how many results to return, 1 to 10,000 (API default 1,000).
	Limit int
}

// ResultsOptions configures Client.Results.
type ResultsOptions struct {
	// Limit is how many results each request reads, 1 to 10,000 (API default 1,000).
	Limit int
}

// CrawlWithResults is the response for Client.Get: the crawl and one page of its results.
type CrawlWithResults struct {
	Crawl
	Results []Result `json:"results"`
	// NextCursor is never nil while the crawl runs: polling with it returns only the URLs kept
	// since. It is nil once the crawl has ended and this page holds its last URLs.
	NextCursor *string `json:"next_cursor"`
}

// ListOptions pages Client.List.
type ListOptions struct {
	// Cursor is the NextCursor from the previous page. Leave empty for the newest crawls.
	Cursor string
	// Limit is how many crawls to return, 1 to 100 (API default 20).
	Limit int
}

// ListResponse is the response for Client.List: one page of the account's crawls, newest first,
// without their results.
type ListResponse struct {
	Crawls []Crawl `json:"crawls"`
	// NextCursor is empty on the last page.
	NextCursor string `json:"next_cursor,omitempty"`
}

// StopResponse is the response for Client.Stop: where the crawl stands, without counts. Read
// the coverage and the results with Client.Get.
type StopResponse struct {
	CrawlID    string     `json:"crawl_id"`
	Status     Status     `json:"status"`
	StopReason StopReason `json:"stop_reason,omitempty"`
	Error      *RunError  `json:"error,omitempty"`
	FinishedAt string     `json:"finished_at,omitempty"`
}

// DownloadLine is one line of Client.Download.
type DownloadLine struct {
	URL string `json:"url"`
	// ContentStatus is present only when the crawl has an OutputFormat.
	ContentStatus ContentStatus `json:"content_status,omitempty"`
	// Content is the page as raw JSON, present when ContentStatus is ContentStatusFetched: a
	// JSON string holding the HTML for OutputFormatHTML. Use HTML to decode it.
	Content json.RawMessage `json:"content,omitempty"`
}

// HTML returns the page's HTML, or "" when the line carries no content or its content is not
// a string.
func (l DownloadLine) HTML() string {
	var s string
	if len(l.Content) == 0 || json.Unmarshal(l.Content, &s) != nil {
		return ""
	}
	return s
}

// Problem is an RFC 9457 problem+json body, as returned by every Crawl API error response.
// Crawl errors and account errors (authentication, credits, access) share this shape.
type Problem struct {
	Type     string `json:"type,omitempty"`
	Title    string `json:"title,omitempty"`
	Status   int    `json:"status,omitempty"`
	Code     string `json:"code,omitempty"`
	Detail   string `json:"detail,omitempty"`
	Instance string `json:"instance,omitempty"`
	// Extras holds any non-standard top-level members not modeled above.
	Extras map[string]any `json:"-"`
}
