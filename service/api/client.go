package scraperapi

import (
	"context"
	"math"
	"math/rand/v2"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/zenrows/zenrows-go-sdk/service/api/version"
)

const (
	apiKeyParamName = "apikey"
	urlParamName    = "url"
)

// Client is the ZenRows Fetch API client
type Client struct {
	cfg                  options
	http                 *resty.Client
	concurrencySemaphore chan struct{}
	// optionsErr is the error every scrape call returns when an option was invalid, so no request is sent.
	optionsErr error
	// now and randN are the clock and the random source of the retry waits; tests replace them.
	now   func() time.Time
	randN func(time.Duration) time.Duration
}

// NewClient creates and returns a new ZenRows Fetch API client. When an option has an invalid value,
// every scrape call on the client (Get, Post, Put, Fetch, Extract and Scrape) returns an
// InvalidOptionError and sends nothing.
func NewClient(opts ...Option) *Client {
	client := &Client{cfg: defaultOptions(), now: time.Now, randN: rand.N[time.Duration]}

	for _, opt := range opts {
		opt.apply(&client.cfg)
	}
	client.optionsErr = client.cfg.retryOptions.validate()

	client.http = resty.New().
		SetLogger(noopLogger{}).
		SetBaseURL(client.cfg.baseURL).
		SetHeader("User-Agent", "zenrows-go/"+version.Version).
		SetQueryParam(apiKeyParamName, client.cfg.apiKey).
		SetRetryCount(max(client.cfg.retryOptions.maxRetryCount, 0)).
		// retryAfter returns the wait shouldRetry chose, and resty clamps that wait to
		// [RetryWaitTime, RetryMaxWaitTime]. retryWait already bounds it, so the clamp is opened wide.
		SetRetryWaitTime(0).
		SetRetryMaxWaitTime(math.MaxInt64).
		SetRetryAfter(retryAfter).
		AddRetryCondition(client.shouldRetry)

	// if the maxConcurrentRequests is set, create a semaphore to limit the number of concurrent requests
	if client.cfg.maxConcurrentRequests > 0 {
		client.concurrencySemaphore = make(chan struct{}, client.cfg.maxConcurrentRequests)
	}

	return client
}

// isConfigured returns true if the client is configured with a base url and a secret key
func (c *Client) isConfigured() bool {
	return c.cfg.baseURL != "" && c.cfg.apiKey != ""
}

// Scrape sends a request to the ZenRows Fetch API to scrape the given target URL using the specified method and parameters.
func (c *Client) Scrape(ctx context.Context, method, targetURL string, params *RequestParameters, body any) (*Response, error) {
	// make sure every option is valid before sending the request
	if c.optionsErr != nil {
		return nil, c.optionsErr
	}

	// make sure the client is configured before sending the request
	if !c.isConfigured() {
		return nil, NotConfiguredError{}
	}

	// make sure the method is valid
	if !slices.Contains(validHTTPMethods, method) {
		return nil, InvalidHTTPMethodError{}
	}

	// make sure a target url is provided
	if targetURL == "" {
		return nil, InvalidTargetURLError{Msg: "target url cannot be empty"}
	}

	// make sure the target url is a valid url
	parsedURL, parseErr := url.Parse(targetURL)
	if parseErr != nil {
		return nil, InvalidTargetURLError{URL: targetURL, Err: parseErr}
	}

	// create the request
	req := c.http.R().SetContext(ctx).SetQueryParam(urlParamName, parsedURL.String()).SetBody(body)

	// if parameters are provided, validate them and set them on the request
	if params != nil {
		if err := params.Validate(); err != nil {
			return nil, err
		}

		req.SetHeaderMultiValues(params.CustomHeaders)
		req.SetQueryParamsFromValues(params.ToURLValues())
	}

	// if the concurrency semaphore is initialized, acquire a token before sending the request
	// and release it after the request is done
	if c.concurrencySemaphore != nil {
		c.concurrencySemaphore <- struct{}{}
		defer func() { <-c.concurrencySemaphore }()
	}

	// execute the request, and return the response or an error if one occurred. The retry budget
	// starts now, after any wait for a concurrency slot.
	ctx = c.withRetryCall(ctx, isReplayable(method, req.Header, body))
	res, err := req.SetContext(ctx).Execute(method, "/")
	if err != nil {
		return nil, err
	}
	return &Response{res: res}, nil
}

// Get sends an HTTP GET request to the ZenRows Fetch API to scrape the given target URL using the specified parameters.
func (c *Client) Get(ctx context.Context, targetURL string, params *RequestParameters) (*Response, error) {
	return c.Scrape(ctx, http.MethodGet, targetURL, params, nil)
}

// Fetch sends an HTTP GET request to scrape the given target URL — an alias for Get, named to
// match ZenRows' Fetch product naming for the main page-scraping product, for parity with the
// other ZenRows SDKs.
func (c *Client) Fetch(ctx context.Context, targetURL string, params *RequestParameters) (*Response, error) {
	return c.Get(ctx, targetURL, params)
}

// Extract fetches the given target URL and runs it through Extract — ZenRows' AI-powered
// structured extraction. If params.Extract is unset, it defaults to
// ExtractModeAuto. This is a thin wrapper over Get with the Extract param set — no separate
// endpoint or auth.
//
// Extract works on any prepared domain (you can prepare one yourself with
// POST /v1/extract/prepared-domains), with any active API key. A 403 with problem code
// REQS007 means Extract can't extract this page. Check Reason in the problem body
// (Response.Problem().Reason) when present; older responses may not carry it:
//
//   - "domain_not_prepared": prepare the domain with Client.PrepareDomain, poll
//     Client.PreparedDomainStatus until Status is "ready", then retry.
//   - "page_not_recognised": the domain is served, but this page isn't one Extract extracts
//     (a home, search, login or category page). Try a product, article or listing page, or
//     retry once in a few minutes. Preparing the domain again changes nothing.
//   - "domain_not_enabled": preparing doesn't help. Use CSSExtractor, or contact support.
//
// The legacy domain-not-enabled error is a 402 with problem code AUTH010. Extract returns
// both responses to you as is, in one request; it never retries them with AutoParse.
//
// Extract also sends Adaptive Stealth Mode (wire param "mode=auto") by default, so a target
// needing JSRender/UsePremiumProxies (e.g. a site with heavy anti-bot defenses) escalates
// automatically instead of failing with REQS002. It is skipped when params sets JSRender or
// UsePremiumProxies (the API rejects either combined with Adaptive Stealth Mode) or its own
// Mode. Set params.DisableAdaptiveStealth to turn it off entirely.
//
// A 422 with problem code no_data_extracted means Extract knows this kind of page but found
// no data on it. It is not charged and is never retried; check it with
// Response.IsNoDataExtracted.
func (c *Client) Extract(ctx context.Context, targetURL string, params *RequestParameters) (*Response, error) {
	extractParams := RequestParameters{}
	if params != nil {
		extractParams = *params
	}
	if extractParams.Extract == "" {
		extractParams.Extract = ExtractModeAuto
	}
	if addsAdaptiveStealth(&extractParams) {
		extractParams.CustomParams = withCustomParam(extractParams.CustomParams, "mode", "auto")
	}

	return c.Get(ctx, targetURL, &extractParams)
}

// addsAdaptiveStealth reports whether Extract should add the wire param "mode=auto". It
// doesn't when the caller turned it off, already chose a mode (Mode or a "mode" custom
// param), or forces JSRender / UsePremiumProxies (also as "js_render" / "premium_proxy"
// custom params): the API rejects either one combined with Adaptive Stealth Mode.
func addsAdaptiveStealth(p *RequestParameters) bool {
	if p.DisableAdaptiveStealth || p.Mode != "" || p.JSRender || p.UsePremiumProxies {
		return false
	}
	if _, ok := p.CustomParams["mode"]; ok {
		return false
	}
	for _, key := range []string{"js_render", "premium_proxy"} {
		if strings.EqualFold(strings.TrimSpace(p.CustomParams[key]), "true") {
			return false
		}
	}
	return true
}

// withCustomParam returns a copy of params with key=value added, without mutating the
// caller's map.
func withCustomParam(params map[string]string, key, value string) map[string]string {
	merged := make(map[string]string, len(params)+1)
	for k, v := range params {
		merged[k] = v
	}
	merged[key] = value
	return merged
}

// Post sends an HTTP POST request to the ZenRows Fetch API to scrape the given target URL using the specified parameters.
func (c *Client) Post(ctx context.Context, targetURL string, params *RequestParameters, body any) (*Response, error) {
	return c.Scrape(ctx, http.MethodPost, targetURL, params, body)
}

// Put sends an HTTP PUT request to the ZenRows Fetch API to scrape the given target URL using the specified parameters.
func (c *Client) Put(ctx context.Context, targetURL string, params *RequestParameters, body any) (*Response, error) {
	return c.Scrape(ctx, http.MethodPut, targetURL, params, body)
}
