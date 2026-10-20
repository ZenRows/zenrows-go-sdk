package batch

import (
	"context"
	"errors"
	"fmt"
	"net/http"
)

// GetJobWebhook returns the job's current webhook config. Returns an APIError (404) when
// none is set.
func (c *Client) GetJobWebhook(ctx context.Context, jobID string) (*WebhookConfig, error) {
	if !c.isConfigured() {
		return nil, NotConfiguredError{}
	}
	var result WebhookConfig
	path := fmt.Sprintf("/jobs/%s/webhook", jobID)
	req := c.request(ctx, &result)
	if err := c.do(ctx, req, http.MethodGet, path); err != nil {
		return nil, err
	}
	return &result, nil
}

// PutJobWebhook replaces the job's webhook config wholesale and always sends
// config.Signature, so a WebhookConfig without Signature set sends false and turns HMAC
// signing off. Returns the persisted config.
//
// Deprecated: use SetJobWebhook (or JobRef.SetWebhook), which takes the signing choice as a
// required argument so it can't be left out by accident, or SetJobWebhookURL, which keeps the
// current signing choice.
func (c *Client) PutJobWebhook(ctx context.Context, jobID string, config WebhookConfig) (*WebhookConfig, error) {
	if !c.isConfigured() {
		return nil, NotConfiguredError{}
	}
	var result WebhookConfig
	path := fmt.Sprintf("/jobs/%s/webhook", jobID)
	req := c.request(ctx, &result).SetBody(config)
	if err := c.do(ctx, req, http.MethodPut, path); err != nil {
		return nil, err
	}
	return &result, nil
}

// SetJobWebhook replaces the job's webhook config wholesale. The API requires both the URL and
// the signing choice on a replace, so signature is a required argument: pass true to keep (or
// turn on) HMAC signing of deliveries, false to turn it off. Turning it on requires an active
// HMAC key (else a 400 webhook_signing_requires_active_key). Returns the persisted config.
func (c *Client) SetJobWebhook(ctx context.Context, jobID, url string, signature bool) (*WebhookConfig, error) {
	return c.PutJobWebhook(ctx, jobID, WebhookConfig{URL: url, Signature: signature})
}

// SetJobWebhookURL points the job's webhook at url and keeps its current signing choice: it
// reads the current config first (GetJobWebhook), and uses false when the job has none (404).
// Use SetJobWebhook to set both explicitly.
func (c *Client) SetJobWebhookURL(ctx context.Context, jobID, url string) (*WebhookConfig, error) {
	current, err := c.GetJobWebhook(ctx, jobID)
	signature := false
	var apiErr APIError
	switch {
	case err == nil:
		signature = current.Signature
	case errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound:
	default:
		return nil, err
	}
	return c.SetJobWebhook(ctx, jobID, url, signature)
}

// DeleteJobWebhook clears the job's webhook config. Idempotent: succeeds whether or not one
// was set.
func (c *Client) DeleteJobWebhook(ctx context.Context, jobID string) error {
	if !c.isConfigured() {
		return NotConfiguredError{}
	}
	path := fmt.Sprintf("/jobs/%s/webhook", jobID)
	return c.do(ctx, c.request(ctx, nil), http.MethodDelete, path)
}

// TestWebhook dispatches a synthetic webhook.test event to a receiver URL and reports the
// outcome, without touching any job. Handy to verify a receiver before wiring it to a job.
// req.Signature defaults to false; set true to exercise the HMAC path (requires an active
// signing key, else a 400 webhook_signing_requires_active_key error).
func (c *Client) TestWebhook(ctx context.Context, req TestWebhookRequest) (*TestWebhookResponse, error) {
	if !c.isConfigured() {
		return nil, NotConfiguredError{}
	}
	var result TestWebhookResponse
	r := c.request(ctx, &result).SetBody(req)
	if err := c.do(ctx, r, http.MethodPost, "/webhook/test"); err != nil {
		return nil, err
	}
	return &result, nil
}
