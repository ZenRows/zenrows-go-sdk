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

    "github.com/zenrows/zenrows-go-sdk/service/batch"
)

client := batch.NewClient(batch.WithAPIKey("YOUR_API_KEY"))
ctx := context.Background()

// One-shot batch: every task known upfront.
job, err := client.SubmitRegular(ctx, batch.SubmitRegularOptions{
    Tasks: []batch.Task{{URL: "https://example.com/1"}, {URL: "https://example.com/2"}},
})

// Streaming batch: keep the job open, add tasks as they arrive, close when done.
streaming, err := client.SubmitOpen(ctx, batch.SubmitOpenOptions{})
_, err = streaming.AddTasks(ctx, []batch.Task{{URL: "https://example.com/3"}}, false)
closed, err := streaming.Close(ctx)

// Scheduled: fire on a recurring or one-shot schedule. At/Rate/Calendar validate client-side the
// same way the server does.
rate, err := batch.NewRate(15, "minute")
scheduled, err := client.SubmitScheduled(ctx, batch.SubmitScheduledOptions{
    Schedule: rate, Tasks: []batch.Task{{URL: "https://example.com/poll"}},
})

// Chainable navigation: job.Run() is the current-run facet, job.Schedule() the schedule facet.
run, err := job.Run().Wait(ctx, batch.WaitForRunOptions{})     // block until terminal
for result, err := range run.Results(ctx, "") { _ = result }    // auto-paginated
_, err = job.Schedule().Pause(ctx)                              // scheduled jobs only

// Estimate credit cost before submitting (pure, no network call).
estimate := client.EstimateCost([]batch.Task{{URL: "https://example.com"}}, nil)
fmt.Println(estimate) // "1 credits (1 tasks)"
```

## Client Initialization

Configure the client with `WithAPIKey` or the `ZENROWS_API_KEY` environment variable, and optionally
`WithBaseURL` (defaults to `https://async.api.zenrows.com/v1`) and `WithRetries` (defaults to 3 —
transient failures, 429/502/503/504 and network errors, are retried on idempotent requests with
jittered exponential backoff honoring `Retry-After`).

## Resource handles

- **`JobRef`** (`Client.Job(id)`, or returned by `Submit*`) — `Load`, `Close`, `Delete`, `Rerun`,
  `RetryFailed`, `AddTasks`, `Runs` (paginated), `AddFileInput`, `GetWebhook`/`SetWebhook`/
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
Jobs: `ListJobs`/`IterJobs`, `GetJob`, `DeleteJob`, `AddTasks`, `CloseJob`, `StopRun`, `Rerun`,
`WaitForRun`, `WaitForIngest`. Runs: `ListRuns`/`IterRuns`, `GetRun`, `DeleteRun`. Results:
`GetResults`/`IterResults` (leave `RunID` empty for the latest run), `GetTaskContent`,
`GetTaskHistory`. Downloads: `DownloadToDir`, `DownloadToMemory` (concurrency-limited, with
`max_files`/`max_bytes_per_file`/`max_count`/`max_total_bytes` safety caps). Webhooks:
`GetJobWebhook`/`PutJobWebhook`/`DeleteJobWebhook`/`TestWebhook`. HMAC signing keys:
`ListHMACKeys`/`RotateHMACKey`/`FinalizeHMACKey`/`CancelHMACRotation`. CSV task input:
`CreateJobInput`/`UploadCSV`. Results export: `StartResultsExport`/`GetResultsExport`/
`WaitForExport`/`DownloadAllResults`.

## Error Handling

- `NotConfiguredError`: the client is missing an API key.
- `APIError`: a non-2xx response. `StatusCode` carries the HTTP status; `Detail` carries the parsed
  RFC 7807 Problem JSON body when the response could be decoded as such; `.Code()` returns the
  stable problem code (e.g. `idempotency_key_conflict`), or `"internal"` if the body wasn't parseable.

## License

This project is licensed under the MIT License - see the [LICENSE](../../LICENSE) file for details.
