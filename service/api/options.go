package scraperapi

import (
	"os"
	"time"
)

const (
	defaultBaseURL          = "https://api.zenrows.com/v1"
	defaultMaxRetryCount    = 2
	defaultRetryWaitTime    = 1 * time.Second
	defaultRetryMaxWaitTime = 30 * time.Second
	defaultRetryBudget      = 90 * time.Second
)

// Option configures the ZenRows Fetch API client.
type Option interface {
	apply(*options)
}

// options holds the configuration for the ZenRows Fetch API service
type options struct {
	// baseURL is the base url of the ZenRows Fetch API service. Defaults to: "https://api.zenrows.com/v1"
	baseURL string
	// apiKey is the secret token to use to authenticate with the ZenRows Fetch API client
	apiKey string
	// retryOptions holds the configuration for the retry mechanism of the ZenRows Fetch API client
	retryOptions retryOptions
	// maxConcurrentRequests is the maximum number of concurrent requests that can be handled by the ZenRows Fetch API client at a time
	maxConcurrentRequests int
}

// retryOptions holds the configuration for the retry mechanism of the ZenRows Fetch API client. Only the response status codes
// in retryableStatusCodes and connection errors before any response are retried; timeouts never are.
type retryOptions struct {
	// maxRetryCount is the maximum number of retries to perform. Defaults to 2; 0 turns retries off.
	maxRetryCount int

	// retryWaitTime is the base of the exponential backoff: retry n waits a random time between 250ms and
	// min(retryMaxWaitTime, retryWaitTime * 2^n), with n = 0 for the first retry. Defaults to 1 second.
	retryWaitTime time.Duration

	// retryMaxWaitTime is the upper bound of the backoff wait. Defaults to 30 seconds. A Retry-After header
	// replaces the backoff wait, and is honored up to 30 seconds.
	retryMaxWaitTime time.Duration

	// retryBudget bounds a call with its retries: no retry starts when the time since the first attempt
	// plus the wait before the retry would exceed it. Defaults to 90 seconds.
	retryBudget time.Duration
}

// defaultOptions returns the default options for the ZenRows Fetch API client.
func defaultOptions() options {
	return options{
		baseURL: defaultBaseURL,
		apiKey:  os.Getenv("ZENROWS_API_KEY"),
		retryOptions: retryOptions{
			maxRetryCount:    defaultMaxRetryCount,
			retryWaitTime:    defaultRetryWaitTime,
			retryMaxWaitTime: defaultRetryMaxWaitTime,
			retryBudget:      defaultRetryBudget,
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

// WithBaseURL returns an Option which configures the base URL of the ZenRows Fetch API client.
func WithBaseURL(baseURL string) Option {
	return newFuncDialOption(func(o *options) {
		o.baseURL = baseURL
	})
}

// WithAPIKey returns an Option which configures the API key of the ZenRows Fetch API client.
func WithAPIKey(apiKey string) Option {
	return newFuncDialOption(func(o *options) {
		o.apiKey = apiKey
	})
}

// WithMaxRetryCount returns an Option which configures the maximum number of retries to perform. Defaults to 2; set 0 to
// turn retries off.
func WithMaxRetryCount(maxRetryCount int) Option {
	return newFuncDialOption(func(o *options) {
		o.retryOptions.maxRetryCount = maxRetryCount
	})
}

// WithRetryWaitTime returns an Option which configures the base of the exponential backoff between retries: retry n waits a
// random time between 250ms and min(RetryMaxWaitTime, RetryWaitTime * 2^n), with n = 0 for the first retry. Defaults to 1 second.
func WithRetryWaitTime(retryWaitTime time.Duration) Option {
	return newFuncDialOption(func(o *options) {
		o.retryOptions.retryWaitTime = retryWaitTime
	})
}

// WithRetryMaxWaitTime returns an Option which configures the maximum backoff wait before a retry. Defaults to 30 seconds.
// A Retry-After header sent by the API replaces the backoff wait, and is honored up to 30 seconds.
func WithRetryMaxWaitTime(retryMaxWaitTime time.Duration) Option {
	return newFuncDialOption(func(o *options) {
		o.retryOptions.retryMaxWaitTime = retryMaxWaitTime
	})
}

// WithRetryBudget returns an Option which bounds the time a call spends on retries: no retry starts when the time since the
// first attempt plus the wait before the retry would exceed it. It does not cut an attempt short. Defaults to 90 seconds; a
// budget of zero or less allows no retries.
func WithRetryBudget(retryBudget time.Duration) Option {
	return newFuncDialOption(func(o *options) {
		o.retryOptions.retryBudget = retryBudget
	})
}

// WithMaxConcurrentRequests returns an Option which configures the maximum number of concurrent requests to the ZenRows Fetch API.
// See https://docs.zenrows.com/scraper-api/features/concurrency for more information.
//
// IMPORTANT: Breaking the concurrency limit will result in a 429 Too Many Requests error. If you exceed the limit repeatedly, your
// account may be temporarily suspended, so make sure to set this value to a reasonable number according to your subscription plan.
func WithMaxConcurrentRequests(maxConcurrentRequests int) Option {
	return newFuncDialOption(func(o *options) {
		o.maxConcurrentRequests = maxConcurrentRequests
	})
}
