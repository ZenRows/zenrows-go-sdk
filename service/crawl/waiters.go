package crawl

import (
	"context"
	"fmt"
	"math/rand"
	"time"
)

// Wait defaults.
const (
	defaultWaitTimeout         = 600 * time.Second
	defaultWaitPollInterval    = 2 * time.Second
	defaultWaitMaxPollInterval = 15 * time.Second
	waitBackoff                = 1.5
	waitJitter                 = 0.2
)

// WaiterTimeoutError is returned when Wait's timeout elapsed before the crawl ended. The crawl
// keeps running; Wait never stops it.
type WaiterTimeoutError struct {
	Timeout time.Duration
}

func (e WaiterTimeoutError) Error() string {
	return fmt.Sprintf("waiter: timed out after %s waiting for target state", e.Timeout)
}

// WaitOptions configures Client.Wait.
type WaitOptions struct {
	Timeout         time.Duration // defaults to 600s
	PollInterval    time.Duration // defaults to 2s; each wait is 1.5x the last, jittered
	MaxPollInterval time.Duration // defaults to 15s
}

// Wait blocks until the crawl reaches a terminal status (any but StatusRunning) and returns
// it, polling with jittered exponential backoff. A failed crawl is returned with its Error,
// not as a Go error. Every poll shares the timeout, so Wait returns by then: on timeout it
// returns WaiterTimeoutError and leaves the crawl running; ctx cancellation returns ctx.Err().
// Read the results afterwards with IterResults.
func (c *Client) Wait(ctx context.Context, crawlID string, opts WaitOptions) (Crawl, error) {
	if opts.Timeout <= 0 {
		opts.Timeout = defaultWaitTimeout
	}
	if opts.PollInterval <= 0 {
		opts.PollInterval = defaultWaitPollInterval
	}
	if opts.MaxPollInterval <= 0 {
		opts.MaxPollInterval = defaultWaitMaxPollInterval
	}

	waitCtx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()
	timedOut := func(err error) error {
		if ctx.Err() == nil && waitCtx.Err() != nil {
			return WaiterTimeoutError{Timeout: opts.Timeout}
		}
		return err
	}

	interval := opts.PollInterval
	for {
		// limit=1 keeps each poll cheap: only the crawl's status is needed here.
		page, err := c.Get(waitCtx, crawlID, GetOptions{Limit: 1})
		if err != nil {
			return Crawl{}, timedOut(err)
		}
		if page.Status.IsTerminal() {
			return page.Crawl, nil
		}

		jitterFactor := 1.0 + (rand.Float64()*2-1)*waitJitter //nolint:gosec // timing jitter, not security-sensitive
		if !sleepCtx(waitCtx, time.Duration(float64(interval)*jitterFactor)) {
			return Crawl{}, timedOut(ctx.Err())
		}
		interval = min(time.Duration(float64(interval)*waitBackoff), opts.MaxPollInterval)
	}
}
