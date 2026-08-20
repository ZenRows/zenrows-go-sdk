package scraperapi

import "context"

//go:generate mockery
type IClient interface {
	// Scrape sends a request to the ZenRows Fetch API to scrape the given target URL using the specified method and parameters.
	Scrape(ctx context.Context, targetURL, method string, params RequestParameters) (*Response, error)
	// Get sends a GET request to the ZenRows Fetch API to scrape the given target URL using the specified parameters.
	Get(ctx context.Context, targetURL string, params RequestParameters) (*Response, error)
	// Fetch sends a GET request to scrape the given target URL — an alias for Get, named to match
	// ZenRows' "Fetch" product naming and for parity with the other ZenRows SDKs.
	Fetch(ctx context.Context, targetURL string, params RequestParameters) (*Response, error)
	// Extract fetches the given target URL and runs it through Extract, ZenRows' AI-powered
	// structured extraction (beta).
	Extract(ctx context.Context, targetURL string, params RequestParameters) (*Response, error)
	// Post sends a POST request to the ZenRows Fetch API to scrape the given target URL using the specified parameters.
	Post(ctx context.Context, targetURL string, params RequestParameters) (*Response, error)
	// Put sends a PUT request to the ZenRows Fetch API to scrape the given target URL using the specified parameters.
	Put(ctx context.Context, targetURL string, params RequestParameters) (*Response, error)
}
