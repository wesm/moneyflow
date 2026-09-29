package profiletransfer

import (
	"errors"
	"strings"
	"time"

	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/store"
)

type money struct {
	Amount   string          `json:"amount"`
	Minor    int64           `json:"amount_minor"`
	Currency domain.Currency `json:"currency"`
	Scale    uint8           `json:"scale"`
}

func moneyFrom(value domain.Money) money {
	return money{value.DecimalString(), value.Minor, value.Currency, value.Scale}
}

func (value money) parse() (domain.Money, error) {
	if !domain.IsValidCurrency(value.Currency) || value.Scale > 9 {
		return domain.Money{}, errors.New("invalid money partition")
	}
	parsed, err := domain.ParseMoney(value.Amount, value.Currency, value.Scale)
	if err != nil || parsed.Minor != value.Minor {
		return domain.Money{}, errors.New("money representations disagree")
	}
	return parsed, nil
}

type transaction struct {
	ID         domain.EntityID   `json:"id"`
	ProviderID string            `json:"provider_id"`
	Provider   string            `json:"provider"`
	AccountID  domain.EntityID   `json:"account_id"`
	MerchantID domain.EntityID   `json:"merchant_id"`
	CategoryID domain.EntityID   `json:"category_id"`
	Date       domain.Date       `json:"date"`
	Money      money             `json:"money"`
	Notes      string            `json:"notes"`
	Hidden     bool              `json:"hidden"`
	Pending    bool              `json:"pending"`
	Metadata   map[string]string `json:"metadata"`
}

func transactionFrom(value domain.TransactionRecord) transaction {
	return transaction{value.ID, value.ProviderID, value.Provider, value.AccountID, value.MerchantID,
		value.CategoryID, value.Date, moneyFrom(value.Amount), value.Notes, value.Hidden, value.Pending, value.Metadata}
}

func (value transaction) record() (domain.TransactionRecord, error) {
	amount, err := value.Money.parse()
	return domain.TransactionRecord{ID: value.ID, ProviderID: value.ProviderID, Provider: value.Provider,
		AccountID: value.AccountID, MerchantID: value.MerchantID, CategoryID: value.CategoryID,
		Date: value.Date, Amount: amount, Notes: value.Notes, Hidden: value.Hidden, Pending: value.Pending,
		Metadata: value.Metadata}, err
}

type amazonSettings struct {
	Currency  domain.Currency `json:"currency"`
	Scale     uint8           `json:"scale"`
	CreatedAt time.Time       `json:"created_at"`
}

type amazonItem struct {
	amazonItemFields
	Amount    string  `json:"amount"`
	UnitPrice *string `json:"unit_price"`
}

func amazonItemFrom(value store.AmazonOrderItem) amazonItem {
	item := amazonItem{amazonItemFields: amazonItemFields(value),
		Amount: domain.Money{Minor: value.AmountMinor, Currency: value.Currency, Scale: value.Scale}.DecimalString()}
	if value.UnitPriceMinor != nil {
		text := (domain.Money{Minor: *value.UnitPriceMinor, Currency: value.Currency, Scale: value.Scale}).DecimalString()
		item.UnitPrice = &text
	}
	return item
}

func (value amazonItem) record() (store.AmazonOrderItem, error) {
	_, err := (money{value.Amount, value.AmountMinor, value.Currency, value.Scale}).parse()
	if err != nil {
		return store.AmazonOrderItem{}, err
	}
	if (value.UnitPrice == nil) != (value.UnitPriceMinor == nil) {
		return store.AmazonOrderItem{}, errors.New("unit price representations disagree")
	}
	if value.UnitPrice != nil {
		if _, err = (money{*value.UnitPrice, *value.UnitPriceMinor, value.Currency, value.Scale}).parse(); err != nil {
			return store.AmazonOrderItem{}, err
		}
	}
	return store.AmazonOrderItem(value.amazonItemFields), nil
}

type ynabSplit struct {
	ynabSplitFields
	Amount   string          `json:"amount"`
	Currency domain.Currency `json:"currency"`
	Scale    uint8           `json:"scale"`
}

func (value ynabSplit) record() (store.YNABTransactionSplit, error) {
	parsed, err := (money{value.Amount, value.AmountMinor, value.Currency, value.Scale}).parse()
	if err != nil {
		return store.YNABTransactionSplit{}, err
	}
	original := (domain.Money{Minor: value.AmountMilliunits, Currency: value.Currency, Scale: 3}).DecimalString()
	original = strings.TrimSuffix(strings.TrimRight(original, "0"), ".")
	fromSource, err := domain.ParseMoney(original, value.Currency, value.Scale)
	if err != nil || fromSource != parsed {
		return store.YNABTransactionSplit{}, errors.New("split milliunits disagree")
	}
	return store.YNABTransactionSplit(value.ynabSplitFields), nil
}

type account struct {
	ID           domain.EntityID `json:"id"`
	Label        string          `json:"label"`
	CollisionKey string          `json:"collision_key"`
	Retired      bool            `json:"retired"`
}

type merchant struct {
	ID               domain.EntityID  `json:"id"`
	Label            string           `json:"label"`
	CollisionKey     string           `json:"collision_key"`
	Retired          bool             `json:"retired"`
	MergeDestination *domain.EntityID `json:"merge_destination"`
}

type group struct {
	ID               domain.EntityID  `json:"id"`
	Label            string           `json:"label"`
	CollisionKey     string           `json:"collision_key"`
	Protected        bool             `json:"protected"`
	Retired          bool             `json:"retired"`
	MergeDestination *domain.EntityID `json:"merge_destination"`
}

type category struct {
	ID               domain.EntityID  `json:"id"`
	GroupID          domain.EntityID  `json:"group_id"`
	Label            string           `json:"label"`
	CollisionKey     string           `json:"collision_key"`
	Protected        bool             `json:"protected"`
	Retired          bool             `json:"retired"`
	MergeDestination *domain.EntityID `json:"merge_destination"`
}

type externalIdentity struct {
	EntityType domain.EntityKind `json:"entity_type"`
	EntityID   domain.EntityID   `json:"entity_id"`
	Namespace  string            `json:"namespace"`
	ExternalID string            `json:"external_id"`
}

type knownDrill struct {
	Dimension domain.Dimension `json:"dimension"`
	Currency  domain.Currency  `json:"currency"`
	Scale     uint8            `json:"scale"`
	Key       string           `json:"identity_key"`
}

type providerBinding struct {
	Kind            string          `json:"kind"`
	Namespace       string          `json:"namespace"`
	RemoteProfileID string          `json:"remote_profile_id"`
	Currency        domain.Currency `json:"currency"`
	Scale           uint8           `json:"scale"`
	BoundAt         time.Time       `json:"bound_at"`
}

type labelAllocation struct {
	Kind             domain.EntityKind `json:"kind"`
	Namespace        string            `json:"namespace"`
	ExternalID       string            `json:"external_id"`
	BaseCollisionKey string            `json:"base_collision_key"`
	DisplayLabel     string            `json:"display_label"`
	ProviderLabel    string            `json:"provider_label"`
	SuffixToken      string            `json:"suffix_token"`
	Unsuffixed       bool              `json:"unsuffixed"`
}

type providerLineage struct {
	Kind           domain.EntityKind `json:"kind"`
	Namespace      string            `json:"namespace"`
	ExternalID     string            `json:"external_id"`
	PriorLocalID   domain.EntityID   `json:"prior_local_id"`
	CurrentLocalID domain.EntityID   `json:"current_local_id"`
	ProviderLabel  string            `json:"provider_label"`
	Disposition    string            `json:"disposition"`
	BatchVersion   uint64            `json:"batch_version"`
}

type writeRestriction struct {
	Kind     domain.EntityKind `json:"kind"`
	EntityID domain.EntityID   `json:"entity_id"`
	Reason   string            `json:"reason"`
}

type ynabSplitFields struct {
	ParentTransactionID           domain.EntityID `json:"parent_transaction_id"`
	Position                      int             `json:"position"`
	ExternalID                    string          `json:"external_id"`
	AmountMilliunits              int64           `json:"amount_milliunits"`
	AmountMinor                   int64           `json:"amount_minor"`
	Memo                          string          `json:"memo"`
	PayeeExternalID               string          `json:"payee_external_id"`
	PayeeLabel                    string          `json:"payee_label"`
	CategoryExternalID            string          `json:"category_external_id"`
	CategoryLabel                 string          `json:"category_label"`
	TransferAccountExternalID     string          `json:"transfer_account_external_id"`
	TransferTransactionExternalID string          `json:"transfer_transaction_external_id"`
}

type amazonItemFields struct {
	LocalTransactionID  domain.EntityID `json:"local_transaction_id"`
	SourceIdentity      string          `json:"source_identity"`
	OrderID             string          `json:"order_id"`
	ASIN                string          `json:"asin"`
	ASINLessKey         string          `json:"asin_less_key"`
	ProductName         string          `json:"product_name"`
	OrderDate           domain.Date     `json:"order_date"`
	Quantity            int64           `json:"quantity"`
	AmountMinor         int64           `json:"amount_minor"`
	UnitPriceMinor      *int64          `json:"unit_price_minor"`
	Currency            domain.Currency `json:"currency"`
	Scale               uint8           `json:"scale"`
	OrderStatus         string          `json:"order_status"`
	ShipmentStatus      string          `json:"shipment_status"`
	IdentityFingerprint string          `json:"identity_fingerprint"`
	FullFingerprint     string          `json:"full_fingerprint"`
	Retired             bool            `json:"retired"`
	LocalAccountID      domain.EntityID `json:"local_account_id"`
	LocalMerchantID     domain.EntityID `json:"local_merchant_id"`
	LocalCategoryID     domain.EntityID `json:"local_category_id"`
	LocalNotes          string          `json:"local_notes"`
	LocalHidden         bool            `json:"local_hidden"`
}
