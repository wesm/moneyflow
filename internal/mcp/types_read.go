//nolint:revive // Exported names document the stable MCP JSON wire contract in one place.
package mcp

import (
	"strconv"
	"time"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
)

type EmptyInput struct{}

type AccountInfoInput struct {
	PartitionOffset int  `json:"partition_offset,omitempty"`
	PartitionLimit  *int `json:"partition_limit,omitempty"`
}

type SearchTransactionsInput struct {
	Query  string `json:"query" jsonschema:"literal text to find in merchant, category, or notes"`
	Offset int    `json:"offset,omitempty" jsonschema:"zero-based result offset"`
	Limit  *int   `json:"limit,omitempty" jsonschema:"result count, from 1 through 1000"`
}

type GetTransactionsInput struct {
	StartDate     string `json:"start_date,omitempty"`
	EndDate       string `json:"end_date,omitempty"`
	CategoryID    string `json:"category_id,omitempty"`
	CategoryLabel string `json:"category_label,omitempty"`
	Merchant      string `json:"merchant,omitempty"`
	MinAmount     string `json:"min_amount,omitempty"`
	MaxAmount     string `json:"max_amount,omitempty"`
	Currency      string `json:"currency,omitempty"`
	Scale         *uint8 `json:"scale,omitempty"`
	IncludeHidden *bool  `json:"include_hidden,omitempty"`
	Offset        int    `json:"offset,omitempty"`
	Limit         *int   `json:"limit,omitempty"`
}

type SpendingSummaryInput struct {
	StartDate string `json:"start_date,omitempty"`
	EndDate   string `json:"end_date,omitempty"`
	GroupBy   string `json:"group_by,omitempty"`
	Offset    int    `json:"offset,omitempty"`
	Limit     *int   `json:"limit,omitempty"`
}

type CategoriesInput struct {
	GroupOffset    int  `json:"group_offset,omitempty"`
	GroupLimit     *int `json:"group_limit,omitempty"`
	CategoryOffset int  `json:"category_offset,omitempty"`
	CategoryLimit  *int `json:"category_limit,omitempty"`
}

type WindowInput struct {
	Offset int  `json:"offset,omitempty"`
	Limit  *int `json:"limit,omitempty"`
}

type UncategorizedInput struct {
	Merchant string `json:"merchant,omitempty"`
	Offset   int    `json:"offset,omitempty"`
	Limit    *int   `json:"limit,omitempty"`
}

type TransactionDetailsInput struct {
	TransactionID string `json:"transaction_id"`
	MatchOffset   int    `json:"match_offset,omitempty"`
	MatchLimit    *int   `json:"match_limit,omitempty"`
	ItemOffset    int    `json:"item_offset,omitempty"`
	ItemLimit     *int   `json:"item_limit,omitempty"`
}

type ReviewChangesInput struct {
	ExpectedRevision string `json:"expected_revision"`
	OperationID      string `json:"operation_id,omitempty"`
	OperationOffset  int    `json:"operation_offset,omitempty"`
	OperationLimit   *int   `json:"operation_limit,omitempty"`
	TargetOffset     int    `json:"target_offset,omitempty"`
	TargetLimit      *int   `json:"target_limit,omitempty"`
}

type RefreshStatusInput struct {
	AttemptID string `json:"attempt_id,omitempty"`
}

type ConfirmRefreshInput struct {
	AttemptID         string `json:"attempt_id"`
	ConfirmationToken string `json:"confirmation_token"`
}

type TransactionDocument struct {
	ID       string         `json:"id"`
	Date     string         `json:"date"`
	Account  Entity         `json:"account"`
	Merchant Entity         `json:"merchant"`
	Category CategoryEntity `json:"category"`
	Money    Money          `json:"money"`
	Notes    string         `json:"notes,omitempty"`
	Hidden   bool           `json:"hidden"`
	Pending  bool           `json:"pending"`
}

type Entity struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type CategoryEntity struct {
	ID         string `json:"id"`
	Label      string `json:"label"`
	GroupID    string `json:"group_id"`
	GroupLabel string `json:"group_label"`
}

type TransactionWindowDocument struct {
	Header
	Total        int                   `json:"total"`
	Offset       int                   `json:"offset"`
	Limit        int                   `json:"limit"`
	Returned     int                   `json:"returned"`
	Transactions []TransactionDocument `json:"transactions"`
	Pending      PendingDocument       `json:"pending"`
}

type PendingDocument struct {
	ActiveOperations     int `json:"active_operations"`
	InactiveOperations   int `json:"inactive_operations"`
	AffectedTransactions int `json:"affected_transactions"`
}

type CollectionWindow struct {
	Total    int `json:"total"`
	Offset   int `json:"offset"`
	Limit    int `json:"limit"`
	Returned int `json:"returned"`
}

type GroupDocument struct {
	ID               string `json:"id"`
	Label            string `json:"label"`
	TransactionCount int    `json:"transaction_count"`
}

type CategoryDocument struct {
	ID               string `json:"id"`
	Label            string `json:"label"`
	GroupID          string `json:"group_id"`
	Protected        bool   `json:"protected"`
	TransactionCount int    `json:"transaction_count"`
}

type MerchantDocument struct {
	ID               string  `json:"id"`
	Label            string  `json:"label"`
	TransactionCount int     `json:"transaction_count"`
	Totals           []Money `json:"totals"`
}

type CategoriesDocument struct {
	Header
	GroupWindow    CollectionWindow   `json:"group_window"`
	Groups         []GroupDocument    `json:"groups"`
	CategoryWindow CollectionWindow   `json:"category_window"`
	Categories     []CategoryDocument `json:"categories"`
}

type MerchantsDocument struct {
	Header
	Window    CollectionWindow   `json:"window"`
	Merchants []MerchantDocument `json:"merchants"`
}

type MoneyPartitionDocument struct {
	Currency         string `json:"currency"`
	Scale            uint8  `json:"scale"`
	TransactionCount int    `json:"transaction_count"`
}

type DateRangeDocument struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

type CapabilityDocument struct {
	Action    string `json:"action"`
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}

type ProviderStatusDocument struct {
	Code         string `json:"code,omitempty"`
	Generation   string `json:"generation"`
	LastSuccess  string `json:"last_success,omitempty"`
	NextEligible string `json:"next_eligible,omitempty"`
	Fetched      int    `json:"fetched"`
	Total        int    `json:"total"`
}

type WriteStatusDocument struct {
	Phase      string `json:"phase,omitempty"`
	Version    string `json:"version"`
	Generation string `json:"generation"`
	Total      int    `json:"total"`
	Completed  int    `json:"completed"`
	Failed     int    `json:"failed"`
	Remaining  int    `json:"remaining"`
	Overrides  int    `json:"overrides"`
}

type AccountDocument struct {
	Header
	ProfileID        string                   `json:"profile_id"`
	ProfileName      string                   `json:"profile_name"`
	ProfileKind      string                   `json:"profile_kind"`
	PartitionWindow  CollectionWindow         `json:"partition_window"`
	MoneyPartitions  []MoneyPartitionDocument `json:"money_partitions"`
	TransactionCount int                      `json:"transaction_count"`
	DateRange        *DateRangeDocument       `json:"date_range,omitempty"`
	CategoryCount    int                      `json:"category_count"`
	Pending          PendingDocument          `json:"pending"`
	Capabilities     []CapabilityDocument     `json:"capabilities"`
	Provider         ProviderStatusDocument   `json:"provider"`
	Write            WriteStatusDocument      `json:"write"`
}

type SpendingGroupDocument struct {
	ID               string `json:"id"`
	Label            string `json:"label"`
	TransactionCount int    `json:"transaction_count"`
	Money            Money  `json:"money"`
}

type SpendingDocument struct {
	Header
	GroupBy string                  `json:"group_by"`
	Window  CollectionWindow        `json:"window"`
	Groups  []SpendingGroupDocument `json:"groups"`
}

type TransactionInfoDocument struct {
	Header
	Transaction     TransactionDocument   `json:"transaction"`
	AmazonQualified bool                  `json:"amazon_qualified"`
	AmazonItem      *AmazonItemDocument   `json:"amazon_item,omitempty"`
	TotalMatches    int                   `json:"total_matches"`
	MatchOffset     int                   `json:"match_offset"`
	MatchLimit      int                   `json:"match_limit"`
	ItemOffset      int                   `json:"item_offset"`
	ItemLimit       int                   `json:"item_limit"`
	Matches         []AmazonMatchDocument `json:"matches"`
}

type AmazonItemDocument struct {
	OrderID        string `json:"order_id"`
	ProductName    string `json:"product_name"`
	ASIN           string `json:"asin,omitempty"`
	Quantity       int64  `json:"quantity"`
	OrderStatus    string `json:"order_status"`
	ShipmentStatus string `json:"shipment_status"`
	UnitPrice      *Money `json:"unit_price,omitempty"`
}

type AmazonMatchDocument struct {
	Class                 string               `json:"class"`
	Confidence            string               `json:"confidence"`
	ProfileID             string               `json:"profile_id"`
	ProfileName           string               `json:"profile_name"`
	OrderID               string               `json:"order_id"`
	OrderDate             string               `json:"order_date"`
	OrderTotal            Money                `json:"order_total"`
	DateDistanceDays      int                  `json:"date_distance_days"`
	AmountDifferenceMinor string               `json:"amount_difference_minor"`
	FirstProduct          string               `json:"first_product"`
	TotalItems            int                  `json:"total_items"`
	Items                 []AmazonItemDocument `json:"items"`
}

type ReviewDocument struct {
	Header
	Pending         PendingDocument           `json:"pending"`
	OperationWindow CollectionWindow          `json:"operation_window"`
	Operations      []ReviewOperationDocument `json:"operations"`
	TargetWindow    CollectionWindow          `json:"target_window"`
	Targets         []ReviewTargetDocument    `json:"targets"`
}

type ReviewOperationDocument struct {
	OperationID    string `json:"operation_id"`
	Type           string `json:"type"`
	Active         bool   `json:"active"`
	AffectedCount  int    `json:"affected_count"`
	Before         string `json:"before,omitempty"`
	After          string `json:"after,omitempty"`
	TaxonomyEffect string `json:"taxonomy_effect,omitempty"`
	Annotation     string `json:"annotation,omitempty"`
}

type ReviewTargetDocument struct {
	TransactionID string `json:"transaction_id"`
	Date          string `json:"date"`
	Merchant      string `json:"merchant"`
	Category      string `json:"category"`
	Hidden        bool   `json:"hidden"`
}

type CommitStatusDocument struct {
	Header
	Write WriteStatusDocument `json:"write"`
}

func transactionDocument(transaction domain.Transaction) TransactionDocument {
	return TransactionDocument{
		ID: transaction.ID, Date: transaction.Date.String(),
		Account:  Entity{ID: transaction.Account.ID, Label: transaction.Account.Name},
		Merchant: Entity{ID: transaction.Merchant.ID, Label: transaction.Merchant.Name},
		Category: CategoryEntity{ID: transaction.Category.ID, Label: transaction.Category.Name, GroupID: transaction.Category.GroupID, GroupLabel: transaction.Category.Group},
		Money:    MoneyDocument(transaction.Amount), Notes: transaction.Notes, Hidden: transaction.Hidden, Pending: transaction.Pending,
	}
}

func pendingDocument(pending app.PendingSummary) PendingDocument {
	return PendingDocument{ActiveOperations: pending.ActiveOperations, InactiveOperations: pending.InactiveOperations, AffectedTransactions: pending.AffectedTransactions}
}

func providerStatusDocument(status app.ProviderStatus) ProviderStatusDocument {
	return ProviderStatusDocument{
		Code: string(status.Code), Generation: strconv.FormatUint(status.Generation, 10),
		LastSuccess: formatOptionalTime(status.LastSuccess), NextEligible: formatOptionalTime(status.NextEligible),
		Fetched: status.Fetched, Total: status.Total,
	}
}

func writeStatusDocument(status app.ProviderWriteStatus) WriteStatusDocument {
	return WriteStatusDocument{
		Phase: string(status.Phase), Version: strconv.FormatUint(status.Version, 10), Generation: strconv.FormatUint(status.Generation, 10),
		Total: status.Total, Completed: status.Completed, Failed: status.Failed, Remaining: status.Remaining, Overrides: status.Overrides,
	}
}

func formatOptionalTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}
