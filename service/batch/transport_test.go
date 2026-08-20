package batch_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zenrows/zenrows-go-sdk/service/batch"
)

func TestRetriesTransientServiceUnavailable(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&attempts, 1)
		if n < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"job_id":"job_123"}`))
	}))
	defer server.Close()

	client := batch.NewClient(batch.WithBaseURL(server.URL), batch.WithAPIKey("k"))
	if _, err := client.GetJob(context.Background(), "job_123"); err != nil {
		t.Fatalf("expected the retry to eventually succeed, got: %v", err)
	}
	if got := atomic.LoadInt32(&attempts); got != 3 {
		t.Fatalf("expected exactly 3 attempts (2 failures + 1 success), got %d", got)
	}
}

func TestExhaustsRetriesAndReturnsTheLastError(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client := batch.NewClient(batch.WithBaseURL(server.URL), batch.WithAPIKey("k"), batch.WithRetries(2))
	_, err := client.GetJob(context.Background(), "job_123")
	if err == nil {
		t.Fatal("expected an error after exhausting retries")
	}
	if got := atomic.LoadInt32(&attempts); got != 3 { // 1 initial + 2 retries
		t.Fatalf("expected 3 total attempts (1 + WithRetries(2)), got %d", got)
	}
}

func TestDoesNotRetryNonIdempotentPostWithoutIdempotencyKey(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client := batch.NewClient(batch.WithBaseURL(server.URL), batch.WithAPIKey("k"))
	_, err := client.SubmitJob(context.Background(), batch.SubmitJobRequest{})
	if err == nil {
		t.Fatal("expected an error")
	}
	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Fatalf("expected exactly 1 attempt (POST without Idempotency-Key is not retried), got %d", got)
	}
}

func TestRetriesPostWhenIdempotencyKeyIsSet(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&attempts, 1)
		if n < 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"job_id":"job_123"}`))
	}))
	defer server.Close()

	client := batch.NewClient(batch.WithBaseURL(server.URL), batch.WithAPIKey("k"))
	_, err := client.SubmitJob(context.Background(), batch.SubmitJobRequest{IdempotencyKey: "once"})
	if err != nil {
		t.Fatalf("expected the retry to succeed: %v", err)
	}
	if got := atomic.LoadInt32(&attempts); got != 2 {
		t.Fatalf("expected 2 attempts (1 failure + 1 success) once an Idempotency-Key makes POST retryable, got %d", got)
	}
}

func TestDoesNotRetryNonRetryableStatus(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := batch.NewClient(batch.WithBaseURL(server.URL), batch.WithAPIKey("k"))
	if _, err := client.GetJob(context.Background(), "job_123"); err == nil {
		t.Fatal("expected an error")
	}
	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Fatalf("expected exactly 1 attempt (404 is not retryable), got %d", got)
	}
}

func TestHonorsRetryAfterHeader(t *testing.T) {
	var attempts int32
	var firstAttemptAt, secondAttemptAt time.Time
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&attempts, 1)
		if n == 1 {
			firstAttemptAt = time.Now()
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		secondAttemptAt = time.Now()
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"job_id":"job_123"}`))
	}))
	defer server.Close()

	client := batch.NewClient(batch.WithBaseURL(server.URL), batch.WithAPIKey("k"))
	if _, err := client.GetJob(context.Background(), "job_123"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	elapsed := secondAttemptAt.Sub(firstAttemptAt)
	if elapsed < 900*time.Millisecond {
		t.Fatalf("expected the client to wait ~1s per Retry-After before retrying, only waited %s", elapsed)
	}
}

func TestContextCancellationIsNotRetried(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client := batch.NewClient(batch.WithBaseURL(server.URL), batch.WithAPIKey("k"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.GetJob(ctx, "job_123"); err == nil {
		t.Fatal("expected an error for a canceled context")
	}
}

func TestWithRetriesZeroDisablesRetries(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client := batch.NewClient(batch.WithBaseURL(server.URL), batch.WithAPIKey("k"), batch.WithRetries(0))
	if _, err := client.GetJob(context.Background(), "job_123"); err == nil {
		t.Fatal("expected an error")
	}
	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Fatalf("expected exactly 1 attempt with WithRetries(0), got %d", got)
	}
}
