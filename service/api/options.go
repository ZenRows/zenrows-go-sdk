package scraperapi

import (
	"os"
	"time"
)

const (
	defaultBaseURL          = "https://api.zenrows.com/v1"
	defaultMaxRetryCount    = 0
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

// retryOptions holds the configuration for the retry mechanism of the ZenRows Fetch API client (see retry.go).
type retryOptions struct {
	maxRetryCount    int
	retryWaitTime    time.Duration
	retryMaxWaitTime time.Duration
	retryBudget      time.Duration
}

// validate returns an InvalidOptionError for a negative retry count, or, when retries are on, for an invalid wait or
// budget. With a count of 0 the other retry options do nothing, so they are not checked.
func (o retryOptions) validate() error {
	switch {
	case o.maxRetryCount < 0:
		return InvalidOptionError{Msg: "WithMaxRetryCount must be 0 or more"}
	case o.maxRetryCount == 0:
		return nil
	case o.retryWaitTime < 0:
		return InvalidOptionError{Msg: "WithRetryWaitTime must be 0 or more"}
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

// WithMaxRetryCount returns an Option which configures the maximum number of retries of a failed GET. Defaults to 0: the
// client sends each request once. A POST or a PUT is never retried, and neither is a status that comes from the target
// site (RequestParameters.ReturnOriginalStatus or AllowedStatusCodes). See the README for the full retry policy.
func WithMaxRetryCount(maxRetryCount int) Option {
	return newFuncDialOption(func(o *options) {
		o.retryOptions.maxRetryCount = maxRetryCount
	})
}

// WithRetryWaitTime returns an Option which configures the base of the backoff: retry n (from 0) waits a random time
// between 250ms and max(250ms, min(RetryMaxWaitTime, RetryWaitTime * 2^n)). Defaults to 1 second; 0 means 250ms.
func WithRetryWaitTime(retryWaitTime time.Duration) Option {
	return newFuncDialOption(func(o *options) {
		if retryWaitTime == 0 {
			retryWaitTime = minRetryWait
		}
		o.retryOptions.retryWaitTime = retryWaitTime
	})
}

// WithRetryMaxWaitTime returns an Option which configures the maximum backoff wait before a retry. Defaults to 30 seconds.
// It does not limit a Retry-After wait, which is capped at 30 seconds.
func WithRetryMaxWaitTime(retryMaxWaitTime time.Duration) Option {
	return newFuncDialOption(func(o *options) {
		o.retryOptions.retryMaxWaitTime = retryMaxWaitTime
	})
}

// WithRetryBudget returns an Option which configures the retry budget: no retry starts when the time since the first
// attempt plus the wait before it would pass the budget. Defaults to 90 seconds.
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
