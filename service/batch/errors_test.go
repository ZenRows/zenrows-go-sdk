package batch_test

import (
	"testing"

	"github.com/zenrows/zenrows-go-sdk/service/batch"
)

func TestNotConfiguredErrorMessage(t *testing.T) {
	err := batch.NotConfiguredError{}
	if err.Error() != "zenrows batch api client is not configured" {
		t.Fatalf("unexpected message: %q", err.Error())
	}
}

func TestAPIErrorWithDetail(t *testing.T) {
	err := batch.APIError{StatusCode: 402, Detail: &batch.ProblemDetail{Title: "Payment Required", Detail: "no credit available"}}
	want := "zenrows batch api request failed with status 402: no credit available"
	if got := err.Error(); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestAPIErrorWithoutDetail(t *testing.T) {
	err := batch.APIError{StatusCode: 503}
	want := "zenrows batch api request failed with status 503"
	if got := err.Error(); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestAPIErrorWithDetailButEmptyDetailString(t *testing.T) {
	// Detail is non-nil but its Detail field is empty — should fall back to the plain message,
	// not print a trailing ": ".
	err := batch.APIError{StatusCode: 500, Detail: &batch.ProblemDetail{Title: "Internal Server Error"}}
	want := "zenrows batch api request failed with status 500"
	if got := err.Error(); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
