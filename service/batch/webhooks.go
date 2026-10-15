package batch

import (
	"context"
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

// PutJobWebhook replaces the job's webhook config wholesale. The API requires both the URL
// and the signing choice, and this always sends config.Signature, so leaving it unset sends
// false and turns HMAC signing off. When changing only the URL of a signed webhook, read the
// current config first (GetJobWebhook) or use JobRef.SetWebhook, which takes the signing
// choice as an explicit argument. Returns the persisted config.
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
