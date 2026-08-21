package batch

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
)

// Safety caps. Deliberate, conservative defaults — bulk downloads of scraping jobs can
// easily reach gigabytes if not bounded.
const (
	DefaultMaxFiles              = 100_000
	DefaultMaxBytesPerFile       = 50 * 1024 * 1024 // 50 MiB
	DefaultMaxCountInMemory      = 10_000
	DefaultMaxTotalBytesInMemory = 500 * 1024 * 1024 // 500 MiB

	dirPerm  = 0o755 // directories we create for downloaded results
	filePerm = 0o600 // downloaded result files - owner read/write only
)

// DownloadLimitExceededError is returned when a download exceeds a configured cap.
type DownloadLimitExceededError struct {
	LimitName string
	Limit     int64
	Observed  int64
}

func (e DownloadLimitExceededError) Error() string {
	return fmt.Sprintf("download: %s cap exceeded (%d > %d)", e.LimitName, e.Observed, e.Limit)
}

// DownloadedResult is one row's worth of downloaded content held in memory.
type DownloadedResult struct {
	TaskID      string
	ExternalID  string
	ContentType string
	Body        []byte
}

// ----- single task, straight from the presigned result_url -----
//
// NOTE: the API also exposes a GET .../tasks/{id}/content endpoint, but it exists for the
// web UI — it proxies the body back through the API with display-friendly headers so a
// browser can render it inline. This deliberately does NOT use it: result_url is a presigned
// storage URL, so fetching it is one hop straight to the bucket (no API round-trip, no auth
// header, and it keeps body bandwidth off the API).

func fetchResultURL(ctx context.Context, task TaskResult) (body []byte, contentType string, err error) {
	if task.ResultURL == "" {
		return nil, "", fmt.Errorf("task %q has no result_url — only successful tasks have a downloadable body", task.TaskID)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, task.ResultURL, http.NoBody)
	if err != nil {
		return nil, "", err
	}
	res, err := (&http.Client{}).Do(req) //nolint:gosec // fetching a presigned URL returned by our own trusted API, not caller-controlled
	if err != nil {
		return nil, "", err
	}
	defer res.Body.Close()
	body, err = io.ReadAll(res.Body)
	if err != nil {
		return nil, "", err
	}
	if res.StatusCode >= http.StatusBadRequest {
		return nil, "", newAPIError(res.StatusCode, body)
	}
	return body, res.Header.Get("Content-Type"), nil
}

// downloadTaskToFile downloads one task's body straight from its result_url and writes it to
// targetPath.
func downloadTaskToFile(task TaskResult, targetPath string) error {
	body, _, err := fetchResultURL(context.Background(), task)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(targetPath), dirPerm); err != nil {
		return err
	}
	return os.WriteFile(targetPath, body, filePerm)
}

// downloadTaskToMemory downloads one task's body straight from its result_url and returns
// the raw bytes.
func downloadTaskToMemory(task TaskResult) ([]byte, error) {
	body, _, err := fetchResultURL(context.Background(), task)
	return body, err
}

func downloadExportToPath(ctx context.Context, export Export, targetPath string, chunkSize int) (string, error) {
	if export.Status != ExportStatusCompleted {
		msg := export.Error
		if msg == "" {
			msg = "export not completed"
		}
		return "", newAPIError(0, []byte(msg))
	}
	if export.DownloadURL == "" {
		return "", newAPIError(0, []byte("export completed but server returned no download_url"))
	}
	if err := os.MkdirAll(filepath.Dir(targetPath), dirPerm); err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, export.DownloadURL, http.NoBody)
	if err != nil {
		return "", err
	}
	res, err := (&http.Client{}).Do(req) //nolint:gosec // fetching a presigned URL returned by our own trusted API, not caller-controlled
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

// ----- bulk: to-disk -----

// nameAllocator hands out unique filenames against a running set — thread-safe, shared
// across concurrent workers. Claim("order-1.html") returns "order-1.html" the first time and
// "order-1_01.html", "order-1_02.html", ... on subsequent claims with the same input.
type nameAllocator struct {
	mu     sync.Mutex
	counts map[string]int
}

func newNameAllocator() *nameAllocator { return &nameAllocator{counts: map[string]int{}} }

func (a *nameAllocator) claim(name string) string {
	a.mu.Lock()
	n := a.counts[name]
	a.counts[name] = n + 1
	a.mu.Unlock()
	if n == 0 {
		return name
	}
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	return fmt.Sprintf("%s_%02d%s", stem, n, ext)
}

func extFor(row TaskResult) string {
	if row.Type == "" {
		return "bin"
	}
	return string(row.Type)
}

func defaultFilename(row TaskResult) string {
	return fmt.Sprintf("%s.%s", row.TaskID, extFor(row))
}

func externalIDFilename(row TaskResult) string {
	raw := row.ExternalID
	var b strings.Builder
	for _, c := range raw {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '.' || c == '_' || c == '-' {
			b.WriteRune(c)
		} else {
			b.WriteRune('_')
		}
	}
	base := b.String()
	if base == "" {
		base = row.TaskID
	}
	return fmt.Sprintf("%s.%s", base, extFor(row))
}

// DownloadToDirOptions configures Client.DownloadToDir / RunRef.DownloadToDir /
// CurrentRun.DownloadToDir.
type DownloadToDirOptions struct {
	RunID           string // set internally by RunRef/CurrentRun; leave empty at the Client level for the job's latest run
	Status          string // defaults to "successful"
	NameFn          func(TaskResult) string
	UseExternalID   bool
	Concurrency     int // defaults to 1 (serial)
	MaxFiles        int // defaults to DefaultMaxFiles
	MaxBytesPerFile int // defaults to DefaultMaxBytesPerFile
}

// DownloadToDir streams every (matching) task's body into targetDir. Returns the count
// written. Concurrency parallelizes the body-fetch + write; with the default 1, behavior is
// exactly serial. Results are NOT guaranteed to be in any particular order when
// Concurrency > 1.
func (c *Client) DownloadToDir(ctx context.Context, jobID, targetDir string, opts DownloadToDirOptions) (int, error) {
	status := opts.Status
	if status == "" {
		status = string(TaskStatusSuccessful)
	}
	maxFiles := opts.MaxFiles
	if maxFiles <= 0 {
		maxFiles = DefaultMaxFiles
	}
	maxBytesPerFile := opts.MaxBytesPerFile
	if maxBytesPerFile <= 0 {
		maxBytesPerFile = DefaultMaxBytesPerFile
	}
	nameFn := opts.NameFn
	if nameFn == nil {
		if opts.UseExternalID {
			nameFn = externalIDFilename
		} else {
			nameFn = defaultFilename
		}
	}

	if err := os.MkdirAll(targetDir, dirPerm); err != nil {
		return 0, err
	}
	allocator := newNameAllocator()

	handle := func(row TaskResult) error {
		body, _, err := fetchResultURL(ctx, row)
		if err != nil {
			return err
		}
		if len(body) > maxBytesPerFile {
			return DownloadLimitExceededError{LimitName: "max_bytes_per_file", Limit: int64(maxBytesPerFile), Observed: int64(len(body))}
		}
		chosen := allocator.claim(nameFn(row))
		path := filepath.Join(targetDir, chosen)
		if err := os.MkdirAll(filepath.Dir(path), dirPerm); err != nil {
			return err
		}
		return os.WriteFile(path, body, filePerm)
	}

	return runBulk(ctx, c, jobID, opts.RunID, status, handle, opts.Concurrency, maxFiles, "max_files")
}

// ----- bulk: to-memory -----

// DownloadToMemoryOptions configures Client.DownloadToMemory / RunRef.DownloadToMemory /
// CurrentRun.DownloadToMemory.
type DownloadToMemoryOptions struct {
	RunID           string // set internally by RunRef/CurrentRun
	Status          string // defaults to "successful"
	Concurrency     int    // defaults to 1 (serial)
	MaxCount        int    // defaults to DefaultMaxCountInMemory
	MaxTotalBytes   int64  // defaults to DefaultMaxTotalBytesInMemory
	MaxBytesPerFile int    // defaults to DefaultMaxBytesPerFile
}

// DownloadToMemory loads every (matching) task body into a slice and returns it. Three
// independent caps that all return DownloadLimitExceededError: MaxCount (row count), MaxTotalBytes
// (running sum of body sizes), MaxBytesPerFile (any single oversize body aborts). Returned
// slice ordering is NOT guaranteed when Concurrency > 1.
func (c *Client) DownloadToMemory(ctx context.Context, jobID string, opts DownloadToMemoryOptions) ([]DownloadedResult, error) {
	status := opts.Status
	if status == "" {
		status = string(TaskStatusSuccessful)
	}
	maxCount := opts.MaxCount
	if maxCount <= 0 {
		maxCount = DefaultMaxCountInMemory
	}
	maxTotalBytes := opts.MaxTotalBytes
	if maxTotalBytes <= 0 {
		maxTotalBytes = DefaultMaxTotalBytesInMemory
	}
	maxBytesPerFile := opts.MaxBytesPerFile
	if maxBytesPerFile <= 0 {
		maxBytesPerFile = DefaultMaxBytesPerFile
	}

	var mu sync.Mutex
	var out []DownloadedResult
	var totalBytes int64

	handle := func(row TaskResult) error {
		body, contentType, err := fetchResultURL(ctx, row)
		if err != nil {
			return err
		}
		if len(body) > maxBytesPerFile {
			return DownloadLimitExceededError{LimitName: "max_bytes_per_file", Limit: int64(maxBytesPerFile), Observed: int64(len(body))}
		}
		newTotal := atomic.AddInt64(&totalBytes, int64(len(body)))
		if newTotal > maxTotalBytes {
			return DownloadLimitExceededError{LimitName: "max_total_bytes", Limit: maxTotalBytes, Observed: newTotal}
		}
		mu.Lock()
		out = append(out, DownloadedResult{TaskID: row.TaskID, ExternalID: row.ExternalID, ContentType: contentType, Body: body})
		mu.Unlock()
		return nil
	}

	if _, err := runBulk(ctx, c, jobID, opts.RunID, status, handle, opts.Concurrency, maxCount, "max_count"); err != nil {
		return nil, err
	}
	return out, nil
}

// ----- shared driver -----

// runBulk iterates results + applies handle to each, with optional parallelism via a
// semaphore-limited goroutine pool. Returns the number of rows processed.
func runBulk(
	ctx context.Context, c *Client, jobID, runID, status string,
	handle func(TaskResult) error, concurrency, maxRows int, maxRowsLimitName string,
) (int, error) {
	if concurrency <= 0 {
		concurrency = 1
	}

	rowsIter := c.IterResults(ctx, jobID, GetResultsOptions{RunID: runID, Status: status})

	if concurrency == 1 {
		written := 0
		for row, err := range rowsIter {
			if err != nil {
				return written, err
			}
			if written >= maxRows {
				return written, DownloadLimitExceededError{LimitName: maxRowsLimitName, Limit: int64(maxRows), Observed: int64(written + 1)}
			}
			if err := handle(row); err != nil {
				return written, err
			}
			written++
		}
		return written, nil
	}

	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex
	written := 0
	var firstErr error

	for row, err := range rowsIter {
		if err != nil {
			mu.Lock()
			if firstErr == nil {
				firstErr = err
			}
			mu.Unlock()
			break
		}
		mu.Lock()
		if written >= maxRows {
			if firstErr == nil {
				firstErr = DownloadLimitExceededError{LimitName: maxRowsLimitName, Limit: int64(maxRows), Observed: int64(written + 1)}
			}
			mu.Unlock()
			break
		}
		mu.Unlock()

		sem <- struct{}{}
		wg.Add(1)
		go func(row TaskResult) {
			defer wg.Done()
			defer func() { <-sem }()
			if err := handle(row); err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
				return
			}
			mu.Lock()
			written++
			mu.Unlock()
		}(row)
	}
	wg.Wait()
	return written, firstErr
}
