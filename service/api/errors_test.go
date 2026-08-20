package scraperapi_test

import (
	"errors"
	"testing"

	scraperapi "github.com/zenrows/zenrows-go-sdk/service/api"
)

func TestNotConfiguredErrorMessage(t *testing.T) {
	err := scraperapi.NotConfiguredError{}
	if err.Error() != "zenrows fetch client is not configured" {
		t.Fatalf("unexpected message: %q", err.Error())
	}
}

func TestInvalidHTTPMethodErrorMessage(t *testing.T) {
	err := scraperapi.InvalidHTTPMethodError{}
	msg := err.Error()
	if msg == "" || !contains(msg, "GET") || !contains(msg, "POST") || !contains(msg, "PUT") {
		t.Fatalf("expected message to list valid methods, got %q", msg)
	}
}

func TestInvalidTargetURLErrorDefaultsMessage(t *testing.T) {
	err := scraperapi.InvalidTargetURLError{}
	if err.Error() != "invalid target url" {
		t.Fatalf("unexpected default message: %q", err.Error())
	}
}

func TestInvalidTargetURLErrorWithCustomMsgAndWrappedErr(t *testing.T) {
	wrapped := errors.New("boom")
	err := scraperapi.InvalidTargetURLError{Msg: "custom", Err: wrapped}

	if got := err.Error(); got != "custom: boom" {
		t.Fatalf("expected combined message, got %q", got)
	}
	if !errors.Is(err, wrapped) {
		t.Fatal("expected errors.Is to unwrap to the underlying error")
	}
}

func TestInvalidTargetURLErrorWithoutWrappedErr(t *testing.T) {
	err := scraperapi.InvalidTargetURLError{Msg: "custom"}
	if got := err.Error(); got != "custom" {
		t.Fatalf("expected message without a wrapped error suffix, got %q", got)
	}
	if err.Unwrap() != nil {
		t.Fatal("expected Unwrap to return nil when no error was wrapped")
	}
}

func TestInvalidParameterErrorDefaultsMessage(t *testing.T) {
	err := scraperapi.InvalidParameterError{}
	if err.Error() != "invalid parameter" {
		t.Fatalf("unexpected default message: %q", err.Error())
	}
}

func TestInvalidParameterErrorWithCustomMsg(t *testing.T) {
	err := scraperapi.InvalidParameterError{Msg: "screenshot quality must be between 1 and 100"}
	if err.Error() != "screenshot quality must be between 1 and 100" {
		t.Fatalf("unexpected message: %q", err.Error())
	}
}
