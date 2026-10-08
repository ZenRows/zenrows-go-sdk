# Zenrows Crawl API Go SDK

This is the Go SDK for the Zenrows Crawl API. Give it one start URL, and it follows the links
behind it and returns the URLs it keeps — optionally with each page's HTML.

## Model

A **crawl** is one run from one start URL. It walks up to `Depth` link hops, keeps the URLs that
match `IncludePatterns` (and none of `ExcludePatterns`), and stops at `MaxItems` kept URLs or
`MaxPages` fetched pages. It runs asynchronously: `Create` returns at once with
`StatusRunning`, and the crawl ends `StatusCompleted`, `StatusStopped` (you called `Stop`) or
`StatusFailed` (`Crawl.Error` says why). With `OutputFormat: crawl.OutputFormatHTML`, each kept
URL's page is fetched too.

## Installation

```bash
go get github.com/zenrows/zenrows-go-sdk/service/crawl
```

## Usage

```go
import (
    "context"
    "fmt"

    "github.com/zenrows/zenrows-go-sdk/service/crawl"
)

client := crawl.NewClient(crawl.WithAPIKey("YOUR_API_KEY"))
ctx := context.Background()

created, err := client.Create(ctx, crawl.CreateParams{
    URL:             "https://www.scrapingcourse.com/ecommerce/",
    Depth:           1,
    MaxItems:        20,
    MaxPages:        30,
    IncludePatterns: []string{"/product/"},
    OutputFormat:    crawl.OutputFormatHTML, // omit for URLs only
})

// Block until the crawl ends (default timeout 300s; the crawl keeps running on timeout).
done, err := client.Wait(ctx, created.CrawlID, crawl.WaitOptions{})
fmt.Println(done.Status, done.Coverage.ItemsFound)

// Every kept URL, auto-paginated.
for result, err := range client.IterResults(ctx, created.CrawlID, crawl.GetOptions{}) {
    if err != nil {
        break
    }
    if result.ContentStatus == crawl.ContentStatusFetched {
        html, err := client.GetContent(ctx, created.CrawlID, result.ContentID())
        _, _ = html, err
    }
}

// Or everything in one NDJSON stream, pages included.
dl, err := client.Download(ctx, created.CrawlID)
defer dl.Close()
for line, err := range dl.Lines() {
    if err != nil {
        break
    }
    fmt.Println(line.URL, len(line.HTML()))
}
```

## Client Initialization

Configure the client with `WithAPIKey` or the `ZENROWS_API_KEY` environment variable, and optionally
`WithBaseURL` (defaults to `https://api.zenrows.com/v1`) and `WithRetries` (defaults to 3 —
transient failures, 429/502/503/504 and network errors, are retried on idempotent requests with
jittered exponential backoff honoring `Retry-After`; `Create` is retried only when
`CreateParams.IdempotencyKey` is set).

## Methods

- `Create(ctx, CreateParams)` starts a crawl. Only the fields you set are sent.
- `Get(ctx, crawlID, GetOptions)` reads the crawl and one page of results. `NextCursor` is never
  nil while the crawl runs (polling with it returns only new URLs) and nil once the crawl has
  ended and the last page was read.
- `IterResults(ctx, crawlID, GetOptions)` yields every result, following `NextCursor` until it is
  nil. Call it after `Wait`: on a running crawl it yields what was kept so far and stops.
- `Wait(ctx, crawlID, WaitOptions)` polls until the crawl ends (2s, x1.5 per poll, capped at 15s;
  timeout 300s) and returns `WaiterTimeoutError` on timeout without stopping the crawl.
- `List(ctx, ListOptions)` / `IterCrawls(ctx, ListOptions)` list the account's crawls, newest first.
- `Stop(ctx, crawlID)` stops a running crawl; on a crawl that already ended it returns the crawl
  as it ended.
- `GetContent(ctx, crawlID, contentID)` returns one kept URL's page (`Result.ContentID()`).
- `Download(ctx, crawlID)` streams every result as NDJSON; `Download.Status` is `StatusRunning`
  when the crawl had not ended yet, so the file is partial.

## Error Handling

- `NotConfiguredError`: the client is missing an API key.
- `APIError`: a non-2xx response. `StatusCode` carries the HTTP status; `Detail` carries the parsed
  RFC 9457 Problem JSON body when the response could be decoded as such; `.Code()` returns the
  stable problem code, or `"internal"` if the body wasn't parseable. Codes worth branching on:
  - `CodeNotEnabled` (`REQS008`, 403): Crawl is not enabled for this account. `.NotEnabled()`
    reports it, and the error message says so.
  - `CodeTooManyCrawls` (`too_many_crawls`, 429): the account has too many crawls running. Nothing
    was created; retry after `APIError.RetryAfter`.
  - `CodeCrawlNotFound` / `CodeContentNotFound` (404), `CodeInvalidParameter` /
    `CodeInvalidStartURL` (422).
- `WaiterTimeoutError`: `Wait` timed out.
- String enums on responses (`Status`, `StopReason`, `RunErrorCode`, `ContentStatus`) are
  extensible: the server may return values this SDK has no constant for. They decode without
  error, so always handle a default case; each type's `IsKnown()` reports whether a value is one
  this SDK version defines. `Status.IsTerminal()` treats every status but `StatusRunning` as ended.

## Running the end-to-end test

See [CONTRIBUTING.md](../../CONTRIBUTING.md#running-the-crawl-end-to-end-test).

## License

This project is licensed under the MIT License - see the [LICENSE](../../LICENSE) file for details.
