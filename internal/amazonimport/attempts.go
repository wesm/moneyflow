package amazonimport

import (
	"context"
	"encoding/base32"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/importer/amazon"
)

const attemptIdleLimit = 30 * time.Minute

type attempt struct {
	id              string
	profileID       string
	root            string
	target          Target
	state           State
	version         uint64
	lastActivity    time.Time
	running         bool
	cancel          context.CancelFunc
	cancelRequested bool
	settings        amazon.Settings
	taxonomyClone   *app.TaxonomyClone
	progress        Progress
	result          app.AmazonImportResult
	failure         Failure
	files           []amazon.SourceFile
	stageDir        string
	lock            *home.Lock
}

func (value *attempt) snapshot() Snapshot {
	return Snapshot{
		ProtocolVersion: ProtocolVersion, AttemptID: value.id, ProfileID: value.profileID,
		State: value.state, StateVersion: value.version, Progress: value.progress,
		Result: value.result, Failure: value.failure,
	}
}

func (coordinator *Coordinator) attempt(profileID, attemptID string) (*attempt, error) {
	value := coordinator.attempts[profileID]
	if value == nil || value.id != attemptID {
		return nil, newError(CodeAttemptInvalid, errors.New("attempt identity does not match"))
	}
	if !value.running && coordinator.now().Sub(value.lastActivity) >= attemptIdleLimit {
		coordinator.cleanupAttempt(value)
		delete(coordinator.attempts, profileID)
		return nil, newError(CodeAttemptInvalid, errors.New("attempt expired"))
	}
	value.lastActivity = coordinator.now()
	return value, nil
}

func checkVersion(value *attempt, expected uint64) error {
	if value.version != expected {
		return newError(CodeAttemptStale, errors.New("attempt version changed"))
	}
	return nil
}

func newAttemptID(randomReader io.Reader) (string, error) {
	buffer := make([]byte, 16)
	if _, err := io.ReadFull(randomReader, buffer); err != nil {
		return "", err
	}
	return strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(buffer)), nil
}
