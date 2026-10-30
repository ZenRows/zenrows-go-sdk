# Zenrows Crawl API Go SDK (new)

This is the Go SDK for the Zenrows Crawl API. Give it one start URL, and it follows the links
behind it and returns the URLs it keeps — optionally with each page's HTML.

Crawl is still evolving: new features are coming, limits may be tuned, and the changelog announces each change.
This module is v0 (`service/crawl/v0.x`), so its API can change before v1.

## Model

A **crawl** is one run from one start URL. It walks up to `Depth` link hops (no limit when you
leave `Depth` out), keeps the URLs that match `IncludePatterns` (and none of `ExcludePatterns`),
and stops at `MaxItems` kept URLs or `MaxPages` fetched pages (100 each by default). It runs
asynchronously: `Create` returns at once with `StatusRunning`, and the crawl ends
`StatusCompleted`, `StatusStopped` (you called `Stop`, or the account ran out of credits or the
API key hit a credit cap: then `StopReason` is empty and `Crawl.Error.Code` is
`RunErrorInsufficientCredits` or `RunErrorAPIKeyCapReached`) or `StatusFailed` (`Crawl.Error`
says why). A crawl whose pages held nothing new to follow (no link, only links to other sites,
or only links the patterns reject) ends `StatusCompleted` with no results and a nil `Error`:
check the patterns against the URLs the start page links to. With
`OutputFormat: crawl.OutputFormatHTML`, each kept URL's page is fetched too. A crawl stays on the start URL's registrable domain; subdomains count.

## Installation

```bash
go get github.com/zenrows/zenrows-go-sdk/service/crawl
```

## Usage

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/zenrows/zenrows-go-sdk/service/crawl"
)

func main() {
    client := crawl.NewClient(crawl.WithAPIKey("YOUR_API_KEY"))
    ctx := context.Background()

    created, err := client.Create(ctx, crawl.CreateParams{
        URL:             "https://www.scrapingcourse.com/ecommerce/",
        Depth:           1, // omit for no depth limit
        MaxItems:        20,
        MaxPages:        30,
        IncludePatterns: []string{"/product/"},
        OutputFormat:    crawl.OutputFormatHTML, // omit for URLs only
    })
    if err != nil {
        log.Fatal(err)
    }

    // Block until the crawl ends, or until the timeout (default 600s) runs out. On timeout the
    // crawl is returned still running, without an error.
    done, err := client.Wait(ctx, created.CrawlID, crawl.WaitOptions{})
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println(done.Status, done.Coverage.ItemsFound)

    // Every kept URL, auto-paginated.
    for result, err := range client.Results(ctx, created.CrawlID, crawl.ResultsOptions{}) {
        if err != nil {
            log.Fatal(err)
        }
        if result.ContentStatus == crawl.ContentStatusFetched {
            html, err := client.Content(ctx, created.CrawlID, result.ContentURL)
            if err != nil {
                log.Fatal(err)
            }
            fmt.Println(result.URL, len(html))
        }
    }

    // Or everything in one NDJSON stream, pages included.
    dl, err := client.Download(ctx, created.CrawlID)
    if err != nil {
        log.Fatal(err)
    }
    defer dl.Close()
    for line, err := range dl.Lines() {
        if err != nil {
            log.Fatal(err)
        }
        fmt.Println(line.URL, len(line.HTML()))
    }
}
```

## Client Initialization

Configure the client with `WithAPIKey` or the `ZENROWS_API_KEY` environment variable, and optionally
`WithBaseURL` (defaults to `https://api.zenrows.com/v1`), `WithTimeout` and `WithRetries`.

- `WithTimeout` (defaults to 30s) bounds each HTTP request. For `Download` it bounds the wait for
  the response headers, not the reading of the stream. A request that runs out is a network error.
- `WithRetries` (defaults to 3): transient failures, 429/502/503/504 and network errors, are
  retried on idempotent requests with jittered exponential backoff honoring `Retry-After` (waiting at most 30 seconds).
  `Create` is retried only when `CreateParams.IdempotencyKey` is set, and never on a 429.

## Methods

- `Create(ctx, CreateParams)` starts a crawl. Only the fields you set are sent. Leave `Depth`
  out for no depth limit; `Crawl.Depth` is then nil. `MaxItems` and `MaxPages` default to 100.
  Set `Webhook` to have the crawl's events sent to your URL (see [Webhooks](#webhooks)).
- `Get(ctx, crawlID, GetOptions)` reads the crawl and one page of results. `NextCursor` is never
  nil while the crawl runs (polling with it returns only new URLs) and nil once the crawl has
  ended and the last page was read.
- `Results(ctx, crawlID, ResultsOptions)` yields every result, following `NextCursor` until it is
  nil. `ResultsOptions.Limit` is the page size of each request. Call it after `Wait`: on a running
  crawl it yields what was kept so far and stops at the first empty page.
- `Wait(ctx, crawlID, WaitOptions)` polls until the crawl ends (2s, x1.5 per poll, capped at 15s)
  or `WaitOptions.Timeout` (default 600s) runs out. On timeout it returns the crawl, still
  `StatusRunning`, without an error, and does not stop it. A failed crawl is returned, not an error,
  and so is a crawl stopped on credits; read `Crawl.Error` on either.
- `List(ctx, ListOptions)` reads one page of the account's crawls, newest first. Pass
  `NextCursor` as `ListOptions.Cursor` for the next page; it is empty on the last page.
- `Stop(ctx, crawlID)` stops a running crawl; on a crawl that already ended it returns the crawl
  as it ended.
- `Content(ctx, crawlID, contentID)` returns one kept URL's page. `contentID` is a content id
  or a `Result.ContentURL`.
- `Download(ctx, crawlID)` streams every result as NDJSON; `Download.Status` is `StatusRunning`
  when the crawl had not ended yet, so the file is partial. `DownloadLine.Content` holds the raw
  JSON content; `HTML()` decodes it for `OutputFormatHTML`.

## Webhooks

Set `CreateParams.Webhook` to be told when a crawl starts and how it ends, instead of polling:

```go
created, err := client.Create(ctx, crawl.CreateParams{
    URL: "https://www.scrapingcourse.com/ecommerce/",
    Webhook: &crawl.Webhook{
        URL:      "https://hooks.example.com/zenrows",
        Events:   []crawl.WebhookEventType{crawl.WebhookEventCrawlCompleted, crawl.WebhookEventCrawlFailed}, // omit for all four
        Metadata: map[string]any{"site": "ecommerce"}, // sent back with every event
    },
})
```

- `URL` is an absolute `https` URL whose host resolves to a public address. `Events` narrows the
  events sent: `WebhookEventCrawlStarted`, then exactly one of `WebhookEventCrawlCompleted`,
  `WebhookEventCrawlStopped` or `WebhookEventCrawlFailed`. `Metadata` takes up to 20 keys, each
  value at most 1,024 bytes as JSON.
- The webhook is fixed at create. `Crawl.Webhook` carries it on every read, with `Events` the
  effective list; it is nil on a crawl without one.
- Every event is signed with the account's webhook signing key, so the account needs one: create
  it in the dashboard (Settings, API Keys). Without it `Create` fails with 422
  `webhook_signing_key_required`.

Each event is a `WebhookEvent` posted as JSON. Verify the raw body before decoding it. An event
can arrive more than once, with the same `EventID` (also the `X-ZenRows-Event-Id` header): drop
one you have already handled. Events can arrive out of order: order them by `OccurredAt`, or read
the crawl with `Get`. Answer with a 2xx within 10 seconds; any other answer is retried, for up to
3 days.

```go
func handleCrawlEvent(w http.ResponseWriter, r *http.Request) {
    body, err := io.ReadAll(r.Body)
    if err != nil {
        http.Error(w, "read", http.StatusBadRequest)
        return
    }
    secrets := map[string]string{"<key id>": os.Getenv("ZENROWS_WEBHOOK_SECRET")} // base64, as the dashboard shows it
    if err := crawl.VerifyWebhookSignature(body, r.Header.Get(crawl.SignatureHeader), secrets, crawl.VerifyWebhookSignatureOptions{}); err != nil {
        http.Error(w, "bad signature", http.StatusUnauthorized)
        return
    }
    var event crawl.WebhookEvent
    if err := json.Unmarshal(body, &event); err != nil {
        http.Error(w, "bad body", http.StatusBadRequest)
        return
    }
    // Dedupe on event.EventID, then act on event.EventType and event.Resource.ID.
    w.WriteHeader(http.StatusNoContent)
}
```

`VerifyWebhookSignature` rejects a signature whose timestamp is more than 5 minutes from now
(`VerifyWebhookSignatureOptions.Tolerance` changes that, `SkipTimestampCheck` turns it off). The
header's `kid` picks the secret, so keep the old and the new key in `secrets` while you rotate.

## Error Handling

- `NotConfiguredError`: the client is missing an API key.
- A request that gets no response within the `WithTimeout` value fails with an error for which
  `errors.Is(err, context.DeadlineExceeded)` is true.
- `APIError`: a non-2xx response. `StatusCode` carries the HTTP status; `Detail` carries the parsed
  RFC 9457 Problem JSON body when the response could be decoded as such; `.Code()` returns the
  stable problem code, or `""` if the body has none. The codes:
  - `CodeNotEnabled` (`REQS008`, 403): Crawl is not enabled for this account. The error message
    says so.
  - 400 `invalid_request`, `unknown_parameter` or `invalid_cursor`: fix the request.
  - 404 `crawl_not_found` or `content_not_found`.
  - 409 `idempotency_request_in_flight`: a create with the same `IdempotencyKey` is still in
    progress. Retry after it ends.
  - 422 `invalid_parameter`, `invalid_start_url`, `domain_not_allowed` or
    `idempotency_key_reused`: do not retry as is. For `idempotency_key_reused`, use a new key or
    no key.
  - 422 `invalid_webhook`: the webhook's `URL`, `Events` or `Metadata` breaks a rule; the detail
    names the field.
  - 422 `webhook_signing_key_required`: the account has no webhook signing key. Create it in the
    dashboard, then retry; nothing was created, and the `IdempotencyKey` stays free.
  - 429 `too_many_crawls`: the account has reached its limit of active jobs (3 by default),
    shared with its Batch jobs. Nothing was created; retry after `APIError.RetryAfter`. `Create`
    does not retry it.
  - 503 `crawl_busy`: the stop could not be saved yet; the crawl is still running. Repeating
    the stop is safe, so `Stop` retries any 429, 502, 503 or 504 up to `WithRetries` times,
    waiting `APIError.RetryAfter` (1s when absent, at most 30s), then returns it.
- String enums on responses (`Status`, `StopReason`, `RunErrorCode`, `ContentStatus`,
  `WebhookEventType`) are extensible: the server may return values this SDK has no constant for. They decode without
  error, so always handle a default case; each type's `IsKnown()` reports whether a value is one
  this SDK version defines.

## Running the end-to-end test

See [CONTRIBUTING.md](../../CONTRIBUTING.md#running-the-crawl-end-to-end-test).

## License

This project is licensed under the MIT License - see the [LICENSE](../../LICENSE) file for details.
