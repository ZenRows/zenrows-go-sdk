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
run, err := job.Run().Wait(ctx, batch.WaitForRunOptions{}) // block until terminal
check(err)
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

// Estimate credit cost before submitting (pure, no network call).
estimate := client.EstimateCost([]batch.Task{{URL: "https://example.com"}}, nil)
fmt.Println(estimate) // "1 credits (1 tasks)"
```

## Verifying webhooks

A job's webhook gets one event when a run completes or fails on its own (`run.completed` or
`run.failed`); stopping a run sends none. Delivery is retried with backoff for a limited time, so
an event can arrive more than once, and delivery isn't guaranteed. When signing is on, check the
`X-Signature` header before trusting the body. Pass the body exactly as received, and the HMAC
secret exactly as the API returned it when you created the key (a base64 string), keyed by its
key id. Keep the old and new secrets during a key rotation.

```go
func handleWebhook(w http.ResponseWriter, r *http.Request) {
    body, err := io.ReadAll(r.Body)
    if err != nil {
        http.Error(w, "bad body", http.StatusBadRequest)
        return
    }
    secrets := map[string]string{"YOUR_KEY_ID": "YOUR_BASE64_SECRET"}
    if err := batch.VerifyWebhook(body, r.Header.Get(batch.SignatureHeader), secrets, batch.VerifyWebhookOptions{}); err != nil {
        http.Error(w, "invalid signature", http.StatusUnauthorized)
        return
    }
    // Deliveries can repeat: dedupe on r.Header.Get(batch.EventIDHeader).
    w.WriteHeader(http.StatusNoContent)
}
```

`VerifyWebhook` rejects a signature whose timestamp is more than 5 minutes from now
(`VerifyWebhookOptions.Tolerance` changes that, `SkipTimestampCheck` turns it off).

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
The Batch API also accepts `extract_fields` on a task and passes it on with the task's other
params; this SDK's tests don't check that it narrows a batch task's output the way it does for a
single Extract request.

## Client Initialization

Configure the client with `WithAPIKey` or the `ZENROWS_API_KEY` environment variable, and optionally
`WithBaseURL` (defaults to `https://async.api.zenrows.com/v1`), `WithTimeout` (how long one request
may take; no limit unless set) and `WithRetries` (defaults to 3: transient failures, 429/502/503/504 and
network errors, are retried on idempotent requests, and on POSTs that carry an Idempotency-Key, with
jittered exponential backoff honoring `Retry-After`).

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
`GetJobWebhook`/`PutJobWebhook`/`DeleteJobWebhook`/`TestWebhook`, plus the package-level
`VerifyWebhook` for receivers. HMAC signing keys:
`ListHMACKeys`/`RotateHMACKey`/`FinalizeHMACKey`/`CancelHMACRotation`. CSV task input:
`CreateJobInput`/`UploadCSV`. Results export: `StartResultsExport`/`GetResultsExport`/
`WaitForExport`/`DownloadAllResults`.

## Error Handling

- `NotConfiguredError`: the client is missing an API key.
- `APIError`: a non-2xx response. `StatusCode` carries the HTTP status; `Detail` carries the parsed
  RFC 7807 Problem JSON body when the response could be decoded as such; `.Code()` returns the
  stable problem code (e.g. `idempotency_key_conflict`), or `"internal"` if the body wasn't parseable.
- Starting, rerunning or resuming a job whose API key hit one of its credit caps returns an `APIError`
  with `StatusCode` 402 and `.Code() == "api_key_cap_reached"`; `Detail.Detail` names the cap and when
  it resets (UTC). A run the server auto-fails carries `FailureReason` (e.g.
  `FailureReasonAPIKeyCapReached`) and a human-readable `FailureDetail`.

- String enums on responses (`JobStatus`, `RunStatus`, `TaskStatus`, `ResultType`, `IngestStatus`,
  `FailureReason`, `ExportStatus`, `RunTrigger`) are extensible: the server may return values this SDK has no
  constant for. They decode without error, so always handle a default case; each type's `IsKnown()`
  reports whether a value is one this SDK version defines.

## License

This project is licensed under the MIT License - see the [LICENSE](../../LICENSE) file for details.
