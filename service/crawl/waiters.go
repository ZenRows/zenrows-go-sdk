package crawl

import (
	"context"
	"fmt"
	"math/rand"
	"time"
)

// Wait defaults, the same as the batch package's WaitForRun.
const (
	defaultWaitTimeout         = 300 * time.Second
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
	Timeout         time.Duration // defaults to 300s
	PollInterval    time.Duration // defaults to 2s; each wait is 1.5x the last, jittered
	MaxPollInterval time.Duration // defaults to 15s
}

// Wait blocks until the crawl reaches a terminal status (any but StatusRunning) and returns
// it, polling with jittered exponential backoff. A failed crawl is returned with its Error,
// not as a Go error. On timeout it returns WaiterTimeoutError and leaves the crawl running;
// ctx cancellation returns ctx.Err(). Read the results afterwards with IterResults.
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

	deadline := time.Now().Add(opts.Timeout)
	interval := opts.PollInterval
	for {
		// limit=1 keeps each poll cheap: only the crawl's status is needed here.
		page, err := c.Get(ctx, crawlID, GetOptions{Limit: 1})
		if err != nil {
			return Crawl{}, err
		}
		if page.Status.IsTerminal() {
			return page.Crawl, nil
		}

		now := time.Now()
		if !now.Before(deadline) {
			return Crawl{}, WaiterTimeoutError{Timeout: opts.Timeout}
		}
		sleep := min(interval, deadline.Sub(now))
		jitterFactor := 1.0 + (rand.Float64()*2-1)*waitJitter //nolint:gosec // timing jitter, not security-sensitive
		if !sleepCtx(ctx, time.Duration(float64(sleep)*jitterFactor)) {
			return Crawl{}, ctx.Err()
		}
		interval = min(time.Duration(float64(interval)*waitBackoff), opts.MaxPollInterval)
	}
}
