package batch_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/zenrows/zenrows-go-sdk/service/batch"
)

func TestJobRefLoadDeleteRerunRetryFailed(t *testing.T) {
	var lastMethod, lastPath, lastQuery string
	client, closeServer := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		lastMethod, lastPath, lastQuery = r.Method, r.URL.Path, r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(batch.Job{JobID: "job_123", Status: batch.JobStatusClosed})
		case r.URL.Path == "/jobs/job_123/rerun":
			_ = json.NewEncoder(w).Encode(batch.RerunJobResponse{JobID: "job_123", LatestRun: batch.Run{RunID: "run_2"}})
		default:
			w.WriteHeader(http.StatusAccepted)
		}
	})
	defer closeServer()

	ref := client.Job("job_123")
	loaded, err := ref.Load(context.Background())
	if err != nil || loaded.Data.JobID != "job_123" {
		t.Fatalf("unexpected Load result: %+v, err=%v", loaded, err)
	}

	run, err := ref.RetryFailed(context.Background(), batch.RetryFailedOptions{IncludePending: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if lastQuery != "status=failed%2Cpending" {
		t.Fatalf("expected RetryFailed(IncludePending:true) to send status=failed,pending, got %q", lastQuery)
	}
	if run.RunID() != "run_2" {
		t.Fatalf("expected the new run's id, got %q", run.RunID())
	}

	if err := ref.Delete(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if lastMethod != http.MethodDelete || lastPath != "/jobs/job_123" {
		t.Fatalf("expected a DELETE to /jobs/job_123, got %s %s", lastMethod, lastPath)
	}
}

func TestJobRefWebhookAccessors(t *testing.T) {
	var gotBody map[string]any
	client, closeServer := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(batch.WebhookConfig{URL: "https://hook.example", Signature: true})
	})
	defer closeServer()

	ref := client.Job("job_123")
	if _, err := ref.SetWebhook(context.Background(), "https://hook.example", true); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotBody["url"] != "https://hook.example" || gotBody["signature"] != true {
		t.Fatalf("unexpected PUT body: %v", gotBody)
	}
	if _, err := ref.GetWebhook(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := ref.DeleteWebhook(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestJobRefRunsIteratesAsRunHandles(t *testing.T) {
	client, closeServer := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(batch.ListRunsResponse{Runs: []batch.Run{{RunID: "run_1", Status: batch.RunStatusCompleted}}})
	})
	defer closeServer()

	var got []string
	for run, err := range client.Job("job_123").Runs(context.Background(), batch.ListRunsOptions{}) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got = append(got, run.RunID())
		if run.Status() != batch.RunStatusCompleted {
			t.Fatalf("expected the run's status to be readable via the handle, got %+v", run.Data)
		}
	}
	if len(got) != 1 || got[0] != "run_1" {
		t.Fatalf("unexpected runs: %v", got)
	}
}

func TestRunRefLoadDeleteResultsTaskHistory(t *testing.T) {
	var lastPath string
	client, closeServer := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		lastPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/jobs/job_123/runs/run_1":
			_ = json.NewEncoder(w).Encode(batch.Run{RunID: "run_1", Status: batch.RunStatusCompleted, Stats: batch.RunStats{Total: 5}})
		case r.URL.Path == "/jobs/job_123/runs/run_1/results":
			_ = json.NewEncoder(w).Encode(batch.GetResultsResponse{Results: []batch.TaskResult{{TaskID: "task_1"}}})
		case r.URL.Path == "/jobs/job_123/runs/run_1/tasks/task_1/history":
			_ = json.NewEncoder(w).Encode(batch.TaskHistoryResponse{Events: []batch.TaskHistoryEvent{{Attempt: 1}}})
		case r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusAccepted)
		}
	})
	defer closeServer()

	ref := client.Run("job_123", "run_1")
	loaded, err := ref.Load(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if loaded.Status() != batch.RunStatusCompleted || loaded.Stats().Total != 5 {
		t.Fatalf("unexpected loaded run: %+v", loaded.Data)
	}

	var resultCount int
	for r, err := range ref.Results(context.Background(), "") {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		resultCount++
		_ = r
	}
	if resultCount != 1 {
		t.Fatalf("expected 1 result, got %d", resultCount)
	}

	if _, err := ref.TaskHistory(context.Background(), "task_1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if lastPath != "/jobs/job_123/runs/run_1/tasks/task_1/history" {
		t.Fatalf("unexpected path: %s", lastPath)
	}

	if err := ref.Delete(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunRefWaitAndStartExport(t *testing.T) {
	polls := 0
	client, closeServer := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/jobs/job_123/runs/run_1" && r.Method == http.MethodGet:
			polls++
			status := batch.RunStatusRunning
			if polls >= 2 {
				status = batch.RunStatusCompleted
			}
			_ = json.NewEncoder(w).Encode(batch.Run{RunID: "run_1", Status: status})
		case r.URL.Path == "/jobs/job_123/runs/run_1/exports":
			_ = json.NewEncoder(w).Encode(batch.StartExportResponse{ExportID: "exp_1", Status: batch.ExportStatusPending})
		}
	})
	defer closeServer()

	ref := client.Run("job_123", "run_1")
	handle, err := ref.Wait(context.Background(), batch.WaitForRunOptions{PollInterval: 1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if handle.Status() != batch.RunStatusCompleted {
		t.Fatalf("expected the run to complete, got %+v", handle.Data)
	}

	exportRef, err := ref.StartExport(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exportRef.ExportID() != "exp_1" {
		t.Fatalf("expected the started export's id, got %q", exportRef.ExportID())
	}
}

func TestClientWaitForRunAndWaitForIngest(t *testing.T) {
	callCount := 0
	client, closeServer := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		status := batch.RunStatusRunning
		ingest := batch.IngestStatusPending
		if callCount >= 2 {
			status = batch.RunStatusCompleted
			ingest = batch.IngestStatusDone
		}
		_ = json.NewEncoder(w).Encode(batch.Job{
			JobID:     "job_123",
			LatestRun: &batch.Run{RunID: "run_1", Status: status, IngestStatus: ingest},
		})
	})
	defer closeServer()

	run, err := client.WaitForRun(context.Background(), "job_123", batch.WaitForRunOptions{PollInterval: 1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if run.Status != batch.RunStatusCompleted {
		t.Fatalf("expected completed, got %+v", run)
	}

	callCount = 0
	job, err := client.WaitForIngest(context.Background(), "job_123", batch.WaitForIngestOptions{PollInterval: 1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if job.LatestRun.IngestStatus != batch.IngestStatusDone {
		t.Fatalf("expected ingestion to finish, got %+v", job.LatestRun)
	}
}

func TestAPIErrorCodeDefaultsToInternalOnNonJSONBody(t *testing.T) {
	client, closeServer := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("not json"))
	})
	defer closeServer()

	_, err := client.GetJob(context.Background(), "job_123")
	apiErr, ok := err.(batch.APIError)
	if !ok {
		t.Fatalf("expected batch.APIError, got %T", err)
	}
	if apiErr.Code() != "internal" {
		t.Fatalf("expected Code() to default to \"internal\" for a non-JSON body, got %q", apiErr.Code())
	}
}

func TestAPIErrorCodeSurfacesTheProblemCode(t *testing.T) {
	client, closeServer := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]any{"code": "idempotency_key_conflict", "detail": "mismatch"})
	})
	defer closeServer()

	_, err := client.GetJob(context.Background(), "job_123")
	apiErr := err.(batch.APIError)
	if apiErr.Code() != "idempotency_key_conflict" {
		t.Fatalf("expected the real problem code, got %q", apiErr.Code())
	}
}

func TestDownloadTaskToFileAndToMemory(t *testing.T) {
	resultServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("single-task-body"))
	}))
	defer resultServer.Close()

	client := batch.NewClient(batch.WithAPIKey("k"))
	task := batch.TaskResult{TaskID: "task_1", ResultURL: resultServer.URL, Status: batch.TaskStatusSuccessful}

	target := filepath.Join(t.TempDir(), "out.bin")
	if err := client.Run("job_123", "run_1").DownloadTaskToFile(task, target); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	body, err := os.ReadFile(target)
	if err != nil || string(body) != "single-task-body" {
		t.Fatalf("unexpected file content: %q, err=%v", body, err)
	}

	memBody, err := client.Run("job_123", "run_1").DownloadTaskToMemory(task)
	if err != nil || string(memBody) != "single-task-body" {
		t.Fatalf("unexpected memory content: %q, err=%v", memBody, err)
	}
}

func TestDownloadTaskToFileRejectsTaskWithNoResultURL(t *testing.T) {
	client := batch.NewClient(batch.WithAPIKey("k"))
	task := batch.TaskResult{TaskID: "task_1", Status: batch.TaskStatusFailed}
	if err := client.Run("job_123", "run_1").DownloadTaskToFile(task, filepath.Join(t.TempDir(), "x")); err == nil {
		t.Fatal("expected an error for a task with no result_url")
	}
}

func TestDownloadToDirUsesExternalIDFilenameWhenRequested(t *testing.T) {
	resultServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("body"))
	}))
	defer resultServer.Close()

	client, closeServer := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(batch.GetResultsResponse{
			Results: []batch.TaskResult{{TaskID: "task_1", ExternalID: "order #1/weird", Status: batch.TaskStatusSuccessful, Type: batch.ResultTypeHTML, ResultURL: resultServer.URL}},
		})
	})
	defer closeServer()

	dir := t.TempDir()
	if _, err := client.DownloadToDir(context.Background(), "job_123", dir, batch.DownloadToDirOptions{UseExternalID: true}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Unsafe filename characters (space, #, /) must be coerced to underscores.
	if _, err := os.Stat(filepath.Join(dir, "order__1_weird.html")); err != nil {
		t.Fatalf("expected a coerced external-id filename, got dir contents error: %v", err)
	}
}
