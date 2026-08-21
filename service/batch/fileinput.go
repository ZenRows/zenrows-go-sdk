package batch

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
)

// CreateJobInput allocates a CSV upload slot (POST /job_inputs). Most callers want the
// higher-level UploadCSV instead.
func (c *Client) CreateJobInput(ctx context.Context, req CreateJobInputRequest) (*CreateJobInputResponse, error) {
	if !c.isConfigured() {
		return nil, NotConfiguredError{}
	}
	var result CreateJobInputResponse
	r := c.request(ctx, &result).SetBody(req)
	if err := c.do(ctx, r, http.MethodPost, "/job_inputs"); err != nil {
		return nil, err
	}
	return &result, nil
}

// UploadCSVOptions configures Client.UploadCSV.
type UploadCSVOptions struct {
	// URLField and ExternalIDField map canonical task fields to CSV columns — either a column
	// index (int) or a column name (string, requires Header: true). URLField is required.
	URLField        any
	ExternalIDField any
	Header          bool
	Delimiter       string
	Quote           string
}

// UploadCSV allocates a CSV slot and PUTs the body to the presigned URL the server returns.
// Returns the file_input_id to pass to SubmitRegular/SubmitScheduled's FileInputID.
//
// source may be a file path or an io.Reader. The presigned URL lives on a different host
// (object storage) — this deliberately uses a bare HTTP client so the API key never leaks
// there.
func (c *Client) UploadCSV(ctx context.Context, source any, opts UploadCSVOptions) (string, error) {
	delimiter := opts.Delimiter
	if delimiter == "" {
		delimiter = ","
	}
	quote := opts.Quote
	if quote == "" {
		quote = "\""
	}
	if opts.URLField == nil {
		return "", fmt.Errorf("UploadCSV: opts.URLField is required")
	}

	created, err := c.CreateJobInput(ctx, CreateJobInputRequest{
		Type: "csv",
		CSV: &CSVSpec{
			Delimiter: delimiter,
			Quote:     quote,
			Header:    opts.Header,
			Fields:    CSVFields{URL: opts.URLField, ExternalID: opts.ExternalIDField},
		},
	})
	if err != nil {
		return "", err
	}

	data, err := readCSVBody(source)
	if err != nil {
		return "", err
	}

	contentType := "text/csv"
	if ct, ok := created.Upload.Headers["Content-Type"]; ok {
		contentType = ct
	}

	bare := &http.Client{}
	httpReq, err := http.NewRequestWithContext(ctx, created.Upload.Method, created.Upload.URL, bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	for k, v := range created.Upload.Headers {
		httpReq.Header.Set(k, v)
	}
	httpReq.Header.Set("Content-Type", contentType)

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
	return created.FileInputID, nil
}

func readCSVBody(source any) ([]byte, error) {
	switch v := source.(type) {
	case string:
		return os.ReadFile(v)
	case []byte:
		return v, nil
	case io.Reader:
		return io.ReadAll(v)
	default:
		return nil, fmt.Errorf("UploadCSV: unsupported source type %T (want string path, []byte, or io.Reader)", source)
	}
}
