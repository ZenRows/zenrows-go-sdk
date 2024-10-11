package scraperapi

import (
	"net/http"
	"os"
	"time"
)

const (
	defaultBaseURL          = "https://api.zenrows.com/v1"
	defaultMaxRetryCount    = 0
	defaultRetryWaitTime    = 5 * time.Second
	defaultRetryMaxWaitTime = 30 * time.Second
)

var retryableStatusCodes = []int{http.StatusUnprocessableEntity, http.StatusTooManyRequests, http.StatusInternalServerError}

// Option configures the ZenRows Scraper API client.
type Option interface {
	apply(*options)
}

// options holds the configuration for the ZenRows Scraper API service
type options struct {
	// baseURL is the base url of the ZenRows Scraper API service. Defaults to: "https://api.zenrows.com/v1"
	baseURL string
	// apiKey is the secret token to use to authenticate with the ZenRows Scraper API client
	apiKey string
	// retryOptions holds the configuration for the retry mechanism of the ZenRows Scraper API client
	retryOptions retryOptions
	// maxConcurrentRequests is the maximum number of concurrent requests that can be handled by the ZenRows Scraper API client at a time
	maxConcurrentRequests int
}

// retryOptions holds the configuration for the retry mechanism of the ZenRows Scraper API client. Only response status codes in
// the retryableStatusCodes list will be retried.
type retryOptions struct {
	// maxRetryCount is the maximum number of retries to perform. If set to a non-zero value, the client will retry the request up to
	// this number of times using a backoff strategy. Defaults to 0.
	maxRetryCount int

	// retryWaitTime is the time to wait before retrying the request. Defaults to 5 seconds.
	retryWaitTime time.Duration

	// retryMaxWaitTime is the maximum time to wait before retrying the request. Defaults to 30 seconds.
	retryMaxWaitTime time.Duration
}

// defaultOptions returns the default options for the ZenRows Scraper API client.
func defaultOptions() options {
	return options{
		baseURL: defaultBaseURL,
		apiKey:  os.Getenv("ZENROWS_API_KEY"),
		retryOptions: retryOptions{
			maxRetryCount:    defaultMaxRetryCount,
			retryWaitTime:    defaultRetryWaitTime,
			retryMaxWaitTime: defaultRetryMaxWaitTime,
		},
	}
}

// funcOption wraps a function that modifies options into an implementation of the Option interface.
type funcOption struct {
	f func(*options)
}

func (fdo *funcOption) apply(do *options) {
	fdo.f(do)
}

func newFuncDialOption(f func(*options)) *funcOption {
	return &funcOption{
		f: f,
	}
}

// WithBaseURL returns an Option which configures the base URL of the ZenRows Scraper API client.
func WithBaseURL(baseURL string) Option {
	return newFuncDialOption(func(o *options) {
		o.baseURL = baseURL
	})
}

// WithAPIKey returns an Option which configures the API key of the ZenRows Scraper API client.
func WithAPIKey(apiKey string) Option {
	return newFuncDialOption(func(o *options) {
		o.apiKey = apiKey
	})
}

// WithMaxRetryCount returns an Option which configures the maximum number of retries to perform.
func WithMaxRetryCount(maxRetryCount int) Option {
	return newFuncDialOption(func(o *options) {
		o.retryOptions.maxRetryCount = maxRetryCount
	})
}

// WithRetryWaitTime returns an Option which configures the time to wait before retrying the request.
func WithRetryWaitTime(retryWaitTime time.Duration) Option {
	return newFuncDialOption(func(o *options) {
		o.retryOptions.retryWaitTime = retryWaitTime
	})
}

// WithRetryMaxWaitTime returns an Option which configures the maximum time to wait before retrying the request.
func WithRetryMaxWaitTime(retryMaxWaitTime time.Duration) Option {
	return newFuncDialOption(func(o *options) {
		o.retryOptions.retryMaxWaitTime = retryMaxWaitTime
	})
}

// WithMaxConcurrentRequests returns an Option which configures the maximum number of concurrent requests to the ZenRows Scraper API.
// See https://docs.zenrows.com/scraper-api/features/concurrency for more information.
//
// IMPORTANT: Breaking the concurrency limit will result in a 429 Too Many Requests error. If you exceed the limit repeatedly, your
// account may be temporarily suspended, so make sure to set this value to a reasonable number according to your subscription plan.
func WithMaxConcurrentRequests(maxConcurrentRequests int) Option {
	return newFuncDialOption(func(o *options) {
		o.maxConcurrentRequests = maxConcurrentRequests
	})
}
