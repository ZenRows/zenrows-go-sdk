// Command checks runs checks 1-21 and the Go extra checks against the Crawl SDK.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/zenrows/zenrows-go-sdk/service/crawl"
)

// noMatch is an include pattern no URL contains: the crawl keeps nothing and fails (check 19).
const noMatch = "/crawl-acceptance-matches-nothing/"

// documentedDefault is the default the README documents for MaxPages and MaxItems (check 21).
const documentedDefault = 100

var (
	key     = os.Getenv("ZENROWS_API_KEY")
	hasKey  = key != ""
	url0    = os.Getenv("CRAWL_URL")
	include = os.Getenv("CRAWL_INCLUDE")
	apiBase = os.Getenv("CRAWL_API_BASE")
	seq     = 0
	ctx     = context.Background()
)

func redact(s string) string {
	if key != "" {
		s = strings.ReplaceAll(s, key, "<REDACTED>")
	}
	return s
}

func trim(s string, n int) string {
	s = redact(s)
	if len(s) > n {
		return fmt.Sprintf("%s… [%d more chars]", s[:n], len(s)-n)
	}
	return s
}

func js(v any) string { b, _ := json.Marshal(v); return string(b) }

func sorted(xs []string) []string {
	out := append([]string(nil), xs...)
	sort.Strings(out)
	return out
}

func record(check, title, status, expected, actual, evidence string) {
	seq++
	r := map[string]string{"surface": "go", "check": check, "title": title, "status": status,
		"expected": expected, "actual": trim(actual, 800), "evidence": trim(evidence, 2500)}
	b, _ := json.MarshalIndent(r, "", "  ")
	name := regexp.MustCompile(`[^A-Za-z0-9_.-]+`).ReplaceAllString(check, "_")
	_ = os.WriteFile(filepath.Join("/results/checks", fmt.Sprintf("1%02d-%s.json", seq, name)), b, 0o644)
	fmt.Printf("[%s] go %s %s: %s\n", status, check, title, strings.SplitN(r["actual"], "\n", 2)[0])
}

func passIf(ok bool) string {
	if ok {
		return "PASS"
	}
	return "FAIL"
}

type outcome struct{ status, actual, evidence string }

func run(check, title, expected string, live bool, fn func() outcome) {
	if live && !hasKey {
		record(check, title, "SKIP", expected, "ZENROWS_API_KEY not set (run without --no-key)", "")
		return
	}
	defer func() {
		if r := recover(); r != nil {
			record(check, title, "FAIL", expected, fmt.Sprintf("panic: %v", r), "")
		}
	}()
	o := fn()
	record(check, title, o.status, expected, o.actual, o.evidence)
}

func ledger(id, purpose string) {
	if id == "" {
		return
	}
	f, err := os.OpenFile("/results/crawls.jsonl", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintln(f, js(map[string]string{"crawl_id": id, "purpose": purpose}))
}

func apiErr(err error) (crawl.APIError, bool) {
	var e crawl.APIError
	ok := errors.As(err, &e)
	return e, ok
}

func esum(err error) string {
	if err == nil {
		return "no error"
	}
	if e, ok := apiErr(err); ok {
		return js(map[string]any{"StatusCode": e.StatusCode, "Code()": e.Code(), "Error()": e.Error()})
	}
	return fmt.Sprintf("%T: %v", err, err)
}

var client = crawl.NewClient(crawl.WithAPIKey(key), crawl.WithBaseURL(apiBase))

// create waits out a 429 (the account's active jobs are shared), harness-side.
func create(p crawl.CreateParams) (*crawl.Crawl, error) {
	for i := 0; i < 10; i++ {
		c, err := client.Create(ctx, p)
		if e, ok := apiErr(err); ok && e.StatusCode == 429 {
			wait := e.RetryAfter
			if wait == 0 {
				wait = 30 * time.Second
			}
			time.Sleep(wait)
			continue
		}
		if c != nil {
			ledger(c.CrawlID, "check")
		}
		return c, err
	}
	return nil, errors.New("still 429 after 10 tries")
}

// standIn records what the client sends. It answers a create with a running crawl, c_running
// with a crawl that never ends, c_nobody with a 502 that has no body, and anything else 404.
func standIn(bodies *[]map[string]any) *httptest.Server {
	const running = `{"crawl_id":"c_running","status":"running","url":"x","depth":1,"max_items":10,"max_pages":10,` +
		`"coverage":{"pages_fetched":0,"pages_failed":0,"items_found":0},"created_at":"2026-01-01T00:00:00Z"`
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost:
			raw, _ := io.ReadAll(r.Body)
			var m map[string]any
			_ = json.Unmarshal(raw, &m)
			*bodies = append(*bodies, m)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(201)
			fmt.Fprint(w, running+"}")
		case strings.Contains(r.URL.Path, "c_running"):
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, running+`,"results":[],"next_cursor":"n"}`)
		case strings.Contains(r.URL.Path, "c_nobody"):
			w.WriteHeader(502)
		default:
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(404)
			fmt.Fprint(w, `{"status":404,"code":"crawl_not_found"}`)
		}
	}))
}

func main() {
	base := crawl.CreateParams{URL: url0, Depth: 1, MaxItems: 5, MaxPages: 10, IncludePatterns: []string{include}}

	// The README run's crawls, created before this program, go in the ledger.
	if b, err := os.ReadFile("/results/readme-start.txt"); err == nil && hasKey {
		since, _ := time.Parse(time.RFC3339, string(b))
		if l, err := client.List(ctx, crawl.ListOptions{Limit: 20}); err == nil {
			for _, c := range l.Crawls {
				if t, err := time.Parse(time.RFC3339Nano, c.CreatedAt); err == nil && t.After(since) {
					ledger(c.CrawlID, "README quickstart")
				}
			}
		}
	}

	// Check 1, offline half: what the client sends.
	var bodies []map[string]any
	srv := standIn(&bodies)
	defer srv.Close()
	capClient := crawl.NewClient(crawl.WithAPIKey("capture-key"), crawl.WithBaseURL(srv.URL+"/v1"), crawl.WithRetries(0))
	_, _ = capClient.Create(ctx, crawl.CreateParams{URL: "https://example.com/", Depth: 1})
	_, _ = capClient.Create(ctx, crawl.CreateParams{URL: "https://example.com/", Depth: 1, MaxItems: 5, MaxPages: 10,
		IncludePatterns: []string{"/p/"}, IdempotencyKey: "idem-1"})
	_, _ = capClient.Create(ctx, crawl.CreateParams{URL: "https://example.com/"})
	sentOK := len(bodies) == 3 && len(bodies[0]) == 2 && bodies[0]["url"] != nil && bodies[0]["depth"] != nil && len(bodies[1]) == 5 &&
		len(bodies[2]) == 1 && bodies[2]["url"] != nil
	sentLine := "sent (stand-in server): " + js(bodies)

	var A, B, C, F, ended *crawl.Crawl
	run("1", "Create", "returns at once: running, a crawl_id, the echoed parameters; only the fields set are sent", false, func() outcome {
		if !hasKey {
			st := "SKIP"
			if !sentOK {
				st = "FAIL"
			}
			return outcome{st, fmt.Sprintf("live part skipped (no key); only set fields sent: %v", sentOK), sentLine}
		}
		t := time.Now()
		c, err := create(base)
		if err != nil {
			return outcome{"FAIL", esum(err), ""}
		}
		A = c
		ok := sentOK && c.Status == crawl.StatusRunning && strings.HasPrefix(c.CrawlID, "c_") && c.URL == url0 && c.Depth != nil && *c.Depth == 1 &&
			c.MaxItems == 5 && c.MaxPages == 10 && js(c.IncludePatterns) == js([]string{include})
		return outcome{passIf(ok), fmt.Sprintf("%s, %s, %d ms; only set fields sent: %v", c.Status, c.CrawlID, time.Since(t).Milliseconds(), sentOK),
			js(c) + "\n" + sentLine}
	})

	run("5", "Wait runs out", "Wait with Timeout 1 s on a running crawl returns it StatusRunning, no error", true, func() outcome {
		if A == nil {
			return outcome{"SKIP", "no crawl from check 1", ""}
		}
		t := time.Now()
		w, err := client.Wait(ctx, A.CrawlID, crawl.WaitOptions{Timeout: time.Second})
		ms := time.Since(t).Milliseconds()
		if err != nil {
			return outcome{"FAIL", esum(err), ""}
		}
		if w.Status != crawl.StatusRunning {
			return outcome{"SKIP", fmt.Sprintf("the crawl had already ended (%s) after %d ms", w.Status, ms), ""}
		}
		return outcome{passIf(ms < 5000), fmt.Sprintf("%s after %d ms, no error", w.Status, ms), js(w)}
	})

	type partialRead struct {
		running bool
		n       int
		ms      int64
		err     error
	}
	var partial *partialRead
	if A != nil {
		g, _ := client.Get(ctx, A.CrawlID, crawl.GetOptions{Limit: 1})
		p := &partialRead{running: g != nil && g.Status == crawl.StatusRunning}
		t := time.Now()
		for _, err := range client.Results(ctx, A.CrawlID, crawl.ResultsOptions{}) {
			if err != nil {
				p.err = err
				break
			}
			p.n++
		}
		p.ms = time.Since(t).Milliseconds()
		partial = p
	}

	// Extra: Wait returns within its Timeout. Live crawls end in seconds, so this runs against
	// the stand-in, which answers "running" forever.
	run("x-go-wait-timeout", "Wait returns within its Timeout", "a 3 s Wait on a crawl that never ends returns in about 3 s, running, no error", false, func() outcome {
		t := time.Now()
		w, err := capClient.Wait(ctx, "c_running", crawl.WaitOptions{Timeout: 3 * time.Second})
		el := time.Since(t)
		if err != nil {
			return outcome{"FAIL", esum(err), ""}
		}
		return outcome{passIf(w.Status == crawl.StatusRunning && el >= 2500*time.Millisecond && el < 4*time.Second),
			fmt.Sprintf("returned after %s (Timeout 3 s), %s, no error", el.Round(time.Millisecond), w.Status), ""}
	})

	run("3", "Idempotency", "same key + body → same crawl_id; different body → 422 idempotency_key_reused", true, func() outcome {
		rb := make([]byte, 12)
		_, _ = rand.Read(rb)
		body := base
		body.MaxItems, body.MaxPages, body.IdempotencyKey = 3, 3, "acc-"+hex.EncodeToString(rb)
		c, err := create(body)
		if err != nil {
			return outcome{"FAIL", esum(err), ""}
		}
		C = c
		again, err := create(body)
		if err != nil {
			return outcome{"FAIL", "second create: " + esum(err), ""}
		}
		diff := body
		diff.MaxItems = 4
		d, derr := client.Create(ctx, diff)
		if d != nil {
			ledger(d.CrawlID, "idempotency, different body (unexpected create)")
		}
		e, isAPI := apiErr(derr)
		ok := c.CrawlID == again.CrawlID && isAPI && e.StatusCode == 422 && e.Code() == "idempotency_key_reused"
		return outcome{passIf(ok), fmt.Sprintf("first %s, second %s; different body → %s", c.CrawlID, again.CrawlID, esum(derr)), ""}
	})

	run("11", "Stop", "running → stopped, stop_reason user; ended → answers as it ended, no error", true, func() outcome {
		if C == nil {
			return outcome{"SKIP", "no crawl from check 3", ""}
		}
		first, err := client.Stop(ctx, C.CrawlID)
		if err != nil {
			return outcome{"FAIL", esum(err), ""}
		}
		if first.Status == crawl.StatusCompleted && first.StopReason != crawl.StopReasonUser {
			return outcome{"SKIP", fmt.Sprintf("the crawl had already ended on its own (%s/%s)", first.Status, first.StopReason), ""}
		}
		second, err2 := client.Stop(ctx, C.CrawlID)
		two := esum(err2)
		if err2 == nil {
			two = string(second.Status) + "/" + string(second.StopReason)
		}
		ok := first.Status == crawl.StatusStopped && first.StopReason == crawl.StopReasonUser && err2 == nil && second.Status == first.Status
		return outcome{passIf(ok), fmt.Sprintf("first: %s/%s; second: %s", first.Status, first.StopReason, two), js(first)}
	})

	run("4", "Wait", "Wait (default timeout) returns the ended crawl; failed is returned, not an error", true, func() outcome {
		if A == nil {
			return outcome{"SKIP", "no crawl from check 1", ""}
		}
		w, err := client.Wait(ctx, A.CrawlID, crawl.WaitOptions{})
		if err != nil {
			return outcome{"FAIL", esum(err), ""}
		}
		ended = w
		ok := w.Status == crawl.StatusCompleted || w.Status == crawl.StatusStopped || w.Status == crawl.StatusFailed
		return outcome{passIf(ok), fmt.Sprintf("%s/%s; coverage %s", w.Status, w.StopReason, js(w.Coverage)), js(w)}
	})

	run("6", "Get", "limit 2 then the cursor: one page each; NextCursor moves on and is nil on the last page", true, func() outcome {
		if ended == nil {
			return outcome{"SKIP", "no ended crawl", ""}
		}
		var sizes []int
		var urls []string
		seen := map[string]bool{}
		cursor, lastNil, dupCursor := "", false, false
		for i := 0; i < 20; i++ {
			p, err := client.Get(ctx, A.CrawlID, crawl.GetOptions{Limit: 2, Cursor: cursor})
			if err != nil {
				return outcome{"FAIL", esum(err), ""}
			}
			sizes = append(sizes, len(p.Results))
			for _, r := range p.Results {
				urls = append(urls, r.URL)
			}
			if p.NextCursor == nil {
				lastNil = true
				break
			}
			dupCursor = dupCursor || seen[*p.NextCursor]
			seen[*p.NextCursor] = true
			cursor = *p.NextCursor
		}
		distinct := map[string]bool{}
		for _, u := range urls {
			distinct[u] = true
		}
		ok := lastNil && !dupCursor && len(distinct) == len(urls) && (len(sizes) >= 2 || ended.Coverage.ItemsFound <= 2)
		for _, s := range sizes {
			ok = ok && s <= 2
		}
		return outcome{passIf(ok), fmt.Sprintf("%d pages of %v; last NextCursor nil %v; %d URLs", len(sizes), sizes, lastNil, len(urls)), ""}
	})

	run("7", "List", "limit 2 then the next page: newest first; NextCursor empty on the last page", true, func() outcome {
		p1, err := client.List(ctx, crawl.ListOptions{Limit: 2})
		if err != nil {
			return outcome{"FAIL", esum(err), ""}
		}
		all := append([]crawl.Crawl{}, p1.Crawls...)
		cursor := p1.NextCursor
		if cursor != "" {
			p2, err := client.List(ctx, crawl.ListOptions{Limit: 2, Cursor: cursor})
			if err != nil {
				return outcome{"FAIL", esum(err), ""}
			}
			all = append(all, p2.Crawls...)
			cursor = p2.NextCursor
		}
		newest, ids := true, map[string]bool{}
		for i, c := range all {
			ids[c.CrawlID] = true
			if i > 0 && all[i-1].CreatedAt < c.CreatedAt {
				newest = false
			}
		}
		pages := 2
		for cursor != "" && pages < 60 {
			p, err := client.List(ctx, crawl.ListOptions{Limit: 100, Cursor: cursor})
			if err != nil {
				return outcome{"FAIL", esum(err), ""}
			}
			pages++
			cursor = p.NextCursor
		}
		ok := len(p1.Crawls) == 2 && newest && len(ids) == len(all) && cursor == ""
		return outcome{passIf(ok), fmt.Sprintf("newest first %v, no overlap %v; last page reached after %d pages: %v",
			newest, len(ids) == len(all), pages, cursor == ""), ""}
	})

	run("8", "Results", "every kept URL once, each matching CRAWL_INCLUDE; on a running crawl, what is kept so far", true, func() outcome {
		if ended == nil {
			return outcome{"SKIP", "no ended crawl", ""}
		}
		var urls []string
		for r, err := range client.Results(ctx, A.CrawlID, crawl.ResultsOptions{Limit: 2}) {
			if err != nil {
				return outcome{"FAIL", esum(err), ""}
			}
			urls = append(urls, r.URL)
		}
		d, match := map[string]bool{}, true
		for _, u := range urls {
			d[u] = true
			match = match && strings.Contains(u, include)
		}
		part := "not read while running"
		if partial != nil {
			part = fmt.Sprintf("while running (running %v): %d URLs in %d ms, error %v", partial.running, partial.n, partial.ms, partial.err)
		}
		ok := len(urls) > 0 && len(d) == len(urls) && match && len(urls) == ended.Coverage.ItemsFound && (partial == nil || partial.err == nil)
		return outcome{passIf(ok), fmt.Sprintf("%d URLs (items_found %d), distinct %v, all match %v; %s",
			len(urls), ended.Coverage.ItemsFound, len(d) == len(urls), match, part), strings.Join(urls, "\n")}
	})

	// The html crawl (checks 20, 2, 9, 10): stopped with pages in flight, read, then settled.
	settle := func(id string) string {
		for i := 0; i < 45; i++ {
			p, err := client.Get(ctx, id, crawl.GetOptions{Limit: 100})
			if err == nil {
				pending := false
				for _, r := range p.Results {
					pending = pending || r.ContentStatus == crawl.ContentStatusPending
				}
				if !pending {
					return "settled"
				}
			}
			time.Sleep(2 * time.Second)
		}
		return "still pending after 90 s"
	}
	var stopStatus crawl.Status
	var stopItems int
	var stopErr error
	if hasKey {
		p := base
		p.OutputFormat = crawl.OutputFormatHTML
		B, stopErr = create(p)
		if stopErr == nil {
			// Stop as soon as the crawl has kept URLs: their pages are then still being fetched.
			var g *crawl.CrawlWithResults
			for i := 0; i < 100; i++ {
				g, stopErr = client.Get(ctx, B.CrawlID, crawl.GetOptions{Limit: 1})
				if stopErr != nil || g.Status != crawl.StatusRunning || g.Coverage.ItemsFound > 0 {
					break
				}
				time.Sleep(150 * time.Millisecond)
			}
			if stopErr == nil {
				stopItems = g.Coverage.ItemsFound
				var s *crawl.StopResponse
				if s, stopErr = client.Stop(ctx, B.CrawlID); stopErr == nil {
					stopStatus = s.Status
				}
			}
		}
	}
	run("20", "Results after stop", "Results on a crawl stopped with pages in flight ends without looping, and loses no URL", true, func() outcome {
		if B == nil || stopErr != nil {
			return outcome{"FAIL", "setup failed: " + esum(stopErr), ""}
		}
		if stopStatus != crawl.StatusStopped {
			return outcome{"SKIP", fmt.Sprintf("the crawl had already ended (%s) before the stop", stopStatus), ""}
		}
		rctx, cancel := context.WithTimeout(ctx, 60*time.Second)
		defer cancel()
		t := time.Now()
		var now []string
		pending := 0
		for r, err := range client.Results(rctx, B.CrawlID, crawl.ResultsOptions{}) {
			if err != nil {
				return outcome{"FAIL", "Results did not end within 60 s or failed: " + esum(err), ""}
			}
			now = append(now, r.URL)
			if r.ContentStatus == crawl.ContentStatusPending {
				pending++
			}
		}
		ms := time.Since(t).Milliseconds()
		settled := settle(B.CrawlID)
		if settled != "settled" {
			settled += " (an API issue: the stop left these pages pending on an ended crawl)"
		}
		var later []string
		for r, err := range client.Results(ctx, B.CrawlID, crawl.ResultsOptions{}) {
			if err == nil {
				later = append(later, r.URL)
			}
		}
		same := js(sorted(now)) == js(sorted(later))
		return outcome{passIf(same && len(now) > 0), fmt.Sprintf("stopped with %d kept; read %d URLs (%d pending) in %d ms, then %s: %d URLs, same set %v",
			stopItems, len(now), pending, ms, settled, len(later), same), ""}
	})
	run("2", "Create with html", "results get content_status and, once fetched, content_url", true, func() outcome {
		if B == nil {
			return outcome{"SKIP", "no html crawl", ""}
		}
		if stopStatus == crawl.StatusStopped {
			settle(B.CrawlID)
		}
		n, withURL, ok := 0, 0, B.OutputFormat == crawl.OutputFormatHTML
		for r, err := range client.Results(ctx, B.CrawlID, crawl.ResultsOptions{}) {
			if err != nil {
				return outcome{"FAIL", esum(err), ""}
			}
			n++
			if r.ContentURL != "" {
				withURL++
			}
			ok = ok && r.ContentStatus != "" && (r.ContentStatus != crawl.ContentStatusFetched || r.ContentURL != "")
		}
		return outcome{passIf(ok && n > 0), fmt.Sprintf("%s (%s); %d results; with content_url %d", B.CrawlID, stopStatus, n, withURL), ""}
	})

	run("9", "Content", "Content by content id, then by ContentURL: the same HTML", true, func() outcome {
		if B == nil {
			return outcome{"SKIP", "no html crawl", ""}
		}
		firstFetched := func(id string) string {
			for r, err := range client.Results(ctx, id, crawl.ResultsOptions{}) {
				if err == nil && r.ContentURL != "" {
					return r.ContentURL
				}
			}
			return ""
		}
		// The stop in check 20 can land before any page is fetched. Then read a
		// one-item html crawl that runs to the end instead.
		src := B.CrawlID
		cu := firstFetched(src)
		if cu == "" {
			p := base
			p.MaxItems, p.OutputFormat = 1, crawl.OutputFormatHTML
			own, err := create(p)
			if err != nil {
				return outcome{"FAIL", "create: " + esum(err), ""}
			}
			if _, err := client.Wait(ctx, own.CrawlID, crawl.WaitOptions{}); err != nil {
				return outcome{"FAIL", "wait: " + esum(err), ""}
			}
			src = own.CrawlID
			cu = firstFetched(src)
		}
		if cu == "" {
			return outcome{"FAIL", "no result with ContentURL on " + src, ""}
		}
		byID, err1 := client.Content(ctx, src, cu[strings.LastIndex(cu, "/")+1:])
		byURL, err2 := client.Content(ctx, src, cu)
		if err1 != nil || err2 != nil {
			return outcome{"FAIL", esum(errors.Join(err1, err2)), ""}
		}
		ok := string(byID) == string(byURL) && regexp.MustCompile(`(?i)<html|<!doctype`).Match(byID)
		return outcome{passIf(ok), fmt.Sprintf("by id %d bytes, by ContentURL %d bytes, equal %v", len(byID), len(byURL), string(byID) == string(byURL)), ""}
	})

	run("10", "Download", "a status and one line per result, each with url, content_status, content", true, func() outcome {
		if B == nil {
			return outcome{"SKIP", "no html crawl", ""}
		}
		d, err := client.Download(ctx, B.CrawlID)
		if err != nil {
			return outcome{"FAIL", esum(err), ""}
		}
		defer d.Close()
		n, fetched, ok := 0, 0, d.Status != ""
		for l, err := range d.Lines() {
			if err != nil {
				return outcome{"FAIL", esum(err), ""}
			}
			n++
			ok = ok && l.URL != "" && l.ContentStatus != ""
			if l.ContentStatus == crawl.ContentStatusFetched {
				fetched++
				ok = ok && l.HTML() != ""
			}
		}
		return outcome{passIf(ok && n > 0), fmt.Sprintf("status %s; %d lines; %d with HTML", d.Status, n, fetched), ""}
	})

	codes := map[string]string{}
	run("12", "Not found", "Get c_does_not_exist → 404 crawl_not_found", true, func() outcome {
		_, err := client.Get(ctx, "c_does_not_exist", crawl.GetOptions{})
		e, ok := apiErr(err)
		codes["notFound"] = e.Code()
		return outcome{passIf(ok && e.StatusCode == 404 && e.Code() == "crawl_not_found"), esum(err), ""}
	})
	// Depth 0 is the zero value, which the client does not send (no depth limit), so the
	// invalid depth sent is -1.
	run("13", "Invalid input", "Create with Depth -1 → 422 invalid_parameter", true, func() outcome {
		p := base
		p.Depth = -1
		c, err := client.Create(ctx, p)
		if c != nil {
			ledger(c.CrawlID, "depth -1 (unexpected create)")
			_, _ = client.Stop(ctx, c.CrawlID)
		}
		e, ok := apiErr(err)
		codes["invalid"] = e.Code()
		return outcome{passIf(ok && e.StatusCode == 422 && e.Code() == "invalid_parameter"), esum(err), ""}
	})
	// Extra: a create without Depth is accepted and its crawl echoes no depth. MaxPages 1 keeps
	// the unlimited-depth crawl to one page; it is stopped at once.
	run("x-go-no-depth", "Create without Depth", "accepted, running; Crawl.Depth is nil (no depth limit)", true, func() outcome {
		p := base
		p.Depth, p.MaxItems, p.MaxPages = 0, 1, 1
		c, err := create(p)
		if err != nil {
			return outcome{"FAIL", esum(err), ""}
		}
		_, _ = client.Stop(ctx, c.CrawlID)
		depth := "nil"
		if c.Depth != nil {
			depth = fmt.Sprint(*c.Depth)
		}
		return outcome{passIf(c.Depth == nil && strings.HasPrefix(c.CrawlID, "c_")),
			fmt.Sprintf("%s, %s; Depth %s", c.CrawlID, c.Status, depth), js(c)}
	})
	run("14", "Gated option", "OutputFormat json → 422 invalid_parameter for a customer key", true, func() outcome {
		p := base
		p.OutputFormat = crawl.OutputFormat("json")
		c, err := client.Create(ctx, p)
		if c != nil {
			ledger(c.CrawlID, "output_format json (accepted)")
			_, _ = client.Stop(ctx, c.CrawlID)
			return outcome{"PASS", "accepted (" + c.CrawlID + "): the key is in a beta segment, not a bug; stopped it", ""}
		}
		e, ok := apiErr(err)
		return outcome{passIf(ok && e.StatusCode == 422 && e.Code() == "invalid_parameter"), esum(err),
			`OutputFormat: crawl.OutputFormat("json") (a string type, so any value is sent)`}
	})
	run("15", "Bad key", "any call with the key `invalid` → 401", false, func() outcome {
		bad := crawl.NewClient(crawl.WithAPIKey("invalid"), crawl.WithBaseURL(apiBase), crawl.WithRetries(0))
		_, err := bad.Get(ctx, "c_does_not_exist", crawl.GetOptions{})
		e, ok := apiErr(err)
		codes["badKey"] = e.Code()
		return outcome{passIf(ok && e.StatusCode == 401), esum(err), ""}
	})
	run("16", "Errors carry the server code", `Code() holds the server code; "" when the body has none`, false, func() outcome {
		_, err := capClient.Get(ctx, "c_nobody", crawl.GetOptions{})
		e, ok := apiErr(err)
		codes["noBody502"] = fmt.Sprintf("%d code=%q", e.StatusCode, e.Code())
		liveOK := !hasKey || (codes["notFound"] == "crawl_not_found" && codes["invalid"] == "invalid_parameter")
		all := liveOK && codes["badKey"] != "" && ok && e.StatusCode == 502 && e.Code() == ""
		st := passIf(all)
		if all && !hasKey {
			st = "SKIP"
		}
		return outcome{st, js(codes), "12, 13, 15, and a 502 with no body from the stand-in"}
	})

	run("19", "Failed crawl", "a crawl that keeps nothing ends failed (no_items_found); Wait returns it, not an error", true, func() outcome {
		c, err := create(crawl.CreateParams{URL: url0, Depth: 1, IncludePatterns: []string{noMatch}})
		if err != nil {
			return outcome{"FAIL", esum(err), ""}
		}
		F = c
		w, err := client.Wait(ctx, c.CrawlID, crawl.WaitOptions{})
		if err != nil {
			return outcome{"FAIL", "Wait returned an error: " + esum(err), ""}
		}
		ok := w.Status == crawl.StatusFailed && w.Error != nil && w.Error.Code == crawl.RunErrorNoItemsFound && w.Error.Detail != ""
		return outcome{passIf(ok), fmt.Sprintf("%s: %s; error %s; returned, no Go error", c.CrawlID, w.Status, js(w.Error)), js(w)}
	})
	run("21", "Documented defaults", fmt.Sprintf("a create without MaxPages and MaxItems echoes the documented default (%d)", documentedDefault), true, func() outcome {
		if F == nil {
			return outcome{"SKIP", "no crawl from check 19", ""}
		}
		ok := F.MaxPages == documentedDefault && F.MaxItems == documentedDefault
		return outcome{passIf(ok), fmt.Sprintf("the API applied max_pages %d, max_items %d; documented %d", F.MaxPages, F.MaxItems, documentedDefault), ""}
	})

	naming := map[string]any{
		"operations": map[string]string{"create": "Create", "get": "Get", "list": "List", "results": "Results", "content": "Content",
			"download": "Download", "stop": "Stop", "wait": "Wait"},
		"params": map[string]string{"url": "URL", "depth": "Depth", "max_items": "MaxItems", "max_pages": "MaxPages",
			"include_patterns": "IncludePatterns", "exclude_patterns": "ExcludePatterns", "output_format": "OutputFormat",
			"idempotency_key": "IdempotencyKey", "cursor": "Cursor", "limit": "Limit", "timeout": "Timeout",
			"content_id": "contentID"},
	}
	b, _ := json.MarshalIndent(naming, "", "  ")
	_ = os.WriteFile("/results/naming.json", b, 0o644)
	record("17", "Naming", "SKIP", "the API's names in this surface's casing", "computed by the report", "")

	run("18", "Copy", `Crawl is "new", never "beta"; one "still evolving" line; the 429 limit is active jobs shared with Batch`, false, func() outcome {
		dir := os.Getenv("CRAWL_MODULE_DIR")
		readme, _ := os.ReadFile(filepath.Join(dir, "README.md"))
		var src strings.Builder
		files, _ := filepath.Glob(filepath.Join(dir, "*.go"))
		for _, f := range files {
			if !strings.HasSuffix(f, "_test.go") {
				b, _ := os.ReadFile(f)
				src.Write(b)
			}
		}
		var problems []string
		beta, crawlWord := regexp.MustCompile(`(?i)\bbeta\b`), regexp.MustCompile(`(?i)crawl`)
		for _, l := range strings.Split(string(readme)+"\n"+src.String(), "\n") {
			if beta.MatchString(l) && crawlWord.MatchString(l) {
				problems = append(problems, `"beta" on a Crawl line: `+strings.TrimSpace(l))
			}
		}
		if n := len(regexp.MustCompile(`(?i)still evolving`).FindAllString(string(readme), -1)); n != 1 {
			problems = append(problems, fmt.Sprintf(`%d "still evolving" lines in the README (want 1)`, n))
		}
		if !regexp.MustCompile(`(?s)active jobs.{0,80}Batch`).MatchString(string(readme)) {
			problems = append(problems, "no line reads the 429 limit as the account's active jobs, shared with Batch")
		}
		if !strings.Contains(string(readme), "(new)") {
			problems = append(problems, `the README does not say "new"`)
		}
		actual := strings.Join(problems, "; ")
		if actual == "" {
			actual = `README: "new", 1 "still evolving" line, 429 wording ok; no "beta" in the package docs`
		}
		return outcome{passIf(len(problems) == 0), actual, "service/crawl README.md and package sources from the module cache"}
	})
	fmt.Println("go: done")
}
