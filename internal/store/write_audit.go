package store

import "time"

// ProviderWriteOutcomeAudit contains an observed attempt result. Code and Reason
// must come from the provider's allowlisted classifications, never error text.
// Response contains decoded accounting fields only, never a raw provider body.
type ProviderWriteOutcomeAudit struct {
	BatchID    string
	ItemID     string
	Attempt    int
	ObservedAt time.Time
	Failed     bool
	Code       string
	Reason     string
	Response   *WriteResult
}
