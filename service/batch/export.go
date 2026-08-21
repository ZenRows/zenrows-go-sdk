package batch

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

func (c *Client) postExportStart(ctx context.Context, jobID, runID string) (StartExportResponse, error) {
	if !c.isConfigured() {
		return StartExportResponse{}, NotConfiguredError{}
	}
	var result StartExportResponse
	path := fmt.Sprintf("/jobs/%s/runs/%s/exports", jobID, runID)
	req := c.request(ctx, &result)
	if err := c.do(ctx, req, http.MethodPost, path); err != nil {
		return StartExportResponse{}, err
	}
	return result, nil
}

// StartResultsExport kicks off an async zip of every task body in the run. Returns an
// ExportRef carrying the just-issued export id; failure modes: 404 if the job/run is missing
// or not yours. The worker fails the export (status=failed, error="results are larger then 1
// gb") if the pre-zip total exceeds the server's 1 GiB cap.
func (c *Client) StartResultsExport(ctx context.Context, jobID, runID string) (ExportRef, error) {
	resp, err := c.postExportStart(ctx, jobID, runID)
	if err != nil {
		return ExportRef{}, err
	}
	return ExportRef{client: c, jobID: jobID, runID: runID, exportID: resp.ExportID, startResponse: &resp}, nil
}

func (c *Client) getExport(ctx context.Context, jobID, runID, exportID string) (Export, error) {
	if !c.isConfigured() {
		return Export{}, NotConfiguredError{}
	}
	var result Export
	path := fmt.Sprintf("/jobs/%s/runs/%s/exports/%s", jobID, runID, exportID)
	req := c.request(ctx, &result)
	if err := c.do(ctx, req, http.MethodGet, path); err != nil {
		return Export{}, err
	}
	return result, nil
}

// GetResultsExport snapshots one export. Returns a loaded ExportHandle. A 404 covers both
// "no such id" and "TTL-swept".
func (c *Client) GetResultsExport(ctx context.Context, jobID, runID, exportID string) (ExportHandle, error) {
	data, err := c.getExport(ctx, jobID, runID, exportID)
	if err != nil {
		return ExportHandle{}, err
	}
	return ExportHandle{ExportRef: ExportRef{client: c, jobID: jobID, runID: runID, exportID: exportID}, Data: data}, nil
}

// WaitForExportOptions configures WaitForExport.
type WaitForExportOptions struct {
	TargetStatuses  map[ExportStatus]bool // defaults to TerminalExportStatuses
	Timeout         time.Duration         // defaults to 600s
	PollInterval    time.Duration         // defaults to 2s
	MaxPollInterval time.Duration         // defaults to 15s
}

// WaitForExport blocks until an export reaches a terminal state (defaults to
// {completed, failed}). Returns the Export snapshot — callers check .Status and (on
// completed) .DownloadURL. "failed" is NOT treated as an error here; the caller decides
// whether e.g. Error == "results are larger then 1 gb" is fatal or expected.
func (c *Client) WaitForExport(ctx context.Context, jobID, runID, exportID string, opts WaitForExportOptions) (Export, error) {
	target := opts.TargetStatuses
	if target == nil {
		target = TerminalExportStatuses
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 600 * time.Second
	}
	pollInterval := opts.PollInterval
	if pollInterval <= 0 {
		pollInterval = 2 * time.Second
	}
	maxInterval := opts.MaxPollInterval
	if maxInterval <= 0 {
		maxInterval = 15 * time.Second
	}

	fetch := func(ctx context.Context) (Export, error) {
		return c.getExport(ctx, jobID, runID, exportID)
	}
	isDone := func(e Export) bool { return target[e.Status] }

	return pollUntil(ctx, fetch, isDone, nil, pollOptions{
		Timeout: timeout, InitialInterval: pollInterval, MaxInterval: maxInterval,
	})
}

// defaultExportChunkSize is the copy buffer size used when no ChunkSize is configured.
const defaultExportChunkSize = 1 << 20 // 1 MiB

// DownloadAllResultsOptions configures Client.DownloadAllResults.
type DownloadAllResultsOptions struct {
	WaitTimeout  time.Duration // defaults to 600s
	PollInterval time.Duration // defaults to 2s
	ChunkSize    int           // defaults to 1 MiB
}

// DownloadAllResults is an end-to-end helper: start an export, wait for it, and save the zip
// to targetPath. Steps: (1) start the export, (2) poll until completed or failed, (3) on
// completed, stream the presigned URL to targetPath.
//
// Returns a WaiterTimeoutError if the export doesn't reach a terminal state within WaitTimeout.
// Returns an APIError with the server's error message on status=failed. The server-side
// export is capped at 1 GiB per run — for larger runs (or one file per task), use
// DownloadToDir instead: it fetches each body client-side with no size limit, at the cost of
// being slower.
func (c *Client) DownloadAllResults(ctx context.Context, jobID, runID, targetPath string, opts DownloadAllResultsOptions) (string, error) {
	chunkSize := opts.ChunkSize
	if chunkSize <= 0 {
		chunkSize = defaultExportChunkSize
	}

	export, err := c.StartResultsExport(ctx, jobID, runID)
	if err != nil {
		return "", err
	}
	final, err := c.WaitForExport(ctx, jobID, runID, export.exportID, WaitForExportOptions{
		Timeout: opts.WaitTimeout, PollInterval: opts.PollInterval,
	})
	if err != nil {
		return "", err
	}
	if final.Status != ExportStatusCompleted {
		msg := final.Error
		if msg == "" {
			msg = "export failed"
		}
		return "", newAPIError(0, []byte(msg))
	}
	if final.DownloadURL == "" {
		return "", newAPIError(0, []byte("export completed but server returned no download_url"))
	}

	if err := os.MkdirAll(filepath.Dir(targetPath), dirPerm); err != nil {
		return "", err
	}
	bare := &http.Client{}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, final.DownloadURL, http.NoBody)
	if err != nil {
		return "", err
	}
	res, err := bare.Do(httpReq) //nolint:gosec // fetching a presigned URL returned by our own trusted API, not caller-controlled
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode >= http.StatusBadRequest {
		body, readErr := io.ReadAll(res.Body)
		if readErr != nil {
			body = nil
		}
		return "", newAPIError(res.StatusCode, body)
	}

	f, err := os.Create(targetPath)
	if err != nil {
		return "", err
	}
	defer f.Close()
	buf := make([]byte, chunkSize)
	if _, err := io.CopyBuffer(f, res.Body, buf); err != nil {
		return "", err
	}
	return targetPath, nil
}
