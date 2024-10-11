# ZenRows Scraper API Go SDK

This is the Go SDK for interacting with the ZenRows Scraper API, designed to help developers integrate web scraping
capabilities into their Go applications. It simplifies the process of making HTTP requests, handling responses, 
and managing configurations for interacting with the ZenRows Scraper API.

## Introduction

The ZenRows® Scraper API is a versatile tool designed to simplify and enhance the process of extracting data from 
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
go get github.com/zenrows/zenrows-go-sdk/services/api
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
- `WithMaxRetryCount(maxRetryCount int)`: Sets the maximum number of retries for failed requests. _Default is 0 (no retries)._
- `WithRetryWaitTime(retryWaitTime time.Duration)`: Sets the time to wait before retrying a request. _Default is 5 second._
- `WithRetryMaxWaitTime(retryMaxWaitTime time.Duration)`: Sets the maximum time to wait for retries. _Default is 30 seconds._
- `WithMaxConcurrentRequests(maxConcurrentRequests int)`: Limits the number of concurrent requests. _Default is 5._ 
Make sure this value does not exceed your plan's concurrency limit, as it may result in _429 Too Many Requests_ errors.

### Error Handling

The SDK provides custom error types for better error handling:

- `NotConfiguredError`: Thrown when the client is not properly configured (e.g., missing API key).
- `InvalidHTTPMethodError`: Thrown when an unsupported HTTP method is used (e.g., when sending PATCH or DELETE requests).
- `InvalidTargetURLError`: Thrown when an invalid target URL is provided (e.g., target URL is empty, or malformed).
- `InvalidParameterError`: Thrown when invalid parameters are used in the request. See the error message for details.
 
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

The SDK supports automatic retries for failed requests. You can configure the maximum number of retries and the
wait time between retries using the `WithMaxRetryCount`, `WithRetryWaitTime`, and `WithRetryMaxWaitTime` options.

A backoff strategy is used to increase the wait time between retries, starting at the `RetryWaitTime` and doubling
the wait time for each subsequent retry until it reaches the `RetryMaxWaitTime`.

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
		scraperapi.WithRetryWaitTime(20*time.Second),    // waiting at least 20s between retries (just for demonstration purposes!)
		scraperapi.WithRetryMaxWaitTime(25*time.Second), // and waiting a maximum of 20s between retries
	)

	var wg sync.WaitGroup
	for i := 0; i < totalRequests; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			now := time.Now() // store the time, to be able to print the elapsed duration

			// target the https://httpbin.io/unstable endpoint, as it fails half of the times, so the retry mechanism takes care of
			// making sure we eventually receive a successful request
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

This program will output the status of each request, and the elapsed time. As we've set the retry mechanism to retry
up to five times, with a minimum wait time of 20 seconds and a maximum of 25 seconds, the output will look like this:

```
[#6]: 200 OK (in 743.064708ms)
[#2]: 200 OK (in 1.202448208s)
[#1]: 200 OK (in 1.380041292s)
[#5]: 200 OK (in 1.626613583s)
[#8]: 200 OK (in 2.635505541s)
[#4]: 200 OK (in 3.217849791s)
[#9]: 200 OK (in 21.973982334s) <-- this request took longer because it had to retry 1 time
[#3]: 200 OK (in 22.031445708s) <-- this request took longer because it had to retry 1 time
[#7]: 200 OK (in 22.130371583s) <-- this request took longer because it had to retry 1 time
[#0]: 200 OK (in 45.030251042s) <-- this request took longer because it had to retry 2 times
done
```

### Contributing

Contributions are welcome! Please see [CONTRIBUTING.md](../../CONTRIBUTING.md) for details.

## License

This project is licensed under the MIT License - see the [LICENSE](../../LICENSE) file for details.
