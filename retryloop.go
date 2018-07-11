package main

import (
	"context"
	"time"

	"github.com/cenkalti/backoff"
	"github.com/sirupsen/logrus"
)

// loopWithRetries calls a function repeately until context is cancelled; in
// case of a failure retries are scheduled using the given back-off algorithm
func loopWithRetries(ctx context.Context, logger logrus.FieldLogger,
	delay time.Duration, retryBackOff backoff.BackOff,
	fn func(context.Context) error) {

	var pending bool

	// Use existing code to introduce jitter for normal retries
	normalBackOff := backoff.NewExponentialBackOff()
	normalBackOff.InitialInterval = delay
	normalBackOff.RandomizationFactor = 0.1
	normalBackOff.MaxInterval = delay
	normalBackOff.Multiplier = 1
	normalBackOff.MaxElapsedTime = 0
	normalBackOff.Reset()

loop:
	for {
		if err := fn(ctx); err == nil {
			pending = false
		} else {
			if permanent, ok := err.(*backoff.PermanentError); ok {
				logger.Debugf("Giving up on retries due to permanent error: %s", permanent.Err)
				pending = false
			} else {
				logger.Debugf("Operation failed: %s", err)

				if !pending {
					// Start with retries
					pending = true
					retryBackOff.Reset()
				}
			}
		}

		timerDuration := normalBackOff.NextBackOff()

		if timerDuration == backoff.Stop {
			timerDuration = delay
			normalBackOff.Reset()
		}

		if pending {
			if next := retryBackOff.NextBackOff(); next == backoff.Stop {
				logger.Debug("Giving up on retries")
				pending = false
			} else {
				timerDuration = next
			}
		}

		logger.Debugf("Sleeping for %s", timerDuration)

		timer := time.NewTimer(timerDuration)

		select {
		case <-ctx.Done():
			timer.Stop()
			break loop

		case <-timer.C:
		}
	}
}
