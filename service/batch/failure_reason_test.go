package batch_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/zenrows/zenrows-go-sdk/service/batch"
)

func TestRunUnmarshalAPIKeyCapReached(t *testing.T) {
	body := `{"run_id":"r1","job_id":"j1","run_sequence":1,"status":"failed","stats":{"total":1,"completed":0,"successful":0,"failed":0},` +
		`"failure_reason":"api_key_cap_reached","failure_detail":"This API key reached its daily cap; it resets at 2026-10-01T00:00:00Z."}`
	var run batch.Run
	if err := json.Unmarshal([]byte(body), &run); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if run.FailureReason != batch.FailureReasonAPIKeyCapReached || !run.FailureReason.IsKnown() {
		t.Fatalf("unexpected failure reason %q (known=%v)", run.FailureReason, run.FailureReason.IsKnown())
	}
	if run.FailureDetail == nil || *run.FailureDetail != "This API key reached its daily cap; it resets at 2026-10-01T00:00:00Z." {
		t.Fatalf("unexpected failure detail %v", run.FailureDetail)
	}
}

func TestRunUnmarshalUnknownFailureReasonAndStatus(t *testing.T) {
	body := `{"run_id":"r1","job_id":"j1","run_sequence":1,"status":"archived","stats":{"total":0,"completed":0,"successful":0,"failed":0},` +
		`"failure_reason":"a_reason_added_later","failure_detail":null}`
	var run batch.Run
	if err := json.Unmarshal([]byte(body), &run); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if run.FailureReason != "a_reason_added_later" || run.FailureReason.IsKnown() {
		t.Fatalf("unexpected failure reason %q (known=%v)", run.FailureReason, run.FailureReason.IsKnown())
	}
	if run.Status != "archived" {
		t.Fatalf("unexpected status %q", run.Status)
	}
	if run.FailureDetail != nil {
		t.Fatalf("expected nil failure detail, got %q", *run.FailureDetail)
	}
}

func TestRerunAPIKeyCapReachedExposesCodeAndDetail(t *testing.T) {
	const detail = "This API key reached its monthly cap of 50000 credits; it resets at 2026-11-01T00:00:00Z."
	client, closeServer := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusPaymentRequired)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"type": "about:blank", "title": "Payment Required", "status": 402,
			"code": "api_key_cap_reached", "detail": detail,
		})
	})
	defer closeServer()

	_, err := client.Rerun(context.Background(), "j1", batch.RerunOptions{})
	var apiErr batch.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected APIError, got %T: %v", err, err)
	}
	if apiErr.StatusCode != http.StatusPaymentRequired || apiErr.Code() != "api_key_cap_reached" {
		t.Fatalf("unexpected status/code: %d %q", apiErr.StatusCode, apiErr.Code())
	}
	if apiErr.Detail == nil || apiErr.Detail.Detail != detail {
		t.Fatalf("unexpected detail: %+v", apiErr.Detail)
	}
}
