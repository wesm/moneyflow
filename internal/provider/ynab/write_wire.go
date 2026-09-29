package ynab

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"reflect"

	"github.com/wesm/moneyflow/internal/domain"
)

type writeTransactionResponse struct {
	Data struct {
		Transaction json.RawMessage `json:"transaction"`
	} `json:"data"`
}

type writeTransactionSnapshot struct {
	transaction      Transaction
	payeeName        string
	fields           map[string]json.RawMessage
	children         map[string]map[string]json.RawMessage
	transferChildren bool
}

func (writer *transactionWriter) decodeTransaction(raw json.RawMessage) (writeTransactionSnapshot, error) {
	invalid := errors.New("invalid YNAB transaction response")
	var value struct {
		transactionDetail
		PayeeName string `json:"payee_name"`
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &value) != nil || json.Unmarshal(raw, &fields) != nil {
		return writeTransactionSnapshot{}, invalid
	}
	transaction := value.Transaction
	if validateTransactionWire(transaction) != nil || value.Subtransactions == nil {
		return writeTransactionSnapshot{}, invalid
	}
	if _, err := domain.ParseDate(transaction.Date); err != nil {
		return writeTransactionSnapshot{}, invalid
	}
	if transaction.Cleared != "cleared" && transaction.Cleared != "uncleared" && transaction.Cleared != "reconciled" {
		return writeTransactionSnapshot{}, invalid
	}
	if _, err := milliunitsToMoney(*transaction.Amount, writer.currency, writer.scale); err != nil {
		return writeTransactionSnapshot{}, invalid
	}
	category, exists := fields["category_id"]
	if !exists || (!bytes.Equal(bytes.TrimSpace(category), []byte("null")) && validateID(transaction.CategoryID) != nil) {
		return writeTransactionSnapshot{}, invalid
	}
	if transaction.PayeeID != "" && validateID(transaction.PayeeID) != nil {
		return writeTransactionSnapshot{}, invalid
	}
	if value.PayeeName != "" && validateLabel(value.PayeeName) != nil {
		return writeTransactionSnapshot{}, invalid
	}
	result := writeTransactionSnapshot{transaction: transaction, payeeName: value.PayeeName, fields: fields, children: make(map[string]map[string]json.RawMessage)}
	var children []map[string]json.RawMessage
	if json.Unmarshal(fields["subtransactions"], &children) != nil {
		return writeTransactionSnapshot{}, invalid
	}
	var sum int64
	for index, child := range value.Subtransactions {
		if validateID(child.ID) != nil || child.TransactionID != transaction.ID || child.Amount == nil || child.Deleted == nil || (*child.Deleted && !*transaction.Deleted) || validateMemo(child.Memo) != nil {
			return writeTransactionSnapshot{}, invalid
		}
		if _, exists := result.children[child.ID]; exists {
			return writeTransactionSnapshot{}, invalid
		}
		if _, err := milliunitsToMoney(*child.Amount, writer.currency, writer.scale); err != nil {
			return writeTransactionSnapshot{}, invalid
		}
		amount := *child.Amount
		if (amount > 0 && sum > math.MaxInt64-amount) || (amount < 0 && sum < math.MinInt64-amount) {
			return writeTransactionSnapshot{}, invalid
		}
		sum += amount
		result.children[child.ID] = children[index]
		result.transferChildren = result.transferChildren || child.TransferAccountID != "" || child.TransferTransactionID != ""
	}
	if len(result.children) > 0 && sum != *transaction.Amount {
		return writeTransactionSnapshot{}, invalid
	}
	return result, nil
}

func (snapshot writeTransactionSnapshot) transfer() bool {
	return snapshot.transaction.TransferAccountID != "" || snapshot.transaction.TransferTransactionID != "" || snapshot.transferChildren
}

func (snapshot writeTransactionSnapshot) preservedBy(after writeTransactionSnapshot) bool {
	for _, key := range []string{"id", "date", "amount", "memo", "approved", "cleared", "account_id", "flag_color", "flag_name", "import_id", "import_payee_name", "import_payee_name_original", "matched_transaction_id", "transfer_account_id", "transfer_transaction_id", "debt_transaction_type"} {
		if !sameJSONValue(snapshot.fields[key], after.fields[key]) {
			return false
		}
	}
	if len(snapshot.children) != len(after.children) {
		return false
	}
	for id, before := range snapshot.children {
		current, exists := after.children[id]
		if !exists {
			return false
		}
		for _, key := range []string{"id", "transaction_id", "amount", "memo", "payee_id", "category_id", "transfer_account_id", "transfer_transaction_id", "deleted"} {
			if !sameJSONValue(before[key], current[key]) {
				return false
			}
		}
	}
	return true
}

// UseNumber avoids converting preserved provider values to floating-point money.
func sameJSONValue(left, right json.RawMessage) bool {
	if len(left) == 0 || len(right) == 0 {
		return len(left) == len(right)
	}
	var a, b any
	first, second := json.NewDecoder(bytes.NewReader(left)), json.NewDecoder(bytes.NewReader(right))
	first.UseNumber()
	second.UseNumber()
	return first.Decode(&a) == nil && second.Decode(&b) == nil && reflect.DeepEqual(a, b)
}
