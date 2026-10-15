package batch_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/zenrows/zenrows-go-sdk/service/batch"
)

// signLikeTheAPI mirrors how the Batch API signs a delivery: HMAC-SHA256 with the raw key
// bytes over "<unix seconds>.<raw body>", sent as "t=...,v1=<hex>,kid=...".
func signLikeTheAPI(rawKey []byte, kid string, body []byte, at time.Time) string {
	mac := hmac.New(sha256.New, rawKey)
	_, _ = fmt.Fprintf(mac, "%d.%s", at.Unix(), body)
	return fmt.Sprintf("t=%d,v1=%s,kid=%s", at.Unix(), hex.EncodeToString(mac.Sum(nil)), kid)
}

const verifyKID = "kid_1"

func TestVerifyWebhook(t *testing.T) {
	rawKey := []byte("0123456789abcdef0123456789abcdef")
	shown := base64.StdEncoding.EncodeToString(rawKey) // what the API returns when the key is created
	body := []byte(`{"event_type":"run.completed","job_id":"j1"}`)
	at := time.Unix(1_760_000_000, 0)
	now := func() time.Time { return at.Add(time.Minute) }
	header := signLikeTheAPI(rawKey, verifyKID, body, at)
	secrets := map[string]string{"kid_0": base64.StdEncoding.EncodeToString([]byte("old")), verifyKID: shown}

	if err := batch.VerifyWebhook(body, header, secrets, batch.VerifyWebhookOptions{Now: now}); err != nil {
		t.Fatalf("genuine delivery rejected: %v", err)
	}

	cases := []struct {
		name    string
		body    []byte
		header  string
		secrets map[string]string
		opts    batch.VerifyWebhookOptions
		want    error
	}{
		{"tampered body", []byte(`{"event_type":"run.failed"}`), header, secrets, batch.VerifyWebhookOptions{Now: now}, batch.ErrWebhookSignatureMismatch},
		{"secret encoded twice", body, header, map[string]string{verifyKID: base64.StdEncoding.EncodeToString([]byte(shown))}, batch.VerifyWebhookOptions{Now: now}, batch.ErrWebhookSecretMalformed},
		{"wrong 32-byte key", body, header, map[string]string{verifyKID: base64.StdEncoding.EncodeToString([]byte("ffffffffffffffffffffffffffffffff"))}, batch.VerifyWebhookOptions{Now: now}, batch.ErrWebhookSignatureMismatch},
		{"forged with an empty key, secret empty", body, signLikeTheAPI(nil, verifyKID, body, at), map[string]string{verifyKID: ""}, batch.VerifyWebhookOptions{Now: now}, batch.ErrWebhookSecretMalformed},
		{"forged with a short key", body, signLikeTheAPI([]byte("short"), verifyKID, body, at), map[string]string{verifyKID: base64.StdEncoding.EncodeToString([]byte("short"))}, batch.VerifyWebhookOptions{Now: now}, batch.ErrWebhookSecretMalformed},
		{"unknown kid", body, header, map[string]string{"kid_0": shown}, batch.VerifyWebhookOptions{Now: now}, batch.ErrWebhookUnknownKey},
		{"malformed header", body, "sha256=abc", secrets, batch.VerifyWebhookOptions{Now: now}, batch.ErrWebhookSignatureMalformed},
		{"header part without =", body, header + ",junk", secrets, batch.VerifyWebhookOptions{Now: now}, batch.ErrWebhookSignatureMalformed},
		{"timestamp not a number", body, "t=soon,v1=00,kid=" + verifyKID, secrets, batch.VerifyWebhookOptions{Now: now}, batch.ErrWebhookSignatureMalformed},
		{"signature not hex", body, "t=1,v1=zz,kid=" + verifyKID, secrets, batch.VerifyWebhookOptions{Now: now}, batch.ErrWebhookSignatureMalformed},
		{"secret not base64", body, header, map[string]string{verifyKID: "not base64!"}, batch.VerifyWebhookOptions{Now: now}, batch.ErrWebhookSecretMalformed},
		{"stale timestamp", body, header, secrets, batch.VerifyWebhookOptions{Now: func() time.Time { return at.Add(time.Hour) }}, batch.ErrWebhookTimestampOutOfRange},
		{"timestamp too far ahead", body, header, secrets, batch.VerifyWebhookOptions{Now: func() time.Time { return at.Add(-time.Hour) }}, batch.ErrWebhookTimestampOutOfRange},
		{"timestamp in the year 3000", body, signLikeTheAPI(rawKey, verifyKID, body, time.Date(3000, 1, 1, 0, 0, 0, 0, time.UTC)), secrets, batch.VerifyWebhookOptions{Now: now}, batch.ErrWebhookTimestampOutOfRange},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := batch.VerifyWebhook(tc.body, tc.header, tc.secrets, tc.opts)
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}

	late := batch.VerifyWebhookOptions{Now: func() time.Time { return at.Add(time.Hour) }, SkipTimestampCheck: true}
	if err := batch.VerifyWebhook(body, header, secrets, late); err != nil {
		t.Fatalf("SkipTimestampCheck still rejected: %v", err)
	}
	wide := batch.VerifyWebhookOptions{Now: func() time.Time { return at.Add(time.Hour) }, Tolerance: 2 * time.Hour}
	if err := batch.VerifyWebhook(body, header, secrets, wide); err != nil {
		t.Fatalf("custom tolerance still rejected: %v", err)
	}
}
