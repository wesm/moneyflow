package runtime

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/uitest"
)

// Row is committed data keyed by the stable synthetic provider identity.
type Row struct {
	Date     string `json:"date"`
	Merchant string `json:"merchant"`
	Category string `json:"category"`
	Minor    int64  `json:"minor"`
	Hidden   bool   `json:"hidden"`
}

// Observation keeps committed data, pending intent, and provider effects distinguishable.
type Observation struct {
	Rows          map[string]Row          `json:"rows"`
	Targets       []string                `json:"targets"`
	Pending       int                     `json:"pending"`
	Redo          int                     `json:"redo"`
	SnapshotCalls int                     `json:"snapshot_calls"`
	Calls         []uitest.Call           `json:"calls"`
	Write         app.ProviderWriteStatus `json:"write"`
	Audit         string                  `json:"audit"`
}

// Observe reads persisted committed rows and the current public pending/write projections.
func (run *Runtime) Observe(ctx context.Context) (Observation, error) {
	// A worker may finalize between the snapshot and review reads. Retry only
	// that version conflict; a checkpoint must never mix two revisions.
	for range 3 {
		observation, err := run.observeOnce(ctx)
		var conflict *app.AppError
		if !errors.As(err, &conflict) || conflict.Code != app.AppRevisionConflict {
			return observation, err
		}
	}
	return Observation{}, errors.New("scenario observation did not stabilize")
}

func (run *Runtime) observeOnce(ctx context.Context) (Observation, error) {
	snapshot, err := run.Profile.Load(ctx)
	if err != nil {
		return Observation{}, err
	}
	transactions, err := snapshot.Committed.MaterializeTransactions()
	if err != nil {
		return Observation{}, err
	}
	observation := Observation{Rows: make(map[string]Row), Targets: []string{}}
	identities := make(map[domain.EntityID]string)
	for _, transaction := range transactions {
		identities[domain.EntityID(transaction.ID)] = transaction.ProviderID
		observation.Rows[transaction.ProviderID] = Row{Date: transaction.Date.String(), Merchant: transaction.Merchant.Name, Category: transaction.Category.Name, Minor: transaction.Amount.Minor, Hidden: transaction.Hidden}
	}
	review, err := run.Service.Review(ctx, snapshot.Revision, app.ReviewWindow{})
	if err != nil {
		return Observation{}, err
	}
	observation.Pending = review.Pending.ActiveOperations
	observation.Redo = review.Pending.InactiveOperations
	targets := make(map[string]struct{})
	for _, operation := range review.ActiveOperations {
		detail, reviewErr := run.Service.Review(ctx, snapshot.Revision, app.ReviewWindow{OperationID: operation.OperationID, Limit: app.MaxReviewTargetLimit})
		if reviewErr != nil {
			return Observation{}, reviewErr
		}
		for _, target := range detail.Targets {
			targets[identities[target.TransactionID]] = struct{}{}
		}
	}
	observation.Targets = slices.AppendSeq([]string{}, maps.Keys(targets))
	slices.Sort(observation.Targets)
	observation.Calls = run.Provider.State().Calls
	for _, call := range observation.Calls {
		if call.Method == "snapshot" {
			observation.SnapshotCalls++
		}
	}
	observation.Write, err = run.Service.ProviderWriteStatus(ctx)
	if err != nil {
		return Observation{}, err
	}
	audit, err := home.ReadPrivateFile(filepath.Join(run.Paths.Root, "audit.jsonl"), 16<<20)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return Observation{}, err
	}
	observation.Audit = string(audit)
	return observation, nil
}

// CheckTargets compares literal fixture identities, independently of target resolution.
func CheckTargets(observation Observation, expected []string) error {
	expected = slices.Clone(expected)
	slices.Sort(expected)
	if !slices.Equal(observation.Targets, expected) {
		return fmt.Errorf("edit scope: expected %v, got %v", expected, observation.Targets)
	}
	return nil
}
