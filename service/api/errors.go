package scraperapi

import (
	"fmt"
	"strings"
)

// NotConfiguredError results when the ZenRows Fetch API client is used without a valid API Key.
type NotConfiguredError struct{}

func (NotConfiguredError) Error() string {
	return "zenrows fetch client is not configured"
}

// InvalidHTTPMethodError results when the ZenRows Fetch API client is used with an invalid HTTP method.
type InvalidHTTPMethodError struct{}

func (InvalidHTTPMethodError) Error() string {
	return fmt.Sprintf("invalid http method. supported methods are: %s", strings.Join(validHTTPMethods, ", "))
}

// InvalidTargetURLError results when the ZenRows Fetch API client is used with an invalid target URL.
type InvalidTargetURLError struct {
	URL string
	Msg string
	Err error
}

func (e InvalidTargetURLError) Unwrap() error {
	return e.Err
}

func (e InvalidTargetURLError) Error() string {
	if e.Msg == "" {
		e.Msg = "invalid target url"
	}

	if e.Err != nil {
		return e.Msg + ": " + e.Err.Error()
	}

	return e.Msg
}

// InvalidParameterError results when the ZenRows Fetch API client is used with an invalid parameter.
type InvalidParameterError struct {
	Msg string
}

func (e InvalidParameterError) Error() string {
	if e.Msg == "" {
		e.Msg = "invalid parameter"
	}

	return e.Msg
}

// InvalidOptionError results when the ZenRows Fetch API client was created with an invalid retry option value. NewClient
// does not return an error, so every scrape call on such a client (Get, Post, Put, Fetch, Extract and Scrape) returns
// this error and sends no request. PrepareDomain and PreparedDomainStatus do not use the retry options and do not
// return it.
type InvalidOptionError struct {
	Msg string
}

func (e InvalidOptionError) Error() string {
	if e.Msg == "" {
		e.Msg = "invalid option"
	}

	return e.Msg
}
