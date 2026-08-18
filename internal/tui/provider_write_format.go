package tui

import (
	"fmt"
	"time"
)

func formatProviderWriteRemaining(remaining time.Duration) string {
	remaining = max(0, remaining.Round(time.Second))
	if remaining < time.Minute {
		return fmt.Sprintf("about %ds remaining", int(remaining/time.Second))
	}
	if remaining < time.Hour {
		return fmt.Sprintf("about %dm remaining", int(remaining/time.Minute))
	}
	hours := int(remaining / time.Hour)
	minutes := int((remaining % time.Hour).Round(time.Minute) / time.Minute)
	if minutes == 60 {
		hours++
		minutes = 0
	}
	if minutes == 0 {
		return fmt.Sprintf("about %dh remaining", hours)
	}
	return fmt.Sprintf("about %dh %dm remaining", hours, minutes)
}
