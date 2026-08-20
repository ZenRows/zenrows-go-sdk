package scraperapi

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"strings"

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
}

// NewClient creates and returns a new ZenRows Fetch API client
func NewClient(opts ...Option) *Client {
	client := &Client{cfg: defaultOptions()}

	for _, opt := range opts {
		opt.apply(&client.cfg)
	}

	client.http = resty.New().
		SetLogger(noopLogger{}).
		SetBaseURL(client.cfg.baseURL).
		SetHeader("User-Agent", "zenrows-go/"+version.Version).
		SetQueryParam(apiKeyParamName, client.cfg.apiKey).
		SetRetryCount(client.cfg.retryOptions.maxRetryCount).
		SetRetryWaitTime(client.cfg.retryOptions.retryWaitTime).
		SetRetryMaxWaitTime(client.cfg.retryOptions.retryMaxWaitTime).
		AddRetryCondition(func(r *resty.Response, err error) bool {
			return err != nil || slices.Contains(retryableStatusCodes, r.StatusCode())
		})

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

	// execute the request, and return the response or an error if one occurred
	res, err := req.Execute(method, "/")
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
// structured extraction (beta). If params.Extract is unset, it defaults to
// ExtractModeAuto. This is a thin wrapper over Get with the Extract param set — no separate
// endpoint or auth.
//
// ExtractModeAuto is a domain-gated open beta: when the target domain isn't enabled yet,
// the API returns a 402 with problem code AUTH010. By default Extract catches that and
// retries once with AutoParse set instead of returning the error response — set
// params.DisableAutoparseFallback to get the raw AUTH010 response back instead.
//
// Extract also sends Adaptive Stealth Mode (wire param "mode=auto") by default on both the
// extract attempt and the AutoParse fallback, so a target needing JSRender/
// UsePremiumProxies (e.g. a site with heavy anti-bot defenses) escalates automatically
// instead of failing with REQS002. Set params.DisableAdaptiveStealth to disable that and
// set JSRender/UsePremiumProxies yourself.
func (c *Client) Extract(ctx context.Context, targetURL string, params *RequestParameters) (*Response, error) {
	extractParams := RequestParameters{}
	if params != nil {
		extractParams = *params
	}
	if extractParams.Extract == "" {
		extractParams.Extract = ExtractModeAuto
	}
	if !extractParams.DisableAdaptiveStealth {
		extractParams.CustomParams = withCustomParam(extractParams.CustomParams, "mode", "auto")
	}

	response, err := c.Get(ctx, targetURL, &extractParams)
	if err != nil {
		return response, err
	}

	if response.StatusCode() == http.StatusPaymentRequired &&
		extractParams.Extract == ExtractModeAuto &&
		!extractParams.DisableAutoparseFallback &&
		isAuth010(response) {
		autoparseParams := extractParams
		autoparseParams.Extract = ""
		autoparseParams.AutoParse = true
		return c.Get(ctx, targetURL, &autoparseParams)
	}

	return response, nil
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

// isAuth010 reports whether a response's problem envelope carries the Extract
// domain-not-enabled code (AUTH010).
func isAuth010(response *Response) bool {
	prob := response.Problem()
	return prob != nil && strings.EqualFold(prob.Code, "AUTH010")
}

// Post sends an HTTP POST request to the ZenRows Fetch API to scrape the given target URL using the specified parameters.
func (c *Client) Post(ctx context.Context, targetURL string, params *RequestParameters, body any) (*Response, error) {
	return c.Scrape(ctx, http.MethodPost, targetURL, params, body)
}

// Put sends an HTTP PUT request to the ZenRows Fetch API to scrape the given target URL using the specified parameters.
func (c *Client) Put(ctx context.Context, targetURL string, params *RequestParameters, body any) (*Response, error) {
	return c.Scrape(ctx, http.MethodPut, targetURL, params, body)
}
