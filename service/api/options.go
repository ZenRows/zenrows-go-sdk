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

// retryOptions holds the configuration for the retry mechanism of the ZenRows Fetch API client. Only the statuses
// isRetryableStatus accepts and connection failures before the request was sent are retried, and only for a GET
// (see isReplayable in retry.go). A POST or a PUT is always sent once.
type retryOptions struct {
	// maxRetryCount is the maximum number of retries of a GET. Defaults to 2; 0 turns retries off. It must not be
	// negative. It does not apply to a POST or a PUT, which the client never retries.
	maxRetryCount int

	// retryWaitTime is the base of the exponential backoff: retry n waits a random time between 250ms and
	// max(250ms, min(retryMaxWaitTime, retryWaitTime * 2^n)), with n = 0 for the first retry. Defaults to 1 second.
	// It must be greater than zero.
	retryWaitTime time.Duration

	// retryMaxWaitTime is the upper bound of the backoff wait. Defaults to 30 seconds. It does not bound a
	// Retry-After header, which is honored up to a fixed 30 seconds. It must be greater than zero.
	retryMaxWaitTime time.Duration

	// retryBudget bounds when a retry may start: no retry starts when the time since the first attempt plus the
	// wait before the retry would exceed it. Defaults to 90 seconds. It must be greater than zero.
	retryBudget time.Duration
}

// validate returns an InvalidOptionError for the first retry option with an invalid value, or nil.
func (o retryOptions) validate() error {
	switch {
	case o.maxRetryCount < 0:
		return InvalidOptionError{Msg: "WithMaxRetryCount must be 0 or more"}
	case o.retryWaitTime <= 0:
		return InvalidOptionError{Msg: "WithRetryWaitTime must be greater than zero"}
	case o.retryMaxWaitTime <= 0:
		return InvalidOptionError{Msg: "WithRetryMaxWaitTime must be greater than zero"}
	case o.retryBudget <= 0:
		return InvalidOptionError{Msg: "WithRetryBudget must be greater than zero"}
	}
	return nil
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
// turn retries off. A negative count is invalid: every scrape call then returns an InvalidOptionError.
//
// Only a GET is retried (Get, Fetch, Extract, or Scrape with GET). The client sends a POST or a PUT once, whatever this
// count or the request's headers say. The API does not dedupe a request on its Idempotency-Key header, so the client
// does not retry on it. A caller who needs to retry a POST or a PUT must write that retry itself.
//
// The client never retries a status that comes from the target site: any status when the request sets
// RequestParameters.ReturnOriginalStatus, or a status listed in RequestParameters.AllowedStatusCodes.
func WithMaxRetryCount(maxRetryCount int) Option {
	return newFuncDialOption(func(o *options) {
		o.retryOptions.maxRetryCount = maxRetryCount
	})
}

// WithRetryWaitTime returns an Option which configures the base of the exponential backoff between retries: retry n waits a
// random time between 250ms and max(250ms, min(RetryMaxWaitTime, RetryWaitTime * 2^n)), with n = 0 for the first retry.
// Defaults to 1 second. A value of zero or less is invalid: every scrape call then returns an InvalidOptionError.
func WithRetryWaitTime(retryWaitTime time.Duration) Option {
	return newFuncDialOption(func(o *options) {
		o.retryOptions.retryWaitTime = retryWaitTime
	})
}

// WithRetryMaxWaitTime returns an Option which configures the maximum backoff wait before a retry. Defaults to 30 seconds.
// The wait is never under 250ms, even when this is smaller. A Retry-After header sent by the API replaces the backoff wait,
// and is honored up to a fixed 30 seconds, whatever this option says. A value of zero or less is invalid: every scrape call
// then returns an InvalidOptionError.
func WithRetryMaxWaitTime(retryMaxWaitTime time.Duration) Option {
	return newFuncDialOption(func(o *options) {
		o.retryOptions.retryMaxWaitTime = retryMaxWaitTime
	})
}

// WithRetryBudget returns an Option which bounds when a retry may start: no retry starts when the time since the first
// attempt plus the wait before the retry would exceed it. It does not cut an attempt short, so a call can run past the
// budget by one attempt. Defaults to 90 seconds. A value of zero or less is invalid: every scrape call then returns
// an InvalidOptionError. Use WithMaxRetryCount(0) to turn retries off.
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
