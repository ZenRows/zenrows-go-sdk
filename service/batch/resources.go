package batch

import (
	"context"
	"iter"
)

// ======================= JobRef / JobHandle =======================

// JobRef is a reference to a job by id — job-template operations, plus the Run() and
// Schedule() sub-facets. Minted with no network call by Client.Job, and returned by the
// Submit* methods (with a submit response attached). Call Load for a JobHandle with data.
type JobRef struct {
	client         *Client
	jobID          string
	submitResponse *SubmitJobResponse
}

// JobID is this ref's job id.
func (r JobRef) JobID() string { return r.jobID }

// Status is the job status from the submit response. Only known on refs returned by a
// Submit* call (zero value otherwise) — Load for a JobHandle whose .Status reads the fetched
// data.
func (r JobRef) Status() JobStatus {
	if r.submitResponse != nil {
		return r.submitResponse.Status
	}
	return ""
}

// AcceptedTasks is how many tasks landed at submit. Only known on refs returned by a Submit*
// call; returns 0 otherwise.
func (r JobRef) AcceptedTasks() int {
	if r.submitResponse != nil {
		return r.submitResponse.AcceptedTasks
	}
	return 0
}

// Run returns the CurrentRun facet — operations on this job's current run (Pause/Resume/
// Stop/Wait/Results/downloads/StartExport).
func (r JobRef) Run() CurrentRun {
	return CurrentRun{client: r.client, jobID: r.jobID}
}

// Schedule returns the ScheduleControls facet — operations on this job's schedule (scheduled
// jobs only).
func (r JobRef) Schedule() ScheduleControls {
	return ScheduleControls{client: r.client, jobID: r.jobID}
}

// Load fetches the full job and returns a loaded JobHandle.
func (r JobRef) Load(ctx context.Context) (JobHandle, error) {
	return r.client.GetJob(ctx, r.jobID)
}

// Close locks the job (no more AddTasks). Returns the fresh loaded handle.
func (r JobRef) Close(ctx context.Context) (JobHandle, error) {
	job, err := r.client.postClose(ctx, r.jobID)
	if err != nil {
		return JobHandle{}, err
	}
	return JobHandle{JobRef: r, Data: job}, nil
}

// Delete async hard-deletes the job.
func (r JobRef) Delete(ctx context.Context) error {
	return r.client.deleteJob(ctx, r.jobID)
}

// Rerun starts a new run. Without opts.Status: full rerun of the previous run's tasks (or,
// for a scheduled job with no prior run, a manual fire from the template). With opts.Status
// (e.g. "failed" or "failed,pending"): partial retry. Returns a RunHandle for the new run.
func (r JobRef) Rerun(ctx context.Context, opts RerunOptions) (RunHandle, error) {
	resp, err := r.client.postRerun(ctx, r.jobID, opts)
	if err != nil {
		return RunHandle{}, err
	}
	return RunHandle{RunRef: RunRef{client: r.client, jobID: r.jobID, runID: resp.LatestRun.RunID}, Data: resp.LatestRun}, nil
}

// RetryFailedOptions configures JobRef.RetryFailed.
type RetryFailedOptions struct {
	// IncludePending also re-enqueues tasks that never started ("failed,pending") — the usual
	// move after Run().Stop() left orphan pending rows.
	IncludePending bool
	IdempotencyKey string
}

// RetryFailed starts a new run that re-executes only the previous run's failed tasks
// (shortcut for Rerun with Status: "failed"). Requires the previous run to be terminal;
// returns an APIError (409 run_not_terminal) otherwise, or (409 no_matching_tasks) when
// nothing matched.
func (r JobRef) RetryFailed(ctx context.Context, opts RetryFailedOptions) (RunHandle, error) {
	status := "failed"
	if opts.IncludePending {
		status = "failed,pending"
	}
	return r.Rerun(ctx, RerunOptions{Status: status, IdempotencyKey: opts.IdempotencyKey})
}

// AddTasks appends to the open initial run.
func (r JobRef) AddTasks(ctx context.Context, tasks []Task, lastBatch bool) (*AddTasksResponse, error) {
	return r.client.postTasks(ctx, r.jobID, tasks, lastBatch)
}

// Runs auto-paginates runs of this job, yielding (RunHandle, error) pairs. To address a
// specific run by id use Client.Run; for the current run use JobRef.Run().
func (r JobRef) Runs(ctx context.Context, opts ListRunsOptions) iter.Seq2[RunHandle, error] {
	return func(yield func(RunHandle, error) bool) {
		for run, err := range r.client.IterRuns(ctx, r.jobID, opts) {
			if err != nil {
				yield(RunHandle{}, err)
				return
			}
			handle := RunHandle{RunRef: RunRef{client: r.client, jobID: r.jobID, runID: run.RunID}, Data: run}
			if !yield(handle, nil) {
				return
			}
		}
	}
}

// AddFileInput uploads a CSV that this job would consume on a future submission. Returns the
// file_input_id. (File inputs aren't tied to a job at create-time, but living off the handle
// keeps the call site discoverable.)
func (r JobRef) AddFileInput(ctx context.Context, source any, opts UploadCSVOptions) (string, error) {
	return r.client.UploadCSV(ctx, source, opts)
}

// GetWebhook returns this job's current webhook config. Returns an APIError (404) when none
// is set.
func (r JobRef) GetWebhook(ctx context.Context) (*WebhookConfig, error) {
	return r.client.GetJobWebhook(ctx, r.jobID)
}

// SetWebhook replaces this job's webhook config. Both fields are required (no defaulting, so
// you can't silently toggle signing).
func (r JobRef) SetWebhook(ctx context.Context, url string, signature bool) (*WebhookConfig, error) {
	return r.client.PutJobWebhook(ctx, r.jobID, WebhookConfig{URL: url, Signature: signature})
}

// DeleteWebhook clears this job's webhook config. Idempotent.
func (r JobRef) DeleteWebhook(ctx context.Context) error {
	return r.client.DeleteJobWebhook(ctx, r.jobID)
}

// WaitForIngest blocks until the current run's async-carrier ingestion has finished writing
// task rows. Returns the fresh loaded handle, so chains like
// job.WaitForIngest(ctx, opts).AddTasks(...) work. Or opt in at submit time via
// SubmitRegularOptions.WaitForIngest / SubmitOpenOptions.WaitForIngest.
func (r JobRef) WaitForIngest(ctx context.Context, opts WaitForIngestOptions) (JobHandle, error) {
	job, err := r.client.WaitForIngest(ctx, r.jobID, opts)
	if err != nil {
		return JobHandle{}, err
	}
	return JobHandle{JobRef: r, Data: job}, nil
}

// JobHandle is a JobRef plus a guaranteed, synchronous Data snapshot. Returned by GetJob /
// IterJobs and the ops that echo fresh state (Close, Schedule().Pause, ...).
type JobHandle struct {
	JobRef
	Data Job
}

// ======================= RunRef / RunHandle =======================

// RunRef is a reference to a specific run by (jobID, runID). Read/download operations on one
// (usually historical) run. No Pause/Stop — the API only suspends or stops the current run
// (see JobRef.Run(), the CurrentRun facet). Call Load for a RunHandle with data.
type RunRef struct {
	client *Client
	jobID  string
	runID  string
}

// JobID is this run's parent job id.
func (r RunRef) JobID() string { return r.jobID }

// RunID is this run's id.
func (r RunRef) RunID() string { return r.runID }

// Load fetches the run and returns a loaded RunHandle.
func (r RunRef) Load(ctx context.Context) (RunHandle, error) {
	return r.client.GetRun(ctx, r.jobID, r.runID)
}

// Delete scrubs this run only.
func (r RunRef) Delete(ctx context.Context) error {
	return r.client.deleteRun(ctx, r.jobID, r.runID)
}

// Results auto-paginates task results from this run.
func (r RunRef) Results(ctx context.Context, status string) iter.Seq2[TaskResult, error] {
	return r.client.IterResults(ctx, r.jobID, GetResultsOptions{RunID: r.runID, Status: status})
}

// TaskHistory returns the per-attempt event log for one task in this run.
func (r RunRef) TaskHistory(ctx context.Context, taskID string) (*TaskHistoryResponse, error) {
	return r.client.GetTaskHistory(ctx, r.jobID, taskID, r.runID)
}

// DownloadToDir streams every (matching) task body of this run into targetDir. See
// Client-level DownloadToDir (download.go) for the full contract.
func (r RunRef) DownloadToDir(ctx context.Context, targetDir string, opts DownloadToDirOptions) (int, error) {
	opts.RunID = r.runID
	return r.client.DownloadToDir(ctx, r.jobID, targetDir, opts)
}

// DownloadToMemory loads every (matching) task body of this run into memory. See
// Client-level DownloadToMemory (download.go) for the full contract.
func (r RunRef) DownloadToMemory(ctx context.Context, opts DownloadToMemoryOptions) ([]DownloadedResult, error) {
	opts.RunID = r.runID
	return r.client.DownloadToMemory(ctx, r.jobID, opts)
}

// DownloadTaskToFile downloads one task's body straight from its presigned result_url to
// target.
func (r RunRef) DownloadTaskToFile(task TaskResult, targetPath string) error {
	return downloadTaskToFile(task, targetPath)
}

// DownloadTaskToMemory downloads one task's body straight from its presigned result_url and
// returns the raw bytes.
func (r RunRef) DownloadTaskToMemory(task TaskResult) ([]byte, error) {
	return downloadTaskToMemory(task)
}

// Wait blocks until this run reaches a target state. Returns a fresh loaded RunHandle so
// chains like run.Wait(ctx, opts).DownloadToDir(...) work without re-wrapping.
func (r RunRef) Wait(ctx context.Context, opts WaitForRunOptions) (RunHandle, error) {
	opts.RunID = r.runID
	run, err := r.client.WaitForRun(ctx, r.jobID, opts)
	if err != nil {
		return RunHandle{}, err
	}
	return RunHandle{RunRef: r, Data: run}, nil
}

// StartExport kicks off an async zip of this run's bodies. Returns an ExportRef carrying the
// just-issued id; chain Wait to block for completion.
func (r RunRef) StartExport(ctx context.Context) (ExportRef, error) {
	return r.client.StartResultsExport(ctx, r.jobID, r.runID)
}

// Export addresses a specific export of this run by id (no network call). Lazy — the first
// method call surfaces 404 if the id is wrong or TTL-swept.
func (r RunRef) Export(exportID string) ExportRef {
	return ExportRef{client: r.client, jobID: r.jobID, runID: r.runID, exportID: exportID}
}

// DownloadAllResults starts an export of this run's results, waits for it, and saves the zip
// to targetPath. See Client.DownloadAllResults for the contract.
func (r RunRef) DownloadAllResults(ctx context.Context, targetPath string, opts DownloadAllResultsOptions) (string, error) {
	return r.client.DownloadAllResults(ctx, r.jobID, r.runID, targetPath, opts)
}

// RunHandle is a RunRef plus a guaranteed, synchronous Data snapshot.
type RunHandle struct {
	RunRef
	Data Run
}

// Status is this run's status. Shortcut for Data.Status.
func (h RunHandle) Status() RunStatus { return h.Data.Status }

// Stats is this run's task rollup. Shortcut for Data.Stats.
func (h RunHandle) Stats() RunStats { return h.Data.Stats }

// ======================= ExportRef / ExportHandle =======================

// ExportRef is a reference to a results export by id. A results export is async:
// StartResultsExport returns immediately with a pending ref; the server zips the run in the
// background and it reaches completed (download URL ready) or failed (with an error message
// — e.g. the 1 GiB size cap). Call Load / Wait for an ExportHandle with data.
type ExportRef struct {
	client   *Client
	jobID    string
	runID    string
	exportID string
	// startResponse is set only on the ref returned by StartResultsExport; carries the
	// initial status/timestamps without forcing a GET.
	startResponse *StartExportResponse
}

// ExportID is this export's id.
func (r ExportRef) ExportID() string { return r.exportID }

// StartResponse is the immediate wire response from StartResultsExport, if this ref came
// from there (nil otherwise).
func (r ExportRef) StartResponse() *StartExportResponse { return r.startResponse }

// Load fetches the export and returns a loaded ExportHandle.
func (r ExportRef) Load(ctx context.Context) (ExportHandle, error) {
	data, err := r.client.getExport(ctx, r.jobID, r.runID, r.exportID)
	if err != nil {
		return ExportHandle{}, err
	}
	return ExportHandle{ExportRef: r, Data: data}, nil
}

// Wait blocks until the export reaches a terminal state (defaults to {completed, failed}).
// Returns the loaded handle.
func (r ExportRef) Wait(ctx context.Context, opts WaitForExportOptions) (ExportHandle, error) {
	data, err := r.client.WaitForExport(ctx, r.jobID, r.runID, r.exportID, opts)
	if err != nil {
		return ExportHandle{}, err
	}
	return ExportHandle{ExportRef: r, Data: data}, nil
}

// DownloadToPath streams the export zip to targetPath. The export must already be completed
// — call Wait first, or use Client.DownloadAllResults / RunRef.DownloadAllResults for the
// one-shot flow. Fetches once for a fresh presigned URL (the server signs a new URL per
// request).
func (r ExportRef) DownloadToPath(ctx context.Context, targetPath string) (string, error) {
	loaded, err := r.Load(ctx)
	if err != nil {
		return "", err
	}
	return downloadExportToPath(ctx, loaded.Data, targetPath, 1<<20)
}

// ExportHandle is an ExportRef plus a guaranteed, synchronous Data snapshot.
type ExportHandle struct {
	ExportRef
	Data Export
}

// Status is this export's status. Shortcut for Data.Status.
func (h ExportHandle) Status() ExportStatus { return h.Data.Status }

// ======================= CurrentRun facet =======================

// CurrentRun holds operations on a job's current run, reached via JobRef.Run(). The
// pause/stop family only ever targets the latest run (the API's run-less endpoints resolve it
// server-side), which is why they live here and not on RunRef.
type CurrentRun struct {
	client *Client
	jobID  string
}

func (cr CurrentRun) currentRunID(ctx context.Context) (string, error) {
	job, err := cr.client.getJobData(ctx, cr.jobID)
	if err != nil {
		return "", err
	}
	if job.LatestRun == nil {
		return "", errNoRunYet(cr.jobID)
	}
	return job.LatestRun.RunID, nil
}

// Load fetches the latest run as a loaded RunHandle.
func (cr CurrentRun) Load(ctx context.Context) (RunHandle, error) {
	job, err := cr.client.getJobData(ctx, cr.jobID)
	if err != nil {
		return RunHandle{}, err
	}
	if job.LatestRun == nil {
		return RunHandle{}, errNoRunYet(cr.jobID)
	}
	return RunHandle{RunRef: RunRef{client: cr.client, jobID: cr.jobID, runID: job.LatestRun.RunID}, Data: *job.LatestRun}, nil
}

// Pause reversibly suspends the current run: the dispatcher stops pulling its queue
// (in-flight tasks may still settle). Orthogonal to Status; undo with Resume. Returns the
// fresh loaded RunHandle.
func (cr CurrentRun) Pause(ctx context.Context) (RunHandle, error) {
	run, err := cr.client.postPause(ctx, cr.jobID)
	if err != nil {
		return RunHandle{}, err
	}
	return RunHandle{RunRef: RunRef{client: cr.client, jobID: cr.jobID, runID: run.RunID}, Data: run}, nil
}

// Resume un-pauses the current run. Returns the fresh loaded handle.
func (cr CurrentRun) Resume(ctx context.Context) (RunHandle, error) {
	run, err := cr.client.postResume(ctx, cr.jobID)
	if err != nil {
		return RunHandle{}, err
	}
	return RunHandle{RunRef: RunRef{client: cr.client, jobID: cr.jobID, runID: run.RunID}, Data: run}, nil
}

// Stop terminally stops the current run. Returns the fresh loaded handle.
func (cr CurrentRun) Stop(ctx context.Context) (RunHandle, error) {
	run, err := cr.client.postStop(ctx, cr.jobID)
	if err != nil {
		return RunHandle{}, err
	}
	return RunHandle{RunRef: RunRef{client: cr.client, jobID: cr.jobID, runID: run.RunID}, Data: run}, nil
}

// Cancel is an alias for Stop.
func (cr CurrentRun) Cancel(ctx context.Context) (RunHandle, error) { return cr.Stop(ctx) }

// Wait blocks until the current run reaches a target state. Returns a loaded RunHandle.
func (cr CurrentRun) Wait(ctx context.Context, opts WaitForRunOptions) (RunHandle, error) {
	opts.RunID = ""
	run, err := cr.client.WaitForRun(ctx, cr.jobID, opts)
	if err != nil {
		return RunHandle{}, err
	}
	return RunHandle{RunRef: RunRef{client: cr.client, jobID: cr.jobID, runID: run.RunID}, Data: run}, nil
}

// Results auto-paginates task results from the current run.
func (cr CurrentRun) Results(ctx context.Context, status string) iter.Seq2[TaskResult, error] {
	return cr.client.IterResults(ctx, cr.jobID, GetResultsOptions{Status: status})
}

// TaskHistory returns the current-run per-attempt event log for one task.
func (cr CurrentRun) TaskHistory(ctx context.Context, taskID string) (*TaskHistoryResponse, error) {
	return cr.client.GetTaskHistory(ctx, cr.jobID, taskID, "")
}

// DownloadToDir streams every (matching) task body of the current run into targetDir.
func (cr CurrentRun) DownloadToDir(ctx context.Context, targetDir string, opts DownloadToDirOptions) (int, error) {
	opts.RunID = ""
	return cr.client.DownloadToDir(ctx, cr.jobID, targetDir, opts)
}

// DownloadToMemory loads every (matching) task body of the current run into memory.
func (cr CurrentRun) DownloadToMemory(ctx context.Context, opts DownloadToMemoryOptions) ([]DownloadedResult, error) {
	opts.RunID = ""
	return cr.client.DownloadToMemory(ctx, cr.jobID, opts)
}

// DownloadTaskToFile downloads one task's body straight from its presigned result_url.
func (cr CurrentRun) DownloadTaskToFile(task TaskResult, targetPath string) error {
	return downloadTaskToFile(task, targetPath)
}

// DownloadTaskToMemory downloads one task's body straight from its presigned result_url.
func (cr CurrentRun) DownloadTaskToMemory(task TaskResult) ([]byte, error) {
	return downloadTaskToMemory(task)
}

// StartExport kicks off an async zip of the current run's bodies. Resolves the current run id
// first (one GET), then returns an ExportRef.
func (cr CurrentRun) StartExport(ctx context.Context) (ExportRef, error) {
	runID, err := cr.currentRunID(ctx)
	if err != nil {
		return ExportRef{}, err
	}
	return cr.client.StartResultsExport(ctx, cr.jobID, runID)
}

// DownloadAllResults starts an export of the current run's results, waits for it, and saves
// the zip to targetPath.
func (cr CurrentRun) DownloadAllResults(ctx context.Context, targetPath string, opts DownloadAllResultsOptions) (string, error) {
	runID, err := cr.currentRunID(ctx)
	if err != nil {
		return "", err
	}
	return cr.client.DownloadAllResults(ctx, cr.jobID, runID, targetPath, opts)
}

// ======================= ScheduleControls facet =======================

// ScheduleControls holds operations on a scheduled job's schedule, reached via
// JobRef.Schedule(). Scheduled jobs only — regular jobs get a 409.
type ScheduleControls struct {
	client *Client
	jobID  string
}

// Pause skips future scheduled fires (an in-flight run keeps running). The schedule keeps
// ticking server-side but fires are dropped until Resume. Idempotent; returns the fresh
// loaded handle.
func (sc ScheduleControls) Pause(ctx context.Context) (JobHandle, error) {
	job, err := sc.client.postScheduleState(ctx, sc.jobID, ScheduleStatePaused)
	if err != nil {
		return JobHandle{}, err
	}
	return JobHandle{JobRef: JobRef{client: sc.client, jobID: sc.jobID}, Data: job}, nil
}

// Resume re-enables scheduled fires on a paused job. Idempotent; returns the fresh loaded
// handle.
func (sc ScheduleControls) Resume(ctx context.Context) (JobHandle, error) {
	job, err := sc.client.postScheduleState(ctx, sc.jobID, ScheduleStateActive)
	if err != nil {
		return JobHandle{}, err
	}
	return JobHandle{JobRef: JobRef{client: sc.client, jobID: sc.jobID}, Data: job}, nil
}

// Update replaces the schedule. schedule is one of the typed builders (At, Rate, Calendar),
// same as SubmitScheduled. An in-flight run keeps running; the new schedule governs only
// future fires. Returns the fresh loaded handle.
func (sc ScheduleControls) Update(ctx context.Context, schedule Schedule) (JobHandle, error) {
	job, err := sc.client.putSchedule(ctx, sc.jobID, schedule.toSchedule())
	if err != nil {
		return JobHandle{}, err
	}
	return JobHandle{JobRef: JobRef{client: sc.client, jobID: sc.jobID}, Data: job}, nil
}

// noRunYetError is returned by CurrentRun operations when the job has no run yet (e.g. a
// scheduled job that hasn't fired).
type noRunYetError struct{ jobID string }

func (e noRunYetError) Error() string {
	return "zenrows batch: job " + e.jobID + " has no run yet"
}

func errNoRunYet(jobID string) error { return noRunYetError{jobID: jobID} }
