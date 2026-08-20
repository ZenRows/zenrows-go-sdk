package batch_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/zenrows/zenrows-go-sdk/service/batch"
)

func newTestClient(t *testing.T, handler http.HandlerFunc) (*batch.Client, func()) {
	t.Helper()
	server := httptest.NewServer(handler)
	client := batch.NewClient(batch.WithBaseURL(server.URL), batch.WithAPIKey("test-key"))
	return client, server.Close
}

func TestSubmitJobOpenAcceptsMoreTasksLater(t *testing.T) {
	var gotAPIKey string
	client, closeServer := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotAPIKey = r.Header.Get("X-API-Key")
		var body batch.SubmitJobRequest
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Status != batch.JobStatusOpen {
			t.Errorf("expected status open, got %q", body.Status)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(batch.SubmitJobResponse{JobID: "job_123", Status: batch.JobStatusOpen})
	})
	defer closeServer()

	job, err := client.SubmitJob(context.Background(), batch.SubmitJobRequest{Status: batch.JobStatusOpen})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if job.JobID != "job_123" || job.Status != batch.JobStatusOpen {
		t.Fatalf("unexpected job: %+v", job)
	}
	if gotAPIKey != "test-key" {
		t.Fatalf("expected X-API-Key header to be set, got %q", gotAPIKey)
	}
}

func TestAddTasksThenCloseJob(t *testing.T) {
	client, closeServer := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/jobs/job_123/tasks":
			_ = json.NewEncoder(w).Encode(batch.AddTasksResponse{AcceptedTasks: 1})
		case r.Method == http.MethodPost && r.URL.Path == "/jobs/job_123/close":
			_ = json.NewEncoder(w).Encode(batch.Job{JobID: "job_123", Status: batch.JobStatusClosed})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	defer closeServer()

	added, err := client.AddTasks(context.Background(), "job_123", []batch.Task{{URL: "https://example.com"}}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if added.AcceptedTasks != 1 {
		t.Fatalf("expected 1 accepted task, got %d", added.AcceptedTasks)
	}

	job, err := client.CloseJob(context.Background(), "job_123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if job.Status != batch.JobStatusClosed {
		t.Fatalf("expected job to be closed, got %q", job.Status)
	}
}

func TestGetResultsDefaultsToLatestRun(t *testing.T) {
	var gotPath string
	client, closeServer := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(batch.GetResultsResponse{Results: []batch.TaskResult{}})
	})
	defer closeServer()

	if _, err := client.GetResults(context.Background(), "job_123", batch.GetResultsOptions{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotPath != "/jobs/job_123/results" {
		t.Fatalf("expected latest-run results path, got %q", gotPath)
	}
}

func TestGetResultsForSpecificRun(t *testing.T) {
	var gotPath string
	client, closeServer := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(batch.GetResultsResponse{Results: []batch.TaskResult{}})
	})
	defer closeServer()

	_, err := client.GetResults(context.Background(), "job_123", batch.GetResultsOptions{RunID: "run_1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotPath != "/jobs/job_123/runs/run_1/results" {
		t.Fatalf("expected run-specific results path, got %q", gotPath)
	}
}

func TestAPIErrorSurfacesProblemDetail(t *testing.T) {
	client, closeServer := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(batch.ProblemDetail{Title: "Bad Request", Detail: "tasks is required"})
	})
	defer closeServer()

	_, err := client.SubmitJob(context.Background(), batch.SubmitJobRequest{})
	if err == nil {
		t.Fatal("expected an error")
	}
	apiErr, ok := err.(batch.APIError)
	if !ok {
		t.Fatalf("expected batch.APIError, got %T", err)
	}
	if apiErr.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", apiErr.StatusCode)
	}
	if apiErr.Detail == nil || apiErr.Detail.Detail != "tasks is required" {
		t.Fatalf("expected problem detail to be parsed, got %+v", apiErr.Detail)
	}
}

func TestNotConfiguredWithoutAPIKey(t *testing.T) {
	client := batch.NewClient(batch.WithAPIKey(""))
	_, err := client.GetJob(context.Background(), "job_123")
	if _, ok := err.(batch.NotConfiguredError); !ok {
		t.Fatalf("expected NotConfiguredError, got %v", err)
	}
}
