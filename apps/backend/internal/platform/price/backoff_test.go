// apps/backend/internal/platform/price/backoff_test.go
package price

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBackoff_Schedule(t *testing.T) {
	tests := []struct {
		attempt int
		want    time.Duration
	}{
		{1, 15 * time.Minute},
		{2, 1 * time.Hour},
		{3, 6 * time.Hour},
		{4, 24 * time.Hour},
		{5, 24 * time.Hour},
		{10, 24 * time.Hour},
	}
	for _, tt := range tests {
		got := BackoffDelay(tt.attempt)
		require.Equal(t, tt.want, got, "attempt %d", tt.attempt)
	}
}

func TestBackoff_IsTerminal(t *testing.T) {
	require.False(t, IsTerminalAttempt(10))
	require.True(t, IsTerminalAttempt(11))
	require.True(t, IsTerminalAttempt(99))
}
