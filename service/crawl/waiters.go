package crawl

import (
	"context"
	"math/rand"
	"time"
)

const defaultWaitTimeout = 600 * time.Second

// Wait polls every waitPollInterval at first, 1.5x longer each time, up to waitMaxPollInterval.
var (
	waitPollInterval    = 2 * time.Second
	waitMaxPollInterval = 15 * time.Second
)

const (
	waitBackoff = 1.5
	waitJitter  = 0.2
)

// WaitOptions configures Client.Wait.
type WaitOptions struct {
	Timeout time.Duration // defaults to 600s
}

// Wait polls the crawl until its status is not StatusRunning or Timeout runs out, and returns
// it. On timeout it returns the crawl as last read, still StatusRunning, without an error; the
// crawl keeps running. A failed crawl is returned with its Error, not as a Go error. Wait reads
// the crawl at least once, so it returns an error only when a read fails or ctx ends. Read the
// results afterwards with Results.
func (c *Client) Wait(ctx context.Context, crawlID string, opts WaitOptions) (Crawl, error) {
	if opts.Timeout <= 0 {
		opts.Timeout = defaultWaitTimeout
	}
	waitCtx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	// limit=1 keeps each poll cheap: only the crawl's status is needed here.
	page, err := c.Get(ctx, crawlID, GetOptions{Limit: 1})
	if err != nil {
		return Crawl{}, err
	}
	// Called once waitCtx ends: the timeout returns the running crawl, a caller cancel its error.
	ended := func() (Crawl, error) {
		if err := ctx.Err(); err != nil {
			return Crawl{}, err
		}
		return page.Crawl, nil
	}

	for interval := waitPollInterval; !page.Status.IsTerminal(); {
		jitterFactor := 1.0 + (rand.Float64()*2-1)*waitJitter //nolint:gosec // timing jitter, not security-sensitive
		if !sleepCtx(waitCtx, time.Duration(float64(interval)*jitterFactor)) {
			return ended()
		}
		next, err := c.Get(waitCtx, crawlID, GetOptions{Limit: 1})
		if err != nil {
			if waitCtx.Err() != nil {
				return ended()
			}
			return Crawl{}, err
		}
		page = next
		interval = min(time.Duration(float64(interval)*waitBackoff), waitMaxPollInterval)
	}
	return page.Crawl, nil
}
