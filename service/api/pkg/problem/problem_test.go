package problem_test

import (
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
