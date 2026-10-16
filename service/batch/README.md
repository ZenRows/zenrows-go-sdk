# Zenrows Batch API Go SDK

This is the Go SDK for the Zenrows Batch API — an asynchronous, many-URL scraping service, separate
from [Fetch](../api/README.md) (different base URL, different auth header, and a
job/run/task lifecycle instead of a single request/response).

## Model

A **Job** is a template — a reusable submission with its configuration. A **Run** is one execution
of that template; a job has 1+ runs over its lifetime. A **Task** is one URL within one run.

A job's `Status` is either:

- **`JobStatusOpen`** — keeps accepting more tasks via `AddTasks()` until you call `CloseJob()`.
  Useful when you're streaming URLs in over time rather than knowing the full list upfront.
- **`JobStatusClosed`** (the default when you submit tasks upfront) — a normal one-shot batch.

## Installation

```bash
go get github.com/zenrows/zenrows-go-sdk/service/batch
```

## Usage — resource handles

`Client.Job(id)` / `Client.Run(jobID, runID)` mint zero-network-call resource handles with
chainable lifecycle operations — the same pattern as this SDK's Python counterpart. Prefer these
over the raw `Client.*` methods below for anything beyond a one-off call.

```go
import (
    "context"
    "fmt"
    "log"

    "github.com/zenrows/zenrows-go-sdk/service/batch"
)

client := batch.NewClient(batch.WithAPIKey("YOUR_API_KEY"))
ctx := context.Background()
check := func(err error) {
    if err != nil {
        log.Fatal(err)
    }
}

// One-shot batch: every task known upfront.
job, err := client.SubmitRegular(ctx, batch.SubmitRegularOptions{
    Tasks: []batch.Task{{URL: "https://example.com/1"}, {URL: "https://example.com/2"}},
})
check(err)

// Streaming batch: keep the job open, add tasks as they arrive, close when done.
streaming, err := client.SubmitOpen(ctx, batch.SubmitOpenOptions{})
check(err)
_, err = streaming.AddTasks(ctx, []batch.Task{{URL: "https://example.com/3"}}, false)
check(err)
// With an Idempotency-Key, retrying a finished call with the same body (within 24 hours)
// replays its response instead of appending the tasks twice.
_, err = streaming.AddTasksWithOptions(ctx, []batch.Task{{URL: "https://example.com/4"}},
    batch.AddTasksOptions{IdempotencyKey: "orders-batch-4"})
check(err)
_, err = streaming.Close(ctx)
check(err)

// Scheduled: fire on a recurring or one-shot schedule. At/Rate/Calendar check their shape
// client-side; the API also rejects a rate more frequent than the account minimum (60 minutes
// by default).
rate, err := batch.NewRate(6, "hour")
check(err)
scheduled, err := client.SubmitScheduled(ctx, batch.SubmitScheduledOptions{
    Schedule: rate, Tasks: []batch.Task{{URL: "https://example.com/poll"}},
})
check(err)
_, err = scheduled.Schedule().Pause(ctx) // scheduled jobs only
check(err)

// Chainable navigation: job.Run() is the current-run facet, job.Schedule() the schedule facet.
// Wait blocks until the run is completed, stopped or failed. A paused run ends the wait with a
// RunPausedError, since it makes no progress until it is resumed.
run, err := job.Run().Wait(ctx, batch.WaitForRunOptions{})
check(err)
if run.Status() == batch.RunStatusFailed {
    // An account reason stopped the run: FailureReason is insufficient_credits,
    // subscription_inactive or api_key_cap_reached, and FailureDetail explains it.
    log.Fatalf("run failed: %s", run.Data.FailureReason)
}
for result, err := range run.Results(ctx, "") { // auto-paginated
    check(err)
    fmt.Println(result.TaskID, result.Status)
}
// Bulk downloads skip failed tasks and successful results past their retention window (14 days
// by default, counted from each row's UpdatedAt; a partial rerun resets UpdatedAt on the rows
// it copies). Expired rows come back as TaskStatusExpired; Skipped collects both kinds.
var skipped []batch.TaskResult
written, err := run.DownloadToDir(ctx, "./out", batch.DownloadToDirOptions{Skipped: &skipped})
check(err)
fmt.Println("downloaded", written, "skipped", len(skipped))

// Retry what failed. Every rerun's task rows are written in the background (the API answers
// 202); WaitForIngest returns once they are, so the new run's results pages are complete.
retry, err := job.RetryFailed(ctx, batch.RetryFailedOptions{IncludePending: true, WaitForIngest: true})
check(err)
fmt.Println("retrying in run", retry.RunID())

// Estimate credit cost before submitting (pure, no network call).
estimate := client.EstimateCost([]batch.Task{{URL: "https://example.com"}}, nil)
fmt.Println(estimate) // "1 credits (1 tasks)"
```

## Webhooks

Set a job's webhook at submit time (`Webhook` in the submit options) or later with
`JobRef.SetWebhook(ctx, url, signature)` / `Client.SetJobWebhook(ctx, jobID, url, signature)`.
The signing choice is a required argument, so changing only the URL can't turn signing off.
`PutJobWebhook` is deprecated: a `WebhookConfig` without `Signature` set sends `false`.

A job's webhook gets one event when a run completes or fails on its own
(`WebhookEventRunCompleted` or `WebhookEventRunFailed`); stopping a run sends none. Each attempt
times out after 10 seconds, and delivery is retried with backoff for up to 3 days, so an event
can arrive more than once: dedupe on `WebhookEvent.EventID` (also the `X-ZenRows-Event-Id`
header).

### Verifying a signature

When signing is on, check the `X-Signature` header before trusting the body. Pass the body
exactly as received, and the HMAC secret exactly as the API returned it when you created the key
(a base64 string; `VerifyWebhookSignature` decodes it), keyed by its key id. Keep the old and new
secrets during a key rotation.

```go
func handleWebhook(w http.ResponseWriter, r *http.Request) {
    body, err := io.ReadAll(r.Body)
    if err != nil {
        http.Error(w, "bad body", http.StatusBadRequest)
        return
    }
    secrets := map[string]string{"YOUR_KEY_ID": "YOUR_BASE64_SECRET"}
    if err := batch.VerifyWebhookSignature(body, r.Header.Get(batch.SignatureHeader), secrets, batch.VerifyWebhookSignatureOptions{}); err != nil {
        http.Error(w, "invalid signature", http.StatusUnauthorized)
        return
    }
    var event batch.WebhookEvent
    if err := json.Unmarshal(body, &event); err != nil {
        http.Error(w, "bad json", http.StatusBadRequest)
        return
    }
    // Deliveries can repeat: dedupe on event.EventID.
    w.WriteHeader(http.StatusNoContent)
}
```

`VerifyWebhookSignature` rejects a signature whose timestamp is more than 5 minutes from now
(`VerifyWebhookSignatureOptions.Tolerance` changes that, `SkipTimestampCheck` turns it off).

## Extract in a batch

Set `extract` in `ZenRowsParams` to run tasks through Extract, which returns structured data instead
of raw HTML. It works job-wide or per task; a task's params are merged over the job's, and the
task's value wins on a collision. A task can't switch off a job-level `extract`, so to mix Extract
and raw-HTML tasks in one job, set `extract` on just the tasks that need it:

```go
job, err := client.SubmitRegular(ctx, batch.SubmitRegularOptions{
    Tasks: []batch.Task{
        // Runs through Extract.
        {URL: "https://example.com/products", ExternalID: "p1", ZenRowsParams: map[string]any{"extract": "auto"}},
        // Returns the raw page.
        {URL: "https://example.com/raw"},
    },
})
if err != nil {
    log.Fatal(err)
}
fmt.Println("submitted", job.JobID())
```

An Extract task's result carries two keys — `html` (the raw page) and `parsed` (the structured
data). It costs the same as a regular task, so `EstimateCost` prices it correctly.
`extract_fields` narrows the returned fields, as on a single Extract request.

## Client Initialization

Configure the client with `WithAPIKey` or the `ZENROWS_API_KEY` environment variable, and optionally
`WithBaseURL` (defaults to `https://async.api.zenrows.com/v1`), `WithTimeout` (how long one request
may take; no limit unless set) and `WithRetries` (defaults to 3: transient failures, 429/502/503/504 and
network errors, are retried on idempotent requests, and on POSTs that carry an Idempotency-Key, with
jittered exponential backoff honoring `Retry-After`, capped at 30 seconds). A 429 `quota_exceeded` is
an account limit, not a transient one, so it is returned at once.

## Resource handles

- **`JobRef`** (`Client.Job(id)`, or returned by `Submit*`) — `Load`, `Close`, `Delete`, `Rerun`,
  `RetryFailed`, `AddTasks`/`AddTasksWithOptions`, `Runs` (paginated), `AddFileInput`, `GetWebhook`/`SetWebhook`/
  `DeleteWebhook`, `WaitForIngest`, plus the `Run()` and `Schedule()` facets below.
  **`JobHandle`** adds a loaded `.Data Job` snapshot (from `GetJob` / `IterJobs` / mutating ops).
- **`CurrentRun`** (`jobRef.Run()`) — `Pause`/`Resume`/`Stop`/`Cancel`, `Wait`, `Results`
  (paginated), `TaskHistory`, `DownloadToDir`/`DownloadToMemory`, `StartExport`,
  `DownloadAllResults`. Always resolves to the job's latest run.
- **`ScheduleControls`** (`jobRef.Schedule()`) — `Pause`/`Resume`/`Update` (scheduled jobs only).
- **`RunRef`** (`Client.Run(jobID, runID)`) — same shape as `CurrentRun` but for one specific
  (usually historical) run; no `Pause`/`Stop` (the API only suspends the *current* run).
  **`RunHandle`** adds `.Data Run`, `.Status()`, `.Stats()`.
- **`ExportRef`** (`run.StartExport()` / `run.Export(id)`) — `Load`, `Wait`, `DownloadToPath`.
  **`ExportHandle`** adds `.Data Export`, `.Status()`.

Mutating handle methods always return a **fresh** handle carrying the server's new state — none of
them mutate the receiver in place.

## Client-level methods

Submit: `SubmitJob` (low-level), `SubmitRegular`, `SubmitOpen`, `SubmitScheduled`, `EstimateCost`.
Jobs: `ListJobs`/`IterJobs`, `GetJob`, `DeleteJob`, `AddTasks`/`AddTasksWithOptions`, `CloseJob`, `StopRun`, `Rerun`,
`WaitForRun`, `WaitForIngest`. Runs: `ListRuns`/`IterRuns`, `GetRun`, `DeleteRun`. Results:
`GetResults`/`IterResults` (leave `RunID` empty for the latest run), `GetTaskContent`,
`GetTaskHistory`. Downloads: `DownloadToDir`, `DownloadToMemory` (concurrency-limited, with
`max_files`/`max_bytes_per_file`/`max_count`/`max_total_bytes` safety caps). Webhooks:
`GetJobWebhook`/`SetJobWebhook`/`DeleteJobWebhook`/`TestWebhook` (`PutJobWebhook` is deprecated),
plus the package-level `VerifyWebhookSignature` and `WebhookEvent` for receivers. HMAC signing keys:
`ListHMACKeys`/`RotateHMACKey`/`FinalizeHMACKey`/`CancelHMACRotation`. CSV task input:
`CreateJobInput`/`UploadCSV`. Results export: `StartResultsExport`/`GetResultsExport`/
`WaitForExport`/`DownloadAllResults`.

## Error Handling

- `NotConfiguredError`: the client is missing an API key.
- `APIError`: a non-2xx response. `StatusCode` carries the HTTP status; `Detail` carries the parsed
  RFC 9457 problem JSON body when the response could be decoded as such; `.Code()` returns the
  stable problem code, or `"internal"` if the body wasn't parseable. Compare it with the `Code*`
  constants (`codes.go`), keeping a default branch for codes added later:

  | Code | Status | Meaning |
  |---|---|---|
  | `CodeUnauthenticated` | 401 | Missing or invalid API key. |
  | `CodeSubscriptionInactive` | 401 | The key is valid but the subscription isn't active (for example past due). Check Billing. |
  | `CodePaymentRequired` | 402 | Out of credits. |
  | `CodeAPIKeyCapReached` | 402 | The API key reached one of its credit caps; `Detail.Detail` names the cap and when it resets (UTC). |
  | `CodeQuotaExceeded` | 429 | An account limit: 3 active jobs (shared with Crawl), 5 scheduled jobs, or an open job's 100,000-task cap. Not retried. |
  | `CodeIdempotencyKeyConflict` | 409 | The Idempotency-Key was reused with a different body, or the first call is still running. |
  | `CodePartialRerunWindowExpired` | 409 | A partial rerun came more than 10 days after the run it retries; a full rerun still works. |
  | `CodeResultExpired` | 410 | The task's result is past its retention window (14 days by default). |
  | `CodeInvalidArgument` | 400 | Validation failed; per-task reasons are in `Detail.InvalidTasks`. |
- A run that stops for an account reason ends `RunStatusFailed` (not an error from the waiter) with
  `FailureReason` (`FailureReasonInsufficientCredits`, `FailureReasonSubscriptionInactive`,
  `FailureReasonAPIKeyCapReached`) and a human-readable `FailureDetail`.
- `RunPausedError`: a run waiter found the run paused. Resume it, or set
  `WaitForRunOptions.WaitThroughPause` to keep polling.
- `WaiterTimeoutError`: a waiter ran out of its own timeout. The run itself is unaffected.

- String enums on responses (`JobStatus`, `RunStatus`, `TaskStatus`, `ResultType`, `IngestStatus`,
  `FailureReason`, `ExportStatus`, `RunTrigger`) are extensible: the server may return values this SDK has no
  constant for. They decode without error, so always handle a default case; each type's `IsKnown()`
  reports whether a value is one this SDK version defines.

## Examples

- [`examples/submit-and-download`](examples/submit-and-download/main.go): submit, wait, retry the
  failed tasks, download the results, and turn error codes into hints.
- [`examples/webhook-receiver`](examples/webhook-receiver/main.go): verify, decode and dedupe
  webhook deliveries.

## License

This project is licensed under the MIT License - see the [LICENSE](../../LICENSE) file for details.
