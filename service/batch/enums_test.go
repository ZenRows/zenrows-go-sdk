package batch_test

import (
	"encoding/json"
	"testing"

	"github.com/zenrows/zenrows-go-sdk/service/batch"
)

func TestResponseEnumsIsKnown(t *testing.T) {
	cases := []struct {
		name  string
		known interface{ IsKnown() bool }
		other interface{ IsKnown() bool }
	}{
		{"JobStatus", batch.JobStatusDeleted, batch.JobStatus("x_added_later")},
		{"RunStatus", batch.RunStatusDeleted, batch.RunStatus("x_added_later")},
		{"TaskStatus", batch.TaskStatusFailed, batch.TaskStatus("x_added_later")},
		{"ResultType", batch.ResultTypePDF, batch.ResultType("x_added_later")},
		{"IngestStatus", batch.IngestStatusDone, batch.IngestStatus("x_added_later")},
		{"FailureReason", batch.FailureReasonSubscriptionInactive, batch.FailureReason("x_added_later")},
		{"ExportStatus", batch.ExportStatusFailed, batch.ExportStatus("x_added_later")},
	}
	for _, c := range cases {
		if !c.known.IsKnown() {
			t.Errorf("%s: defined constant reported unknown", c.name)
		}
		if c.other.IsKnown() {
			t.Errorf("%s: undefined value reported known", c.name)
		}
	}
}

func TestRunUnmarshalUnknownEnumValues(t *testing.T) {
	body := `{"run_id":"r1","job_id":"j1","run_sequence":1,"status":"archived",` +
		`"stats":{"total":0,"completed":0,"successful":0,"failed":0},"failure_reason":"a_reason_added_later"}`
	var run batch.Run
	if err := json.Unmarshal([]byte(body), &run); err != nil {
		t.Fatalf("unknown enum values must decode, got: %v", err)
	}
	if run.Status != "archived" || run.Status.IsKnown() {
		t.Fatalf("status = %q (known=%v), want archived, unknown", run.Status, run.Status.IsKnown())
	}
	if run.FailureReason != "a_reason_added_later" || run.FailureReason.IsKnown() {
		t.Fatalf("failure reason = %q (known=%v)", run.FailureReason, run.FailureReason.IsKnown())
	}
}
