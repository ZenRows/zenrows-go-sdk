package batch

import (
	"context"
	"fmt"
	"math/rand"
	"time"
)

// WaiterTimeoutError is returned when a waiter's timeout elapsed before the target state.
type WaiterTimeoutError struct {
	Timeout time.Duration
}

func (e WaiterTimeoutError) Error() string {
	return fmt.Sprintf("waiter: timed out after %s waiting for target state", e.Timeout)
}

// WaiterError is returned when the resource entered a failure state the caller flagged.
type WaiterError struct {
	Value any
}

func (e WaiterError) Error() string {
	return fmt.Sprintf("waiter: resource entered failure state (%+v)", e.Value)
}

// pollOptions configures pollUntil. Zero-value Backoff/Jitter fall back to sane defaults.
type pollOptions struct {
	Timeout         time.Duration
	InitialInterval time.Duration
	MaxInterval     time.Duration
	Backoff         float64
	Jitter          float64
}

func defaultPollOptions() pollOptions {
	return pollOptions{
		Timeout:         300 * time.Second,
		InitialInterval: 1 * time.Second,
		MaxInterval:     15 * time.Second,
		Backoff:         1.5,
		Jitter:          0.2,
	}
}

// withPollDefaults fills any zero-value field in opts from defaultPollOptions.
func withPollDefaults(opts pollOptions) pollOptions {
	defaults := defaultPollOptions()
	if opts.Timeout <= 0 {
		opts.Timeout = defaults.Timeout
	}
	if opts.InitialInterval <= 0 {
		opts.InitialInterval = defaults.InitialInterval
	}
	if opts.MaxInterval <= 0 {
		opts.MaxInterval = defaults.MaxInterval
	}
	if opts.Backoff <= 0 {
		opts.Backoff = defaults.Backoff
	}
	return opts
}

// pollUntil calls fetch repeatedly until isDone(value) is true (returns the value), or
// isFailure(value) is true (returns WaiterError), or the timeout elapses (returns
// WaiterTimeoutError), or ctx is done (returns ctx.Err()).
//
// The wait between calls starts at InitialInterval, multiplies by Backoff each iteration,
// caps at MaxInterval, and is jittered by +/-Jitter fraction so concurrent waiters don't
// synchronize into thundering-herd patterns against the API.
func pollUntil[T any](
	ctx context.Context,
	fetch func(context.Context) (T, error),
	isDone func(T) bool,
	isFailure func(T) bool,
	opts pollOptions,
) (T, error) {
	opts = withPollDefaults(opts)

	deadline := time.Now().Add(opts.Timeout)
	interval := opts.InitialInterval

	for {
		var zero T
		value, err := fetch(ctx)
		if err != nil {
			return zero, err
		}
		if isDone(value) {
			return value, nil
		}
		if isFailure != nil && isFailure(value) {
			return zero, WaiterError{Value: value}
		}

		now := time.Now()
		if !now.Before(deadline) {
			return zero, WaiterTimeoutError{Timeout: opts.Timeout}
		}

		remaining := deadline.Sub(now)
		sleep := interval
		if sleep > remaining {
			sleep = remaining
		}
		jitterFactor := 1.0 + (rand.Float64()*2-1)*opts.Jitter //nolint:gosec // timing jitter, not security-sensitive
		sleep = time.Duration(float64(sleep) * jitterFactor)
		if sleep > 0 {
			timer := time.NewTimer(sleep)
			select {
			case <-ctx.Done():
				timer.Stop()
				return zero, ctx.Err()
			case <-timer.C:
			}
		}
		interval = time.Duration(float64(interval) * opts.Backoff)
		if interval > opts.MaxInterval {
			interval = opts.MaxInterval
		}
	}
}
