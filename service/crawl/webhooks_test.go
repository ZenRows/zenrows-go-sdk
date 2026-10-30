package crawl_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/zenrows/zenrows-go-sdk/service/crawl"
)

func TestCreateSendsTheWebhook(t *testing.T) {
	var gotBody map[string]any
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		writeJSON(w, http.StatusAccepted, `{"crawl_id":"c_123","status":"running","url":"https://example.com/",
			"max_items":100,"max_pages":100,
			"webhook":{"url":"https://hooks.example.com/zenrows","events":["crawl.completed"],"metadata":{"k":"v"}},
			"coverage":{"pages_fetched":0,"pages_failed":0,"items_found":0},"created_at":"2026-10-08T10:00:00Z"}`)
	})

	got, err := client.Create(context.Background(), crawl.CreateParams{
		URL: "https://example.com/",
		Webhook: &crawl.Webhook{
			URL:      "https://hooks.example.com/zenrows",
			Events:   []crawl.WebhookEventType{crawl.WebhookEventCrawlCompleted},
			Metadata: map[string]any{"k": "v"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	sent, _ := json.Marshal(gotBody["webhook"])
	want := `{"events":["crawl.completed"],"metadata":{"k":"v"},"url":"https://hooks.example.com/zenrows"}`
	if string(sent) != want {
		t.Fatalf("sent webhook = %s, want %s", sent, want)
	}
	if got.Webhook == nil || got.Webhook.URL != "https://hooks.example.com/zenrows" ||
		!slices.Equal(got.Webhook.Events, []crawl.WebhookEventType{crawl.WebhookEventCrawlCompleted}) ||
		got.Webhook.Metadata["k"] != "v" {
		t.Fatalf("Webhook = %+v", got.Webhook)
	}
}

func TestCreateWebhookSendsOnlyItsSetFields(t *testing.T) {
	var gotBody map[string]map[string]any
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		writeJSON(w, http.StatusAccepted, `{"crawl_id":"c_123","status":"running","url":"https://example.com/",
			"webhook":{"url":"https://hooks.example.com/zenrows",
				"events":["crawl.started","crawl.completed","crawl.stopped","crawl.failed"],"metadata":null},
			"coverage":{"pages_fetched":0,"pages_failed":0,"items_found":0},"created_at":"2026-10-08T10:00:00Z"}`)
	})

	got, err := client.Create(context.Background(), crawl.CreateParams{
		URL: "https://example.com/", Webhook: &crawl.Webhook{URL: "https://hooks.example.com/zenrows"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(gotBody["webhook"]) != 1 || gotBody["webhook"]["url"] != "https://hooks.example.com/zenrows" {
		t.Fatalf("sent webhook = %v, want only the url", gotBody["webhook"])
	}
	all := []crawl.WebhookEventType{
		crawl.WebhookEventCrawlStarted, crawl.WebhookEventCrawlCompleted,
		crawl.WebhookEventCrawlStopped, crawl.WebhookEventCrawlFailed,
	}
	if got.Webhook == nil || !slices.Equal(got.Webhook.Events, all) || got.Webhook.Metadata != nil {
		t.Fatalf("Webhook = %+v, want every event and nil metadata", got.Webhook)
	}
}

func TestCrawlWithoutWebhookReadsNil(t *testing.T) {
	var gotBody map[string]any
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		writeJSON(w, http.StatusAccepted, `{"crawl_id":"c_123","status":"running","url":"https://example.com/",
			"coverage":{"pages_fetched":0,"pages_failed":0,"items_found":0},"created_at":"2026-10-08T10:00:00Z"}`)
	})

	got, err := client.Create(context.Background(), crawl.CreateParams{URL: "https://example.com/"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, sent := gotBody["webhook"]; sent {
		t.Fatalf("body carries a webhook nobody set: %v", gotBody)
	}
	if got.Webhook != nil {
		t.Fatalf("Webhook = %+v, want nil", got.Webhook)
	}
}

func TestGetAndListReadTheWebhook(t *testing.T) {
	const withHook = `{"crawl_id":"c_123","status":"completed","url":"https://example.com/",
		"webhook":{"url":"https://hooks.example.com/zenrows","events":["crawl.failed"],"metadata":{"n":1}},
		"coverage":{"pages_fetched":1,"pages_failed":0,"items_found":0},"created_at":"2026-10-08T10:00:00Z"`
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/crawls/"+testCrawlID {
			writeJSON(w, http.StatusOK, withHook+`,"results":[],"next_cursor":null}`)
			return
		}
		writeJSON(w, http.StatusOK, `{"crawls":[`+withHook+`}]}`)
	})

	got, err := client.Get(context.Background(), testCrawlID, crawl.GetOptions{})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	list, err := client.List(context.Background(), crawl.ListOptions{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for name, c := range map[string]*crawl.Crawl{"Get": &got.Crawl, "List": &list.Crawls[0]} {
		if c.Webhook == nil || !slices.Equal(c.Webhook.Events, []crawl.WebhookEventType{crawl.WebhookEventCrawlFailed}) ||
			c.Webhook.Metadata["n"] != float64(1) {
			t.Errorf("%s: Webhook = %+v", name, c.Webhook)
		}
	}
}

func TestWebhookErrorCodes(t *testing.T) {
	for _, code := range []string{"invalid_webhook", "webhook_signing_key_required"} {
		client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
			writeProblem(w, http.StatusUnprocessableEntity, code)
		})
		_, err := client.Create(context.Background(), crawl.CreateParams{
			URL: "https://example.com/", Webhook: &crawl.Webhook{URL: "https://hooks.example.com/zenrows"},
		})
		var apiErr crawl.APIError
		if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusUnprocessableEntity || apiErr.Code() != code {
			t.Errorf("%s: got %v", code, err)
		}
	}
}

func TestWebhookEventTypeIsKnown(t *testing.T) {
	for _, e := range []crawl.WebhookEventType{
		crawl.WebhookEventCrawlStarted, crawl.WebhookEventCrawlCompleted,
		crawl.WebhookEventCrawlStopped, crawl.WebhookEventCrawlFailed,
	} {
		if !e.IsKnown() {
			t.Errorf("%q: defined constant reported unknown", e)
		}
	}
	if crawl.WebhookEventType("crawl.x_added_later").IsKnown() {
		t.Error("undefined value reported known")
	}
}

func TestWebhookEventDecodes(t *testing.T) {
	body := []byte(`{"schema":"zenrows.webhook/v1","event_id":"ev_01M5AKXYZ_completed","event_type":"crawl.completed",
		"occurred_at":"2026-10-27T10:42:10Z",
		"resource":{"type":"crawl","id":"c_01M5AKXYZ","url":"https://api.zenrows.com/v1/crawls/c_01M5AKXYZ"},
		"metadata":{"site":"golfclub"}}`)
	var ev crawl.WebhookEvent
	if err := json.Unmarshal(body, &ev); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if ev.Schema != crawl.WebhookSchema || ev.EventID != "ev_01M5AKXYZ_completed" ||
		ev.EventType != crawl.WebhookEventCrawlCompleted || ev.OccurredAt != "2026-10-27T10:42:10Z" ||
		ev.Resource.Type != "crawl" || ev.Resource.ID != "c_01M5AKXYZ" ||
		ev.Resource.URL != "https://api.zenrows.com/v1/crawls/c_01M5AKXYZ" || ev.Metadata["site"] != "golfclub" {
		t.Fatalf("event = %+v", ev)
	}
}

// signLikeTheAPI mirrors how a crawl event is signed: HMAC-SHA256 with the raw key bytes over
// "<unix seconds>.<raw body>", sent as "t=...,v1=<hex>,kid=...".
func signLikeTheAPI(rawKey []byte, kid string, body []byte, at time.Time) string {
	mac := hmac.New(sha256.New, rawKey)
	_, _ = fmt.Fprintf(mac, "%d.%s", at.Unix(), body)
	return fmt.Sprintf("t=%d,v1=%s,kid=%s", at.Unix(), hex.EncodeToString(mac.Sum(nil)), kid)
}

const verifyKID = "kid_1"

func TestVerifyWebhookSignature(t *testing.T) {
	rawKey := []byte("0123456789abcdef0123456789abcdef")
	shown := base64.StdEncoding.EncodeToString(rawKey) // the signing key as the dashboard shows it
	body := []byte(`{"schema":"zenrows.webhook/v1","event_type":"crawl.completed"}`)
	at := time.Unix(1_760_000_000, 0)
	now := func() time.Time { return at.Add(time.Minute) }
	header := signLikeTheAPI(rawKey, verifyKID, body, at)
	secrets := map[string]string{"kid_0": base64.StdEncoding.EncodeToString([]byte("old")), verifyKID: shown}
	opts := crawl.VerifyWebhookSignatureOptions{Now: now}

	if err := crawl.VerifyWebhookSignature(body, header, secrets, opts); err != nil {
		t.Fatalf("valid signature rejected: %v", err)
	}

	other := base64.StdEncoding.EncodeToString([]byte("ffffffffffffffffffffffffffffffff"))
	short := base64.StdEncoding.EncodeToString([]byte("short"))
	cases := []struct {
		name    string
		body    []byte
		header  string
		secrets map[string]string
		opts    crawl.VerifyWebhookSignatureOptions
		want    error
	}{
		{"tampered body", []byte(`{"event_type":"crawl.failed"}`), header, secrets, opts, crawl.ErrWebhookSignatureMismatch},
		{
			"secret encoded twice", body, header,
			map[string]string{verifyKID: base64.StdEncoding.EncodeToString([]byte(shown))},
			opts,
			crawl.ErrWebhookSecretMalformed,
		},
		{"wrong 32-byte key", body, header, map[string]string{verifyKID: other}, opts, crawl.ErrWebhookSignatureMismatch},
		{
			"forged with an empty key", body, signLikeTheAPI(nil, verifyKID, body, at),
			map[string]string{verifyKID: ""},
			opts,
			crawl.ErrWebhookSecretMalformed,
		},
		{
			"forged with a short key", body, signLikeTheAPI([]byte("short"), verifyKID, body, at),
			map[string]string{verifyKID: short},
			opts,
			crawl.ErrWebhookSecretMalformed,
		},
		{"unknown kid", body, header, map[string]string{"kid_0": shown}, opts, crawl.ErrWebhookUnknownKey},
		{"malformed header", body, "sha256=abc", secrets, opts, crawl.ErrWebhookSignatureMalformed},
		{"header part without =", body, header + ",junk", secrets, opts, crawl.ErrWebhookSignatureMalformed},
		{"timestamp not a number", body, "t=soon,v1=00,kid=" + verifyKID, secrets, opts, crawl.ErrWebhookSignatureMalformed},
		{"signature not hex", body, "t=1,v1=zz,kid=" + verifyKID, secrets, opts, crawl.ErrWebhookSignatureMalformed},
		{"secret not base64", body, header, map[string]string{verifyKID: "not base64!"}, opts, crawl.ErrWebhookSecretMalformed},
		{
			"stale timestamp", body, header, secrets,
			crawl.VerifyWebhookSignatureOptions{Now: func() time.Time { return at.Add(time.Hour) }},
			crawl.ErrWebhookTimestampOutOfRange,
		},
		{
			"timestamp too far ahead", body, header, secrets,
			crawl.VerifyWebhookSignatureOptions{Now: func() time.Time { return at.Add(-time.Hour) }},
			crawl.ErrWebhookTimestampOutOfRange,
		},
		{
			"timestamp in the year 3000", body, signLikeTheAPI(rawKey, verifyKID, body, time.Date(3000, 1, 1, 0, 0, 0, 0, time.UTC)), secrets, opts,
			crawl.ErrWebhookTimestampOutOfRange,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := crawl.VerifyWebhookSignature(tc.body, tc.header, tc.secrets, tc.opts)
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}

	late := crawl.VerifyWebhookSignatureOptions{Now: func() time.Time { return at.Add(time.Hour) }, SkipTimestampCheck: true}
	if err := crawl.VerifyWebhookSignature(body, header, secrets, late); err != nil {
		t.Fatalf("SkipTimestampCheck still rejected: %v", err)
	}
	wide := crawl.VerifyWebhookSignatureOptions{Now: func() time.Time { return at.Add(time.Hour) }, Tolerance: 2 * time.Hour}
	if err := crawl.VerifyWebhookSignature(body, header, secrets, wide); err != nil {
		t.Fatalf("custom tolerance still rejected: %v", err)
	}
}
