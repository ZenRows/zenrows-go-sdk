package scraperapi

import (
	"context"
	"net/http"
	"net/url"
	"slices"

	"github.com/go-resty/resty/v2"
	"github.com/zenrows/zenrows-go-sdk/service/api/version"
)

const (
	apiKeyParamName = "apikey"
	urlParamName    = "url"
)

// Client is the ZenRows Scraper API client
type Client struct {
	cfg                  options
	http                 *resty.Client
	concurrencySemaphore chan struct{}
}

// NewClient creates and returns a new ZenRows Scraper API client
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

// Scrape sends a request to the ZenRows Scraper API to scrape the given target URL using the specified method and parameters.
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

// Get sends an HTTP GET request to the ZenRows Scraper API to scrape the given target URL using the specified parameters.
func (c *Client) Get(ctx context.Context, targetURL string, params *RequestParameters) (*Response, error) {
	return c.Scrape(ctx, http.MethodGet, targetURL, params, nil)
}

// Post sends an HTTP POST request to the ZenRows Scraper API to scrape the given target URL using the specified parameters.
func (c *Client) Post(ctx context.Context, targetURL string, params *RequestParameters, body any) (*Response, error) {
	return c.Scrape(ctx, http.MethodPost, targetURL, params, body)
}

// Put sends an HTTP PUT request to the ZenRows Scraper API to scrape the given target URL using the specified parameters.
func (c *Client) Put(ctx context.Context, targetURL string, params *RequestParameters, body any) (*Response, error) {
	return c.Scrape(ctx, http.MethodPut, targetURL, params, body)
}
