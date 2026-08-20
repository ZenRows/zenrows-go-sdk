package batch

import "net/http"
import "context"

// ListHMACKeys lists the org's current HMAC key slots (active/candidate metadata — never the
// secret; that's only returned by RotateHMACKey).
func (c *Client) ListHMACKeys(ctx context.Context) (*HMACKeyList, error) {
	if !c.isConfigured() {
		return nil, NotConfiguredError{}
	}
	var result HMACKeyList
	req := c.request(ctx, &result)
	if err := c.do(ctx, req, http.MethodGet, "/hmac/keys"); err != nil {
		return nil, err
	}
	return &result, nil
}

// RotateHMACKey generates the initial key or stages a rotation candidate. Capture the
// returned Secret NOW — it is not revealed again.
func (c *Client) RotateHMACKey(ctx context.Context) (*HMACKeyCreated, error) {
	if !c.isConfigured() {
		return nil, NotConfiguredError{}
	}
	var result HMACKeyCreated
	req := c.request(ctx, &result)
	if err := c.do(ctx, req, http.MethodPost, "/hmac/keys/rotate"); err != nil {
		return nil, err
	}
	return &result, nil
}

// FinalizeHMACKey promotes the pending rotation candidate to active.
func (c *Client) FinalizeHMACKey(ctx context.Context) (*HMACKeyFinalized, error) {
	if !c.isConfigured() {
		return nil, NotConfiguredError{}
	}
	var result HMACKeyFinalized
	req := c.request(ctx, &result)
	if err := c.do(ctx, req, http.MethodPost, "/hmac/keys/rotate/finalize"); err != nil {
		return nil, err
	}
	return &result, nil
}

// CancelHMACRotation discards the pending rotation candidate.
func (c *Client) CancelHMACRotation(ctx context.Context) error {
	if !c.isConfigured() {
		return NotConfiguredError{}
	}
	return c.do(ctx, c.request(ctx, nil), http.MethodDelete, "/hmac/keys/rotate")
}
