package storage

import (
	"testing"
	"time"
)

func TestRetryBackoffDuration(t *testing.T) {
	tests := []struct {
		name       string
		retryCount int
		backoffs   []int
		want       time.Duration
	}{
		{name: "empty backoff", retryCount: 0, backoffs: nil, want: 0},
		{name: "first retry", retryCount: 0, backoffs: []int{30, 120, 300}, want: 30 * time.Second},
		{name: "second retry", retryCount: 1, backoffs: []int{30, 120, 300}, want: 120 * time.Second},
		{name: "clamps to last value", retryCount: 5, backoffs: []int{30, 120, 300}, want: 300 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := retryBackoffDuration(tt.retryCount, tt.backoffs); got != tt.want {
				t.Fatalf("retryBackoffDuration() = %v, want %v", got, tt.want)
			}
		})
	}
}
