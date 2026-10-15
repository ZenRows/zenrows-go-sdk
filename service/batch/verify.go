package batch

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

// SignatureHeader is the request header that carries a signed webhook's signature, in the form
// "t=<unix seconds>,v1=<hex HMAC-SHA256>,kid=<key id>".
const SignatureHeader = "X-Signature"

// EventIDHeader carries a webhook delivery's event id. Deliveries can repeat (a retry after a
// slow or failed response), so dedupe on it.
const EventIDHeader = "X-ZenRows-Event-Id"

// webhookSecretBytes is the length of every HMAC key the Batch API issues: 32 random bytes.
const webhookSecretBytes = 32

// DefaultWebhookTolerance is how far a signature's timestamp may be from the current time
// before VerifyWebhook rejects it as a possible replay.
const DefaultWebhookTolerance = 5 * time.Minute

// Errors VerifyWebhook returns. Match them with errors.Is.
var (
	ErrWebhookSignatureMalformed  = errors.New("batch: webhook signature header is malformed")
	ErrWebhookUnknownKey          = errors.New("batch: webhook signed with a key id that has no secret")
	ErrWebhookSignatureMismatch   = errors.New("batch: webhook signature does not match the body")
	ErrWebhookTimestampOutOfRange = errors.New("batch: webhook signature timestamp is outside the tolerance")
	ErrWebhookSecretMalformed     = errors.New("batch: webhook secret is not a base64-encoded 32-byte key")
)

// VerifyWebhookOptions tunes VerifyWebhook. The zero value checks the timestamp against
// time.Now with DefaultWebhookTolerance (5 minutes). Unlike the Python and Node helpers, where
// a tolerance of 0 turns the check off, a zero Tolerance here means "use the default", because
// it is also the value of an unset field; set SkipTimestampCheck to turn the check off.
type VerifyWebhookOptions struct {
	// Tolerance is the largest accepted gap between the signature's timestamp and now.
	// Zero (or less) means DefaultWebhookTolerance.
	Tolerance time.Duration
	// SkipTimestampCheck turns off the replay window entirely.
	SkipTimestampCheck bool
	// Now overrides the clock (for tests). Nil means time.Now.
	Now func() time.Time
}

// VerifyWebhook checks a signed webhook delivery. rawBody must be the request body exactly as
// received (before any JSON decoding), and signatureHeader the value of the X-Signature header.
// secrets maps each HMAC key id to its secret as the API returned it when the key was created:
// a base64 string that decodes to 32 bytes. Any other secret, including "", is rejected with
// ErrWebhookSecretMalformed and never verifies. Keep both the old and the new key during a rotation; the header's kid picks
// the one that signed. Returns nil when the signature is valid, and otherwise an error that
// matches one of the ErrWebhook* sentinels under errors.Is (the Go form of the boolean the
// other SDKs return).
func VerifyWebhook(rawBody []byte, signatureHeader string, secrets map[string]string, opts VerifyWebhookOptions) error {
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
	// a delivery forged with an empty HMAC key would otherwise pass.
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
func checkWebhookTimestamp(ts int64, opts VerifyWebhookOptions) error {
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
