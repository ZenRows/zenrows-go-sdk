package batch

// JobType is the kind of job being submitted.
type JobType string

const (
	JobTypeRegular   JobType = "regular"
	JobTypeScheduled JobType = "scheduled"
)

// JobStatus is the lifecycle state of a job.
type JobStatus string

const (
	// JobStatusOpen keeps a job accepting more tasks via Client.AddTasks until Client.CloseJob is called.
	// Use this when you're streaming URLs in over time rather than knowing the full list upfront.
	JobStatusOpen    JobStatus = "open"
	JobStatusClosed  JobStatus = "closed"
	JobStatusDeleted JobStatus = "deleted"
)

// ScheduleState is the run/pause flag on a scheduled job's future fires.
type ScheduleState string

const (
	ScheduleStateActive ScheduleState = "active"
	ScheduleStatePaused ScheduleState = "paused"
)

// RunStatus is the lifecycle state of a single run of a job.
type RunStatus string

const (
	RunStatusRunning   RunStatus = "running"
	RunStatusPending   RunStatus = "pending"
	RunStatusCompleted RunStatus = "completed"
	RunStatusStopped   RunStatus = "stopped"
	RunStatusFailed    RunStatus = "failed"
	RunStatusDeleted   RunStatus = "deleted"
)

// TerminalRunStatuses are the statuses a run never transitions out of. The default target
// for WaitForRun.
var TerminalRunStatuses = map[RunStatus]bool{
	RunStatusCompleted: true,
	RunStatusStopped:   true,
	RunStatusDeleted:   true,
}

// TaskStatus is the lifecycle state of one task within a run.
type TaskStatus string

const (
	TaskStatusPending    TaskStatus = "pending"
	TaskStatusProcessing TaskStatus = "processing"
	TaskStatusSuccessful TaskStatus = "successful"
	TaskStatusFailed     TaskStatus = "failed"
)

// ResultType is the body format of a successful task result.
type ResultType string

const (
	ResultTypeHTML      ResultType = "html"
	ResultTypeJSON      ResultType = "json"
	ResultTypeMarkdown  ResultType = "markdown"
	ResultTypePlaintext ResultType = "plaintext"
	ResultTypePDF       ResultType = "pdf"
)

// PauseState is the reversible-suspend flag on a run, orthogonal to RunStatus.
type PauseState string

const (
	PauseStateActive PauseState = "active"
	PauseStatePaused PauseState = "paused"
)

// IngestStatus reports whether a large (202) submission's task rows have finished streaming
// into storage. Present only on runs created that way.
type IngestStatus string

const (
	IngestStatusPending IngestStatus = "pending"
	IngestStatusDone    IngestStatus = "done"
)

// FailureReason is the account-level cause of a run auto-failing. Present only when
// Run.Status == RunStatusFailed.
type FailureReason string

const (
	FailureReasonInsufficientCredits  FailureReason = "insufficient_credits"
	FailureReasonSubscriptionInactive FailureReason = "subscription_inactive"
)

// ExportStatus is the lifecycle state of a results export.
type ExportStatus string

const (
	ExportStatusPending   ExportStatus = "pending"
	ExportStatusRunning   ExportStatus = "running"
	ExportStatusCompleted ExportStatus = "completed"
	ExportStatusFailed    ExportStatus = "failed"
)

// TerminalExportStatuses are the export statuses that don't transition again. The default
// target for WaitForExport.
var TerminalExportStatuses = map[ExportStatus]bool{
	ExportStatusCompleted: true,
	ExportStatusFailed:    true,
}

// Task is one URL to scrape within a job.
type Task struct {
	URL           string         `json:"url"`
	ExternalID    string         `json:"external_id,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
	Method        string         `json:"method,omitempty"`
	Body          any            `json:"body,omitempty"`
	ZenRowsParams map[string]any `json:"zenrows_params,omitempty"`
}

// WebhookConfig describes where (and how) to notify on run.completed / run.failed events.
type WebhookConfig struct {
	URL string `json:"url"`
	// Signature opts into HMAC signing of deliveries with the org's active HMAC key. Absent
	// on the wire the field defaults to false server-side; PUT requires it explicitly.
	Signature bool `json:"signature"`
}

// Spend reports credits and cost for a scoped piece of work. Indicative, not billing-grade.
type Spend struct {
	Credits float64 `json:"credits"`
	Cost    float64 `json:"cost"`
}

// TaskSpend is a task's indicative spend, both the running total across every attempt and
// just the most recent one.
type TaskSpend struct {
	Total       Spend `json:"total"`
	LastAttempt Spend `json:"last_attempt"`
}

// RunStats summarizes progress and spend for a run.
type RunStats struct {
	Total          int            `json:"total"`
	Completed      int            `json:"completed"`
	Successful     int            `json:"successful"`
	Failed         int            `json:"failed"`
	FailureReasons map[string]int `json:"failure_reasons,omitempty"`
	Spend          *Spend         `json:"spend,omitempty"`
}

// Run is one execution of a Job.
type Run struct {
	RunID             string        `json:"run_id"`
	JobID             string        `json:"job_id"`
	RunSequence       int           `json:"run_sequence"`
	Status            RunStatus     `json:"status"`
	Stats             RunStats      `json:"stats"`
	LastBatchReceived *bool         `json:"last_batch_received,omitempty"`
	PauseState        PauseState    `json:"pause_state,omitempty"`
	IngestStatus      IngestStatus  `json:"ingest_status,omitempty"`
	FailureReason     FailureReason `json:"failure_reason,omitempty"`
	CreatedAt         string        `json:"created_at"`
	UpdatedAt         string        `json:"updated_at"`
}

// ScheduleRate is an interval-based schedule fire policy — every N units.
type ScheduleRate struct {
	Every int    `json:"every"`
	Unit  string `json:"unit"`
}

// ScheduleCadence picks which days a Calendar schedule fires on. Exactly one field is set.
type ScheduleCadence struct {
	Daily   map[string]any   `json:"daily,omitempty"`
	Weekly  *ScheduleWeekly  `json:"weekly,omitempty"`
	Monthly *ScheduleMonthly `json:"monthly,omitempty"`
}

// ScheduleWeekly fires on specific 3-letter lower-case day names.
type ScheduleWeekly struct {
	Days []string `json:"days"`
}

// ScheduleMonthly fires on specific days of the month (1-31).
type ScheduleMonthly struct {
	Days []int `json:"days"`
}

// ScheduleCalendar is a calendar-style fire policy: times-of-day on a daily/weekly/monthly
// cadence.
type ScheduleCalendar struct {
	TimesOfDay []string        `json:"times_of_day"`
	Cadence    ScheduleCadence `json:"cadence"`
}

// JobSchedule is the wire schedule block attached to type=scheduled jobs. Exactly one of At,
// Rate, or Calendar is set. Build one with the At/Rate/Calendar constructors rather than by
// hand — they validate the same rules the server enforces.
type JobSchedule struct {
	At       string            `json:"at,omitempty"`
	Rate     *ScheduleRate     `json:"rate,omitempty"`
	Calendar *ScheduleCalendar `json:"calendar,omitempty"`
	Timezone string            `json:"timezone,omitempty"`
}

// Job is the reusable submission template plus a projection of its latest run.
type Job struct {
	JobID            string         `json:"job_id"`
	Type             JobType        `json:"type"`
	Status           JobStatus      `json:"status"`
	ZenRowsParams    map[string]any `json:"zenrows_params,omitempty"`
	ExternalID       string         `json:"external_id,omitempty"`
	Name             string         `json:"name,omitempty"`
	Metadata         map[string]any `json:"metadata,omitempty"`
	Schedule         *JobSchedule   `json:"schedule,omitempty"`
	NextScheduledRun string         `json:"next_scheduled_run,omitempty"`
	ScheduleState    ScheduleState  `json:"schedule_state,omitempty"`
	Webhook          *WebhookConfig `json:"webhook,omitempty"`
	LatestRun        *Run           `json:"latest_run,omitempty"`
	CreatedAt        string         `json:"created_at"`
	UpdatedAt        string         `json:"updated_at"`
}

// SubmitJobRequest is the body for Client.SubmitJob. Most callers prefer the type-specific
// SubmitRegular / SubmitOpen / SubmitScheduled, which build this for you.
type SubmitJobRequest struct {
	Type JobType `json:"type,omitempty"`
	// Status lets you open a job for incremental task submission (JobStatusOpen) instead of a
	// normal one-shot batch where every task is already known.
	Status        JobStatus      `json:"status,omitempty"`
	ZenRowsParams map[string]any `json:"zenrows_params,omitempty"`
	Schedule      *JobSchedule   `json:"schedule,omitempty"`
	Tasks         []Task         `json:"tasks,omitempty"`
	FileInputID   string         `json:"file_input_id,omitempty"`
	ExternalID    string         `json:"external_id,omitempty"`
	Name          string         `json:"name,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
	Webhook       *WebhookConfig `json:"webhook,omitempty"`
	// IdempotencyKey scopes a single submit/rerun request; resubmitting with the same key
	// returns the original response (or a conflict if the body doesn't match). Sent as a
	// header, never part of the JSON body.
	IdempotencyKey string `json:"-"`
}

// SubmitJobResponse is the response for Client.SubmitJob.
type SubmitJobResponse struct {
	JobID         string         `json:"job_id"`
	Status        JobStatus      `json:"status"`
	LatestRun     *Run           `json:"latest_run,omitempty"`
	AcceptedTasks int            `json:"accepted_tasks"`
	Webhook       *WebhookConfig `json:"webhook,omitempty"`
}

// ListJobsOptions filters/paginates Client.ListJobs.
type ListJobsOptions struct {
	Status JobStatus
	Type   JobType
	Limit  int
	Cursor string
}

// ListJobsResponse is the response for Client.ListJobs.
type ListJobsResponse struct {
	Jobs       []Job  `json:"jobs"`
	NextCursor string `json:"next_cursor,omitempty"`
}

// AddTasksResponse is the response for Client.AddTasks.
type AddTasksResponse struct {
	AcceptedTasks int       `json:"accepted_tasks"`
	JobStatus     JobStatus `json:"job_status"`
	LatestRun     *Run      `json:"latest_run,omitempty"`
}

// RerunOptions configures Client.Rerun. Status filters to a partial retry (e.g. "failed" or
// "failed,pending"); leave empty for a full replay.
type RerunOptions struct {
	Status         string
	IdempotencyKey string
}

// RerunJobResponse is the response for Client.Rerun.
type RerunJobResponse struct {
	JobID          string    `json:"job_id"`
	Status         JobStatus `json:"status"`
	LatestRun      Run       `json:"latest_run"`
	RerunOf        string    `json:"rerun_of,omitempty"`
	RetriedTasks   int       `json:"retried_tasks"`
	InheritedTasks int       `json:"inherited_tasks"`
}

// ListRunsOptions paginates Client.ListRuns.
type ListRunsOptions struct {
	Limit  int
	Cursor string
}

// ListRunsResponse is the response for Client.ListRuns.
type ListRunsResponse struct {
	Runs       []Run  `json:"runs"`
	NextCursor string `json:"next_cursor,omitempty"`
}

// TaskResult is one task's outcome within a run.
type TaskResult struct {
	TaskID      string         `json:"task_id"`
	ExternalID  string         `json:"external_id,omitempty"`
	RunID       string         `json:"run_id"`
	URL         string         `json:"url"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	Method      string         `json:"method,omitempty"`
	Status      TaskStatus     `json:"status"`
	Type        ResultType     `json:"type,omitempty"`
	ResultURL   string         `json:"result_url,omitempty"`
	Error       *Problem       `json:"error,omitempty"`
	SourceRunID string         `json:"source_run_id,omitempty"`
	Spend       *TaskSpend     `json:"spend,omitempty"`
}

// GetResultsOptions selects which run to page results for, plus pagination and a status
// filter.
type GetResultsOptions struct {
	// RunID pages results for a specific run. Leave empty to page the job's latest run.
	RunID  string
	Status string
	Cursor string
	Limit  int
}

// GetResultsResponse is the response for Client.GetResults.
type GetResultsResponse struct {
	Results    []TaskResult `json:"results"`
	NextCursor string       `json:"next_cursor,omitempty"`
}

// GetTaskContentOptions selects which run a task's content is fetched from.
type GetTaskContentOptions struct {
	// RunID fetches content from a specific run. Leave empty for the job's latest run.
	RunID string
}

// TaskHistoryEvent is one attempt in a task's history.
type TaskHistoryEvent struct {
	StartedAt string   `json:"started_at"`
	EndedAt   string   `json:"ended_at"`
	Attempt   int      `json:"attempt"`
	Error     *Problem `json:"error,omitempty"`
	Spend     *Spend   `json:"spend,omitempty"`
}

// TaskHistoryResponse is the response for Client.GetTaskHistory.
type TaskHistoryResponse struct {
	Events []TaskHistoryEvent `json:"events"`
}

// TestWebhookRequest is the body for Client.TestWebhook.
type TestWebhookRequest struct {
	URL       string `json:"url"`
	Signature bool   `json:"signature,omitempty"`
}

// TestWebhookResponse is the outcome of a synthetic webhook test dispatch.
type TestWebhookResponse struct {
	Delivered  bool   `json:"delivered"`
	EventID    string `json:"event_id"`
	StatusCode int    `json:"status_code,omitempty"`
	Error      string `json:"error,omitempty"`
	ElapsedMs  int    `json:"elapsed_ms"`
}

// CSVFields maps canonical task fields to CSV columns — a column index (int) or column name
// (string, requires Header: true). Only URL (required) and ExternalID (optional) are
// accepted.
type CSVFields struct {
	URL        any `json:"url"`
	ExternalID any `json:"external_id,omitempty"`
}

// CSVSpec configures how an uploaded CSV maps to task fields.
type CSVSpec struct {
	Delimiter string    `json:"delimiter,omitempty"`
	Quote     string    `json:"quote,omitempty"`
	Header    bool      `json:"header,omitempty"`
	Fields    CSVFields `json:"fields"`
}

// CreateJobInputRequest is the body for Client.CreateJobInput.
type CreateJobInputRequest struct {
	Type string   `json:"type"`
	CSV  *CSVSpec `json:"csv,omitempty"`
}

// FileInputUploadTarget is the presigned PUT target returned by CreateJobInput.
type FileInputUploadTarget struct {
	Method    string            `json:"method"`
	URL       string            `json:"url"`
	Headers   map[string]string `json:"headers,omitempty"`
	ExpiresAt string            `json:"expires_at"`
}

// CreateJobInputResponse is the response for Client.CreateJobInput.
type CreateJobInputResponse struct {
	FileInputID string                `json:"file_input_id"`
	Upload      FileInputUploadTarget `json:"upload"`
	ExpiresAt   string                `json:"expires_at"`
}

// HMACKeyMeta is the public view of one HMAC key — id and creation time, never the secret.
type HMACKeyMeta struct {
	Kid       string `json:"kid"`
	CreatedAt string `json:"created_at"`
}

// HMACKeyList is the org's current HMAC key slots.
type HMACKeyList struct {
	Active    *HMACKeyMeta `json:"active,omitempty"`
	Candidate *HMACKeyMeta `json:"candidate,omitempty"`
}

// HMACKeyCreated is the response to RotateHMACKey. Secret is base64-encoded raw key
// material and is returned ONLY here — capture it now.
type HMACKeyCreated struct {
	Kid       string `json:"kid"`
	Secret    string `json:"secret"` //nolint:gosec // this is the actual API response field carrying the rotated HMAC secret
	CreatedAt string `json:"created_at"`
}

// HMACKeyFinalized is the response to FinalizeHMACKey.
type HMACKeyFinalized struct {
	ActiveKid string `json:"active_kid"`
	CreatedAt string `json:"created_at"`
}

// InvalidTask describes one rejected task on a validation error.
type InvalidTask struct {
	Index  int    `json:"index"`
	Reason string `json:"reason"`
	Value  string `json:"value,omitempty"`
}

// Problem is an RFC 7807 problem+json body, as returned by every Batch API error response.
type Problem struct {
	Type         string        `json:"type,omitempty"`
	Title        string        `json:"title,omitempty"`
	Status       int           `json:"status,omitempty"`
	Code         string        `json:"code,omitempty"`
	Detail       string        `json:"detail,omitempty"`
	Instance     string        `json:"instance,omitempty"`
	InvalidTasks []InvalidTask `json:"invalid_tasks,omitempty"`
	// Extras holds any non-standard top-level members not modeled above.
	Extras map[string]any `json:"-"`
}

// StartExportResponse is the response for Client.StartResultsExport.
type StartExportResponse struct {
	ExportID  string       `json:"export_id"`
	Status    ExportStatus `json:"status"`
	CreatedAt string       `json:"created_at"`
	ExpiresAt string       `json:"expires_at"`
}

// Export is the polled view of a results export.
type Export struct {
	ExportID    string       `json:"export_id"`
	Status      ExportStatus `json:"status"`
	Error       string       `json:"error,omitempty"`
	DownloadURL string       `json:"download_url,omitempty"`
	CreatedAt   string       `json:"created_at"`
	ExpiresAt   string       `json:"expires_at"`
}
