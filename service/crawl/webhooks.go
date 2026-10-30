package crawl

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// WebhookSchema is the WebhookEvent.Schema of every event this SDK version decodes.
const WebhookSchema = "zenrows.webhook/v1"

// SignatureHeader is the request header that carries an event's signature, in the form
// "t=<unix seconds>,v1=<hex HMAC-SHA256>,kid=<key id>".
const SignatureHeader = "X-Signature"

// EventIDHeader carries an event's WebhookEvent.EventID. An event can be delivered more than
// once (a retry after a slow or failed response), so dedupe on it.
const EventIDHeader = "X-ZenRows-Event-Id"

// webhookSecretBytes is the length of every webhook signing key: 32 random bytes.
const webhookSecretBytes = 32

// DefaultWebhookTolerance is how far a signature's timestamp may be from the current time
// before VerifyWebhookSignature rejects it as a possible replay.
const DefaultWebhookTolerance = 5 * time.Minute

// WebhookResource is the crawl a WebhookEvent is about.
type WebhookResource struct {
	// Type is "crawl".
	Type string `json:"type"`
	// ID is the crawl's CrawlID.
	ID string `json:"id"`
	// URL is the crawl on the API. Read it with Client.Get for its status, coverage and results.
	URL string `json:"url,omitempty"`
}

// WebhookEvent is the JSON body of one event a crawl's webhook receives. Verify the raw body
// with VerifyWebhookSignature before decoding it into this type. An event can arrive more than
// once, with the same EventID each time: drop one you have already handled. Events can also
// arrive out of order, so a short crawl's end can come before its WebhookEventCrawlStarted:
// order them by OccurredAt, or read the crawl.
type WebhookEvent struct {
	// Schema is WebhookSchema.
	Schema string `json:"schema"`
	// EventID is the same on every delivery of one event (e.g. "ev_01M5AKXYZ_completed"), and is
	// also sent as the EventIDHeader header.
	EventID   string           `json:"event_id"`
	EventType WebhookEventType `json:"event_type"`
	// OccurredAt is when the crawl was created, for WebhookEventCrawlStarted, and its FinishedAt
	// for the others.
	OccurredAt string          `json:"occurred_at"`
	Resource   WebhookResource `json:"resource"`
	// Metadata is the webhook's Metadata, nil when the crawl has none.
	Metadata map[string]any `json:"metadata,omitempty"`
}

// Errors VerifyWebhookSignature returns. Match them with errors.Is.
var (
	ErrWebhookSignatureMalformed  = errors.New("crawl: webhook signature header is malformed")
	ErrWebhookUnknownKey          = errors.New("crawl: webhook signed with a key id that has no secret")
	ErrWebhookSignatureMismatch   = errors.New("crawl: webhook signature does not match the body")
	ErrWebhookTimestampOutOfRange = errors.New("crawl: webhook signature timestamp is outside the tolerance")
	ErrWebhookSecretMalformed     = errors.New("crawl: webhook secret is not a base64-encoded 32-byte key")
)

// VerifyWebhookSignatureOptions tunes VerifyWebhookSignature. The zero value checks the
// timestamp against time.Now with DefaultWebhookTolerance (5 minutes). A zero Tolerance means
// "use the default", because it is also the value of an unset field; set SkipTimestampCheck to
// turn the check off.
type VerifyWebhookSignatureOptions struct {
	// Tolerance is the largest accepted gap between the signature's timestamp and now.
	// Zero (or less) means DefaultWebhookTolerance.
	Tolerance time.Duration
	// SkipTimestampCheck turns off the replay window entirely.
	SkipTimestampCheck bool
	// Now overrides the clock (for tests). Nil means time.Now.
	Now func() time.Time
}

// VerifyWebhookSignature checks that a crawl event was signed by Zenrows. rawBody must be the
// request body exactly as received (before any JSON decoding), and signatureHeader the value of
// the SignatureHeader header. secrets maps each key id to the account's webhook signing key as
// the dashboard shows it (Settings, API Keys): a base64 string that decodes to 32 bytes. The
// secret is decoded before use; any other secret, including "", is rejected with
// ErrWebhookSecretMalformed and never verifies. Keep both the old and the new key during a
// rotation; the header's kid picks the one that signed. The signature is compared in constant
// time. Returns nil when the signature is valid, and otherwise an error that matches one of the
// ErrWebhook* sentinels under errors.Is.
func VerifyWebhookSignature(rawBody []byte, signatureHeader string, secrets map[string]string, opts VerifyWebhookSignatureOptions) error {
	ts, want, kid, err := parseSignatureHeader(signatureHeader)
	if err != nil {
		return err
	}
	secretB64, ok := secrets[kid]
	if !ok {
		return fmt.Errorf("%w: %q", ErrWebhookUnknownKey, kid)
	}
	secret, err := base64.StdEncoding.DecodeString(secretB64)
	if err != nil {
		return fmt.Errorf("%w: key id %q: %w", ErrWebhookSecretMalformed, kid, err)
	}
	// An empty or short key must never verify: "" decodes to zero bytes without an error, and
	// an event forged with an empty HMAC key would otherwise pass.
	if len(secret) != webhookSecretBytes {
		return fmt.Errorf("%w: key id %q decodes to %d bytes, want %d", ErrWebhookSecretMalformed, kid, len(secret), webhookSecretBytes)
	}

	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(strconv.FormatInt(ts, 10) + "."))
	mac.Write(rawBody)
	if !hmac.Equal(mac.Sum(nil), want) {
		return ErrWebhookSignatureMismatch
	}
	if opts.SkipTimestampCheck {
		return nil
	}
	return checkWebhookTimestamp(ts, opts)
}

// parseSignatureHeader splits "t=<unix>,v1=<hex>,kid=<id>" into its parts.
func parseSignatureHeader(header string) (ts int64, sig []byte, kid string, err error) {
	var rawTS, rawSig string
	for _, part := range strings.Split(header, ",") {
		key, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			return 0, nil, "", ErrWebhookSignatureMalformed
		}
		switch key {
		case "t":
			rawTS = value
		case "v1":
			rawSig = value
		case "kid":
			kid = value
		}
	}
	if rawTS == "" || rawSig == "" || kid == "" {
		return 0, nil, "", ErrWebhookSignatureMalformed
	}
	if ts, err = strconv.ParseInt(rawTS, 10, 64); err != nil {
		return 0, nil, "", ErrWebhookSignatureMalformed
	}
	if sig, err = hex.DecodeString(rawSig); err != nil {
		return 0, nil, "", ErrWebhookSignatureMalformed
	}
	return ts, sig, kid, nil
}

// checkWebhookTimestamp rejects a signature whose timestamp is more than the tolerance before
// or after now.
func checkWebhookTimestamp(ts int64, opts VerifyWebhookSignatureOptions) error {
	tolerance := opts.Tolerance
	if tolerance <= 0 {
		tolerance = DefaultWebhookTolerance
	}
	now := time.Now
	if opts.Now != nil {
		now = opts.Now
	}
	// Compare the signed difference against both bounds. Negating it instead would overflow
	// for a timestamp far in the future, where Sub saturates at the minimum Duration.
	age := now().Sub(time.Unix(ts, 0))
	if age > tolerance || age < -tolerance {
		return ErrWebhookTimestampOutOfRange
	}
	return nil
}
