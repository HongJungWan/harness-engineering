package support

import (
	"sync"
	"time"
)

type ConcurrentResult struct {
	Index    int
	OrderID  string
	Err      error
	Duration time.Duration
}

// RunConcurrent executes n goroutines simultaneously using a barrier pattern.
// All goroutines are created first, then released at once via channel close
// to maximize contention on the pessimistic lock.
func RunConcurrent(n int, fn func(index int) (string, error)) []ConcurrentResult {
	results := make([]ConcurrentResult, n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(n)

	for i := 0; i < n; i++ {
		go func(idx int) {
			defer wg.Done()
			<-start // barrier: all goroutines wait here
			t0 := time.Now()
			orderID, err := fn(idx)
			results[idx] = ConcurrentResult{
				Index:    idx,
				OrderID:  orderID,
				Err:      err,
				Duration: time.Since(t0),
			}
		}(i)
	}

	close(start) // release all goroutines simultaneously
	wg.Wait()
	return results
}

func PartitionResults(results []ConcurrentResult) (successes, failures []ConcurrentResult) {
	for _, r := range results {
		if r.Err == nil {
			successes = append(successes, r)
		} else {
			failures = append(failures, r)
		}
	}
	return
}
