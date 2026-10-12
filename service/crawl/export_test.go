package crawl

import "time"

// SetWaitPollInterval shortens Wait's polling for a test and returns a function that restores it.
func SetWaitPollInterval(d time.Duration) (restore func()) {
	interval, maxInterval := waitPollInterval, waitMaxPollInterval
	waitPollInterval, waitMaxPollInterval = d, d
	return func() { waitPollInterval, waitMaxPollInterval = interval, maxInterval }
}

// RetryAfterWait exposes the wait a Retry-After header asks for before a retry.
func RetryAfterWait(raw string) (time.Duration, bool) { return retryAfterWait(raw) }
