package inbox

import (
	"testing"
	"time"
)

func TestRetryBackoff(t *testing.T) {
	cases := map[int64]time.Duration{
		-1: 500 * time.Millisecond,
		0:  500 * time.Millisecond,
		1:  time.Second,
		2:  2 * time.Second,
		3:  4 * time.Second,
		4:  8 * time.Second,
		5:  RetryBackoffMax,
		40: RetryBackoffMax,
	}
	for n, want := range cases {
		if got := RetryBackoff(n); got != want {
			t.Errorf("RetryBackoff(%d) = %v, muốn %v", n, got, want)
		}
	}
}
