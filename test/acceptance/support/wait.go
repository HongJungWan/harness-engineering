package support

import (
	"fmt"
	"time"
)

// Eventually polls condition every interval until it returns true or timeout is exceeded.
// Uses polling instead of time.Sleep as required by 03_Hook.md.
func Eventually(timeout, interval time.Duration, condition func() (bool, error)) error {
	deadline := time.After(timeout)
	for {
		ok, err := condition()
		if err != nil {
			return err
		}
		if ok {
			return nil
		}
		select {
		case <-deadline:
			return fmt.Errorf("timeout after %v", timeout)
		case <-time.After(interval):
			// retry
		}
	}
}
