package batch_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zenrows/zenrows-go-sdk/service/batch"
)

const (
	consistencyJobID = "job_1"
	consistencyRunID = "run_2"
	problemJSON      = "application/problem+json"
	consistencyHook  = "https://hook.example/batch"
	consistencyEvt   = "evt_42"
)

func TestSetJobWebhookAlwaysSendsTheSigningChoice(t *testing.T) {
	for _, signature := range []bool{true, false} {
		var body map[string]any
		client, closeServer := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPut || r.URL.Path != "/jobs/"+consistencyJobID+"/webhook" {
				t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			}
			raw, _ := io.ReadAll(r.Body)
			if err := json.Unmarshal(raw, &body); err != nil {
				t.Errorf("body: %v", err)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(raw)
		})
		cfg, err := client.SetJobWebhook(context.Background(), consistencyJobID, consistencyHook, signature)
		closeServer()
		if err != nil {
			t.Fatal(err)
		}
		got, present := body["signature"]
		if !present || got != signature {
			t.Fatalf("signature sent = %v (present %v), want %v", got, present, signature)
		}
		if cfg.Signature != signature || cfg.URL != consistencyHook {
			t.Fatalf("returned config %+v", cfg)
		}
	}
}

func TestJobRefSetWebhookUsesSetJobWebhook(t *testing.T) {
	var sent string
	client, closeServer := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		sent = string(raw)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(raw)
	})
	defer closeServer()
	if _, err := client.Job(consistencyJobID).SetWebhook(context.Background(), consistencyHook, true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sent, `"signature":true`) {
		t.Fatalf("body %s", sent)
	}
}

func TestJobFormatAndWebhookEventDecode(t *testing.T) {
	var job batch.Job
	if err := json.Unmarshal([]byte(`{"job_id":"j","format":"markdown"}`), &job); err != nil {
		t.Fatal(err)
	}
	if job.Format != batch.ResultTypeMarkdown {
		t.Fatalf("format = %q", job.Format)
	}

	raw := `{"schema":"zenrows.webhook/v1","event_id":"` + consistencyEvt + `","event_type":"run.failed",` +
		`"occurred_at":"2026-10-01T00:00:00Z","job_id":"j","external_id":"x","metadata":{"k":"v"},` +
		`"run_id":"r","run_sequence":2,"stats":{"total":3,"completed":1,"successful":1,"failed":0},` +
		`"failure_reason":"insufficient_credits","failure_detail":"out of credits"}`
	var event batch.WebhookEvent
	if err := json.Unmarshal([]byte(raw), &event); err != nil {
		t.Fatal(err)
	}
	if event.EventType != batch.WebhookEventRunFailed || event.RunSequence != 2 || event.Stats == nil ||
		event.Stats.Total != 3 || event.FailureReason != batch.FailureReasonInsufficientCredits ||
		event.Metadata["k"] != "v" || event.EventID != consistencyEvt {
		t.Fatalf("unexpected decode: %+v", event)
	}
}

func TestQuotaExceededIsNotRetried(t *testing.T) {
	var attempts int32
	client, closeServer := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.Header().Set("Content-Type", problemJSON)
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"code":"quota_exceeded","status":429,"detail":"3 active jobs"}`))
	})
	defer closeServer()

	_, err := client.SubmitJob(context.Background(), batch.SubmitJobRequest{
		Tasks: []batch.Task{{URL: "https://a.example"}}, IdempotencyKey: "k1",
	})
	var apiErr batch.APIError
	if !errors.As(err, &apiErr) || apiErr.Code() != batch.CodeQuotaExceeded {
		t.Fatalf("err = %v", err)
	}
	if n := atomic.LoadInt32(&attempts); n != 1 {
		t.Fatalf("attempts = %d, want 1", n)
	}
}

func TestOtherTooManyRequestsIsStillRetried(t *testing.T) {
	var attempts int32
	client, closeServer := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		if atomic.AddInt32(&attempts, 1) == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"job_id":"j"}`))
	})
	defer closeServer()
	if _, err := client.GetJob(context.Background(), "j"); err != nil {
		t.Fatal(err)
	}
	if n := atomic.LoadInt32(&attempts); n != 2 {
		t.Fatalf("attempts = %d, want 2", n)
	}
}

func TestRerunWaitForIngest(t *testing.T) {
	// The job's latest run is a newer run_3: the response replayed for this key names run_2, and
	// the wait must follow run_2 rather than the latest run.
	var gets int32
	client, closeServer := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/jobs/"+consistencyJobID+"/rerun":
			if r.URL.Query().Get("status") != string(batch.TaskStatusFailed) {
				t.Errorf("status query = %q", r.URL.Query().Get("status"))
			}
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"job_id":"job_1","latest_run":{"run_id":"run_2","status":"pending","ingest_status":"pending"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/jobs/"+consistencyJobID+"/runs/"+consistencyRunID:
			ingest := "pending"
			if atomic.AddInt32(&gets, 1) >= 2 {
				ingest = "done"
			}
			_, _ = w.Write([]byte(`{"run_id":"run_2","job_id":"job_1","status":"completed","ingest_status":"` + ingest + `"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/jobs/"+consistencyJobID:
			_, _ = w.Write([]byte(`{"job_id":"job_1","latest_run":{"run_id":"run_3","status":"running","ingest_status":"pending"}}`))
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	})
	defer closeServer()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	run, err := client.Job(consistencyJobID).RetryFailed(ctx, batch.RetryFailedOptions{IdempotencyKey: "k", WaitForIngest: true})
	if err != nil {
		t.Fatal(err)
	}
	if run.RunID() != consistencyRunID || run.Data.IngestStatus != batch.IngestStatusDone || run.Status() != batch.RunStatusCompleted {
		t.Fatalf("run = %+v", run.Data)
	}
	if n := atomic.LoadInt32(&gets); n < 2 {
		t.Fatalf("polled run_2 %d times", n)
	}
}

func TestRerunWithoutWaitReturnsThe202Body(t *testing.T) {
	client, closeServer := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("unexpected %s %s: no wait was asked for", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"job_id":"job_1","latest_run":{"run_id":"run_2","status":"pending","ingest_status":"pending"}}`))
	})
	defer closeServer()
	resp, err := client.Rerun(context.Background(), consistencyJobID, batch.RerunOptions{Status: string(batch.TaskStatusFailed)})
	if err != nil {
		t.Fatal(err)
	}
	if resp.LatestRun.IngestStatus != batch.IngestStatusPending {
		t.Fatalf("without WaitForIngest the 202 body is returned as is, got %+v", resp.LatestRun)
	}
}

func TestWaitForIngestWaitsPastAStopMidIngest(t *testing.T) {
	// A run stopped mid-ingest is terminal while ingest_status is still pending; its rows and
	// totals are settled only once the API marks ingestion done.
	var gets int32
	client, closeServer := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		ingest := "pending"
		if atomic.AddInt32(&gets, 1) >= 2 {
			ingest = "done"
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"job_id":"job_1","latest_run":{"run_id":"run_2","status":"stopped","ingest_status":"` + ingest + `"}}`))
	})
	defer closeServer()

	job, err := client.WaitForIngest(context.Background(), consistencyJobID, batch.WaitForIngestOptions{PollInterval: 10 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if job.LatestRun.IngestStatus != batch.IngestStatusDone {
		t.Fatalf("returned before ingestion finished: %+v", job.LatestRun)
	}
}

func TestWaitStopsOnPausedRun(t *testing.T) {
	var gets int32
	client, closeServer := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&gets, 1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"run_id":"run_2","job_id":"job_1","status":"running","pause_state":"paused"}`))
	})
	defer closeServer()

	start := time.Now()
	handle, err := client.Run(consistencyJobID, consistencyRunID).Wait(context.Background(), batch.WaitForRunOptions{Timeout: 30 * time.Second})
	var paused batch.RunPausedError
	if !errors.As(err, &paused) {
		t.Fatalf("err = %v, want RunPausedError", err)
	}
	if paused.Run.RunID != consistencyRunID || handle.Data.PauseState != batch.PauseStatePaused {
		t.Fatalf("paused = %+v, handle = %+v", paused.Run, handle.Data)
	}
	if time.Since(start) > 5*time.Second || atomic.LoadInt32(&gets) != 1 {
		t.Fatalf("waited %s over %d polls", time.Since(start), gets)
	}

	_, err = client.Run(consistencyJobID, consistencyRunID).Wait(context.Background(), batch.WaitForRunOptions{
		Timeout: 50 * time.Millisecond, PollInterval: 10 * time.Millisecond, WaitThroughPause: true,
	})
	var timeout batch.WaiterTimeoutError
	if !errors.As(err, &timeout) {
		t.Fatalf("WaitThroughPause: err = %v, want WaiterTimeoutError", err)
	}
}

func TestResultExpiredCode(t *testing.T) {
	client, closeServer := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", problemJSON)
		w.WriteHeader(http.StatusGone)
		_, _ = w.Write([]byte(`{"code":"result_expired","status":410}`))
	})
	defer closeServer()
	_, err := client.GetTaskContent(context.Background(), consistencyJobID, "t1", batch.GetTaskContentOptions{})
	var apiErr batch.APIError
	if !errors.As(err, &apiErr) || apiErr.Code() != batch.CodeResultExpired {
		t.Fatalf("err = %v", err)
	}
}

func TestSetJobWebhookURLKeepsTheSigningChoice(t *testing.T) {
	for _, tc := range []struct {
		name    string
		current string // GET response body; "" means 404
		want    string
	}{
		{"signed stays signed", `{"url":"https://old.example","signature":true}`, `"signature":true`},
		{"unsigned stays unsigned", `{"url":"https://old.example","signature":false}`, `"signature":false`},
		{"no webhook yet", "", `"signature":false`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var sent string
			client, closeServer := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					if tc.current == "" {
						w.Header().Set("Content-Type", problemJSON)
						w.WriteHeader(http.StatusNotFound)
						_, _ = w.Write([]byte(`{"code":"not_found","status":404}`))
						return
					}
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(tc.current))
					return
				}
				raw, _ := io.ReadAll(r.Body)
				sent = string(raw)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write(raw)
			})
			defer closeServer()
			if _, err := client.SetJobWebhookURL(context.Background(), consistencyJobID, consistencyHook); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(sent, tc.want) || !strings.Contains(sent, consistencyHook) {
				t.Fatalf("PUT body %s, want %s", sent, tc.want)
			}
		})
	}
}

func TestSetJobWebhookURLStopsOnOtherErrors(t *testing.T) {
	var puts int32
	client, closeServer := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			atomic.AddInt32(&puts, 1)
		}
		w.Header().Set("Content-Type", problemJSON)
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"code":"unauthenticated","status":401}`))
	})
	defer closeServer()
	if _, err := client.SetJobWebhookURL(context.Background(), consistencyJobID, consistencyHook); err == nil {
		t.Fatal("want an error")
	}
	if atomic.LoadInt32(&puts) != 0 {
		t.Fatal("must not PUT when the current config can't be read")
	}
}
