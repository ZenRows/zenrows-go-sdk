package batch

import (
	"context"
	"fmt"
)

// SubmitRegularOptions configures Client.SubmitRegular.
type SubmitRegularOptions struct {
	// Tasks and FileInputID are mutually exclusive — exactly one must be set.
	Tasks          []Task
	FileInputID    string
	ZenRowsParams  map[string]any
	ExternalID     string
	Name           string
	Metadata       map[string]any
	Webhook        *WebhookConfig
	IdempotencyKey string
	WaitForIngest  bool
}

func (c *Client) buildSubmitBody(jobType JobType, status JobStatus, tasks []Task, fileInputID string,
	zenrowsParams map[string]any, schedule *JobSchedule, externalID, name string,
	metadata map[string]any, webhook *WebhookConfig,
) (SubmitJobRequest, error) {
	if len(tasks) > 0 && fileInputID != "" {
		return SubmitJobRequest{}, fmt.Errorf("submit: pass Tasks OR FileInputID, not both")
	}
	if len(tasks) == 0 && fileInputID == "" && status == JobStatusClosed {
		return SubmitJobRequest{}, fmt.Errorf("submit: closed jobs require Tasks or FileInputID (use SubmitOpen for the open/extend pattern)")
	}

	body := SubmitJobRequest{
		Type:          jobType,
		Status:        status,
		ZenRowsParams: zenrowsParams,
		Schedule:      schedule,
		FileInputID:   fileInputID,
		ExternalID:    externalID,
		Name:          name,
		Metadata:      metadata,
		Webhook:       webhook,
	}
	if len(tasks) > 0 {
		body.Tasks = tasks
	}
	return body, nil
}

func (c *Client) submitAndWrap(ctx context.Context, body SubmitJobRequest, idempotencyKey string, waitForIngest bool) (JobRef, error) {
	body.IdempotencyKey = idempotencyKey
	resp, err := c.SubmitJob(ctx, body)
	if err != nil {
		return JobRef{}, err
	}
	ref := JobRef{client: c, jobID: resp.JobID, submitResponse: resp}
	if waitForIngest && resp.LatestRun != nil && resp.LatestRun.IngestStatus == IngestStatusPending {
		if _, err := ref.WaitForIngest(ctx, WaitForIngestOptions{}); err != nil {
			return ref, err
		}
	}
	return ref, nil
}

// SubmitRegular submits a one-shot scraping job (closed, all tasks upfront). Tasks and
// FileInputID are mutually exclusive — exactly one must be set. The job is created with
// status=closed, so no further AddTasks calls are accepted; for the open-and-extend pattern
// see SubmitOpen.
func (c *Client) SubmitRegular(ctx context.Context, opts SubmitRegularOptions) (JobRef, error) {
	body, err := c.buildSubmitBody(JobTypeRegular, JobStatusClosed, opts.Tasks, opts.FileInputID,
		opts.ZenRowsParams, nil, opts.ExternalID, opts.Name, opts.Metadata, opts.Webhook)
	if err != nil {
		return JobRef{}, err
	}
	return c.submitAndWrap(ctx, body, opts.IdempotencyKey, opts.WaitForIngest)
}

// SubmitOpenOptions configures Client.SubmitOpen.
type SubmitOpenOptions struct {
	// Tasks may be empty — tasks can be added later via JobRef.AddTasks.
	Tasks          []Task
	ZenRowsParams  map[string]any
	ExternalID     string
	Name           string
	Metadata       map[string]any
	Webhook        *WebhookConfig
	IdempotencyKey string
	WaitForIngest  bool
}

// SubmitOpen submits a streaming-style job that stays open for more tasks. Created with
// status=open — Tasks can be empty and tasks added later via JobRef.AddTasks. Close the job
// with JobRef.Close once done. File-input upload is not supported here (the server only
// accepts CSV inputs for closed regular jobs and scheduled jobs).
func (c *Client) SubmitOpen(ctx context.Context, opts SubmitOpenOptions) (JobRef, error) {
	body, err := c.buildSubmitBody(JobTypeRegular, JobStatusOpen, opts.Tasks, "",
		opts.ZenRowsParams, nil, opts.ExternalID, opts.Name, opts.Metadata, opts.Webhook)
	if err != nil {
		return JobRef{}, err
	}
	return c.submitAndWrap(ctx, body, opts.IdempotencyKey, opts.WaitForIngest)
}

// SubmitScheduledOptions configures Client.SubmitScheduled.
type SubmitScheduledOptions struct {
	Schedule       Schedule // an At, Rate, or Calendar
	Tasks          []Task
	FileInputID    string
	ZenRowsParams  map[string]any
	ExternalID     string
	Name           string
	Metadata       map[string]any
	Webhook        *WebhookConfig
	IdempotencyKey string
}

// SubmitScheduled submits a scheduled job. opts.Schedule is one of the typed builders (At,
// Rate, Calendar — see NewAt/NewRate/NewCalendar), which validate their inputs the same way
// the server does. Tasks and FileInputID are mutually exclusive, same as SubmitRegular.
func (c *Client) SubmitScheduled(ctx context.Context, opts SubmitScheduledOptions) (JobRef, error) {
	if opts.Schedule == nil {
		return JobRef{}, fmt.Errorf("SubmitScheduled: Schedule is required")
	}
	schedule := opts.Schedule.toSchedule()
	body, err := c.buildSubmitBody(JobTypeScheduled, JobStatusClosed, opts.Tasks, opts.FileInputID,
		opts.ZenRowsParams, &schedule, opts.ExternalID, opts.Name, opts.Metadata, opts.Webhook)
	if err != nil {
		return JobRef{}, err
	}
	return c.submitAndWrap(ctx, body, opts.IdempotencyKey, false)
}

// EstimateCost estimates the credit cost of a job before submitting it, assuming every task
// succeeds once. Takes the same tasks/zenrowsParams you'd hand a submit call, so you estimate
// the exact job you're about to submit. Pure — no network call. See EstimateCost (the
// package-level function in estimate.go) for the exact rate card.
func (c *Client) EstimateCost(tasks []Task, zenrowsParams map[string]any) CostEstimate {
	return EstimateCost(tasks, zenrowsParams)
}
