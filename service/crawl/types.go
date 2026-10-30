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
	// StatusStopped means the caller stopped it with Client.Stop (StopReason is StopReasonUser),
	// or it ended because the account ran out of credits or the API key hit a credit cap
	// (StopReason is empty, and Crawl.Error carries RunErrorInsufficientCredits or
	// RunErrorAPIKeyCapReached). The URLs kept before the stop stay readable.
	StatusStopped Status = "stopped"
	// StatusFailed means the crawl ended on any other error; Crawl.Error says why. The URLs kept
	// so far stay readable.
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

// StopReason is what ended a crawl before nothing was left to open. Absent when nothing was
// left, on a failed crawl, and on a crawl that stopped because credits ran out or the API key
// hit a credit cap (that one carries Crawl.Error instead).
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

// RunErrorCode is why a crawl failed, carried by Crawl.Error on a StatusFailed crawl, or why it
// stopped on credits: RunErrorInsufficientCredits and RunErrorAPIKeyCapReached come with
// StatusStopped.
// The server may add values not listed here, and they decode as-is; callers must handle
// unknown values (e.g. a default branch, or IsKnown) rather than assume the constants are exhaustive.
type RunErrorCode string

const (
	RunErrorInsufficientCredits RunErrorCode = "insufficient_credits"
	//nolint:gosec // G101: an error code the API answers with, not a credential.
	RunErrorAPIKeyCapReached RunErrorCode = "api_key_cap_reached"
	// RunErrorSeedUnreachable means the start page could not be reached, was blocked, or came
	// back empty, with no visible text and no link.
	RunErrorSeedUnreachable  RunErrorCode = "seed_unreachable"
	RunErrorDomainNotAllowed RunErrorCode = "domain_not_allowed"
	// RunErrorNoItemsFound means the crawl read the start page but found no data it could use, so
	// it had nothing to follow; start from a listing or product page. A crawl that returns nothing
	// for any other reason is not a failure: it ends StatusCompleted with no results.
	RunErrorNoItemsFound  RunErrorCode = "no_items_found"
	RunErrorInternalError RunErrorCode = "internal_error"
)

// IsKnown reports whether c is one of the RunErrorCode constants this SDK version defines.
func (c RunErrorCode) IsKnown() bool {
	switch c {
	case RunErrorInsufficientCredits, RunErrorAPIKeyCapReached, RunErrorSeedUnreachable, RunErrorDomainNotAllowed,
		RunErrorNoItemsFound, RunErrorInternalError:
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

const (
	// OutputFormatHTML returns each kept URL's page HTML, as fetched.
	OutputFormatHTML OutputFormat = "html"
	// OutputFormatMarkdown returns each kept URL's page converted to Markdown.
	OutputFormatMarkdown OutputFormat = "markdown"
)

// DiscoveryStrategy is how a crawl finds the URLs it follows on each page it walks.
// The server may add values not listed here, and they decode as-is; callers must handle
// unknown values (e.g. a default branch, or IsKnown) rather than assume the constants are exhaustive.
type DiscoveryStrategy string

// DiscoveryLinks follows the links in each page's HTML. It is what a crawl runs with when
// CreateParams.Discovery is empty.
const DiscoveryLinks DiscoveryStrategy = "links"

// IsKnown reports whether d is one of the DiscoveryStrategy constants this SDK version defines.
func (d DiscoveryStrategy) IsKnown() bool {
	return d == DiscoveryLinks
}

// WebhookEventType names an event a crawl's webhook is sent.
// The server may add values not listed here, and they decode as-is; callers must handle
// unknown values (e.g. a default branch, or IsKnown) rather than assume the constants are exhaustive.
type WebhookEventType string

const (
	// WebhookEventCrawlStarted is sent once the crawl is created and runs.
	WebhookEventCrawlStarted WebhookEventType = "crawl.started"
	// WebhookEventCrawlCompleted is sent when the crawl ends StatusCompleted.
	WebhookEventCrawlCompleted WebhookEventType = "crawl.completed"
	// WebhookEventCrawlStopped is sent when the crawl ends StatusStopped, after the pages fetched
	// before the stop have been read.
	WebhookEventCrawlStopped WebhookEventType = "crawl.stopped"
	// WebhookEventCrawlFailed is sent when the crawl ends StatusFailed.
	WebhookEventCrawlFailed WebhookEventType = "crawl.failed"
)

// IsKnown reports whether e is one of the WebhookEventType constants this SDK version defines.
func (e WebhookEventType) IsKnown() bool {
	switch e {
	case WebhookEventCrawlStarted, WebhookEventCrawlCompleted, WebhookEventCrawlStopped, WebhookEventCrawlFailed:
		return true
	default:
		return false
	}
}

// Webhook is where a crawl's events are sent, and which ones. It is set at create and cannot be
// changed afterwards. Every event is signed with the account's webhook signing key, so the
// account needs one: without it Create fails with a 422 webhook_signing_key_required. A field
// that breaks the rules below fails with a 422 invalid_webhook, whose detail names the field.
type Webhook struct {
	// URL is the absolute https URL each event is posted to, up to 2,048 characters. Its host
	// must resolve to a public address. Required.
	URL string `json:"url"`
	// Events narrows which events are sent: each WebhookEventType at most once, in any order.
	// Leave it empty for all four. On a returned Crawl it is always the effective list.
	Events []WebhookEventType `json:"events,omitempty"`
	// Metadata is sent back with every event, to match it to your own records: up to 20 keys of
	// 1 to 64 characters, each value any JSON value of at most 1,024 bytes as compact JSON. Nil
	// when the crawl has none. Numbers decode as float64; send a json.Number to keep every digit.
	Metadata map[string]any `json:"metadata,omitempty"`
}

// CreateParams is the body for Client.Create. Only the fields you set are sent; the API applies
// its defaults to the rest.
type CreateParams struct {
	// URL is the page the crawl starts from: an absolute http or https URL. Required.
	URL string `json:"url"`
	// Depth is how many link hops the crawl follows from URL, 1 to 100,000. Leave it 0 for no
	// depth limit: the crawl follows links until MaxPages or MaxItems stops it, or nothing is
	// left to open.
	Depth int `json:"depth,omitempty"`
	// MaxItems stops the crawl once it has kept this many URLs, 1 to 100,000 (API default 100).
	MaxItems int `json:"max_items,omitempty"`
	// MaxPages stops the crawl once it has fetched this many pages, 1 to 100,000 (API default
	// 100). It bounds what the crawl costs: each fetch is one request on your account.
	MaxPages int `json:"max_pages,omitempty"`
	// IncludePatterns keeps a URL only if it contains at least one of these substrings. A crawl
	// whose patterns reject every link ends StatusCompleted with no results and a nil Error.
	IncludePatterns []string `json:"include_patterns,omitempty"`
	// ExcludePatterns drops a URL containing any of these substrings, even if it is included.
	ExcludePatterns []string `json:"exclude_patterns,omitempty"`
	// OutputFormat fetches each kept URL's page too, in this format. Empty means URLs only.
	OutputFormat OutputFormat `json:"output_format,omitempty"`
	// Discovery is how the crawl finds the URLs it follows on each page it walks. Leave it
	// empty for DiscoveryLinks, which follows the links in the page's HTML.
	Discovery []DiscoveryStrategy `json:"discovery,omitempty"`
	// Webhook sends the crawl's events, when it starts and how it ends, to a URL you choose. Nil
	// for none.
	Webhook *Webhook `json:"webhook,omitempty"`
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

// RunError is why a crawl failed, or why it stopped because the account ran out of credits or
// the API key hit a credit cap. Present when Status is StatusFailed, and when it is
// StatusStopped with Code RunErrorInsufficientCredits or RunErrorAPIKeyCapReached. It is part of
// the crawl, not an error response.
type RunError struct {
	Code   RunErrorCode `json:"code"`
	Detail string       `json:"detail"`
}

// Crawl is one crawl: the parameters it runs with, its status and its coverage.
type Crawl struct {
	CrawlID    string     `json:"crawl_id"`
	Status     Status     `json:"status"`
	StopReason StopReason `json:"stop_reason,omitempty"`
	Error      *RunError  `json:"error,omitempty"`
	URL        string     `json:"url"`
	// Depth is the link-hop limit the crawl was created with. Nil means no depth limit: the
	// create left Depth out.
	Depth           *int         `json:"depth,omitempty"`
	MaxItems        int          `json:"max_items"`
	MaxPages        int          `json:"max_pages"`
	IncludePatterns []string     `json:"include_patterns,omitempty"`
	ExcludePatterns []string     `json:"exclude_patterns,omitempty"`
	OutputFormat    OutputFormat `json:"output_format,omitempty"`
	// Discovery is the strategies the crawl finds URLs with. It reads ["links"] when the
	// create left it out, and is empty when the server does not send the field.
	Discovery []DiscoveryStrategy `json:"discovery,omitempty"`
	// Webhook is where the crawl's events are sent, with Events the effective list. Nil when the
	// crawl was created without one.
	Webhook           *Webhook `json:"webhook,omitempty"`
	Coverage          Coverage `json:"coverage"`
	DuplicatesRemoved int      `json:"duplicates_removed,omitempty"`
	CreatedAt         string   `json:"created_at"`
	FinishedAt        string   `json:"finished_at,omitempty"`
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
