// Package batch is a client for the Zenrows Batch API — an asynchronous batch web-scraping
// service. Submit a job containing many URLs; the API scrapes each one, tracks progress, and
// serves the results back to you.
//
// A Job is a template — a reusable submission with its configuration. A Run is one execution
// of that template; a job has 1+ runs over its lifetime. A Task is one URL within one run.
//
// The main entry point is Client (via NewClient). For most submissions prefer the
// type-specific SubmitRegular / SubmitOpen / SubmitScheduled over the low-level SubmitJob.
// Job(id) / Run(jobID, runID) mint zero-network-call resource handles with chainable
// lifecycle operations (Close, Rerun, AddTasks, Run().Pause(), Schedule().Update(), ...) — see
// resources.go. EstimateCost prices a job client-side before you submit it.
package batch

import (
	"context"
	"fmt"
	"iter"
	"net/http"
	"time"

	"github.com/go-resty/resty/v2"
)

const apiKeyHeader = "X-API-Key"

// Client is the Zenrows Batch API client.
type Client struct {
	cfg  options
	http *resty.Client
}

// NewClient creates and returns a new Zenrows Batch API client.
func NewClient(opts ...Option) *Client {
	client := &Client{cfg: defaultOptions()}
	for _, opt := range opts {
		opt.apply(&client.cfg)
	}

	client.http = resty.New().
		SetBaseURL(client.cfg.baseURL).
		SetHeader(apiKeyHeader, client.cfg.apiKey)

	return client
}

func (c *Client) isConfigured() bool {
	return c.cfg.baseURL != "" && c.cfg.apiKey != ""
}

func (c *Client) request(ctx context.Context, result any) *resty.Request {
	req := c.http.R().SetContext(ctx)
	if result != nil {
		req.SetResult(result)
	}
	return req
}

func (c *Client) do(ctx context.Context, req *resty.Request, method, path string) error {
	res, err := executeWithRetry(ctx, req, method, path, c.cfg.retries)
	if err != nil {
		return err
	}
	if res.IsError() {
		return newAPIError(res.StatusCode(), res.Body())
	}
	return nil
}

// ----- jobs (raw) -----

// SubmitJob submits a new scraping job. Nothing is created if the request fails validation —
// retrying is safe. Most callers prefer the type-specific SubmitRegular / SubmitOpen /
// SubmitScheduled, which build this request and return a JobRef.
func (c *Client) SubmitJob(ctx context.Context, jobReq SubmitJobRequest) (*SubmitJobResponse, error) {
	if !c.isConfigured() {
		return nil, NotConfiguredError{}
	}

	var result SubmitJobResponse
	req := c.request(ctx, &result).SetBody(jobReq)
	if jobReq.IdempotencyKey != "" {
		req.SetHeader("Idempotency-Key", jobReq.IdempotencyKey)
	}
	if err := c.do(ctx, req, http.MethodPost, "/jobs"); err != nil {
		return nil, err
	}
	return &result, nil
}

// ListJobs lists jobs for the authenticated caller, newest first. For most uses prefer the
// auto-paginating IterJobs.
func (c *Client) ListJobs(ctx context.Context, opts ListJobsOptions) (*ListJobsResponse, error) {
	if !c.isConfigured() {
		return nil, NotConfiguredError{}
	}

	var result ListJobsResponse
	req := c.request(ctx, &result)
	if opts.Status != "" {
		req.SetQueryParam("status", string(opts.Status))
	}
	if opts.Type != "" {
		req.SetQueryParam("type", string(opts.Type))
	}
	if opts.Limit > 0 {
		req.SetQueryParam("limit", fmt.Sprintf("%d", opts.Limit))
	}
	if opts.Cursor != "" {
		req.SetQueryParam("cursor", opts.Cursor)
	}
	if err := c.do(ctx, req, http.MethodGet, "/jobs"); err != nil {
		return nil, err
	}
	return &result, nil
}

// IterJobs auto-paginates ListJobs, yielding (Job, error) pairs. Stops (without yielding a
// further error) once the pages are exhausted; a non-nil error from the sequence should stop
// the range loop.
func (c *Client) IterJobs(ctx context.Context, opts ListJobsOptions) iter.Seq2[Job, error] {
	return func(yield func(Job, error) bool) {
		cursor := opts.Cursor
		for {
			page, err := c.ListJobs(ctx, ListJobsOptions{Status: opts.Status, Type: opts.Type, Limit: opts.Limit, Cursor: cursor})
			if err != nil {
				var zero Job
				yield(zero, err)
				return
			}
			for _, job := range page.Jobs {
				if !yield(job, nil) {
					return
				}
			}
			if page.NextCursor == "" {
				return
			}
			cursor = page.NextCursor
		}
	}
}

// Job returns a JobRef for an existing job with no network call. Lifecycle operations act on
// the id directly. Prefer this over GetJob when you just want to act on a known id.
func (c *Client) Job(jobID string) JobRef {
	return JobRef{client: c, jobID: jobID}
}

func (c *Client) getJobData(ctx context.Context, jobID string) (Job, error) {
	if !c.isConfigured() {
		return Job{}, NotConfiguredError{}
	}
	var result Job
	path := fmt.Sprintf("/jobs/%s", jobID)
	req := c.request(ctx, &result)
	if err := c.do(ctx, req, http.MethodGet, path); err != nil {
		return Job{}, err
	}
	return result, nil
}

// GetJob fetches a job (template + latest_run projection) by id and returns a loaded
// JobHandle.
func (c *Client) GetJob(ctx context.Context, jobID string) (JobHandle, error) {
	data, err := c.getJobData(ctx, jobID)
	if err != nil {
		return JobHandle{}, err
	}
	return JobHandle{JobRef: JobRef{client: c, jobID: jobID}, Data: data}, nil
}

func (c *Client) deleteJob(ctx context.Context, jobID string) error {
	if !c.isConfigured() {
		return NotConfiguredError{}
	}
	path := fmt.Sprintf("/jobs/%s", jobID)
	return c.do(ctx, c.request(ctx, nil), http.MethodDelete, path)
}

// DeleteJob deletes a job and all its artifacts. Deletion is asynchronous server-side.
func (c *Client) DeleteJob(ctx context.Context, jobID string) error {
	return c.deleteJob(ctx, jobID)
}

func (c *Client) postTasks(ctx context.Context, jobID string, tasks []Task, lastBatch bool) (*AddTasksResponse, error) {
	if !c.isConfigured() {
		return nil, NotConfiguredError{}
	}
	var result AddTasksResponse
	path := fmt.Sprintf("/jobs/%s/tasks", jobID)
	body := map[string]any{"tasks": tasks}
	if lastBatch {
		body["last_batch"] = true
	}
	req := c.request(ctx, &result).SetBody(body)
	if err := c.do(ctx, req, http.MethodPost, path); err != nil {
		return nil, err
	}
	return &result, nil
}

// AddTasks appends tasks to an open job's initial run. Fails once the job has been closed.
// Pass lastBatch=true to close the job as part of this call.
func (c *Client) AddTasks(ctx context.Context, jobID string, tasks []Task, lastBatch bool) (*AddTasksResponse, error) {
	return c.postTasks(ctx, jobID, tasks, lastBatch)
}

func (c *Client) postClose(ctx context.Context, jobID string) (Job, error) {
	if !c.isConfigured() {
		return Job{}, NotConfiguredError{}
	}
	var result Job
	path := fmt.Sprintf("/jobs/%s/close", jobID)
	req := c.request(ctx, &result)
	if err := c.do(ctx, req, http.MethodPost, path); err != nil {
		return Job{}, err
	}
	return result, nil
}

// CloseJob signals that no more tasks are coming for an open job.
func (c *Client) CloseJob(ctx context.Context, jobID string) (*Job, error) {
	job, err := c.postClose(ctx, jobID)
	if err != nil {
		return nil, err
	}
	return &job, nil
}

func (c *Client) postStop(ctx context.Context, jobID string) (Run, error) {
	if !c.isConfigured() {
		return Run{}, NotConfiguredError{}
	}
	var result Run
	path := fmt.Sprintf("/jobs/%s/stop", jobID)
	req := c.request(ctx, &result)
	if err := c.do(ctx, req, http.MethodPost, path); err != nil {
		return Run{}, err
	}
	return result, nil
}

// StopRun terminally stops the current run of a job.
func (c *Client) StopRun(ctx context.Context, jobID string) (*Run, error) {
	run, err := c.postStop(ctx, jobID)
	if err != nil {
		return nil, err
	}
	return &run, nil
}

func (c *Client) postPause(ctx context.Context, jobID string) (Run, error) {
	if !c.isConfigured() {
		return Run{}, NotConfiguredError{}
	}
	var result Run
	path := fmt.Sprintf("/jobs/%s/pause", jobID)
	req := c.request(ctx, &result)
	if err := c.do(ctx, req, http.MethodPost, path); err != nil {
		return Run{}, err
	}
	return result, nil
}

func (c *Client) postResume(ctx context.Context, jobID string) (Run, error) {
	if !c.isConfigured() {
		return Run{}, NotConfiguredError{}
	}
	var result Run
	path := fmt.Sprintf("/jobs/%s/resume", jobID)
	req := c.request(ctx, &result)
	if err := c.do(ctx, req, http.MethodPost, path); err != nil {
		return Run{}, err
	}
	return result, nil
}

func (c *Client) postRerun(ctx context.Context, jobID string, opts RerunOptions) (RerunJobResponse, error) {
	if !c.isConfigured() {
		return RerunJobResponse{}, NotConfiguredError{}
	}
	var result RerunJobResponse
	path := fmt.Sprintf("/jobs/%s/rerun", jobID)
	req := c.request(ctx, &result)
	if opts.Status != "" {
		req.SetQueryParam("status", opts.Status)
	}
	if opts.IdempotencyKey != "" {
		req.SetHeader("Idempotency-Key", opts.IdempotencyKey)
	}
	if err := c.do(ctx, req, http.MethodPost, path); err != nil {
		return RerunJobResponse{}, err
	}
	return result, nil
}

// Rerun starts a new run of a job — a full replay (opts.Status == "") or a partial retry
// (opts.Status is a comma-separated status filter like "failed" or "failed,pending").
func (c *Client) Rerun(ctx context.Context, jobID string, opts RerunOptions) (*RerunJobResponse, error) {
	resp, err := c.postRerun(ctx, jobID, opts)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

// ----- runs (raw) -----

// ListRuns lists runs of a job, newest first. For most uses prefer the auto-paginating
// IterRuns.
func (c *Client) ListRuns(ctx context.Context, jobID string, opts ListRunsOptions) (*ListRunsResponse, error) {
	if !c.isConfigured() {
		return nil, NotConfiguredError{}
	}

	var result ListRunsResponse
	path := fmt.Sprintf("/jobs/%s/runs", jobID)
	req := c.request(ctx, &result)
	if opts.Limit > 0 {
		req.SetQueryParam("limit", fmt.Sprintf("%d", opts.Limit))
	}
	if opts.Cursor != "" {
		req.SetQueryParam("cursor", opts.Cursor)
	}
	if err := c.do(ctx, req, http.MethodGet, path); err != nil {
		return nil, err
	}
	return &result, nil
}

// IterRuns auto-paginates ListRuns for jobID, yielding (Run, error) pairs.
func (c *Client) IterRuns(ctx context.Context, jobID string, opts ListRunsOptions) iter.Seq2[Run, error] {
	return func(yield func(Run, error) bool) {
		cursor := opts.Cursor
		for {
			page, err := c.ListRuns(ctx, jobID, ListRunsOptions{Limit: opts.Limit, Cursor: cursor})
			if err != nil {
				var zero Run
				yield(zero, err)
				return
			}
			for _, run := range page.Runs {
				if !yield(run, nil) {
					return
				}
			}
			if page.NextCursor == "" {
				return
			}
			cursor = page.NextCursor
		}
	}
}

// Run returns a RunRef for an existing run with no network call — the run counterpart of
// Job. Addresses a specific historical run by (jobID, runID); for the current run use
// JobRef.Run().
func (c *Client) Run(jobID, runID string) RunRef {
	return RunRef{client: c, jobID: jobID, runID: runID}
}

func (c *Client) getRunData(ctx context.Context, jobID, runID string) (Run, error) {
	if !c.isConfigured() {
		return Run{}, NotConfiguredError{}
	}
	var result Run
	path := fmt.Sprintf("/jobs/%s/runs/%s", jobID, runID)
	req := c.request(ctx, &result)
	if err := c.do(ctx, req, http.MethodGet, path); err != nil {
		return Run{}, err
	}
	return result, nil
}

// GetRun fetches one run of a job by id and returns a loaded RunHandle.
func (c *Client) GetRun(ctx context.Context, jobID, runID string) (RunHandle, error) {
	data, err := c.getRunData(ctx, jobID, runID)
	if err != nil {
		return RunHandle{}, err
	}
	return RunHandle{RunRef: RunRef{client: c, jobID: jobID, runID: runID}, Data: data}, nil
}

func (c *Client) deleteRun(ctx context.Context, jobID, runID string) error {
	if !c.isConfigured() {
		return NotConfiguredError{}
	}
	path := fmt.Sprintf("/jobs/%s/runs/%s", jobID, runID)
	return c.do(ctx, c.request(ctx, nil), http.MethodDelete, path)
}

// DeleteRun deletes one run and its artifacts. Deletion is asynchronous server-side.
func (c *Client) DeleteRun(ctx context.Context, jobID, runID string) error {
	return c.deleteRun(ctx, jobID, runID)
}

// ----- results / content (raw) -----

// GetResults pages through per-task results. Leave opts.RunID empty to page the job's latest
// run. For most uses prefer the auto-paginating IterResults.
func (c *Client) GetResults(ctx context.Context, jobID string, opts GetResultsOptions) (*GetResultsResponse, error) {
	if !c.isConfigured() {
		return nil, NotConfiguredError{}
	}

	var result GetResultsResponse
	path := fmt.Sprintf("/jobs/%s/results", jobID)
	if opts.RunID != "" {
		path = fmt.Sprintf("/jobs/%s/runs/%s/results", jobID, opts.RunID)
	}
	req := c.request(ctx, &result)
	if opts.Status != "" {
		req.SetQueryParam("status", opts.Status)
	}
	if opts.Cursor != "" {
		req.SetQueryParam("cursor", opts.Cursor)
	}
	if opts.Limit > 0 {
		req.SetQueryParam("limit", fmt.Sprintf("%d", opts.Limit))
	}
	if err := c.do(ctx, req, http.MethodGet, path); err != nil {
		return nil, err
	}
	return &result, nil
}

// IterResults auto-paginates GetResults, yielding (TaskResult, error) pairs.
func (c *Client) IterResults(ctx context.Context, jobID string, opts GetResultsOptions) iter.Seq2[TaskResult, error] {
	return func(yield func(TaskResult, error) bool) {
		cursor := opts.Cursor
		for {
			page, err := c.GetResults(ctx, jobID, GetResultsOptions{RunID: opts.RunID, Status: opts.Status, Limit: opts.Limit, Cursor: cursor})
			if err != nil {
				var zero TaskResult
				yield(zero, err)
				return
			}
			for _, r := range page.Results {
				if !yield(r, nil) {
					return
				}
			}
			if page.NextCursor == "" {
				return
			}
			cursor = page.NextCursor
		}
	}
}

// GetTaskContent fetches the scraped content for one task. Leave opts.RunID empty for the
// job's latest run. The returned bytes are the raw scraped content (HTML, JSON, etc.) —
// unlike other methods here, this is not decoded as JSON.
func (c *Client) GetTaskContent(ctx context.Context, jobID, taskID string, opts GetTaskContentOptions) ([]byte, error) {
	if !c.isConfigured() {
		return nil, NotConfiguredError{}
	}

	path := fmt.Sprintf("/jobs/%s/tasks/%s/content", jobID, taskID)
	if opts.RunID != "" {
		path = fmt.Sprintf("/jobs/%s/runs/%s/tasks/%s/content", jobID, opts.RunID, taskID)
	}

	req := c.http.R().SetContext(ctx)
	res, err := executeWithRetry(ctx, req, http.MethodGet, path, c.cfg.retries)
	if err != nil {
		return nil, err
	}
	if res.IsError() {
		return nil, newAPIError(res.StatusCode(), res.Body())
	}
	return res.Body(), nil
}

func (c *Client) getTaskHistoryRaw(ctx context.Context, jobID, taskID, runID string) (TaskHistoryResponse, error) {
	if !c.isConfigured() {
		return TaskHistoryResponse{}, NotConfiguredError{}
	}
	var result TaskHistoryResponse
	path := fmt.Sprintf("/jobs/%s/tasks/%s/history", jobID, taskID)
	if runID != "" {
		path = fmt.Sprintf("/jobs/%s/runs/%s/tasks/%s/history", jobID, runID, taskID)
	}
	req := c.request(ctx, &result)
	if err := c.do(ctx, req, http.MethodGet, path); err != nil {
		return TaskHistoryResponse{}, err
	}
	return result, nil
}

// GetTaskHistory returns the attempt history for one task. Leave runID empty for the job's
// latest run.
func (c *Client) GetTaskHistory(ctx context.Context, jobID, taskID, runID string) (*TaskHistoryResponse, error) {
	result, err := c.getTaskHistoryRaw(ctx, jobID, taskID, runID)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// ----- schedule (raw) -----

func (c *Client) putSchedule(ctx context.Context, jobID string, schedule JobSchedule) (Job, error) {
	if !c.isConfigured() {
		return Job{}, NotConfiguredError{}
	}
	var result Job
	path := fmt.Sprintf("/jobs/%s/schedule", jobID)
	req := c.request(ctx, &result).SetBody(schedule)
	if err := c.do(ctx, req, http.MethodPut, path); err != nil {
		return Job{}, err
	}
	return result, nil
}

func (c *Client) postScheduleState(ctx context.Context, jobID string, state ScheduleState) (Job, error) {
	if !c.isConfigured() {
		return Job{}, NotConfiguredError{}
	}
	var result Job
	path := fmt.Sprintf("/jobs/%s/schedule/state", jobID)
	req := c.request(ctx, &result).SetBody(map[string]string{"schedule_state": string(state)})
	if err := c.do(ctx, req, http.MethodPost, path); err != nil {
		return Job{}, err
	}
	return result, nil
}

// ----- waiters -----

// WaitForRunOptions configures WaitForRun / JobRef.Run().Wait / RunRef.Wait.
type WaitForRunOptions struct {
	// RunID waits on a specific run. Leave empty to wait on the job's current run.
	RunID           string
	TargetStatuses  map[RunStatus]bool // defaults to TerminalRunStatuses
	FailureStatuses map[RunStatus]bool // nil disables failure detection
	Timeout         time.Duration      // defaults to 300s
	PollInterval    time.Duration      // defaults to 2s
	MaxPollInterval time.Duration      // defaults to 15s
}

// WaitForRun blocks until a run reaches one of opts.TargetStatuses, polling with jittered
// exponential backoff.
func (c *Client) WaitForRun(ctx context.Context, jobID string, opts WaitForRunOptions) (Run, error) {
	target := opts.TargetStatuses
	if target == nil {
		target = TerminalRunStatuses
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 300 * time.Second
	}
	pollInterval := opts.PollInterval
	if pollInterval <= 0 {
		pollInterval = 2 * time.Second
	}
	maxInterval := opts.MaxPollInterval
	if maxInterval <= 0 {
		maxInterval = 15 * time.Second
	}

	fetch := func(ctx context.Context) (Run, error) {
		if opts.RunID != "" {
			return c.getRunData(ctx, jobID, opts.RunID)
		}
		job, err := c.getJobData(ctx, jobID)
		if err != nil {
			return Run{}, err
		}
		if job.LatestRun == nil {
			return Run{}, nil // pending sentinel: zero-value Run, Status == ""
		}
		return *job.LatestRun, nil
	}
	isDone := func(r Run) bool {
		if r.Status == "" {
			return false
		}
		return target[r.Status]
	}
	var isFailure func(Run) bool
	if opts.FailureStatuses != nil {
		isFailure = func(r Run) bool {
			if r.Status == "" {
				return false
			}
			return opts.FailureStatuses[r.Status]
		}
	}

	return pollUntil(ctx, fetch, isDone, isFailure, pollOptions{
		Timeout: timeout, InitialInterval: pollInterval, MaxInterval: maxInterval,
	})
}

// WaitForIngestOptions configures WaitForIngest.
type WaitForIngestOptions struct {
	Timeout         time.Duration // defaults to 300s
	PollInterval    time.Duration // defaults to 2s
	MaxPollInterval time.Duration // defaults to 15s
}

// WaitForIngest blocks until the current run's async-carrier ingestion has finished writing
// task rows. Large submissions return 202 Accepted and stream task rows into storage off the
// request path; until that finishes, results pages may be partial and AddTasks on an open job
// is rejected with 409. Runs that never ingested asynchronously are done on the first poll.
func (c *Client) WaitForIngest(ctx context.Context, jobID string, opts WaitForIngestOptions) (Job, error) {
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 300 * time.Second
	}
	pollInterval := opts.PollInterval
	if pollInterval <= 0 {
		pollInterval = 2 * time.Second
	}
	maxInterval := opts.MaxPollInterval
	if maxInterval <= 0 {
		maxInterval = 15 * time.Second
	}

	fetch := func(ctx context.Context) (Job, error) {
		return c.getJobData(ctx, jobID)
	}
	isDone := func(j Job) bool {
		run := j.LatestRun
		if run == nil || TerminalRunStatuses[run.Status] {
			return true
		}
		return run.IngestStatus != IngestStatusPending
	}

	return pollUntil(ctx, fetch, isDone, nil, pollOptions{
		Timeout: timeout, InitialInterval: pollInterval, MaxInterval: maxInterval,
	})
}
