// Command submit-and-download submits a small Batch job, waits for its run, retries the failed
// tasks once, and downloads every result body into a directory.
//
//	ZENROWS_API_KEY=... go run ./examples/submit-and-download
package main

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/zenrows/zenrows-go-sdk/service/batch"
)

const (
	requestTimeout = 30 * time.Second
	runTimeout     = 10 * time.Minute
)

func main() {
	// The client reads ZENROWS_API_KEY when WithAPIKey isn't given.
	client := batch.NewClient(batch.WithTimeout(requestTimeout))
	ctx := context.Background()

	estimate := client.EstimateCost([]batch.Task{{URL: "https://example.com/1"}, {URL: "https://example.com/2"}}, nil)
	fmt.Println("estimate:", estimate)

	job, err := client.SubmitRegular(ctx, batch.SubmitRegularOptions{
		Name: "example",
		Tasks: []batch.Task{
			{URL: "https://example.com/1", ExternalID: "page-1"},
			{URL: "https://example.com/2", ExternalID: "page-2"},
		},
		// Reusing the key on a retry replays the first response instead of creating a second job.
		IdempotencyKey: rand.Text(),
		WaitForIngest:  true,
	})
	if err != nil {
		log.Fatal(explain(err))
	}
	fmt.Println("submitted", job.JobID())

	run, err := job.Run().Wait(ctx, batch.WaitForRunOptions{Timeout: runTimeout})
	var paused batch.RunPausedError
	switch {
	case errors.As(err, &paused):
		log.Fatalf("run %s is paused; resume it to continue", paused.Run.RunID)
	case err != nil:
		log.Fatal(explain(err))
	}
	if run.Status() == batch.RunStatusFailed {
		// The run stopped for an account reason (credits, subscription, key cap).
		log.Fatalf("run failed: %s", run.Data.FailureReason)
	}
	stats := run.Stats()
	fmt.Printf("run %s: %d successful, %d failed\n", run.Status(), stats.Successful, stats.Failed)

	if stats.Failed > 0 {
		retry, err := job.RetryFailed(ctx, batch.RetryFailedOptions{IdempotencyKey: rand.Text(), WaitForIngest: true})
		if err != nil {
			log.Fatal(explain(err))
		}
		if run, err = retry.Wait(ctx, batch.WaitForRunOptions{Timeout: runTimeout}); err != nil {
			log.Fatal(explain(err))
		}
	}

	// Failed rows, and successful rows past their retention window (TaskStatusExpired), have no
	// body: they are skipped and collected here instead of stopping the download.
	var skipped []batch.TaskResult
	written, err := run.DownloadToDir(ctx, "./results", batch.DownloadToDirOptions{Concurrency: 4, Skipped: &skipped})
	if err != nil {
		log.Fatal(explain(err))
	}
	fmt.Println("downloaded", written, "skipped", len(skipped))
}

// explain turns the Batch API errors a caller can act on into a hint.
func explain(err error) error {
	var apiErr batch.APIError
	if !errors.As(err, &apiErr) {
		return err
	}
	switch apiErr.Code() {
	case batch.CodePaymentRequired:
		return fmt.Errorf("out of credits, top up in Billing: %w", err)
	case batch.CodeSubscriptionInactive:
		return fmt.Errorf("the subscription isn't active, check Billing: %w", err)
	case batch.CodeAPIKeyCapReached:
		return fmt.Errorf("this API key reached its credit cap: %w", err)
	case batch.CodeQuotaExceeded:
		return fmt.Errorf("too many active or scheduled jobs; wait for one to finish: %w", err)
	default:
		return err
	}
}
