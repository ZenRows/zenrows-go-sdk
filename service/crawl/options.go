package crawl

import (
	"os"
	"time"
)

const defaultBaseURL = "https://api.zenrows.com/v1"

// defaultRetries bounds automatic retries of transient failures (HTTP 429/502/503/504 and
// network errors) on idempotent requests. Create never retries a 429. Retries use jittered
// exponential backoff and honor Retry-After; set WithRetries(0) to disable.
const defaultRetries = 3

const defaultTimeout = 30 * time.Second

// Option configures the Zenrows Crawl API client.
type Option interface {
	apply(*options)
}

type options struct {
	baseURL string
	apiKey  string
	retries int
	timeout time.Duration
}

func defaultOptions() options {
	return options{
		baseURL: defaultBaseURL,
		apiKey:  os.Getenv("ZENROWS_API_KEY"),
		retries: defaultRetries,
		timeout: defaultTimeout,
	}
}

type funcOption struct {
	f func(*options)
}

func (fo *funcOption) apply(o *options) {
	fo.f(o)
}

// WithBaseURL configures the base URL of the Zenrows Crawl API client.
func WithBaseURL(baseURL string) Option {
	return &funcOption{f: func(o *options) { o.baseURL = baseURL }}
}

// WithAPIKey configures the API key of the Zenrows Crawl API client.
func WithAPIKey(apiKey string) Option {
	return &funcOption{f: func(o *options) { o.apiKey = apiKey }}
}

// WithRetries configures how many times a transient failure (429/502/503/504, or a network
// error) is retried on idempotent requests. Create never retries a 429. Defaults to 3; pass 0
// to disable.
func WithRetries(retries int) Option {
	return &funcOption{f: func(o *options) {
		if retries < 0 {
			retries = 0
		}
		o.retries = retries
	}}
}

// WithTimeout configures how long one HTTP request may take; a request that runs out is a
// network error, retried as such. For Download it bounds the wait for the response headers,
// not the reading of the stream. Wait has its own WaitOptions.Timeout. Defaults to 30s;
// 0 or less keeps the default.
func WithTimeout(timeout time.Duration) Option {
	return &funcOption{f: func(o *options) {
		if timeout > 0 {
			o.timeout = timeout
		}
	}}
}
