package scraperapi_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	scraperapi "github.com/zenrows/zenrows-go-sdk/service/api"
)

func TestFetchSendsModeAuto(t *testing.T) {
	var gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := scraperapi.NewClient(scraperapi.WithBaseURL(server.URL), scraperapi.WithAPIKey("test-key"))

	params := &scraperapi.RequestParameters{Mode: scraperapi.ModeAuto}
	if _, err := client.Get(context.Background(), "https://example.com", params); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(gotQuery, "mode=auto") {
		t.Fatalf("expected query to contain mode=auto, got %q", gotQuery)
	}
}

func TestValidateRejectsInvalidMode(t *testing.T) {
	params := &scraperapi.RequestParameters{Mode: "bogus"}
	if err := params.Validate(); err == nil {
		t.Fatal("expected an error for an invalid mode")
	}
}

func TestValidateAcceptsModeAuto(t *testing.T) {
	params := &scraperapi.RequestParameters{Mode: scraperapi.ModeAuto}
	if err := params.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateAcceptsEmptyMode(t *testing.T) {
	params := &scraperapi.RequestParameters{}
	if err := params.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
