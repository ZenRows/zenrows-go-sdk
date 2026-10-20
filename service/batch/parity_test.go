package batch_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zenrows/zenrows-go-sdk/service/batch"
)

const (
	parityJobID   = "job_1"
	parityBad     = "bad"
	parityURLA    = "https://a.example"
	parityExpired = "old"
)

func TestTaskStatusExpiredIsKnown(t *testing.T) {
	if !batch.TaskStatusExpired.IsKnown() {
		t.Fatal("expired must be a known task status")
	}
	var row batch.TaskResult
	if err := json.Unmarshal([]byte(`{"task_id":"t","status":"expired","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-02T00:00:00Z"}`), &row); err != nil {
		t.Fatal(err)
	}
	if row.Status != batch.TaskStatusExpired || row.CreatedAt == "" || row.UpdatedAt == "" {
		t.Fatalf("unexpected decode: %+v", row)
	}
}

func TestRunTriggerDecodes(t *testing.T) {
	var run batch.Run
	if err := json.Unmarshal([]byte(`{"run_id":"r","trigger":"scheduled"}`), &run); err != nil {
		t.Fatal(err)
	}
	if run.Trigger != batch.RunTriggerScheduled || !run.Trigger.IsKnown() {
		t.Fatalf("trigger = %q", run.Trigger)
	}
	if batch.RunTrigger("future").IsKnown() {
		t.Fatal("unknown trigger reported as known")
	}
}

func TestCostEstimateString(t *testing.T) {
	client := batch.NewClient(batch.WithAPIKey("k"))
	exact := client.EstimateCost([]batch.Task{{URL: parityURLA}, {URL: "https://b.example"}}, map[string]any{string(batch.TierJS): true})
	if got := exact.String(); got != "10 credits (2 tasks)" {
		t.Fatalf("exact: %q", got)
	}
	var autoParams map[string]any
	if err := json.Unmarshal([]byte(`{"mode":"auto"}`), &autoParams); err != nil {
		t.Fatal(err)
	}
	ranged := client.EstimateCost([]batch.Task{{URL: parityURLA}}, autoParams)
	if got := ranged.String(); got != "1-25 credits (1 tasks)" {
		t.Fatalf("range: %q", got)
	}
}

func TestBulkDownloadsSkipFailedAndExpiredRows(t *testing.T) {
	resultServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("body"))
	}))
	defer resultServer.Close()

	client, closeServer := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(batch.GetResultsResponse{Results: []batch.TaskResult{
			{TaskID: "ok", Status: batch.TaskStatusSuccessful, Type: batch.ResultTypeHTML, ResultURL: resultServer.URL},
			{TaskID: parityExpired, Status: batch.TaskStatusExpired},
			{TaskID: parityBad, Status: batch.TaskStatusFailed},
			{TaskID: "ok2", Status: batch.TaskStatusSuccessful, Type: batch.ResultTypeHTML, ResultURL: resultServer.URL},
		}})
	})
	defer closeServer()

	for _, concurrency := range []int{1, 4} {
		var dirSkipped, memSkipped []batch.TaskResult
		// MaxFiles 2 holds exactly the two rows with a body: skipped rows don't count.
		n, err := client.DownloadToDir(context.Background(), parityJobID, t.TempDir(), batch.DownloadToDirOptions{Concurrency: concurrency, Skipped: &dirSkipped, MaxFiles: 2})
		if err != nil || n != 2 {
			t.Fatalf("concurrency %d: DownloadToDir = %d, %v", concurrency, n, err)
		}
		mem, err := client.DownloadToMemory(context.Background(), parityJobID, batch.DownloadToMemoryOptions{Concurrency: concurrency, Skipped: &memSkipped})
		if err != nil || len(mem) != 2 {
			t.Fatalf("concurrency %d: DownloadToMemory = %d, %v", concurrency, len(mem), err)
		}
		for _, got := range [][]batch.TaskResult{dirSkipped, memSkipped} {
			if len(got) != 2 || got[0].TaskID != parityExpired || got[1].TaskID != parityBad {
				t.Fatalf("concurrency %d: skipped = %+v", concurrency, got)
			}
		}
		// Without Skipped the rows are still skipped, just not reported.
		if _, err := client.DownloadToMemory(context.Background(), parityJobID, batch.DownloadToMemoryOptions{Concurrency: concurrency}); err != nil {
			t.Fatalf("concurrency %d: DownloadToMemory without Skipped: %v", concurrency, err)
		}
	}
}

func TestDownloadToMemoryOptionsStaysComparable(t *testing.T) {
	// Callers could compare these options with == before; a func field would break that.
	a, b := batch.DownloadToMemoryOptions{Concurrency: 2}, batch.DownloadToMemoryOptions{Concurrency: 2}
	if a != b {
		t.Fatal("equal options compare unequal")
	}
}

func TestBulkDownloadsFailOnSuccessfulRowWithoutResultURL(t *testing.T) {
	client, closeServer := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(batch.GetResultsResponse{Results: []batch.TaskResult{
			{TaskID: "unsigned", Status: batch.TaskStatusSuccessful, Type: batch.ResultTypeHTML},
		}})
	})
	defer closeServer()

	for _, concurrency := range []int{1, 4} {
		var skipped []batch.TaskResult
		_, err := client.DownloadToMemory(context.Background(), parityJobID, batch.DownloadToMemoryOptions{Concurrency: concurrency, Skipped: &skipped})
		if err == nil || !strings.Contains(err.Error(), "unsigned") {
			t.Fatalf("concurrency %d: want an error naming the task, got %v", concurrency, err)
		}
		if len(skipped) != 0 {
			t.Fatalf("concurrency %d: a successful row must never be skipped, got %+v", concurrency, skipped)
		}
	}
}

func TestConcurrentDownloadsRespectTheRowCap(t *testing.T) {
	var mu sync.Mutex
	fetched := 0
	resultServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		fetched++
		mu.Unlock()
		time.Sleep(50 * time.Millisecond) // keep workers busy so they overlap
		_, _ = w.Write([]byte("body"))
	}))
	defer resultServer.Close()

	rows := make([]batch.TaskResult, 6)
	for i := range rows {
		rows[i] = batch.TaskResult{TaskID: string(rune('a' + i)), Status: batch.TaskStatusSuccessful, Type: batch.ResultTypeHTML, ResultURL: resultServer.URL}
	}
	client, closeServer := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(batch.GetResultsResponse{Results: rows})
	})
	defer closeServer()

	mem, err := client.DownloadToMemory(context.Background(), parityJobID, batch.DownloadToMemoryOptions{Concurrency: 4, MaxCount: 1})
	var limitErr batch.DownloadLimitExceededError
	if !errors.As(err, &limitErr) || limitErr.LimitName != "max_count" {
		t.Fatalf("want a max_count limit error, got %v", err)
	}
	if mem != nil {
		t.Fatalf("an over-cap download must return no results, got %d", len(mem))
	}
	if fetched > 1 {
		t.Fatalf("cap 1 but %d bodies were fetched", fetched)
	}
}

func TestNoDownloadStartsAfterTheFirstError(t *testing.T) {
	// Two workers: "slow" holds one slot until the test releases it, and "bad" fails after a
	// short delay, so the driver is already waiting for a free slot when the failure frees
	// one. It must stop there instead of starting "third".
	release := make(chan struct{})
	var mu sync.Mutex
	hits := map[string]int{}
	resultServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/")
		mu.Lock()
		hits[name]++
		mu.Unlock()
		switch name {
		case "slow":
			<-release
			_, _ = w.Write([]byte("body"))
		case parityBad:
			time.Sleep(100 * time.Millisecond)
			w.WriteHeader(http.StatusForbidden)
		default:
			_, _ = w.Write([]byte("body"))
		}
	}))
	defer resultServer.Close()

	names := []string{"slow", parityBad, "third"}
	rows := make([]batch.TaskResult, 0, len(names))
	for _, name := range names {
		rows = append(rows, batch.TaskResult{TaskID: name, Status: batch.TaskStatusSuccessful, Type: batch.ResultTypeHTML, ResultURL: resultServer.URL + "/" + name})
	}
	client, closeServer := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(batch.GetResultsResponse{Results: rows})
	})
	defer closeServer()

	done := make(chan error, 1)
	go func() {
		_, err := client.DownloadToMemory(context.Background(), parityJobID, batch.DownloadToMemoryOptions{Concurrency: 2})
		done <- err
	}()
	time.Sleep(400 * time.Millisecond) // long after "bad" failed and freed its slot
	close(release)
	if err := <-done; err == nil {
		t.Fatal("want the first download error")
	}
	mu.Lock()
	defer mu.Unlock()
	if hits["third"] != 0 {
		t.Fatalf("a download started after the first error: %v", hits)
	}
}

func TestAddTasksWithOptionsSendsIdempotencyKey(t *testing.T) {
	var gotKey string
	var gotBody map[string]any
	client, closeServer := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("Idempotency-Key")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(batch.AddTasksResponse{AcceptedTasks: 1})
	})
	defer closeServer()

	_, err := client.Job(parityJobID).AddTasksWithOptions(context.Background(), []batch.Task{{URL: parityURLA}}, batch.AddTasksOptions{LastBatch: true, IdempotencyKey: "batch-7"})
	if err != nil {
		t.Fatal(err)
	}
	if gotKey != "batch-7" || gotBody["last_batch"] != true {
		t.Fatalf("key=%q body=%v", gotKey, gotBody)
	}

	// The client-level method sends the same request.
	gotKey, gotBody = "", nil
	if _, err := client.AddTasksWithOptions(context.Background(), parityJobID, []batch.Task{{URL: parityURLA}}, batch.AddTasksOptions{IdempotencyKey: "batch-8"}); err != nil {
		t.Fatal(err)
	}
	if gotKey != "batch-8" || gotBody["last_batch"] != nil {
		t.Fatalf("client-level: key=%q body=%v", gotKey, gotBody)
	}
}

func TestClientSendsUserAgentAndNoResultsLimit(t *testing.T) {
	var ua, rawQuery string
	client, closeServer := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		ua = r.Header.Get("User-Agent")
		rawQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(batch.GetResultsResponse{})
	})
	defer closeServer()

	if _, err := client.GetResults(context.Background(), parityJobID, batch.GetResultsOptions{Status: "failed", Limit: 10}); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(ua, "zenrows-go-batch/") {
		t.Fatalf("User-Agent = %q", ua)
	}
	if strings.Contains(rawQuery, "limit") || !strings.Contains(rawQuery, "status=failed") {
		t.Fatalf("query = %q", rawQuery)
	}
}

func TestTimedOutAttemptIsRetried(t *testing.T) {
	var mu sync.Mutex
	attempts := 0
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		attempts++
		n := attempts
		mu.Unlock()
		if n == 1 {
			<-release // the first attempt hangs past the client timeout
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(batch.Job{JobID: parityJobID})
	}))
	defer server.Close()
	defer close(release)

	client := batch.NewClient(batch.WithBaseURL(server.URL), batch.WithAPIKey("k"), batch.WithTimeout(100*time.Millisecond), batch.WithRetries(1))
	job, err := client.GetJob(context.Background(), parityJobID)
	if err != nil {
		t.Fatalf("a timed-out attempt should be retried, got %v", err)
	}
	if job.JobID() != parityJobID || attempts != 2 {
		t.Fatalf("job=%q attempts=%d", job.JobID(), attempts)
	}
}

func TestNoRequestTimeoutByDefault(t *testing.T) {
	// Without WithTimeout (or with 0) a request may take as long as the server needs, as
	// before this option existed.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(batch.Job{JobID: parityJobID})
	}))
	defer server.Close()
	for _, opts := range [][]batch.Option{{}, {batch.WithTimeout(0)}} {
		client := batch.NewClient(append([]batch.Option{batch.WithBaseURL(server.URL), batch.WithAPIKey("k"), batch.WithRetries(0)}, opts...)...)
		if _, err := client.GetJob(context.Background(), parityJobID); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}
}

func TestWithTimeoutBoundsARequest(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
	}))
	defer server.Close()
	defer close(release)

	client := batch.NewClient(batch.WithBaseURL(server.URL), batch.WithAPIKey("k"), batch.WithTimeout(50*time.Millisecond), batch.WithRetries(0))
	start := time.Now()
	if _, err := client.GetJob(context.Background(), parityJobID); err == nil {
		t.Fatal("expected a timeout error")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("request was not bounded: %v", elapsed)
	}
}
