package scraperapi_test

import (
	"net/http"
	"net/url"
	"testing"

	scraperapi "github.com/zenrows/zenrows-go-sdk/service/api"
)

func TestValidateAcceptsSensibleDefaults(t *testing.T) {
	p := &scraperapi.RequestParameters{}
	if err := p.Validate(); err != nil {
		t.Fatalf("expected zero-value parameters to be valid, got: %v", err)
	}
}

func TestValidateRejectsOutOfRangeScreenshotQuality(t *testing.T) {
	for _, quality := range []int{-1, 101} {
		p := &scraperapi.RequestParameters{Screenshot: true, ScreenshotFormat: scraperapi.ScreenshotFormatJPEG, ScreenshotQuality: quality}
		if err := p.Validate(); err == nil {
			t.Fatalf("quality %d: expected an error", quality)
		}
	}
}

func TestValidateRejectsOutOfRangeSessionID(t *testing.T) {
	for _, id := range []int{-1, 100_000} {
		p := &scraperapi.RequestParameters{SessionID: id}
		if err := p.Validate(); err == nil {
			t.Fatalf("session id %d: expected an error", id)
		}
	}
}

func TestValidateRejectsOutOfRangeWait(t *testing.T) {
	p := &scraperapi.RequestParameters{JSRender: true, WaitMilliseconds: 30_001}
	if err := p.Validate(); err == nil {
		t.Fatal("expected an error for wait over 30000ms")
	}
}

func TestValidateRejectsUnknownResponseType(t *testing.T) {
	p := &scraperapi.RequestParameters{ResponseType: "xml"}
	if err := p.Validate(); err == nil {
		t.Fatal("expected an error for an unknown response type")
	}
}

func TestValidateRejectsUnknownScreenshotFormat(t *testing.T) {
	p := &scraperapi.RequestParameters{Screenshot: true, ScreenshotFormat: "gif"}
	if err := p.Validate(); err == nil {
		t.Fatal("expected an error for an unknown screenshot format")
	}
}

func TestValidateRejectsUnknownOutputType(t *testing.T) {
	p := &scraperapi.RequestParameters{Outputs: []scraperapi.OutputType{"bogus"}}
	if err := p.Validate(); err == nil {
		t.Fatal("expected an error for an unknown output type")
	}
}

func TestValidateRejectsUnknownResourceType(t *testing.T) {
	p := &scraperapi.RequestParameters{JSRender: true, BlockResources: []scraperapi.ResourceType{"bogus"}}
	if err := p.Validate(); err == nil {
		t.Fatal("expected an error for an unknown resource type")
	}
}

func TestValidateRejectsJSRenderOnlyFieldsWithoutJSRender(t *testing.T) {
	cases := map[string]*scraperapi.RequestParameters{
		"screenshot":      {Screenshot: true},
		"js_instructions": {JSInstructions: "[]"},
		"wait":            {WaitMilliseconds: 100},
		"wait_for":        {WaitForSelector: "#id"},
		"block_resources": {BlockResources: []scraperapi.ResourceType{scraperapi.ResourceTypeImage}},
	}
	for name, p := range cases {
		if err := p.Validate(); err == nil {
			t.Fatalf("%s: expected an error when JSRender is not enabled", name)
		}
	}
}

func TestValidateAllowsJSRenderOnlyFieldsWithJSRender(t *testing.T) {
	p := &scraperapi.RequestParameters{
		JSRender:         true,
		Screenshot:       true,
		ScreenshotFormat: scraperapi.ScreenshotFormatPNG,
		JSInstructions:   "[]",
		WaitMilliseconds: 100,
		WaitForSelector:  "#id",
		BlockResources:   []scraperapi.ResourceType{scraperapi.ResourceTypeImage},
	}
	if err := p.Validate(); err != nil {
		t.Fatalf("expected no error with JSRender enabled, got: %v", err)
	}
}

func TestValidateRejectsScreenshotOnlyFieldsWithoutScreenshot(t *testing.T) {
	cases := map[string]*scraperapi.RequestParameters{
		"screenshot_fullpage": {ScreenshotFullPage: true},
		"screenshot_selector": {ScreenshotSelector: "#id"},
		"screenshot_format":   {ScreenshotFormat: scraperapi.ScreenshotFormatPNG},
		"screenshot_quality":  {ScreenshotQuality: 80},
	}
	for name, p := range cases {
		if err := p.Validate(); err == nil {
			t.Fatalf("%s: expected an error when Screenshot is not enabled", name)
		}
	}
}

func TestValidateRejectsScreenshotQualityWithoutJPEGFormat(t *testing.T) {
	p := &scraperapi.RequestParameters{Screenshot: true, ScreenshotFormat: scraperapi.ScreenshotFormatPNG, ScreenshotQuality: 80}
	if err := p.Validate(); err == nil {
		t.Fatal("expected an error: screenshot_quality requires jpeg format")
	}
}

func TestValidateAllowsScreenshotQualityWithJPEGFormat(t *testing.T) {
	p := &scraperapi.RequestParameters{JSRender: true, Screenshot: true, ScreenshotFormat: scraperapi.ScreenshotFormatJPEG, ScreenshotQuality: 80}
	if err := p.Validate(); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestValidateRejectsProxyCountryWithoutPremiumProxies(t *testing.T) {
	p := &scraperapi.RequestParameters{ProxyCountry: "us"}
	if err := p.Validate(); err == nil {
		t.Fatal("expected an error: proxy_country requires premium proxies")
	}
}

func TestValidateAllowsProxyCountryWithPremiumProxies(t *testing.T) {
	p := &scraperapi.RequestParameters{ProxyCountry: "us", UsePremiumProxies: true}
	if err := p.Validate(); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestToURLValuesSerializesSlicesAsCommaJoined(t *testing.T) {
	p := &scraperapi.RequestParameters{
		JSRender:       true,
		BlockResources: []scraperapi.ResourceType{scraperapi.ResourceTypeImage, scraperapi.ResourceTypeFont},
		Outputs:        []scraperapi.OutputType{scraperapi.OutputTypeEmails, scraperapi.OutputTypeLinks},
	}
	values := p.ToURLValues()

	if got := values.Get("block_resources"); got != "image,font" {
		t.Fatalf("block_resources: got %q", got)
	}
	if got := values.Get("outputs"); got != "emails,links" {
		t.Fatalf("outputs: got %q", got)
	}
}

func TestToURLValuesIncludesCustomParams(t *testing.T) {
	p := &scraperapi.RequestParameters{CustomParams: map[string]string{"some_future_flag": "1"}}
	values := p.ToURLValues()

	if got := values.Get("some_future_flag"); got != "1" {
		t.Fatalf("expected custom param to be passed through, got %q", got)
	}
}

func TestToURLValuesSetsCustomHeadersFlagWhenHeadersPresent(t *testing.T) {
	withHeaders := &scraperapi.RequestParameters{CustomHeaders: http.Header{"Referer": []string{"https://google.com"}}}
	if got := withHeaders.ToURLValues().Get("custom_headers"); got != "true" {
		t.Fatalf("expected custom_headers=true, got %q", got)
	}

	withoutHeaders := &scraperapi.RequestParameters{}
	if got := withoutHeaders.ToURLValues().Get("custom_headers"); got != "" {
		t.Fatalf("expected custom_headers to be unset, got %q", got)
	}
}

func TestParseQueryRequestParametersDecodesCommaSeparatedSlices(t *testing.T) {
	query := url.Values{
		"block_resources": {"image,font"},
		"outputs":         {"emails,links"},
		"js_render":       {"true"},
	}

	params, err := scraperapi.ParseQueryRequestParameters(query)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(params.BlockResources) != 2 || params.BlockResources[0] != scraperapi.ResourceTypeImage || params.BlockResources[1] != scraperapi.ResourceTypeFont {
		t.Fatalf("unexpected block_resources: %v", params.BlockResources)
	}
	if len(params.Outputs) != 2 || params.Outputs[0] != scraperapi.OutputTypeEmails || params.Outputs[1] != scraperapi.OutputTypeLinks {
		t.Fatalf("unexpected outputs: %v", params.Outputs)
	}
	if !params.JSRender {
		t.Fatal("expected js_render to decode to true")
	}
}

func TestParseQueryRequestParametersRejectsUndecodableValues(t *testing.T) {
	query := url.Values{"wait": {"not-a-number"}}

	if _, err := scraperapi.ParseQueryRequestParameters(query); err == nil {
		t.Fatal("expected a decode error for a non-numeric wait value")
	}
}
