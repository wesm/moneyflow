package tui

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestFormatProviderWriteRemaining(t *testing.T) {
	t.Parallel()

	tests := []struct {
		remaining time.Duration
		want      string
	}{
		{remaining: 42 * time.Second, want: "about 42s remaining"},
		{remaining: 17 * time.Minute, want: "about 17m remaining"},
		{remaining: 3*time.Hour + 20*time.Minute, want: "about 3h 20m remaining"},
	}
	for _, test := range tests {
		assert.Equal(t, test.want, formatProviderWriteRemaining(test.remaining))
	}
}
