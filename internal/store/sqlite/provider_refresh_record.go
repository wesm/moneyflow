package sqlite

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/wesm/moneyflow/internal/home"
)

// providerRefreshRecordName sits beside moneyflow.db. It is advisory scheduling state, not
// profile data: it is outside the schema and the JSONL transfer, and losing it only makes
// the next complete-history refresh due.
const providerRefreshRecordName = "provider-refresh.json"

const maxProviderRefreshRecordBytes = 1 << 10

type providerRefreshRecord struct {
	LastFullRefresh time.Time `json:"last_full_refresh"`
}

// LastFullProviderRefresh returns the recorded time, or zero when it is missing or unreadable.
func (profile *profile) LastFullProviderRefresh() time.Time {
	if profile.recordPath == "" {
		return time.Time{}
	}
	contents, err := home.ReadPrivateFile(profile.recordPath, maxProviderRefreshRecordBytes)
	if err != nil {
		return time.Time{}
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	var record providerRefreshRecord
	if err = decoder.Decode(&record); err != nil {
		return time.Time{}
	}
	return record.LastFullRefresh.UTC()
}

// RecordFullProviderRefresh atomically replaces the owner-only advisory record.
func (profile *profile) RecordFullProviderRefresh(at time.Time) error {
	if profile.recordPath == "" {
		return errors.New("record full provider refresh: profile has no folder")
	}
	contents, err := json.Marshal(providerRefreshRecord{LastFullRefresh: at.UTC()})
	if err != nil {
		return fmt.Errorf("record full provider refresh: encode: %w", err)
	}
	if err = home.WritePrivateFile(profile.recordPath, append(contents, '\n')); err != nil {
		return fmt.Errorf("record full provider refresh in %s: %w", profile.recordPath, err)
	}
	return nil
}
