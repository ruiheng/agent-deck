package ui

import (
	"testing"
	"time"

	"github.com/asheshgoplani/agent-deck/internal/session"
)

func TestShouldSkipQuietStatusSweep(t *testing.T) {
	now := time.Unix(100, 0)
	quiet := now.Add(-quietStatusWindow - time.Second)
	recent := now.Add(-quietStatusWindow + time.Second)

	tests := []struct {
		name       string
		status     session.Status
		lastOutput time.Time
		want       bool
	}{
		{name: "running remains eligible for refresh", status: session.StatusRunning, lastOutput: quiet, want: false},
		{name: "starting remains eligible for refresh", status: session.StatusStarting, lastOutput: quiet, want: false},
		{name: "quiet waiting can be skipped", status: session.StatusWaiting, lastOutput: quiet, want: true},
		{name: "quiet idle can be skipped", status: session.StatusIdle, lastOutput: quiet, want: true},
		{name: "recent idle remains eligible", status: session.StatusIdle, lastOutput: recent, want: false},
		{name: "missing output timestamp remains eligible", status: session.StatusIdle, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldSkipQuietStatusSweep(tt.status, tt.lastOutput, now); got != tt.want {
				t.Fatalf("shouldSkipQuietStatusSweep(%q, %v) = %v, want %v", tt.status, tt.lastOutput, got, tt.want)
			}
		})
	}
}
