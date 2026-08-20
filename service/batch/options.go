package batch

import "os"

const defaultBaseURL = "https://async.api.zenrows.com/v1"

// defaultRetries bounds automatic retries of transient failures (HTTP 429/502/503/504 and
// network errors) on idempotent requests. Retries use jittered exponential backoff and honor
// Retry-After; set WithRetries(0) to disable.
const defaultRetries = 3

// Option configures the Zenrows Batch API client.
type Option interface {
	apply(*options)
}

type options struct {
	baseURL string
	apiKey  string
	retries int
}

func defaultOptions() options {
	return options{
		baseURL: defaultBaseURL,
		apiKey:  os.Getenv("ZENROWS_API_KEY"),
		retries: defaultRetries,
	}
}

type funcOption struct {
	f func(*options)
}

func (fo *funcOption) apply(o *options) {
	fo.f(o)
}

// WithBaseURL configures the base URL of the Zenrows Batch API client.
func WithBaseURL(baseURL string) Option {
	return &funcOption{f: func(o *options) { o.baseURL = baseURL }}
}

// WithAPIKey configures the API key of the Zenrows Batch API client.
func WithAPIKey(apiKey string) Option {
	return &funcOption{f: func(o *options) { o.apiKey = apiKey }}
}

// WithRetries configures how many times a transient failure (429/502/503/504, or a network
// error) is retried on idempotent requests. Defaults to 3; pass 0 to disable.
func WithRetries(retries int) Option {
	return &funcOption{f: func(o *options) {
		if retries < 0 {
			retries = 0
		}
		o.retries = retries
	}}
}
