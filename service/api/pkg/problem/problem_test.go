package problem_test

import (
	"encoding/json"
	"testing"

	"github.com/zenrows/zenrows-go-sdk/service/api/pkg/problem"
)

func TestErrorWithDetail(t *testing.T) {
	p := &problem.Problem{Title: "Service Unavailable", Status: 503, Detail: "upstream is down"}

	got := p.Error()
	want := "Service Unavailable [HTTP 503]: upstream is down"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestErrorWithoutDetail(t *testing.T) {
	p := &problem.Problem{Title: "Bad Request", Status: 400}

	got := p.Error()
	want := "Bad Request [HTTP 400]"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestReasonIsParsed(t *testing.T) {
	var p problem.Problem
	body := `{"code":"REQS007","reason":"page_not_recognised","status":403,"title":"Forbidden"}`
	if err := json.Unmarshal([]byte(body), &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if p.Code != "REQS007" || p.Reason != "page_not_recognised" {
		t.Fatalf("got code %q reason %q", p.Code, p.Reason)
	}
}

func TestReasonIsOptional(t *testing.T) {
	var p problem.Problem
	if err := json.Unmarshal([]byte(`{"code":"REQS007","status":403}`), &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if p.Reason != "" {
		t.Fatalf("expected an empty reason, got %q", p.Reason)
	}
}
