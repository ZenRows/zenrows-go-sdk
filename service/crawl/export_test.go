package crawl

import "time"

// SetWaitPollInterval shortens Wait's polling for a test and returns a function that restores it.
func SetWaitPollInterval(d time.Duration) (restore func()) {
	interval, maxInterval := waitPollInterval, waitMaxPollInterval
	waitPollInterval, waitMaxPollInterval = d, d
	return func() { waitPollInterval, waitMaxPollInterval = interval, maxInterval }
}

// StopRetryWait exposes how long Stop waits after a 503 crawl_busy.
func StopRetryWait(retryAfter time.Duration) time.Duration { return stopRetryWait(retryAfter) }
