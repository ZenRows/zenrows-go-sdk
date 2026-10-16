// Command webhook-receiver serves an HTTP endpoint that verifies and decodes Batch webhook
// deliveries.
//
//	WEBHOOK_KEY_ID=... WEBHOOK_SECRET=... go run ./examples/webhook-receiver
//
// WEBHOOK_SECRET is the base64 secret RotateHMACKey returned, exactly as returned.
package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/zenrows/zenrows-go-sdk/service/batch"
)

const maxBody = 1 << 20

func main() {
	// During a key rotation, list both the old and the new key.
	secrets := map[string]string{os.Getenv("WEBHOOK_KEY_ID"): os.Getenv("WEBHOOK_SECRET")}
	var seen sync.Map // event ids already handled; use durable storage in production

	http.HandleFunc("/batch-webhook", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, maxBody))
		if err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		// Verify the raw bytes before decoding them.
		if err := batch.VerifyWebhookSignature(body, r.Header.Get(batch.SignatureHeader), secrets,
			batch.VerifyWebhookSignatureOptions{}); err != nil {
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return
		}
		var event batch.WebhookEvent
		if err := json.Unmarshal(body, &event); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		// Deliveries can repeat: acknowledge a duplicate without handling it again.
		if _, dup := seen.LoadOrStore(event.EventID, true); dup {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		switch event.EventType {
		case batch.WebhookEventRunCompleted:
			log.Printf("job %s run %d completed: %+v", event.JobID, event.RunSequence, event.Stats)
		case batch.WebhookEventRunFailed:
			log.Printf("job %s run %d failed: %s %s", event.JobID, event.RunSequence, event.FailureReason, event.FailureDetail)
		default:
			log.Printf("event %s", event.EventType)
		}
		// Answer fast (deliveries time out after 10 seconds); do slow work elsewhere.
		w.WriteHeader(http.StatusNoContent)
	})

	server := &http.Server{Addr: ":8080", ReadHeaderTimeout: 5 * time.Second}
	log.Fatal(server.ListenAndServe())
}
