package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json/v2"
	"errors"
	"reflect"
	"time"

	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/store"
)

// Intent and acknowledgment records describe observations, not SQLite commits.
// Only the separate *_committed/finalized/reconciled marker confirms that commit.
// A missing marker leaves its outcome unconfirmed; it never authorizes a resend.
type writeAuditEvent struct {
	Version             int                  `json:"version"`
	Event               string               `json:"event"`
	Time                time.Time            `json:"time"`
	BatchID             string               `json:"batch_id,omitempty"`
	ItemID              string               `json:"item_id,omitempty"`
	OperationIDs        []string             `json:"operation_ids,omitempty"`
	Operations          []domain.Operation   `json:"operations,omitempty"`
	Revision            uint64               `json:"revision,omitzero"`
	ResultingRevision   uint64               `json:"resulting_revision,omitzero"`
	Attempt             int                  `json:"attempt,omitzero"`
	Provider            string               `json:"provider,omitempty"`
	RemoteProfileID     string               `json:"remote_profile_id,omitempty"`
	Renderer            string               `json:"renderer,omitempty"`
	TransactionID       domain.EntityID      `json:"transaction_id,omitempty"`
	TransactionExternal string               `json:"transaction_external_id,omitempty"`
	TransactionDate     string               `json:"transaction_date,omitempty"`
	Kind                store.WriteItemKind  `json:"kind,omitempty"`
	Before              *auditValues         `json:"before,omitempty"`
	Requested           *auditValues         `json:"requested,omitempty"`
	Acknowledged        *auditAcknowledgment `json:"acknowledged,omitempty"`
	Reconciled          *auditValues         `json:"reconciled,omitempty"`
	EntityID            domain.EntityID      `json:"entity_id,omitempty"`
	EntityType          domain.EntityKind    `json:"entity_type,omitempty"`
	BeforeEntity        *auditEntityValues   `json:"before_entity,omitempty"`
	RequestedEntity     *auditEntityValues   `json:"requested_entity,omitempty"`
	Outcome             string               `json:"outcome,omitempty"`
	ErrorCode           string               `json:"error_code,omitempty"`
	Reason              string               `json:"reason,omitempty"`
}

type auditEntityValues struct {
	Label            string           `json:"label"`
	GroupID          domain.EntityID  `json:"group_id,omitempty"`
	Retired          bool             `json:"retired"`
	MergeDestination *domain.EntityID `json:"merge_destination,omitempty"`
}

type auditValues struct {
	MerchantID         domain.EntityID `json:"merchant_id,omitempty"`
	MerchantName       string          `json:"merchant_name,omitempty"`
	MerchantExternalID string          `json:"merchant_external_id,omitempty"`
	CategoryID         domain.EntityID `json:"category_id,omitempty"`
	CategoryName       string          `json:"category_name,omitempty"`
	CategoryExternalID *string         `json:"category_external_id,omitzero"`
	Hidden             *bool           `json:"hidden,omitzero"`
	Deleted            *bool           `json:"deleted,omitzero"`
}

type auditAcknowledgment struct {
	TransactionExternalID string  `json:"transaction_external_id"`
	MerchantExternalID    *string `json:"merchant_external_id,omitzero"`
	MerchantName          *string `json:"merchant_name,omitzero"`
	CategoryExternalID    *string `json:"category_external_id,omitzero"`
	CategoryCleared       bool    `json:"category_cleared,omitzero"`
	Hidden                *bool   `json:"hidden,omitzero"`
	Deleted               bool    `json:"deleted,omitzero"`
	AlreadyAbsent         bool    `json:"already_absent,omitzero"`
	OverrideCount         int     `json:"override_count,omitzero"`
}

// appendAudit runs only while the caller holds BEGIN IMMEDIATE. This serializes
// the file across profile handles and processes using the existing SQLite lock.
func (profile *profile) appendAudit(events []writeAuditEvent) error {
	var contents bytes.Buffer
	for _, event := range events {
		event.Version = 1
		event.Time = event.Time.UTC()
		line, err := json.Marshal(event)
		if err != nil {
			return store.NewError(store.CodeStoreError, err)
		}
		contents.Write(line)
		contents.WriteByte('\n')
	}
	if err := home.AppendPrivateJSONL(profile.auditPath, contents.Bytes()); err != nil {
		return store.NewError(store.CodeStoreError, err)
	}
	return nil
}

// recordAudit acquires the same lock for observations made outside a store
// mutation, including completion markers written after a successful SQL commit.
func (profile *profile) recordAudit(ctx context.Context, events []writeAuditEvent) error {
	_, finish, err := profile.beginImmediate(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = finish(false) }()
	if err = profile.appendAudit(events); err != nil {
		return err
	}
	return finish(true)
}

// RecordProviderWriteOutcomes retains each returned attempt, including failures
// and responses rejected by validation. It never changes retry eligibility.
func (profile *profile) RecordProviderWriteOutcomes(ctx context.Context, observations []store.ProviderWriteOutcomeAudit) error {
	events := make([]writeAuditEvent, 0, len(observations))
	for _, observation := range observations {
		if observation.BatchID == "" || observation.ItemID == "" || observation.Attempt < 1 || observation.ObservedAt.IsZero() {
			return store.NewError(store.CodeInvalidOperation, errors.New("audit outcome is incomplete"))
		}
		event := writeAuditEvent{
			Event: "provider_response", Time: observation.ObservedAt,
			BatchID: observation.BatchID, ItemID: observation.ItemID, Attempt: observation.Attempt,
			Outcome: "received", ErrorCode: observation.Code, Reason: observation.Reason,
			Acknowledged: auditResponse(observation.Response),
		}
		if observation.Failed {
			event.Outcome = "failed"
		}
		switch observation.ReadbackDisposition {
		case "":
		case "matches_requested":
			event.Event, event.Outcome = "provider_read_observed", "matches_requested"
		case "matches_before":
			event.Event, event.Outcome = "provider_read_retry_authorized", "matches_before"
		case "unresolved":
			event.Event, event.Outcome = "provider_read_unresolved", "unresolved"
		default:
			return store.NewError(store.CodeInvalidOperation, errors.New("audit readback disposition is invalid"))
		}
		events = append(events, event)
	}
	return profile.recordAudit(ctx, events)
}

func auditResponse(result *store.WriteResult) *auditAcknowledgment {
	if result == nil {
		return nil
	}
	return &auditAcknowledgment{
		TransactionExternalID: result.TransactionExternalID,
		MerchantExternalID:    result.MerchantExternalID, MerchantName: result.MerchantLabel,
		CategoryExternalID: result.CategoryExternalID, CategoryCleared: result.CategoryCleared,
		Hidden: result.Hidden, Deleted: result.Kind == store.WriteItemDelete,
		AlreadyAbsent: result.AlreadyAbsent, OverrideCount: result.OverrideCount,
	}
}

type auditProfileIndex struct {
	transactions         map[domain.EntityID]domain.TransactionRecord
	merchants            map[domain.EntityID]string
	categories           map[domain.EntityID]string
	external             map[string]map[domain.EntityID]string
	categoriesByExternal map[string]map[string]domain.EntityID
}

func newAuditProfileIndex(committed domain.CommittedProfile) auditProfileIndex {
	index := auditProfileIndex{
		transactions:         make(map[domain.EntityID]domain.TransactionRecord, len(committed.Transactions)),
		merchants:            make(map[domain.EntityID]string, len(committed.Merchants)),
		categories:           make(map[domain.EntityID]string, len(committed.Categories)),
		external:             make(map[string]map[domain.EntityID]string),
		categoriesByExternal: make(map[string]map[string]domain.EntityID),
	}
	for _, value := range committed.Transactions {
		index.transactions[value.ID] = value
	}
	for _, value := range committed.Merchants {
		index.merchants[value.ID] = value.Label
	}
	for _, value := range committed.Categories {
		index.categories[value.ID] = value.Label
	}
	for _, value := range committed.ExternalIdentities {
		if index.external[value.Namespace] == nil {
			index.external[value.Namespace] = make(map[domain.EntityID]string)
		}
		index.external[value.Namespace][value.EntityID] = value.ExternalID
		if value.EntityType == domain.EntityKindCategory {
			if index.categoriesByExternal[value.Namespace] == nil {
				index.categoriesByExternal[value.Namespace] = make(map[string]domain.EntityID)
			}
			index.categoriesByExternal[value.Namespace][value.ExternalID] = value.EntityID
		}
	}
	return index
}

func (index auditProfileIndex) values(transactionID domain.EntityID) *auditValues {
	transaction, exists := index.transactions[transactionID]
	if !exists {
		return &auditValues{Deleted: new(true)}
	}
	return &auditValues{
		MerchantID: transaction.MerchantID, MerchantName: index.merchants[transaction.MerchantID],
		MerchantExternalID: index.external[transaction.Provider+"/merchant"][transaction.MerchantID],
		CategoryID:         transaction.CategoryID, CategoryName: index.categories[transaction.CategoryID],
		CategoryExternalID: new(index.external[transaction.Provider+"/category"][transaction.CategoryID]),
		Hidden:             new(transaction.Hidden), Deleted: new(false),
	}
}

func providerAuditEvents(
	event string,
	snapshot domain.ProfileSnapshot,
	binding *store.ProviderBinding,
	batch store.WriteBatch,
	items []store.WriteItem,
	results []store.WriteResult,
	now time.Time,
) []writeAuditEvent {
	index := newAuditProfileIndex(snapshot.Committed)
	resultByItem := make(map[string]store.WriteResult, len(results))
	for _, result := range results {
		resultByItem[result.ItemID] = result
	}
	events := make([]writeAuditEvent, 0, len(items))
	for _, item := range items {
		transaction := index.transactions[item.TransactionID]
		providerKind := transaction.Provider
		if binding != nil {
			providerKind = binding.Kind
		}
		requested := &auditValues{
			MerchantID:         item.RequestedMerchantLocalID,
			MerchantExternalID: item.ExpectedMerchantExternalID,
			CategoryExternalID: item.RequestedCategoryExternalID,
			Hidden:             item.RequestedHidden,
		}
		if item.RequestedMerchantName != nil {
			requested.MerchantName = *item.RequestedMerchantName
		}
		if item.ClearCategory {
			requested.CategoryID = domain.UncategorizedCategoryID
			requested.CategoryExternalID = new("")
			requested.CategoryName = domain.UncategorizedLabel
		}
		if item.RequestedCategoryExternalID != nil {
			id := index.categoriesByExternal[providerKind+"/category"][*item.RequestedCategoryExternalID]
			requested.CategoryID, requested.CategoryName = id, index.categories[id]
		}
		if item.Kind == store.WriteItemDelete {
			requested.Deleted = new(true)
		}
		entry := writeAuditEvent{
			Event: event, Time: now, BatchID: batch.ID, ItemID: item.ID,
			OperationIDs: item.OriginatingOperationIDs, Revision: snapshot.Revision,
			Attempt: item.AttemptCount, TransactionID: item.TransactionID,
			TransactionExternal: item.TransactionExternalID, Kind: item.Kind,
			TransactionDate: transaction.Date.String(),
			Before:          index.values(item.TransactionID), Requested: requested,
			Provider: providerKind,
		}
		if binding != nil {
			entry.Provider, entry.RemoteProfileID = binding.Kind, binding.RemoteProfileID
		}
		if result, ok := resultByItem[item.ID]; ok {
			entry.Acknowledged = auditResponse(&result)
		}
		events = append(events, entry)
	}
	return events
}

func localAuditEvents(snapshot domain.ProfileSnapshot, plan store.FoldPlan, now time.Time) []writeAuditEvent {
	before, after := newAuditProfileIndex(snapshot.Committed), newAuditProfileIndex(plan.Effective)
	events := make([]writeAuditEvent, 0)
	for _, transaction := range snapshot.Committed.Transactions {
		previous, requested := before.values(transaction.ID), after.values(transaction.ID)
		if reflect.DeepEqual(previous, requested) {
			continue
		}
		events = append(events, writeAuditEvent{
			Event: "local_commit_intent", Time: now, Revision: snapshot.Revision,
			OperationIDs: plan.ActiveOperationIDs, TransactionID: transaction.ID,
			TransactionExternal: transaction.ProviderID, Provider: transaction.Provider,
			TransactionDate: transaction.Date.String(),
			Before:          previous, Requested: requested,
		})
	}
	events = append(events, localEntityAuditEvents(snapshot.Committed.Merchants, plan.Effective.Merchants, domain.EntityKindMerchant, snapshot.Revision, plan.ActiveOperationIDs, now)...)
	events = append(events, localEntityAuditEvents(snapshot.Committed.Categories, plan.Effective.Categories, domain.EntityKindCategory, snapshot.Revision, plan.ActiveOperationIDs, now)...)
	events = append(events, localEntityAuditEvents(snapshot.Committed.Groups, plan.Effective.Groups, domain.EntityKindGroup, snapshot.Revision, plan.ActiveOperationIDs, now)...)
	if len(events) == 0 {
		events = append(events, writeAuditEvent{
			Event: "local_commit_intent", Time: now, Revision: snapshot.Revision,
			OperationIDs: plan.ActiveOperationIDs,
		})
	}
	// Preserve taxonomy-only operations and exact resolved targets once per commit.
	events[0].Operations = snapshot.Journal[:snapshot.Cursor]
	return events
}

func localEntityAuditEvents[T foldEntity](before, after []T, kind domain.EntityKind, revision uint64, operationIDs []string, now time.Time) []writeAuditEvent {
	previous := make(map[domain.EntityID]*auditEntityValues, len(before))
	for _, entity := range before {
		previous[foldEntityID(entity)] = auditEntity(entity)
	}
	var events []writeAuditEvent
	for _, entity := range after {
		id, requested := foldEntityID(entity), auditEntity(entity)
		if reflect.DeepEqual(previous[id], requested) {
			continue
		}
		events = append(events, writeAuditEvent{
			Event: "local_commit_intent", Time: now, Revision: revision, OperationIDs: operationIDs,
			EntityID: id, EntityType: kind, BeforeEntity: previous[id], RequestedEntity: requested,
		})
	}
	return events
}

func auditEntity[T foldEntity](entity T) *auditEntityValues {
	switch value := any(entity).(type) {
	case domain.Merchant:
		return &auditEntityValues{Label: value.Label, Retired: value.Retired, MergeDestination: value.MergeDestination}
	case domain.Category:
		return &auditEntityValues{Label: value.Label, GroupID: value.GroupID, Retired: value.Retired, MergeDestination: value.MergeDestination}
	case domain.CategoryGroup:
		return &auditEntityValues{Label: value.Label, Retired: value.Retired, MergeDestination: value.MergeDestination}
	}
	return nil
}

// loadClaimAuditSnapshot reads only the at most four claimed transactions and
// their referenced labels/identities. It does not load/replay a whole profile.
func loadClaimAuditSnapshot(ctx context.Context, connection *sql.Conn, items []store.WriteItem) (domain.ProfileSnapshot, error) {
	var snapshot domain.ProfileSnapshot
	if err := connection.QueryRowContext(ctx, "SELECT revision FROM profile_state WHERE singleton = 1").Scan(&snapshot.Revision); err != nil {
		return snapshot, mapDriverError(err, store.CodeStoreError)
	}
	for _, item := range items {
		var transaction domain.TransactionRecord
		var merchantName, categoryName, date, merchantExternal, categoryExternal string
		if err := connection.QueryRowContext(ctx, `
			SELECT t.id, t.provider, t.provider_id, t.merchant_id, t.category_id, t.transaction_date, t.hidden,
				m.label, c.label,
				COALESCE(me.external_id, ''), COALESCE(ce.external_id, '')
			FROM transactions t JOIN merchants m ON m.id = t.merchant_id JOIN categories c ON c.id = t.category_id
			LEFT JOIN external_identities me ON me.entity_id = t.merchant_id AND me.namespace = t.provider || '/merchant'
			LEFT JOIN external_identities ce ON ce.entity_id = t.category_id AND ce.namespace = t.provider || '/category'
			WHERE t.id = ?`, item.TransactionID).Scan(&transaction.ID, &transaction.Provider,
			&transaction.ProviderID, &transaction.MerchantID, &transaction.CategoryID, &date, &transaction.Hidden,
			&merchantName, &categoryName, &merchantExternal, &categoryExternal); err != nil {
			return snapshot, mapDriverError(err, store.CodeStoreError)
		}
		var err error
		transaction.Date, err = domain.ParseDate(date)
		if err != nil {
			return snapshot, store.NewError(store.CodeStoreCorrupt, err)
		}
		snapshot.Committed.Transactions = append(snapshot.Committed.Transactions, transaction)
		snapshot.Committed.Merchants = append(snapshot.Committed.Merchants, domain.Merchant{ID: transaction.MerchantID, Label: merchantName})
		snapshot.Committed.Categories = append(snapshot.Committed.Categories, domain.Category{ID: transaction.CategoryID, Label: categoryName})
		snapshot.Committed.ExternalIdentities = append(snapshot.Committed.ExternalIdentities,
			domain.ExternalIdentity{EntityType: domain.EntityKindMerchant, EntityID: transaction.MerchantID, Namespace: transaction.Provider + "/merchant", ExternalID: merchantExternal},
			domain.ExternalIdentity{EntityType: domain.EntityKindCategory, EntityID: transaction.CategoryID, Namespace: transaction.Provider + "/category", ExternalID: categoryExternal})
		if item.RequestedCategoryExternalID != nil {
			var category domain.Category
			err = connection.QueryRowContext(ctx, `SELECT c.id, c.label FROM categories c
				JOIN external_identities e ON e.entity_id = c.id
				WHERE e.namespace = ? AND e.external_id = ?`, transaction.Provider+"/category", *item.RequestedCategoryExternalID).
				Scan(&category.ID, &category.Label)
			if err != nil {
				return snapshot, mapDriverError(err, store.CodeStoreError)
			}
			snapshot.Committed.Categories = append(snapshot.Committed.Categories, category)
			snapshot.Committed.ExternalIdentities = append(snapshot.Committed.ExternalIdentities, domain.ExternalIdentity{
				EntityType: domain.EntityKindCategory, EntityID: category.ID, Namespace: transaction.Provider + "/category", ExternalID: *item.RequestedCategoryExternalID,
			})
		}
	}
	return snapshot, nil
}
