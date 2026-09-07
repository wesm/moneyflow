package ynab

// PlanSummary is the bounded onboarding projection returned by GET /plans.
type PlanSummary struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	LastModifiedOn string `json:"last_modified_on"`
}

// CurrencyFormat is the exact money interpretation declared by one YNAB plan.
type CurrencyFormat struct {
	ISOCode       string `json:"iso_code"`
	DecimalDigits int    `json:"decimal_digits"`
}

// Account is one YNAB plan account.
type Account struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	OnBudget *bool  `json:"on_budget"`
	Closed   *bool  `json:"closed"`
	Deleted  *bool  `json:"deleted"`
}

// Payee is one YNAB payee.
type Payee struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Deleted *bool  `json:"deleted"`
}

// CategoryGroup is one YNAB category group.
type CategoryGroup struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Hidden  *bool  `json:"hidden"`
	Deleted *bool  `json:"deleted"`
}

// Category is one YNAB category.
type Category struct {
	ID              string `json:"id"`
	CategoryGroupID string `json:"category_group_id"`
	Name            string `json:"name"`
	Hidden          *bool  `json:"hidden"`
	Deleted         *bool  `json:"deleted"`
}

// Transaction is one parent YNAB transaction.
type Transaction struct {
	ID                    string `json:"id"`
	Date                  string `json:"date"`
	Amount                int64  `json:"amount"`
	Memo                  string `json:"memo"`
	Cleared               string `json:"cleared"`
	Approved              *bool  `json:"approved"`
	AccountID             string `json:"account_id"`
	PayeeID               string `json:"payee_id"`
	CategoryID            string `json:"category_id"`
	CategoryName          string `json:"category_name,omitempty"`
	TransferAccountID     string `json:"transfer_account_id"`
	TransferTransactionID string `json:"transfer_transaction_id"`
	Deleted               *bool  `json:"deleted"`
}

// Subtransaction is one retained split detail.
type Subtransaction struct {
	ID                    string `json:"id"`
	TransactionID         string `json:"transaction_id"`
	Amount                int64  `json:"amount"`
	Memo                  string `json:"memo"`
	PayeeID               string `json:"payee_id"`
	CategoryID            string `json:"category_id"`
	CategoryName          string `json:"category_name,omitempty"`
	TransferAccountID     string `json:"transfer_account_id"`
	TransferTransactionID string `json:"transfer_transaction_id"`
	Deleted               *bool  `json:"deleted"`
}

type transactionDetail struct {
	Transaction
	Subtransactions []Subtransaction `json:"subtransactions"`
}

type transactionsResponse struct {
	Data struct {
		Transactions    *[]transactionDetail `json:"transactions"`
		ServerKnowledge *int64               `json:"server_knowledge"`
	} `json:"data"`
}

// PlanDocument is the complete non-delta plan response consumed by normalization.
type PlanDocument struct {
	ID              string
	Name            string
	CurrencyFormat  CurrencyFormat
	Accounts        []Account
	Payees          []Payee
	CategoryGroups  []CategoryGroup
	Categories      []Category
	Transactions    []Transaction
	Subtransactions []Subtransaction
	ServerKnowledge int64
}

type plansResponse struct {
	Data struct {
		Plans *[]PlanSummary `json:"plans"`
	} `json:"data"`
}

type planResponse struct {
	Data struct {
		Plan struct {
			ID              string            `json:"id"`
			Name            string            `json:"name"`
			CurrencyFormat  CurrencyFormat    `json:"currency_format"`
			Accounts        *[]Account        `json:"accounts"`
			Payees          *[]Payee          `json:"payees"`
			CategoryGroups  *[]CategoryGroup  `json:"category_groups"`
			Categories      *[]Category       `json:"categories"`
			Transactions    *[]Transaction    `json:"transactions"`
			Subtransactions *[]Subtransaction `json:"subtransactions"`
		} `json:"plan"`
		ServerKnowledge *int64 `json:"server_knowledge"`
	} `json:"data"`
}
