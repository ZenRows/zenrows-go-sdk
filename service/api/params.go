package scraperapi

import (
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"strings"

	"github.com/fatih/structs"
	"github.com/gorilla/schema"
)

// decoder is a schema decoder that will be used to decode the query parameters into a RequestParameters object.
var decoder = schema.NewDecoder()

// validHTTPMethods is a list of valid HTTP methods that can be used in a request.
var validHTTPMethods = []string{http.MethodGet, http.MethodPost, http.MethodPut}

// ResponseType represents the type of response that the ZenRows Scraper API should return.
type ResponseType string

const (
	ResponseTypeMarkdown  ResponseType = "markdown"
	ResponseTypePlainText ResponseType = "plaintext"
	ResponseTypePDF       ResponseType = "pdf"
)

var AllResponseTypes = map[ResponseType]struct{}{
	ResponseTypeMarkdown:  {},
	ResponseTypePlainText: {},
	ResponseTypePDF:       {},
}

type OutputType string

const (
	OutputTypeEmails       OutputType = "emails"
	OutputTypePhoneNumbers OutputType = "phone_numbers"
	OutputTypeHeadings     OutputType = "headings"
	OutputTypeImages       OutputType = "images"
	OutputTypeAudios       OutputType = "audios"
	OutputTypeVideos       OutputType = "videos"
	OutputTypeLinks        OutputType = "links"
	OutputTypeTables       OutputType = "tables"
	OutputTypeMenus        OutputType = "menus"
	OutputTypeHashtags     OutputType = "hashtags"
	OutputTypeMetadata     OutputType = "metadata"
	OutputTypeFavicon      OutputType = "favicon"
	OutputTypeAll          OutputType = "*"
)

var AllOutputTypes = map[OutputType]struct{}{
	OutputTypeEmails:       {},
	OutputTypePhoneNumbers: {},
	OutputTypeHeadings:     {},
	OutputTypeImages:       {},
	OutputTypeAudios:       {},
	OutputTypeVideos:       {},
	OutputTypeLinks:        {},
	OutputTypeTables:       {},
	OutputTypeMenus:        {},
	OutputTypeHashtags:     {},
	OutputTypeMetadata:     {},
	OutputTypeFavicon:      {},
	OutputTypeAll:          {},
}

type ScreenshotFormat string

const (
	ScreenshotFormatPNG  ScreenshotFormat = "png"
	ScreenshotFormatJPEG ScreenshotFormat = "jpeg"
)

var AllScreenshotFormats = map[ScreenshotFormat]struct{}{
	ScreenshotFormatPNG:  {},
	ScreenshotFormatJPEG: {},
}

type ResourceType string

const (
	ResourceTypeEventSource ResourceType = "eventsource"
	ResourceTypeFetch       ResourceType = "fetch"
	ResourceTypeFont        ResourceType = "font"
	ResourceTypeImage       ResourceType = "image"
	ResourceTypeManifest    ResourceType = "manifest"
	ResourceTypeMedia       ResourceType = "media"
	ResourceTypeOther       ResourceType = "other"
	ResourceTypeScript      ResourceType = "script"
	ResourceTypeStylesheet  ResourceType = "stylesheet"
	ResourceTypeTextTrack   ResourceType = "texttrack"
	ResourceTypeWebSocket   ResourceType = "websocket"
	ResourceTypeXHR         ResourceType = "xhr"
)

var AllResourceTypes = map[ResourceType]struct{}{
	ResourceTypeEventSource: {},
	ResourceTypeFetch:       {},
	ResourceTypeFont:        {},
	ResourceTypeImage:       {},
	ResourceTypeManifest:    {},
	ResourceTypeMedia:       {},
	ResourceTypeOther:       {},
	ResourceTypeScript:      {},
	ResourceTypeStylesheet:  {},
	ResourceTypeTextTrack:   {},
	ResourceTypeWebSocket:   {},
	ResourceTypeXHR:         {},
}

// RequestParameters represents the parameters that can be passed to the ZenRows Scraper API when making a request to modify the behavior
// of the scraping engine.
//
// See https://docs.zenrows.com/scraper-api/api-reference for more information.
type RequestParameters struct {
	// Proxy settings
	UsePremiumProxies bool   `json:"premium_proxy,omitempty" structs:"premium_proxy,omitempty" schema:"premium_proxy"`
	ProxyCountry      string `json:"proxy_country,omitempty" structs:"proxy_country,omitempty" schema:"proxy_country"`

	// Output modifiers
	AutoParse    bool         `json:"autoparse,omitempty" structs:"autoparse,omitempty" schema:"autoparse"`
	CSSExtractor string       `json:"css_extractor,omitempty" structs:"css_extractor,omitempty" schema:"css_extractor"`
	JSONResponse bool         `json:"json_response,omitempty" structs:"json_response,omitempty" schema:"json_response"`
	ResponseType ResponseType `json:"response_type,omitempty" structs:"response_type,omitempty" schema:"response_type"`
	Outputs      []OutputType `json:"outputs,omitempty" structs:"outputs,omitempty" schema:"outputs"`

	////////////////////////////////////////////////
	// Headless settings
	////////////////////////////////////////////////

	// JSRender enables JavaScript rendering for the request. If not enabled, the request will be processed by the standard scraping engine,
	// which is faster but does not execute JavaScript and may not bypass some anti-bot systems.
	//
	// See https://docs.zenrows.com/scraper-api/features/js-rendering for more information.
	JSRender bool `json:"js_render,omitempty" structs:"js_render,omitempty" schema:"js_render"`

	// JSInstructions is a serialized JSON object that contains custom JavaScript instructions that will be executed in the page before
	// returning the response (only available when using JSRender).
	//
	// See https://docs.zenrows.com/scraper-api/features/js-rendering#using-the-javascript-instructions for more information.
	JSInstructions string `json:"js_instructions,omitempty" structs:"js_instructions,omitempty" schema:"js_instructions"`

	// WaitMilliseconds will wait for the specified number of milliseconds before returning the response (only available when
	// using JSRender). The maximum wait time is 30 seconds (30000 ms).
	WaitMilliseconds int `json:"wait,omitempty" structs:"wait,omitempty" schema:"wait"`

	// WaitForSelector will wait for the specified element to appear in the page before returning the response (only available when
	// using JSRender).
	//
	// See https://docs.zenrows.com/scraper-api/features/js-rendering#wait-for-selector for more information.
	//
	// IMPORTANT: Make sure that the element you are waiting for is present in the page. If the element does not appear, the request will
	// fail by a timeout error after a few seconds.
	WaitForSelector string `json:"wait_for,omitempty" structs:"wait_for,omitempty" schema:"wait_for"`

	// Screenshot will return a screenshot of the page (only available when using JSRender)
	Screenshot bool `json:"screenshot,omitempty" structs:"screenshot,omitempty" schema:"screenshot"`

	// ScreenshotFullPage will take a screenshot of the full page (only available when using JSRender and Screenshot is set to true)
	ScreenshotFullPage bool `json:"screenshot_fullpage,omitempty" structs:"screenshot_fullpage,omitempty" schema:"screenshot_fullpage"`

	// ScreenshotSelector will take a screenshot of the specified element (only available when using JSRender and Screenshot is set to true)
	ScreenshotSelector string `json:"screenshot_selector,omitempty" structs:"screenshot_selector,omitempty" schema:"screenshot_selector"`

	// ScreenshotFormat will set the format of the screenshot (only available when using JSRender and Screenshot is set to true).
	// The available formats are ScreenshotFormatPNG and ScreenshotFormatJPEG. The default format is ScreenshotFormatPNG.
	ScreenshotFormat ScreenshotFormat `json:"screenshot_format,omitempty" structs:"screenshot_format,omitempty" schema:"screenshot_format"`

	// ScreenshotQuality will set the quality of the screenshot (only available when using JSRender and Screenshot is set to true, and
	// the format is ScreenshotFormatJPEG). The quality must be between 1 and 100. The default quality is 100.
	ScreenshotQuality int `json:"screenshot_quality,omitempty" structs:"screenshot_quality,omitempty" schema:"screenshot_quality"`

	////////////////////////////////////////////////
	// Advanced settings - USE WITH CAUTION
	////////////////////////////////////////////////

	// ReturnOriginalStatus will return the original status code of the response wthen the request is not successful. When a request is not
	// successful, the ZenRows Scraper API will always return a 422 status code. If you enable this feature, the original status code will
	// be returned instead.
	ReturnOriginalStatus bool `json:"original_status,omitempty" structs:"original_status,omitempty" schema:"original_status"`

	// SessionID is an integer between 0 and 99999 that can be used to group requests together. If you provide a SessionID, all requests
	// with the same SessionID will use the same IP address for up to 10 minutes. This feature is useful for web scraping sites that track
	// sessions or limit IP rotation. It helps simulate a persistent session and avoids triggering anti-bot systems that flag
	// frequent IP changes.
	//
	// See https://docs.zenrows.com/scraper-api/features/other#session-id for more information.
	//
	// IMPORTANT: Use this feature only if you know what you are doing. If you provide a SessionID, the IP rotation feature will be disabled
	// for all requests with the same SessionID. This may affect the scraping quality and increase the chances of being blocked.
	SessionID int `json:"session_id,omitempty" structs:"session_id,omitempty" schema:"session_id"`

	// AllowedStatusCodes will return the response body of a request even if the status code is not a successful one (2xx), but
	// is one of the specified status codes in this list.
	//
	// See https://docs.zenrows.com/scraper-api/features/other#return-content-on-error for more information.
	//
	// IMPORTANT: ZenRows Scraper API only charges for successful requests. If you use this feature, you will also be charged for
	// unsuccessful requests matching the specified status codes.
	AllowedStatusCodes []int `json:"allowed_status_codes,omitempty" structs:"allowed_status_codes,omitempty" schema:"allowed_status_codes"`

	// BlockResources will block the specified resources from loading (only available when using JSRender)
	//
	// See https://docs.zenrows.com/scraper-api/features/js-rendering#block-resources for more information.
	//
	// IMPORTANT: ZenRows Scraper API already blocks some resources by default to improve the scraping quality. Use this feature only if you
	// know what you are doing.
	BlockResources []ResourceType `json:"block_resources,omitempty" structs:"block_resources,omitempty" schema:"block_resources"`

	// CustomHeaders is a http.Header object that will be used to set custom headers in the request.
	//
	// See https://docs.zenrows.com/scraper-api/features/headers for more information.
	//
	// IMPORTANT: ZenRows Scraper API already rotates and selects the best combination of headers (like User-Agent, Accept-Language, etc.)
	// automatically for each request. If you provide custom headers, the scraping quality may be affected. Use this feature only if you
	// know what you are doing.
	CustomHeaders http.Header `json:"custom_headers,omitempty" structs:"-" schema:"-"`
}

func (p *RequestParameters) Validate() error { //nolint:gocyclo
	if p.ScreenshotQuality < 0 || p.ScreenshotQuality > 100 {
		return InvalidParameterError{Msg: "screenshot quality must be between 1 and 100"}
	}

	if p.SessionID < 0 || p.SessionID > 99_999 {
		return InvalidParameterError{Msg: "session id must be between 0 and 99999"}
	}

	if p.WaitMilliseconds < 0 || p.WaitMilliseconds > 30_000 {
		return InvalidParameterError{Msg: "wait must be between 0 and 30000 (ms)"}
	}

	if p.ResponseType != "" {
		if _, ok := AllResponseTypes[p.ResponseType]; !ok {
			return InvalidParameterError{Msg: "invalid response type"}
		}
	}

	if p.ScreenshotFormat != "" {
		if _, ok := AllScreenshotFormats[p.ScreenshotFormat]; !ok {
			return InvalidParameterError{Msg: "invalid screenshot format"}
		}
	}

	for _, output := range p.Outputs {
		if _, ok := AllOutputTypes[output]; !ok {
			return InvalidParameterError{Msg: "invalid output type"}
		}
	}

	for _, resource := range p.BlockResources {
		if _, ok := AllResourceTypes[resource]; !ok {
			return InvalidParameterError{Msg: "invalid resource type"}
		}
	}

	if !p.JSRender {
		if p.Screenshot {
			return InvalidParameterError{Msg: "screenshot is only available when using javascript rendering"}
		}
		if p.JSInstructions != "" {
			return InvalidParameterError{Msg: "js_instructions is only available when using javascript rendering"}
		}
		if p.WaitMilliseconds > 0 {
			return InvalidParameterError{Msg: "wait is only available when using javascript rendering"}
		}
		if p.WaitForSelector != "" {
			return InvalidParameterError{Msg: "wait_for is only available when using javascript rendering"}
		}
		if len(p.BlockResources) > 0 {
			return InvalidParameterError{Msg: "block_resources is only available when using javascript rendering"}
		}
	}

	if !p.Screenshot {
		if p.ScreenshotFullPage {
			return InvalidParameterError{Msg: "screenshot_fullpage is only available when screenshot parameter is set to true"}
		}
		if p.ScreenshotSelector != "" {
			return InvalidParameterError{Msg: "screenshot_selector is only available when screenshot parameter is set to true"}
		}
		if p.ScreenshotFormat != "" {
			return InvalidParameterError{Msg: "screenshot_format is only available when screenshot parameter is set to true"}
		}
		if p.ScreenshotQuality > 0 {
			return InvalidParameterError{Msg: "screenshot_quality is only available when screenshot parameter is set to true"}
		}
	}

	if p.ScreenshotQuality > 0 && p.ScreenshotFormat != ScreenshotFormatJPEG {
		return InvalidParameterError{Msg: "screenshot_quality is only available when screenshot_format is set to jpeg"}
	}

	if p.ProxyCountry != "" && !p.UsePremiumProxies {
		return InvalidParameterError{Msg: "proxy country is only available when using premium proxies"}
	}

	return nil
}

// ToURLValues converts the RequestParameters to a url.Values object
func (p *RequestParameters) ToURLValues() url.Values {
	values := make(url.Values)
	for k, v := range structs.Map(p) {
		rv := reflect.ValueOf(v)
		if rv.Kind() == reflect.Slice {
			var strValues []string
			for i := 0; i < rv.Len(); i++ {
				strValues = append(strValues, fmt.Sprintf("%v", rv.Index(i)))
			}
			values.Set(k, strings.Join(strValues, ","))
		} else {
			values.Set(k, fmt.Sprintf("%v", v))
		}
	}

	// if custom headers are set, we need to set the custom_headers flag to true
	if len(p.CustomHeaders) > 0 {
		values.Set("custom_headers", "true")
	}

	return values
}

// ParseQueryRequestParameters parses the provided url.Values object and returns a RequestParameters object, or an error if the parsing
// fails.
func ParseQueryRequestParameters(query url.Values) (*RequestParameters, error) {
	var requestParameters RequestParameters
	if err := decoder.Decode(&requestParameters, query); err != nil {
		return nil, err
	}

	return &requestParameters, nil
}

func init() {
	decoder.RegisterConverter([]ResourceType{}, func(input string) reflect.Value {
		var resourceTypes []ResourceType
		for _, resourceType := range strings.Split(input, ",") {
			resourceTypes = append(resourceTypes, ResourceType(resourceType))
		}
		return reflect.ValueOf(resourceTypes)
	})
	decoder.RegisterConverter([]OutputType{}, func(input string) reflect.Value {
		var outputTypes []OutputType
		for _, outputType := range strings.Split(input, ",") {
			outputTypes = append(outputTypes, OutputType(outputType))
		}
		return reflect.ValueOf(outputTypes)
	})
}
