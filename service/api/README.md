# ZenRows Fetch API Go SDK

This is the Go SDK for interacting with the ZenRows Fetch API, designed to help developers integrate web scraping
capabilities into their Go applications. It simplifies the process of making HTTP requests, handling responses, 
and managing configurations for interacting with the ZenRows Fetch API.

## Introduction

The ZenRows® Fetch API is a versatile tool designed to simplify and enhance the process of extracting data from 
websites. Whether you’re dealing with static or dynamic content, our API provides a range of features to meet your 
scraping needs efficiently.

With Premium Proxies, ZenRows gives you access to over 55 million residential IPs from 190+ countries, 
ensuring 99.9% uptime and highly reliable scraping sessions. Our system also handles advanced fingerprinting, header 
rotation, and IP management, **enabling you to scrape even the most protected sites without needing to manually 
configure these elements**.

ZenRows makes it easy to bypass complex anti-bot measures, handle JavaScript-heavy sites, and interact with web 
elements dynamically — all with the right features enabled.

## Table of Contents

- [Installation](#installation)
- [Getting Started](#getting-started)
- [Usage](#usage)
  - [Client Initialization](#client-initialization)
  - [Sending Requests](#sending-requests)
    - [GET Requests](#get-requests)
    - [POST/PUT Requests](#post-or-put-requests)
  - [Custom Request Parameters](#custom-request-parameters)
  - [Extract](#extract)
    - [Preparing a domain](#preparing-a-domain)
  - [Batch](#batch)
  - [Handling Responses](#handling-responses)
- [Configuration Options](#configuration-options)
- [Error Handling](#error-handling)
- [Examples](#examples)
  - [Concurrency](#concurrency)
  - [Retrying](#retrying)
- [Contributing](#contributing)
- [License](#license)

## Installation

To install the SDK, run:

```bash
go get github.com/zenrows/zenrows-go-sdk/service/api
```

## Getting Started

To use the SDK, you need a ZenRows API key. You can find your API key in the 
ZenRows [dashboard](https://app.zenrows.com/builder).

## Usage

### Client Initialization

Initialize the ZenRows client with your API key by either using the `WithAPIKey` client option or setting
the `ZENROWS_API_KEY` environment variable:

```go
import (
    "context"
    scraperapi "github.com/zenrows/zenrows-go-sdk/service/api"
)

client := scraperapi.NewClient(
    scraperapi.WithAPIKey("YOUR_API_KEY"),
)
```

### Sending Requests

#### GET Requests

`client.Fetch()` is available as an alias for `client.Get()`, for parity with the other ZenRows SDKs — both call the same endpoint.

```go
response, err := client.Get(context.Background(), "https://httpbin.io/anything", nil)
if err != nil {
    // handle error
}

if err = response.Error(); err != nil {
    // handle error
}

fmt.Println("Response Body:", string(response.Body()))
```

#### POST or PUT Requests

```go
body := map[string]string{"key": "value"}
response, err := client.Post(context.Background(), "https://httpbin.io/anything", nil, body)
if err != nil {
    // handle error
}

if err = response.Error(); err != nil {
    // handle error
}

fmt.Println("Response Body:", string(response.Body()))
```

### Custom Request Parameters

You can customize your requests using `RequestParameters` to modify the behavior of the scraping engine:

```go
params := &scraperapi.RequestParameters{
    JSRender:          true,
    UsePremiumProxies: true,
    ProxyCountry:      "US",
}

response, err := client.Get(context.Background(), "https://httpbin.io/anything", params)
if err != nil {
    // handle error
}

if err = response.Error(); err != nil {
    // handle error
}

fmt.Println("Response Body:", response.String())
```

### Extract

[Extract](https://docs.zenrows.com/extract/introduction) runs a page through ZenRows' AI-powered structured extraction instead of returning raw HTML. Use `client.Extract()` — it's the same request as `Get()`/`Fetch()`, with `params.Extract` set for you (defaults to `scraperapi.ExtractModeAuto` when left empty; other values are `ExtractModeNative` and `ExtractModeStandard`).

```go
response, err := client.Extract(context.Background(), "https://httpbin.io/anything", nil) // extract: "auto"
// response, err := client.Extract(context.Background(), url, &scraperapi.RequestParameters{Extract: scraperapi.ExtractModeNative})
if err != nil {
    // handle error
}

fmt.Println("Response Body:", string(response.Body()))
```

`Extract()` also sends [Adaptive Stealth Mode](https://docs.zenrows.com/fetch/features/adaptive-stealth-mode) (`mode=auto`) by default, except when you set `JSRender` or `UsePremiumProxies`: the API rejects either combined with Adaptive Stealth Mode, so your fixed configuration is sent as is. Set `DisableAdaptiveStealth` to turn it off entirely.

Extract works on any prepared domain (you can prepare one yourself with `POST /v1/extract/prepared-domains`). A `403` with problem code `REQS007` means Extract can't extract this page. Check `reason` in the problem body when present (`response.Problem().Reason`); older responses may not carry it:

- `domain_not_prepared`: prepare the domain with `client.PrepareDomain()` (see below), poll `client.PreparedDomainStatus()` until `Status` is `"ready"`, then retry.
- `page_not_recognised`: the domain is served, but this page isn't one Extract extracts (a home, search, login or category page). Try a product, article or listing page, or retry once in a few minutes. Preparing the domain again changes nothing.
- `domain_not_enabled`: preparing doesn't help. Use `CSSExtractor`, or contact support.

```go
response, err := client.Extract(context.Background(), url, nil)
if err != nil {
    // handle error
}
if prob := response.Problem(); prob != nil && prob.Code == "REQS007" {
    switch prob.Reason {
    case "domain_not_prepared":
        // client.PrepareDomain(...), then poll client.PreparedDomainStatus(...)
    case "page_not_recognised":
        // try a product, article or listing page
    case "domain_not_enabled":
        // use CSSExtractor, or contact support
    }
}
```

The legacy `402` with problem code `AUTH010` means the domain isn't enabled. `Extract()` returns both responses to you as is, in one request, and never retries them with autoparse. `DisableAutoparseFallback` is deprecated and has no effect.

#### Preparing a domain

`client.PrepareDomain(ctx, domain, exampleURLs)` submits a domain for Extract (`POST /v1/extract/prepared-domains`), and `client.PreparedDomainStatus(ctx, domain)` reads where it is (`GET /v1/extract/prepared-domains/{domain}`). Both return the parsed `*scraperapi.PreparedDomainStatus`. Pass the domain without a scheme and 1 to 5 example URLs, one per kind of page you want to extract. These calls cost no credits, and the client's retry settings don't apply to them.

Preparation takes minutes, and the client does not wait for it. Poll the status until it is no longer `"preparing"`:

```go
ctx := context.Background()
status, err := client.PrepareDomain(ctx, "www.scrapingcourse.com", []string{
    "https://www.scrapingcourse.com/ecommerce/",
})
for err == nil && status.Status == "preparing" {
    time.Sleep(30 * time.Second)
    status, err = client.PreparedDomainStatus(ctx, "www.scrapingcourse.com")
}

var perr *scraperapi.PreparedDomainError
if errors.As(err, &perr) {
    // e.g. 409 in_progress, 429 out_of_allowance, 503 overloaded
    fmt.Println(perr.StatusCode, perr.Code, perr.Detail)
} else if err == nil {
    fmt.Println(status.Status) // "ready" or "failed" (see status.Failure)
}
```

An empty or whitespace-only domain returns an `InvalidParameterError` before any request.

Both methods return a `*scraperapi.PreparedDomainError` on an error answer, with `StatusCode`, `Code` (`invalid_argument`, `not_found`, `in_progress`, `out_of_allowance`, `overloaded`, `disabled`, ...), `Detail`, `Reason`, `RetryAfter` and the raw `Body`. See the [Extract endpoints docs](https://docs.zenrows.com/extract/endpoints) for every status and failure code.

A `422` with problem code `no_data_extracted` means Extract knows this kind of page but found no data on it. It isn't charged, and the client never retries it, even with `WithMaxRetryCount` set. `response.IsNoDataExtracted()` tells you when you got one. See the [Extract docs](https://docs.zenrows.com/extract/setup).

### Batch

For asynchronous, many-URL scraping jobs, see the separate [Batch API SDK](../batch/README.md) — it's a different service with its own base URL and lifecycle (open/closed jobs, runs, results), so it lives in its own Go module.

### Adaptive Stealth Mode

Set `Mode: scraperapi.ModeAuto` to let Zenrows pick the request configuration for you — it starts with the cheapest viable setup and escalates to `JSRender`/`UsePremiumProxies` only when the target needs it, billing only for the configuration that succeeds.

```go
params := &scraperapi.RequestParameters{Mode: scraperapi.ModeAuto}
response, err := client.Get(context.Background(), "https://httpbin.io/anything", params)
```

### Handling Responses

The `Response` object provides several methods to access details about the HTTP response:

- `Body() []byte`: Returns the raw response body.
- `String() string`: Returns the response body as a string.
- `Status() string`: Returns the status text (e.g., "200 OK").
- `StatusCode() int`: Returns the HTTP status code (e.g., 200).
- `Header() http.Header`: Returns the response headers.
- `Time() time.Duration`: Returns the duration of the request.
- `ReceivedAt() time.Time`: Returns the time when the response was received.
- `Size() int64`: Returns the size of the response in bytes.
- `IsSuccess() bool`: Returns `true` if the response status is in the 2xx range.
- `IsError() bool`: Returns `true` if the response status is 4xx or higher.
- `Problem() *problem.Problem`: Returns a parsed problem description if the response contains an error.
- `Error() error`: Same as `Problem()`, but returns an error type.

In order to access additional details about the scraping process, you can use the following methods:

- `TargetHeaders() http.Header`: Returns headers from the target page.
- `TargetCookies() []*http.Cookie`: Returns cookies set by the target page.

### Example

```go
response, err := client.Get(context.Background(), "https://httpbin.io/anything", nil)
if err != nil {
    // handle error
} else {
    if prob := response.Problem(); prob != nil {
        fmt.Println("API Error:", prob.Detail)
        return
    }
    
    fmt.Println("Response Body:", response.String())
    fmt.Println("Response Target Headers:", response.TargetHeaders())
    fmt.Println("Response Target Cookies:", response.TargetCookies())
}
```

### Configuration Options

You can customize the client using different options:

- `WithAPIKey(apiKey string)`: Sets the API key for authentication. If not provided, the SDK will look for
the `ZENROWS_API_KEY` environment variable.
- `WithMaxRetryCount(maxRetryCount int)`: Sets the maximum number of retries for a failed `GET` request. `POST` and `PUT` requests are never retried (see [Retrying](#retrying)). _Default is 2; set 0 to turn retries off._
- `WithRetryWaitTime(retryWaitTime time.Duration)`: Sets the base of the exponential backoff between retries. Must be greater than zero. _Default is 1 second._
- `WithRetryMaxWaitTime(retryMaxWaitTime time.Duration)`: Sets the maximum backoff wait before a retry. It does not limit a `Retry-After` wait. Must be greater than zero. _Default is 30 seconds._
- `WithRetryBudget(retryBudget time.Duration)`: No retry starts after this time, counted from the first attempt. Must be greater than zero. _Default is 90 seconds._
- `WithMaxConcurrentRequests(maxConcurrentRequests int)`: Limits the number of concurrent requests. _Default is 5._ 
Make sure this value does not exceed your plan's concurrency limit, as it may result in _429 Too Many Requests_ errors.

### Error Handling

The SDK provides custom error types for better error handling:

- `NotConfiguredError`: Thrown when the client is not properly configured (e.g., missing API key).
- `InvalidHTTPMethodError`: Thrown when an unsupported HTTP method is used (e.g., when sending PATCH or DELETE requests).
- `InvalidTargetURLError`: Thrown when an invalid target URL is provided (e.g., target URL is empty, or malformed).
- `InvalidParameterError`: Thrown when invalid parameters are used in the request. See the error message for details.
- `InvalidOptionError`: Returned by every scrape call (`Get`, `Post`, `Put`, `Fetch`, `Extract` and `Scrape`) on a client created
with an invalid retry option value (for example, `WithRetryBudget(0)` or `WithMaxRetryCount(-1)`). `NewClient` does not return an
error, so the client reports it on each scrape call and sends no request.
 
### Examples

#### Concurrency

Concurrency in web scraping is essential for efficient data extraction, especially when dealing with multiple URLs.
Managing the number of concurrent requests helps prevent overwhelming the target server and ensures you stay within
rate limits. Depending on your subscription plan, you can perform twenty or more concurrent requests.

To limit the concurrency, the SDK uses a semaphore to control the number of concurrent requests that a single client
can make. This value is set by the `WithMaxConcurrentRequests` option when initializing the client and defaults to 5.

See the [example](examples/concurrency/main.go) below for a demonstration of how to use the SDK with concurrency:

```go
package main

import (
	"context"
	"fmt"
	"sync"

	scraperapi "github.com/zenrows/zenrows-go-sdk/service/api"
)

const (
	maxConcurrentRequests = 5  // run 5 scraping requests at the same time
	totalRequests         = 10 // send a total of 10 scraping requests
)

func main() {
	client := scraperapi.NewClient(
		scraperapi.WithAPIKey("YOUR_API_KEY"),
		scraperapi.WithMaxConcurrentRequests(maxConcurrentRequests),
	)

	var wg sync.WaitGroup
	for i := 0; i < totalRequests; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()

			res, err := client.Get(context.Background(), "https://httpbin.io/anything", &scraperapi.RequestParameters{})
			if err != nil {
				fmt.Println(i, err)
				return
			}

			if err = res.Error(); err != nil {
				fmt.Println(i, err)
				return
			}

			fmt.Printf("[#%d]: %s\n", i, res.Status())
		}(i)
	}

	wg.Wait()
	fmt.Println("done")
}
```

This program will output the status of each request, running up to five concurrent requests at a time:

```
[#1]: 200 OK
[#0]: 200 OK
[#9]: 200 OK
[#5]: 200 OK
[#2]: 200 OK
[#8]: 200 OK
[#7]: 200 OK
[#6]: 200 OK
[#4]: 200 OK
[#3]: 200 OK
done
```

#### Retrying

By default, the client retries a failed `GET` scrape request up to 2 times. `WithMaxRetryCount(0)` turns retries off.

The client retries these failures:

1. A `429`, `500`, `502`, `503` or `504` response.
2. A `422` response, except Extract's `no_data_extracted`, which is a final answer.
3. A connection that failed before the request was sent: the connection was refused, the name did not resolve, the
   connection timed out, or the network was unreachable.

It never retries:

1. Any other status.
2. A connection that dropped after the request was sent (a reset or an EOF), or a read timeout. The API may already be
   working on that request, and may charge it.
3. A canceled or expired context.
4. Any status when the request sets `ReturnOriginalStatus`, or a status listed in `AllowedStatusCodes`. That status
   comes from the target site, so another try would get the same answer. A failed connection is still retried.

It retries only requests that are safe to send again:

1. A `GET` request (`Get()`, `Fetch()`, `Extract()`, or `Scrape()` with `GET`). Its body must be none, a `string`, a
   `[]byte`, or a struct or map that the client encodes before sending. A body passed as an `io.Reader` is read by the
   first attempt, so a request with one is never retried.
2. A `POST` or `PUT` request is never retried. The client sends it once, whatever its headers, its body, the status, or
   the failure, even when it fails to connect.

The API does not dedupe a request on its `Idempotency-Key` header, so the client does not retry on it. If you need to
retry a `POST` or `PUT`, write that retry yourself.

Before each retry, the client waits:

1. The `Retry-After` the API sent, when it is a whole number of seconds. The wait is at least 250ms and at most
   30 seconds. That 30-second cap is fixed: `WithRetryMaxWaitTime` does not change it.
2. Otherwise, a random time between 250ms and `max(250ms, min(RetryMaxWaitTime, RetryWaitTime * 2^n))`, where `n` is 0
   for the first retry ("full jitter", so many clients do not retry at the same moment). With the defaults, the first
   retry waits up to 1 second and the second up to 2 seconds. A `Retry-After` in any other form, such as a date or a
   fraction, is ignored.

No retry starts after 90 seconds: the client does not start a retry when the time since the first attempt plus the next
wait would pass the retry budget (`WithRetryBudget`, 90 seconds by default). The budget does not cut a running attempt
short, so a call can run past it by one attempt.

To stop a call, cancel its context: the wait before a retry ends at once, and the client sends no further request.

`WithRetryWaitTime`, `WithRetryMaxWaitTime` and `WithRetryBudget` must be greater than zero, and `WithMaxRetryCount`
must be 0 or more. A client created with an invalid value returns an `InvalidOptionError` from every scrape call.

These retries apply to scrape calls only. The client never retries `PrepareDomain()` or `PreparedDomainStatus()`: their `429`
and `503` answers are for you to read (see [Preparing a domain](#preparing-a-domain)).

See the [example](examples/retries/main.go) below for a demonstration of how to use the SDK with retries:

```go
package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	scraperapi "github.com/zenrows/zenrows-go-sdk/service/api"
)

const (
	maxConcurrentRequests = 5  // run 5 scraping requests at the same time
	totalRequests         = 10 // send a total of 10 scraping requests
)

func main() {
	client := scraperapi.NewClient(
		scraperapi.WithAPIKey("YOUR_API_KEY"),
		scraperapi.WithMaxConcurrentRequests(maxConcurrentRequests),
		scraperapi.WithMaxRetryCount(5),                 // retry up to five times
		scraperapi.WithRetryWaitTime(2*time.Second),     // the first retry waits up to 2s, the second up to 4s, and so on
		scraperapi.WithRetryMaxWaitTime(10*time.Second), // but no wait is longer than 10s
	)

	var wg sync.WaitGroup
	for i := 0; i < totalRequests; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			now := time.Now() // store the time, to be able to print the elapsed duration

			// target the https://httpbin.io/unstable endpoint, which fails about half of the time. The API reports that failure
			// with a status the client retries (a 500, or a 422 that is not no_data_extracted), so most requests succeed in the end
			res, err := client.Get(context.Background(), "https://httpbin.io/unstable", &scraperapi.RequestParameters{})
			if err != nil {
				fmt.Println(i, err)
				return
			}

			if err = res.Error(); err != nil {
				fmt.Println(i, err)
				return
			}

			fmt.Printf("[#%d]: %s (in %s)\n", i, res.Status(), time.Since(now))
		}(i)
	}

	wg.Wait()
	fmt.Println("done")
}
```

This program prints the status of each request and its elapsed time. The `httpbin.io/unstable` endpoint fails about
half of the time, and the API reports that failure with a status the client retries (a `500`, or a `422` that is not
`no_data_extracted`). A request that needed retries takes longer, by the waits described above. The output looks like
this (the times vary from run to run):

```
[#6]: 200 OK (in 743.064708ms)
[#2]: 200 OK (in 1.202448208s)
[#1]: 200 OK (in 1.380041292s)
[#5]: 200 OK (in 1.626613583s)
[#8]: 200 OK (in 2.635505541s)
[#4]: 200 OK (in 3.217849791s)
[#9]: 200 OK (in 3.973982334s) <-- this request retried 1 time
[#3]: 200 OK (in 4.031445708s) <-- this request retried 1 time
[#7]: 200 OK (in 4.130371583s) <-- this request retried 1 time
[#0]: 200 OK (in 8.030251042s) <-- this request retried 2 times
done
```

### Contributing

Contributions are welcome! Please see [CONTRIBUTING.md](../../CONTRIBUTING.md) for details.

## License

This project is licensed under the MIT License - see the [LICENSE](../../LICENSE) file for details.
