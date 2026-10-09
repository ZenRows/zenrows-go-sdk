package crawl

import "time"

// SetWaitPollInterval shortens Wait's polling for a test and returns a function that restores it.
func SetWaitPollInterval(d time.Duration) (restore func()) {
	interval, maxInterval := waitPollInterval, waitMaxPollInterval
	waitPollInterval, waitMaxPollInterval = d, d
	return func() { waitPollInterval, waitMaxPollInterval = interval, maxInterval }
}
