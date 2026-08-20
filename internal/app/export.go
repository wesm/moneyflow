package app

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/wesm/moneyflow/internal/analytics"
	"github.com/wesm/moneyflow/internal/domain"
)

// ExportDocumentSchemaVersion identifies the lossless renderer-neutral export document.
const ExportDocumentSchemaVersion = 2

// ExportScope selects the complete committed profile or its analytical transaction subset.
type ExportScope string

const (
	// ExportScopeFull includes every committed transaction.
	ExportScopeFull ExportScope = "full"
	// ExportScopeFiltered applies the current analytical transaction predicates.
	ExportScopeFiltered ExportScope = "filtered"
)

// ExportPreview is the bounded count-only state shown before format selection.
type ExportPreview struct {
	Revision           uint64
	FullCount          int
	FilteredCount      int
	ActiveOperations   int
	InactiveOperations int
	CommitAvailable    bool
}

// ExportRequest contains the validated renderer intent and injected document identity.
type ExportRequest struct {
	Scope          ExportScope
	State          ViewState
	CanonicalQuery string
	ExportedAt     time.Time
	AppVersion     string
}

// ExportMetadata records the exact committed frame represented by one document.
type ExportMetadata struct {
	SchemaVersion            int
	AppVersion               string
	ExportedAt               time.Time
	ProfileRevision          uint64
	JournalCursor            int
	ExcludedActiveOperations int
	InactiveRedoOperations   int
	Scope                    ExportScope
	CanonicalQuery           string
	TransactionCount         int
	EarliestDate             *domain.Date
	LatestDate               *domain.Date
	ProviderKinds            []string
}

// ExportRow is one lossless, format-neutral transaction record.
type ExportRow struct {
	TransactionID           string
	Provider                string
	ProviderTransactionID   string
	Date                    domain.Date
	Amount                  string
	AmountMinor             int64
	Currency                string
	Scale                   uint8
	AccountID               string
	Account                 string
	MerchantID              string
	Merchant                string
	CategoryID              string
	Category                string
	GroupID                 string
	Group                   string
	Notes                   string
	Hidden                  bool
	TransactionMetadataJSON string
}

// ExportDocument is detached from the service and safe for output adapters to retain.
type ExportDocument struct {
	Metadata ExportMetadata
	Rows     []ExportRow
}

// PreviewExport returns counts from the latest committed local profile without taking export.lock.
func (service *Service) PreviewExport(
	ctx context.Context,
	state ViewState,
) (ExportPreview, error) {
	if err := state.Validate(); err != nil {
		return ExportPreview{}, newAppError(AppExportInvalid, service.Revision(), err)
	}
	snapshot, providerWrite, err := service.exportSnapshot(ctx)
	if err != nil {
		return ExportPreview{}, err
	}
	committed, err := snapshot.Committed.MaterializeTransactions()
	if err != nil {
		return ExportPreview{}, newAppError(AppExportFailed, snapshot.Revision, err)
	}
	filtered, err := analytics.Filter(committed, analyticalQuerySpec(state.Current))
	if err != nil {
		return ExportPreview{}, newAppError(AppExportInvalid, snapshot.Revision, err)
	}
	return ExportPreview{
		Revision: snapshot.Revision, FullCount: len(committed), FilteredCount: len(filtered),
		ActiveOperations: snapshot.Cursor, InactiveOperations: len(snapshot.Journal) - snapshot.Cursor,
		CommitAvailable: snapshot.Cursor > 0 && !providerWrite,
	}, nil
}

// CaptureExport returns one coherent committed frame at the latest local profile revision.
func (service *Service) CaptureExport(
	ctx context.Context,
	request ExportRequest,
) (ExportDocument, error) {
	if err := validateExportRequest(request); err != nil {
		return ExportDocument{}, newAppError(AppExportInvalid, service.Revision(), err)
	}
	snapshot, _, err := service.exportSnapshot(ctx)
	if err != nil {
		return ExportDocument{}, err
	}
	committed, err := snapshot.Committed.MaterializeTransactions()
	if err != nil {
		return ExportDocument{}, newAppError(AppExportFailed, snapshot.Revision, err)
	}
	selected := committed
	if request.Scope == ExportScopeFiltered {
		selected, err = analytics.Filter(committed, analyticalQuerySpec(request.State.Current))
		if err != nil {
			return ExportDocument{}, newAppError(AppExportInvalid, snapshot.Revision, err)
		}
	}
	if len(selected) == 0 {
		return ExportDocument{}, newAppError(
			AppExportEmpty, snapshot.Revision, errors.New("export contains no committed transactions"),
		)
	}
	rows, providerKinds, earliest, latest, err := exportRows(selected)
	if err != nil {
		return ExportDocument{}, newAppError(AppExportFailed, snapshot.Revision, err)
	}
	return ExportDocument{
		Metadata: ExportMetadata{
			SchemaVersion: ExportDocumentSchemaVersion, AppVersion: request.AppVersion,
			ExportedAt: request.ExportedAt, ProfileRevision: snapshot.Revision,
			JournalCursor: snapshot.Cursor, ExcludedActiveOperations: snapshot.Cursor,
			InactiveRedoOperations: len(snapshot.Journal) - snapshot.Cursor,
			Scope:                  request.Scope, CanonicalQuery: request.CanonicalQuery,
			TransactionCount: len(rows), EarliestDate: earliest, LatestDate: latest,
			ProviderKinds: providerKinds,
		},
		Rows: rows,
	}, nil
}

func (service *Service) exportSnapshot(
	ctx context.Context,
) (EffectiveSnapshot, bool, error) {
	service.interactions.Lock()
	defer service.interactions.Unlock()
	if _, err := service.refreshLocked(ctx); err != nil {
		return EffectiveSnapshot{}, false, err
	}
	snapshot, err := service.effectiveSnapshot()
	if err != nil {
		return EffectiveSnapshot{}, false, mapAppError(err, service.Revision())
	}
	return snapshot, service.providerWriteActive(), nil
}

func validateExportRequest(request ExportRequest) error {
	if request.Scope != ExportScopeFull && request.Scope != ExportScopeFiltered {
		return errors.New("export scope is invalid")
	}
	if err := request.State.Validate(); err != nil {
		return err
	}
	if request.AppVersion == "" {
		return errors.New("export application version is empty")
	}
	if request.ExportedAt.Location() != time.UTC ||
		!request.ExportedAt.Equal(request.ExportedAt.Truncate(time.Millisecond)) {
		return errors.New("export time is not canonical UTC milliseconds")
	}
	if request.Scope == ExportScopeFiltered && request.CanonicalQuery == "" {
		return errors.New("filtered export query is empty")
	}
	if request.Scope == ExportScopeFull && request.CanonicalQuery != "" {
		return errors.New("full export query is not empty")
	}
	return nil
}

func exportRows(
	transactions []domain.Transaction,
) ([]ExportRow, []string, *domain.Date, *domain.Date, error) {
	rows := make([]ExportRow, len(transactions))
	providers := make(map[string]struct{})
	for index, transaction := range transactions {
		metadata := transaction.Metadata
		if metadata == nil {
			metadata = map[string]string{}
		}
		encoded, err := json.Marshal(metadata)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		rows[index] = ExportRow{
			TransactionID: transaction.ID, Provider: transaction.Provider,
			ProviderTransactionID: transaction.ProviderID, Date: transaction.Date,
			Amount: transaction.Amount.DecimalString(), AmountMinor: transaction.Amount.Minor,
			Currency: string(transaction.Amount.Currency), Scale: transaction.Amount.Scale,
			AccountID: transaction.Account.ID, Account: transaction.Account.Name,
			MerchantID: transaction.Merchant.ID, Merchant: transaction.Merchant.Name,
			CategoryID: transaction.Category.ID, Category: transaction.Category.Name,
			GroupID: transaction.Category.GroupID, Group: transaction.Category.Group,
			Notes: transaction.Notes, Hidden: transaction.Hidden,
			TransactionMetadataJSON: string(encoded),
		}
		providers[transaction.Provider] = struct{}{}
	}
	sort.Slice(rows, func(left, right int) bool {
		comparison := rows[left].Date.Compare(rows[right].Date)
		if comparison != 0 {
			return comparison > 0
		}
		return rows[left].TransactionID < rows[right].TransactionID
	})
	providerKinds := make([]string, 0, len(providers))
	for providerKind := range providers {
		providerKinds = append(providerKinds, providerKind)
	}
	sort.Strings(providerKinds)
	earliest := rows[0].Date
	latest := rows[0].Date
	for index := 1; index < len(rows); index++ {
		if rows[index].Date.Compare(earliest) < 0 {
			earliest = rows[index].Date
		}
		if rows[index].Date.Compare(latest) > 0 {
			latest = rows[index].Date
		}
	}
	return rows, providerKinds, &earliest, &latest, nil
}
