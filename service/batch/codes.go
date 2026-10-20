package batch

// Error codes the Batch API sets in a problem's `code` member. Compare them with APIError.Code()
// (or Problem.Code, or an InvalidTasks entry's code). The API may add codes, so keep a default
// branch.
const (
	// Generic.

	// CodeInvalidArgument (400): the request failed validation. Per-task reasons, when there
	// are any, are in Problem.InvalidTasks.
	CodeInvalidArgument = "invalid_argument"
	// CodeNotFound (404): the job, run, task or export doesn't exist or isn't yours.
	CodeNotFound = "not_found"
	// CodeConflict (409): a generic state conflict.
	CodeConflict = "conflict"
	// CodeUnauthenticated (401): the API key is missing or invalid.
	CodeUnauthenticated = "unauthenticated"
	// CodeSubscriptionInactive (401): the key is valid, but the subscription isn't active
	// (for example past due). Paying fixes it.
	CodeSubscriptionInactive = "subscription_inactive"
	// CodePaymentRequired (402): the account is out of credits.
	CodePaymentRequired = "payment_required"
	// CodeAPIKeyCapReached (402): the API key reached one of its credit caps. The key works
	// again when the cap's window resets, or when the cap is raised.
	CodeAPIKeyCapReached = "api_key_cap_reached" //nolint:gosec // an error code, not a credential
	// CodePermissionDenied (403): the account can't use this feature.
	CodePermissionDenied = "permission_denied"
	// CodeQuotaExceeded (429): an account limit was reached: active jobs (shared with Crawl),
	// scheduled jobs, or the task cap of an open job. It clears only when a job finishes or is
	// deleted, so the client does not retry it.
	CodeQuotaExceeded = "quota_exceeded"
	// CodeInternal (500): a server error. APIError.Code() also returns it when the response
	// body wasn't a problem document.
	CodeInternal = "internal"
	// CodeNotImplemented (501): the operation isn't available in this deployment.
	CodeNotImplemented = "not_implemented"

	// Job, run and task lifecycle.

	// CodeJobNotOpen (409): the job no longer accepts tasks.
	CodeJobNotOpen = "job_not_open"
	// CodeLastBatchReceived (409): the job's last batch was already sent.
	CodeLastBatchReceived = "last_batch_received"
	// CodeRunNotTerminal (409): the run must finish (or be stopped) first.
	CodeRunNotTerminal = "run_not_terminal"
	// CodeRunNotStoppable (409): the run is in a state that can't be stopped or paused.
	CodeRunNotStoppable = "run_not_stoppable"
	// CodeNoPriorRun (409): the job has no run to rerun.
	CodeNoPriorRun = "no_prior_run"
	// CodeNoMatchingTasks (409): a partial rerun's status filter matched no task.
	CodeNoMatchingTasks = "no_matching_tasks"
	// CodePartialRerunWindowExpired (409): a partial rerun (a Status filter) came more than 10
	// days after the start of the run it retries. A full rerun still works.
	CodePartialRerunWindowExpired = "partial_rerun_window_expired"
	// CodeRunHasDescendants (409): later runs still reuse this run's results.
	CodeRunHasDescendants = "run_has_descendants"
	// CodeIdempotencyKeyConflict (409): the Idempotency-Key was used with a different body, or
	// the first call with it is still running.
	CodeIdempotencyKeyConflict = "idempotency_key_conflict"
	// CodeTaskNotReady (409): the task has no result yet.
	CodeTaskNotReady = "task_not_ready"
	// CodeResultExpired (410): the task's result is past its retention window (14 days by
	// default). The row reads as TaskStatusExpired.
	CodeResultExpired = "result_expired"

	// HMAC key rotation.

	// CodeCandidatePending (409): a rotation is already staged; finalize or cancel it first.
	CodeCandidatePending = "candidate_pending"
	// CodeNoCandidate (409): there is no staged rotation to finalize or cancel.
	CodeNoCandidate = "no_candidate"

	// Input validation (job level, or per task in Problem.InvalidTasks).

	CodeUnsupportedScheme  = "unsupported_scheme"
	CodeMalformedURL       = "malformed_url"
	CodeMissingHost        = "missing_host"
	CodeURLTooLong         = "url_too_long"
	CodeMetadataTooLarge   = "metadata_too_large"
	CodeMetadataKeyInvalid = "metadata_key_invalid"
	CodeInvalidExternalID  = "invalid_external_id"

	// Per-task method and body.

	CodeUnsupportedMethod   = "unsupported_method"
	CodeBodyNotAllowed      = "body_not_allowed"
	CodeBodyTooLarge        = "body_too_large"
	CodeBodyMalformed       = "body_malformed"
	CodeMethodParamConflict = "method_param_conflict"

	// CSV file inputs.

	CodeFileInputNotFound              = "file_input_not_found"
	CodeFileInputNotUploaded           = "file_input_not_uploaded"
	CodeFileInputUnsupportedForJobType = "file_input_unsupported_for_job_type"

	// Webhooks.

	CodeCallbackURLMustBeHTTPS          = "callback_url_must_be_https"
	CodeCallbackURLUnresolvable         = "callback_url_unresolvable"
	CodeWebhookSigningRequiresActiveKey = "webhook_signing_requires_active_key"

	// Task level.

	// CodeGatewayUnreachable: a task's scrape couldn't reach the scraping API.
	CodeGatewayUnreachable = "gateway_unreachable"
)
