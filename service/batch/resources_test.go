package batch_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/zenrows/zenrows-go-sdk/service/batch"
)

func TestJobRefCloseReturnsAFreshHandleNotMutatingTheOriginal(t *testing.T) {
	// Regression test for the deliberate immutable-snapshot design: Close must return a NEW
	// handle carrying the server's fresh state, never mutate a handle in place.
	client, closeServer := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(batch.Job{JobID: "job_123", Status: batch.JobStatusClosed})
	})
	defer closeServer()

	original := client.Job("job_123")
	closed, err := original.Close(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if closed.Data.Status != batch.JobStatusClosed {
		t.Fatalf("expected the returned handle to carry the fresh status, got %+v", closed.Data)
	}
	// original is a JobRef with no Data at all — proving Close didn't mutate it in place
	// (there's nothing to mutate; a fresh JobHandle was returned instead).
	if original.JobID() != "job_123" {
		t.Fatalf("original ref's id should be unchanged: %q", original.JobID())
	}
}

func TestJobRunFacetResolvesCurrentRunAndPauses(t *testing.T) {
	client, closeServer := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/jobs/job_123/pause" {
			_ = json.NewEncoder(w).Encode(batch.Run{RunID: "run_1", Status: batch.RunStatusRunning, PauseState: batch.PauseStatePaused})
			return
		}
		t.Errorf("unexpected path: %s", r.URL.Path)
	})
	defer closeServer()

	handle, err := client.Job("job_123").Run().Pause(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if handle.Data.PauseState != batch.PauseStatePaused {
		t.Fatalf("expected the fresh run to report paused, got %+v", handle.Data)
	}
}

func TestScheduleControlsUpdateSendsTheResolvedSchedule(t *testing.T) {
	var gotBody map[string]any
	client, closeServer := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/jobs/job_123/schedule" || r.Method != http.MethodPut {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(batch.Job{JobID: "job_123"})
	})
	defer closeServer()

	rate, _ := batch.NewRate(30, "minute")
	if _, err := client.Job("job_123").Schedule().Update(context.Background(), rate); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	rateBody, ok := gotBody["rate"].(map[string]any)
	if !ok {
		t.Fatalf("expected a rate block in the PUT body, got %v", gotBody)
	}
	if rateBody["every"] != float64(30) || rateBody["unit"] != "minute" {
		t.Fatalf("unexpected rate body: %v", rateBody)
	}
}

func TestScheduleControlsPauseAndResume(t *testing.T) {
	var gotBodies []map[string]any
	client, closeServer := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotBodies = append(gotBodies, body)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(batch.Job{JobID: "job_123", ScheduleState: batch.ScheduleState(body["schedule_state"].(string))})
	})
	defer closeServer()

	sched := client.Job("job_123").Schedule()
	paused, err := sched.Pause(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if paused.Data.ScheduleState != batch.ScheduleStatePaused {
		t.Fatalf("expected paused state, got %+v", paused.Data)
	}
	resumed, err := sched.Resume(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resumed.Data.ScheduleState != batch.ScheduleStateActive {
		t.Fatalf("expected active state, got %+v", resumed.Data)
	}
}

func TestSubmitOpenThenAddTasksThenClose(t *testing.T) {
	client, closeServer := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/jobs" && r.Method == http.MethodPost:
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(batch.SubmitJobResponse{JobID: "job_123", Status: batch.JobStatusOpen})
		case r.URL.Path == "/jobs/job_123/tasks":
			_ = json.NewEncoder(w).Encode(batch.AddTasksResponse{AcceptedTasks: 1, JobStatus: batch.JobStatusOpen})
		case r.URL.Path == "/jobs/job_123/close":
			_ = json.NewEncoder(w).Encode(batch.Job{JobID: "job_123", Status: batch.JobStatusClosed})
		default:
			t.Errorf("unexpected request: %s", r.URL.Path)
		}
	})
	defer closeServer()

	ref, err := client.SubmitOpen(context.Background(), batch.SubmitOpenOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ref.Status() != batch.JobStatusOpen {
		t.Fatalf("expected the submit response status to be open, got %q", ref.Status())
	}
	if _, err := ref.AddTasks(context.Background(), []batch.Task{{URL: "https://example.com"}}, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	closed, err := ref.Close(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if closed.Data.Status != batch.JobStatusClosed {
		t.Fatalf("expected the job to be closed, got %+v", closed.Data)
	}
}

func TestSubmitRegularRejectsBothTasksAndFileInput(t *testing.T) {
	client := batch.NewClient(batch.WithAPIKey("k"))
	_, err := client.SubmitRegular(context.Background(), batch.SubmitRegularOptions{
		Tasks: []batch.Task{{URL: "https://a"}}, FileInputID: "fi_1",
	})
	if err == nil {
		t.Fatal("expected an error when both Tasks and FileInputID are set")
	}
}

func TestSubmitRegularRequiresTasksOrFileInput(t *testing.T) {
	client := batch.NewClient(batch.WithAPIKey("k"))
	_, err := client.SubmitRegular(context.Background(), batch.SubmitRegularOptions{})
	if err == nil {
		t.Fatal("expected an error for a closed job with neither Tasks nor FileInputID")
	}
}

func TestSubmitScheduledSendsTheResolvedScheduleBlock(t *testing.T) {
	var gotBody map[string]any
	client, closeServer := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(batch.SubmitJobResponse{JobID: "job_123"})
	})
	defer closeServer()

	at, _ := batch.NewAt("2026-09-01T09:00:00", "Europe/Berlin")
	_, err := client.SubmitScheduled(context.Background(), batch.SubmitScheduledOptions{
		Schedule: at, Tasks: []batch.Task{{URL: "https://example.com"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotBody["type"] != "scheduled" {
		t.Fatalf("expected type=scheduled, got %v", gotBody)
	}
	schedule, ok := gotBody["schedule"].(map[string]any)
	if !ok || schedule["at"] != "2026-09-01T09:00:00" || schedule["timezone"] != "Europe/Berlin" {
		t.Fatalf("expected the resolved At schedule in the body, got %v", gotBody["schedule"])
	}
}

func TestWebhookCRUD(t *testing.T) {
	var lastMethod string
	var lastBody map[string]any
	client, closeServer := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		lastMethod = r.Method
		_ = json.NewDecoder(r.Body).Decode(&lastBody)
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(batch.WebhookConfig{URL: "https://hook.example", Signature: true})
		case http.MethodPut:
			_ = json.NewEncoder(w).Encode(batch.WebhookConfig{URL: "https://hook.example", Signature: false})
		case http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		}
	})
	defer closeServer()

	got, err := client.GetJobWebhook(context.Background(), "job_123")
	if err != nil || got.URL != "https://hook.example" {
		t.Fatalf("unexpected GetJobWebhook result: %+v, err=%v", got, err)
	}
	if _, err := client.PutJobWebhook(context.Background(), "job_123", batch.WebhookConfig{URL: "https://hook.example", Signature: false}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if lastMethod != http.MethodPut {
		t.Fatalf("expected PUT, got %s", lastMethod)
	}
	if err := client.DeleteJobWebhook(context.Background(), "job_123"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestTestWebhookDispatch(t *testing.T) {
	client, closeServer := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/webhook/test" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(batch.TestWebhookResponse{Delivered: true, EventID: "evt_1", StatusCode: 200})
	})
	defer closeServer()

	resp, err := client.TestWebhook(context.Background(), batch.TestWebhookRequest{URL: "https://hook.example"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.Delivered || resp.EventID != "evt_1" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestHMACKeyLifecycle(t *testing.T) {
	client, closeServer := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/hmac/keys":
			if r.Method == http.MethodGet {
				_ = json.NewEncoder(w).Encode(batch.HMACKeyList{Active: &batch.HMACKeyMeta{Kid: "kid_1"}})
			}
		case "/hmac/keys/rotate":
			if r.Method == http.MethodPost {
				_ = json.NewEncoder(w).Encode(batch.HMACKeyCreated{Kid: "kid_2", Secret: "c2VjcmV0"})
			} else if r.Method == http.MethodDelete {
				w.WriteHeader(http.StatusNoContent)
			}
		case "/hmac/keys/rotate/finalize":
			_ = json.NewEncoder(w).Encode(batch.HMACKeyFinalized{ActiveKid: "kid_2"})
		}
	})
	defer closeServer()

	ctx := context.Background()
	if list, err := client.ListHMACKeys(ctx); err != nil || list.Active.Kid != "kid_1" {
		t.Fatalf("unexpected ListHMACKeys result: %+v, err=%v", list, err)
	}
	created, err := client.RotateHMACKey(ctx)
	if err != nil || created.Secret != "c2VjcmV0" {
		t.Fatalf("expected RotateHMACKey to return the secret exactly once: %+v, err=%v", created, err)
	}
	if finalized, err := client.FinalizeHMACKey(ctx); err != nil || finalized.ActiveKid != "kid_2" {
		t.Fatalf("unexpected FinalizeHMACKey result: %+v, err=%v", finalized, err)
	}
	if err := client.CancelHMACRotation(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestUploadCSVAllocatesSlotAndPUTsBody(t *testing.T) {
	var uploadServerHit bool
	uploadServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uploadServerHit = true
		if r.Method != http.MethodPut {
			t.Errorf("expected PUT to the presigned URL, got %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer uploadServer.Close()

	client, closeServer := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(batch.CreateJobInputResponse{
			FileInputID: "fi_123",
			Upload: batch.FileInputUploadTarget{
				Method: "PUT", URL: uploadServer.URL, Headers: map[string]string{"Content-Type": "text/csv"},
			},
		})
	})
	defer closeServer()

	tmp := filepath.Join(t.TempDir(), "tasks.csv")
	if err := os.WriteFile(tmp, []byte("url\nhttps://example.com\n"), 0o600); err != nil {
		t.Fatalf("failed to write fixture: %v", err)
	}

	fileInputID, err := client.UploadCSV(context.Background(), tmp, batch.UploadCSVOptions{URLField: 0, Header: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fileInputID != "fi_123" {
		t.Fatalf("expected the allocated file_input_id, got %q", fileInputID)
	}
	if !uploadServerHit {
		t.Fatal("expected the presigned upload URL to actually be PUT to")
	}
}

func TestUploadCSVRequiresURLField(t *testing.T) {
	client := batch.NewClient(batch.WithAPIKey("k"))
	if _, err := client.UploadCSV(context.Background(), []byte("url\n"), batch.UploadCSVOptions{}); err == nil {
		t.Fatal("expected an error when URLField is not set")
	}
}

func TestExportLifecycle(t *testing.T) {
	poll := 0
	client, closeServer := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(batch.StartExportResponse{ExportID: "exp_1", Status: batch.ExportStatusPending})
		default:
			poll++
			status := batch.ExportStatusRunning
			if poll >= 2 {
				status = batch.ExportStatusCompleted
			}
			_ = json.NewEncoder(w).Encode(batch.Export{ExportID: "exp_1", Status: status, DownloadURL: "https://dl.example/x.zip"})
		}
	})
	defer closeServer()

	ref, err := client.StartResultsExport(context.Background(), "job_123", "run_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ref.StartResponse() == nil || ref.StartResponse().Status != batch.ExportStatusPending {
		t.Fatalf("expected the start response to be attached to the ref")
	}
	handle, err := ref.Wait(context.Background(), batch.WaitForExportOptions{PollInterval: 1, Timeout: 5000000000})
	if err != nil {
		t.Fatalf("unexpected error waiting for export: %v", err)
	}
	if handle.Status() != batch.ExportStatusCompleted {
		t.Fatalf("expected the export to complete, got %+v", handle.Data)
	}
}

func TestPaginationIteratorsFollowCursorUntilExhausted(t *testing.T) {
	page := 0
	client, closeServer := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		page++
		if page == 1 {
			_ = json.NewEncoder(w).Encode(batch.ListJobsResponse{
				Jobs:       []batch.Job{{JobID: "job_1"}},
				NextCursor: "cursor_2",
			})
			return
		}
		_ = json.NewEncoder(w).Encode(batch.ListJobsResponse{Jobs: []batch.Job{{JobID: "job_2"}}})
	})
	defer closeServer()

	var ids []string //nolint:prealloc // length is unknown ahead of a paginated iterator
	for job, err := range client.IterJobs(context.Background(), batch.ListJobsOptions{}) {
		if err != nil {
			t.Fatalf("unexpected error mid-iteration: %v", err)
		}
		ids = append(ids, job.JobID)
	}
	if len(ids) != 2 || ids[0] != "job_1" || ids[1] != "job_2" {
		t.Fatalf("expected to follow the cursor across both pages, got %v", ids)
	}
	if page != 2 {
		t.Fatalf("expected exactly 2 page fetches, got %d", page)
	}
}

func TestDownloadToDirWritesEachSuccessfulResultBody(t *testing.T) {
	resultServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<html>ok</html>"))
	}))
	defer resultServer.Close()

	client, closeServer := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(batch.GetResultsResponse{
			Results: []batch.TaskResult{
				{TaskID: "task_1", Status: batch.TaskStatusSuccessful, Type: batch.ResultTypeHTML, ResultURL: resultServer.URL},
			},
		})
	})
	defer closeServer()

	dir := t.TempDir()
	n, err := client.DownloadToDir(context.Background(), "job_123", dir, batch.DownloadToDirOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 file written, got %d", n)
	}
	body, err := os.ReadFile(filepath.Join(dir, "task_1.html"))
	if err != nil {
		t.Fatalf("expected task_1.html to exist: %v", err)
	}
	if string(body) != "<html>ok</html>" {
		t.Fatalf("unexpected file content: %q", body)
	}
}

func TestDownloadToDirEnforcesMaxBytesPerFile(t *testing.T) {
	resultServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("this body is definitely more than zero bytes"))
	}))
	defer resultServer.Close()

	client, closeServer := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(batch.GetResultsResponse{
			Results: []batch.TaskResult{{TaskID: "task_1", Status: batch.TaskStatusSuccessful, ResultURL: resultServer.URL}},
		})
	})
	defer closeServer()

	_, err := client.DownloadToDir(context.Background(), "job_123", t.TempDir(), batch.DownloadToDirOptions{MaxBytesPerFile: 1})
	if err == nil {
		t.Fatal("expected a DownloadLimitExceededError error")
	}
	var limitErr batch.DownloadLimitExceededError
	if !errors.As(err, &limitErr) {
		t.Fatalf("expected batch.DownloadLimitExceededError, got %T: %v", err, err)
	}
}

func TestDownloadToMemoryLoadsBodies(t *testing.T) {
	resultServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("body-1"))
	}))
	defer resultServer.Close()

	client, closeServer := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(batch.GetResultsResponse{
			Results: []batch.TaskResult{{TaskID: "task_1", ExternalID: "ext_1", Status: batch.TaskStatusSuccessful, ResultURL: resultServer.URL}},
		})
	})
	defer closeServer()

	results, err := client.DownloadToMemory(context.Background(), "job_123", batch.DownloadToMemoryOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 || string(results[0].Body) != "body-1" || results[0].ExternalID != "ext_1" {
		t.Fatalf("unexpected results: %+v", results)
	}
}

func TestGetTaskHistoryLatestAndSpecificRun(t *testing.T) {
	var gotPaths []string
	client, closeServer := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPaths = append(gotPaths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(batch.TaskHistoryResponse{Events: []batch.TaskHistoryEvent{{Attempt: 1}}})
	})
	defer closeServer()

	if _, err := client.GetTaskHistory(context.Background(), "job_123", "task_1", ""); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := client.GetTaskHistory(context.Background(), "job_123", "task_1", "run_1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"/jobs/job_123/tasks/task_1/history", "/jobs/job_123/runs/run_1/tasks/task_1/history"}
	if len(gotPaths) != 2 || gotPaths[0] != want[0] || gotPaths[1] != want[1] {
		t.Fatalf("unexpected paths: %v", gotPaths)
	}
}
