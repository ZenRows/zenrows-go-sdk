package batch_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/zenrows/zenrows-go-sdk/service/batch"
)

func parseQuery(raw string) (url.Values, error) {
	return url.ParseQuery(raw)
}

// TestAllMethodsRejectWhenNotConfigured guards against a regression where a future method is
// added without the isConfigured() check every other method has — each of these must fail fast
// with NotConfiguredError rather than sending a request with an empty API key.
func TestAllMethodsRejectWhenNotConfigured(t *testing.T) {
	client := batch.NewClient(batch.WithBaseURL("http://127.0.0.1:0"), batch.WithAPIKey(""))
	ctx := context.Background()

	cases := map[string]func() error{
		"SubmitJob":  func() error { _, err := client.SubmitJob(ctx, batch.SubmitJobRequest{}); return err },
		"ListJobs":   func() error { _, err := client.ListJobs(ctx, batch.ListJobsOptions{}); return err },
		"GetJob":     func() error { _, err := client.GetJob(ctx, "job_123"); return err },
		"DeleteJob":  func() error { return client.DeleteJob(ctx, "job_123") },
		"AddTasks":   func() error { _, err := client.AddTasks(ctx, "job_123", nil, false); return err },
		"CloseJob":   func() error { _, err := client.CloseJob(ctx, "job_123"); return err },
		"StopRun":    func() error { _, err := client.StopRun(ctx, "job_123"); return err },
		"Rerun":      func() error { _, err := client.Rerun(ctx, "job_123", batch.RerunOptions{}); return err },
		"ListRuns":   func() error { _, err := client.ListRuns(ctx, "job_123", batch.ListRunsOptions{}); return err },
		"GetRun":     func() error { _, err := client.GetRun(ctx, "job_123", "run_1"); return err },
		"DeleteRun":  func() error { return client.DeleteRun(ctx, "job_123", "run_1") },
		"GetResults": func() error { _, err := client.GetResults(ctx, "job_123", batch.GetResultsOptions{}); return err },
		"GetTaskContent": func() error {
			_, err := client.GetTaskContent(ctx, "job_123", "task_1", batch.GetTaskContentOptions{})
			return err
		},
	}

	for name, call := range cases {
		err := call()
		if _, ok := err.(batch.NotConfiguredError); !ok {
			t.Errorf("%s: expected NotConfiguredError, got %v (%T)", name, err, err)
		}
	}
}

func TestListJobsForwardsAllFilters(t *testing.T) {
	var gotQuery string
	client, closeServer := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(batch.ListJobsResponse{})
	})
	defer closeServer()

	_, err := client.ListJobs(context.Background(), batch.ListJobsOptions{
		Status: batch.JobStatusOpen, Type: batch.JobTypeRegular, Limit: 10, Cursor: "abc",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	q, _ := parseQuery(gotQuery)
	if q.Get("status") != "open" || q.Get("type") != "regular" || q.Get("limit") != "10" || q.Get("cursor") != "abc" {
		t.Fatalf("expected all filters forwarded, got %q", gotQuery)
	}
}

func TestListJobsOmitsUnsetFilters(t *testing.T) {
	var gotQuery string
	client, closeServer := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(batch.ListJobsResponse{})
	})
	defer closeServer()

	if _, err := client.ListJobs(context.Background(), batch.ListJobsOptions{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotQuery != "" {
		t.Fatalf("expected no query params when nothing is set, got %q", gotQuery)
	}
}

func TestGetJobReturnsTheProjectedJob(t *testing.T) {
	client, closeServer := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/jobs/job_123" || r.Method != http.MethodGet {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(batch.Job{JobID: "job_123", Status: batch.JobStatusOpen})
	})
	defer closeServer()

	job, err := client.GetJob(context.Background(), "job_123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if job.JobID() != "job_123" {
		t.Fatalf("unexpected job: %+v", job)
	}
}

func TestDeleteJobSendsDeleteAndSucceedsOnNoBody(t *testing.T) {
	var gotMethod string
	client, closeServer := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		w.WriteHeader(http.StatusAccepted) // async delete: 202, no body
	})
	defer closeServer()

	if err := client.DeleteJob(context.Background(), "job_123"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotMethod != http.MethodDelete {
		t.Fatalf("expected DELETE, got %s", gotMethod)
	}
}

func TestStopRunReturnsUpdatedJob(t *testing.T) {
	client, closeServer := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/jobs/job_123/stop" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(batch.Job{JobID: "job_123"})
	})
	defer closeServer()

	if _, err := client.StopRun(context.Background(), "job_123"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRerunWithoutStatusSendsNoQueryParam(t *testing.T) {
	var gotQuery string
	client, closeServer := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(batch.RerunJobResponse{JobID: "job_123"})
	})
	defer closeServer()

	if _, err := client.Rerun(context.Background(), "job_123", batch.RerunOptions{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotQuery != "" {
		t.Fatalf("expected no status query param for a full rerun, got %q", gotQuery)
	}
}

func TestRerunWithStatusSendsItAsQueryParamNotBody(t *testing.T) {
	// Regression test: rerun's real shape (per docs/api/conveyor/api.md) is `status` as a
	// query string and Idempotency-Key as a header — never a JSON body field.
	var gotQuery, gotHeader string
	var gotBody map[string]any
	client, closeServer := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		gotHeader = r.Header.Get("Idempotency-Key")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(batch.RerunJobResponse{JobID: "job_123"})
	})
	defer closeServer()

	_, err := client.Rerun(context.Background(), "job_123", batch.RerunOptions{
		Status: "failed,pending", IdempotencyKey: "retry-1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotQuery != "status=failed%2Cpending" {
		t.Fatalf("expected status as a query param, got %q", gotQuery)
	}
	if gotHeader != "retry-1" {
		t.Fatalf("expected idempotency key as a header, got %q", gotHeader)
	}
	if len(gotBody) != 0 {
		t.Fatalf("expected an empty request body, got %v", gotBody)
	}
}

func TestListRunsForwardsPagination(t *testing.T) {
	var gotQuery string
	client, closeServer := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(batch.ListRunsResponse{})
	})
	defer closeServer()

	if _, err := client.ListRuns(context.Background(), "job_123", batch.ListRunsOptions{Limit: 5, Cursor: "xyz"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	q, _ := parseQuery(gotQuery)
	if q.Get("limit") != "5" || q.Get("cursor") != "xyz" {
		t.Fatalf("expected pagination forwarded, got %q", gotQuery)
	}
}

func TestGetRunReturnsRun(t *testing.T) {
	client, closeServer := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/jobs/job_123/runs/run_1" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(batch.Run{RunID: "run_1", Status: batch.RunStatusCompleted})
	})
	defer closeServer()

	run, err := client.GetRun(context.Background(), "job_123", "run_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if run.Status() != batch.RunStatusCompleted {
		t.Fatalf("unexpected run: %+v", run)
	}
}

func TestDeleteRunSendsDelete(t *testing.T) {
	var gotMethod, gotPath string
	client, closeServer := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		w.WriteHeader(http.StatusAccepted)
	})
	defer closeServer()

	if err := client.DeleteRun(context.Background(), "job_123", "run_1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotMethod != http.MethodDelete || gotPath != "/jobs/job_123/runs/run_1" {
		t.Fatalf("unexpected request: %s %s", gotMethod, gotPath)
	}
}

func TestGetTaskContentLatestRun(t *testing.T) {
	client, closeServer := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/jobs/job_123/tasks/task_1/content" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		_, _ = w.Write([]byte("<html>latest</html>"))
	})
	defer closeServer()

	content, err := client.GetTaskContent(context.Background(), "job_123", "task_1", batch.GetTaskContentOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(content) != "<html>latest</html>" {
		t.Fatalf("unexpected content: %q", content)
	}
}

func TestGetTaskContentSpecificRun(t *testing.T) {
	client, closeServer := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/jobs/job_123/runs/run_1/tasks/task_1/content" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		_, _ = w.Write([]byte("<html>run_1</html>"))
	})
	defer closeServer()

	content, err := client.GetTaskContent(context.Background(), "job_123", "task_1", batch.GetTaskContentOptions{RunID: "run_1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(content) != "<html>run_1</html>" {
		t.Fatalf("unexpected content: %q", content)
	}
}

func TestGetTaskContentSurfacesAPIErrorOnFailure(t *testing.T) {
	client, closeServer := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(batch.ProblemDetail{Title: "Not Found", Detail: "task does not exist"})
	})
	defer closeServer()

	_, err := client.GetTaskContent(context.Background(), "job_123", "missing", batch.GetTaskContentOptions{})
	apiErr, ok := err.(batch.APIError)
	if !ok {
		t.Fatalf("expected batch.APIError, got %v (%T)", err, err)
	}
	if apiErr.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", apiErr.StatusCode)
	}
	if apiErr.Detail == nil || apiErr.Detail.Detail != "task does not exist" {
		t.Fatalf("expected parsed problem detail, got %+v", apiErr.Detail)
	}
}

func TestAPIErrorGracefullyHandlesNonJSONErrorBody(t *testing.T) {
	client, closeServer := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("<html>502 Bad Gateway</html>"))
	})
	defer closeServer()

	_, err := client.GetJob(context.Background(), "job_123")
	apiErr, ok := err.(batch.APIError)
	if !ok {
		t.Fatalf("expected batch.APIError even for a non-JSON error body, got %v (%T)", err, err)
	}
	if apiErr.StatusCode != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", apiErr.StatusCode)
	}
	if apiErr.Detail != nil {
		t.Fatalf("expected no parsed detail for a non-JSON body, got %+v", apiErr.Detail)
	}
	if string(apiErr.Body) != "<html>502 Bad Gateway</html>" {
		t.Fatalf("expected the raw body to be preserved, got %q", apiErr.Body)
	}
}

func TestSubmitJobForwardsIdempotencyKeyAsHeaderNotBody(t *testing.T) {
	var gotHeader string
	var gotBody map[string]any
	client, closeServer := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("Idempotency-Key")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(batch.SubmitJobResponse{JobID: "job_123"})
	})
	defer closeServer()

	_, err := client.SubmitJob(context.Background(), batch.SubmitJobRequest{IdempotencyKey: "submit-once"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotHeader != "submit-once" {
		t.Fatalf("expected Idempotency-Key header, got %q", gotHeader)
	}
	_, hasSnakeCase := gotBody["idempotency_key"]
	_, hasPascalCase := gotBody["IdempotencyKey"]
	if hasSnakeCase || hasPascalCase {
		t.Fatalf("expected the idempotency key to never be serialized into the JSON body, got %v", gotBody)
	}
}

func TestOptionsConfigureBaseURLAndAPIKey(t *testing.T) {
	var gotKey, gotHost string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("X-API-Key")
		gotHost = r.Host
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(batch.Job{})
	}))
	defer server.Close()

	client := batch.NewClient(batch.WithBaseURL(server.URL), batch.WithAPIKey("custom-key"))
	if _, err := client.GetJob(context.Background(), "job_123"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotKey != "custom-key" {
		t.Fatalf("expected WithAPIKey to configure the X-API-Key header, got %q", gotKey)
	}
	if gotHost == "" {
		t.Fatal("expected WithBaseURL to route the request to the test server")
	}
}
