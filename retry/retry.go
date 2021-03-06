package retry

import (
	"time"
)

// ToRetry is a function type which wraps actual logic to be retried and returns error if that needs to happen
type ToRetry func() error // nolint: golint

// Do invokes a function and if invocation fails retries defined amount of time with sleep in between
// Returns accumulated errors if all attempts failed or empty slice otherwise
func Do(retries int, sleep time.Duration, toRetry ToRetry) chan error {
	iteration := 1
	errs := make(chan error, retries)
	defer close(errs)

	err := toRetry()
	if err == nil {
		return errs
	}
	errs <- err

	for {
		select {
		case <-time.After(sleep):
			if iteration == retries {
				return errs
			}
			err := toRetry()
			if err != nil {
				errs <- err
			} else {
				errs := make(chan error, 0)
				close(errs)
				return errs
			}
			iteration++
		}
	}
	return errs
}
