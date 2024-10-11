package scraperapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/zenrows/zenrows-go-sdk/service/api/pkg/problem"
)

// Response struct holds response values of executed requests.
type Response struct {
	// RawResponse is the original `*http.Response` object.
	RawResponse *http.Response

	res *resty.Response
}

// Body method returns the HTTP response as `[]byte` slice for the executed request.
func (r *Response) Body() []byte {
	return r.res.Body()
}

// Status method returns the HTTP status string for the executed request.
//
//	Example: 200 OK
func (r *Response) Status() string {
	return r.res.Status()
}

// StatusCode method returns the HTTP status code for the executed request.
//
//	Example: 200
func (r *Response) StatusCode() int {
	return r.res.StatusCode()
}

// Header method returns the response headers
func (r *Response) Header() http.Header {
	return r.res.Header()
}

// String method returns the body of the HTTP response as a `string`.
// It returns an empty string if it is nil or the body is zero length.
func (r *Response) String() string {
	return r.res.String()
}

// Problem method returns the problem description of the HTTP response if any.
func (r *Response) Problem() *problem.Problem {
	if r.IsError() && r.Header().Get("Content-Type") == problem.ContentTypeJSON {
		var prob *problem.Problem
		if err := json.Unmarshal(r.Body(), &prob); err == nil {
			return prob
		}
	}

	return nil
}

// Error method returns the error message of the HTTP response if any.
func (r *Response) Error() error {
	if prob := r.Problem(); prob != nil {
		return prob
	}

	return nil
}

// Time method returns the duration of HTTP response time from the request we sent
// and received a request.
//
// See [Response.ReceivedAt] to know when the client received a response.
func (r *Response) Time() time.Duration {
	return r.res.Time()
}

// ReceivedAt method returns the time we received a response from the server for the request.
func (r *Response) ReceivedAt() time.Time {
	return r.res.ReceivedAt()
}

// Size method returns the HTTP response size in bytes.
func (r *Response) Size() int64 {
	return r.res.Size()
}

// IsSuccess method returns true if HTTP status `code >= 200 and <= 299` otherwise false.
func (r *Response) IsSuccess() bool {
	return r.res.IsSuccess()
}

// IsError method returns true if HTTP status `code >= 400` otherwise false.
func (r *Response) IsError() bool {
	return r.res.IsError()
}

// TargetHeaders method to returns all the response headers that the target page has set, if any. ZenRows Scraper API encodes these headers
// with a "Z-" prefix, so this method filters out all headers that do not have this prefix.
//
// To get all the headers, see the [Response.Headers] field.
func (r *Response) TargetHeaders() http.Header {
	targetPageHeaders := make(http.Header)
	for k, v := range r.Header() {
		if strings.HasPrefix(k, "Z-") {
			targetPageHeaders[k] = v
		}
	}
	return targetPageHeaders
}

// TargetCookies method to returns all the response cookies that the target page has set, if any.
func (r *Response) TargetCookies() []*http.Cookie {
	cookieCount := len(r.Header()["Z-Set-Cookie"])
	if cookieCount == 0 {
		return []*http.Cookie{}
	}
	cookies := make([]*http.Cookie, 0, cookieCount)
	for _, line := range r.Header()["Z-Set-Cookie"] {
		if cookie, err := http.ParseSetCookie(line); err == nil {
			cookies = append(cookies, cookie)
		}
	}
	return cookies
}
