// Package monarch implements the read-only Monarch Money provider adapter.
package monarch

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/wesm/moneyflow/internal/domain"
)

// Options supplies bounded transport dependencies. Production callers normally use defaults.
type Options struct {
	HTTPClient     *http.Client
	LoginURL       *url.URL
	GraphQLURL     *url.URL
	Now            func() time.Time
	Sleep          func(context.Context, time.Duration) error
	Random         io.Reader
	MaxBodyBytes   int64
	ImportCurrency domain.Currency
	ImportScale    uint8
	PageSize       int
}

// Client is the minimal authenticated Monarch read client.
type Client struct {
	options       Options
	authorization string
	deviceUUID    string
}

// NewClient validates dependencies and constructs a read-only Monarch client.
func NewClient(options Options, authorization string, deviceUUID string) (*Client, error) {
	if options.GraphQLURL == nil {
		options.GraphQLURL = cloneURL(defaultGraphQLURL)
	}
	if options.LoginURL == nil {
		options.LoginURL = cloneURL(defaultLoginURL)
	}
	if err := validateEndpoint("GraphQL", options.GraphQLURL, defaultGraphQLURL); err != nil {
		return nil, err
	}
	if err := validateEndpoint("login", options.LoginURL, defaultLoginURL); err != nil {
		return nil, err
	}
	if options.HTTPClient == nil {
		options.HTTPClient = &http.Client{Timeout: 30 * time.Second}
	} else if options.HTTPClient.Timeout <= 0 {
		return nil, errors.New("monarch HTTP client timeout must be positive")
	}
	options.HTTPClient = cloneHTTPClient(options.HTTPClient)
	if options.MaxBodyBytes == 0 {
		options.MaxBodyBytes = defaultMaxBodyBytes
	}
	if options.MaxBodyBytes < 1 {
		return nil, errors.New("monarch maximum response body must be positive")
	}
	if options.PageSize == 0 {
		options.PageSize = defaultSnapshotPageSize
	}
	if options.PageSize < 1 || options.PageSize > defaultSnapshotPageSize {
		return nil, errors.New("monarch snapshot page size must be between 1 and 1000")
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.Random == nil {
		options.Random = cryptorand.Reader
	}
	if options.Sleep == nil {
		options.Sleep = sleepContext
	}
	return &Client{options: options, authorization: authorization, deviceUUID: deviceUUID}, nil
}

type subscriptionDetailsData struct {
	Subscription Subscription `json:"subscription"`
}

// Subscription contains the stable household-scoped identity used for binding.
type Subscription struct {
	ID string `json:"id"`
}

// Account is the minimal account identity and visibility state required by reconciliation.
type Account struct {
	ID            string  `json:"id"`
	DisplayName   string  `json:"displayName"`
	IsHidden      bool    `json:"isHidden"`
	HideFromList  bool    `json:"hideFromList"`
	DeactivatedAt *string `json:"deactivatedAt"`
}

type accountsData struct {
	Accounts []Account `json:"accounts"`
}

// Merchant is one provider-owned merchant identity.
type Merchant struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type merchantAggregate struct {
	GroupBy struct {
		Merchant *Merchant `json:"merchant"`
	} `json:"groupBy"`
}

type merchantsData struct {
	ByMerchant []merchantAggregate `json:"byMerchant"`
}

// CategoryGroup is one provider-owned category-group identity.
type CategoryGroup struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type categoryGroupsData struct {
	CategoryGroups []CategoryGroup `json:"categoryGroups"`
}

// Category is one provider-owned category identity and group relationship.
type Category struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Group struct {
		ID string `json:"id"`
	} `json:"group"`
}

type categoriesData struct {
	Categories []Category `json:"categories"`
}

// Transaction is the minimal provider transaction wire record.
type Transaction struct {
	ID              string          `json:"id"`
	Amount          json.RawMessage `json:"amount"`
	Pending         bool            `json:"pending"`
	Date            string          `json:"date"`
	HideFromReports bool            `json:"hideFromReports"`
	Notes           string          `json:"notes"`
	Category        struct {
		ID string `json:"id"`
	} `json:"category"`
	Merchant struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"merchant"`
	Account struct {
		ID          string `json:"id"`
		DisplayName string `json:"displayName"`
	} `json:"account"`
}

// TransactionPageRequest selects one unfiltered visible or hidden offset page.
type TransactionPageRequest struct {
	Offset int
	Limit  int
	Hidden bool
}

// TransactionPage is one typed result page plus the provider's advertised total count.
type TransactionPage struct {
	TotalCount int           `json:"totalCount"`
	Results    []Transaction `json:"results"`
}

type transactionsData struct {
	AllTransactions TransactionPage `json:"allTransactions"`
}

// GetSubscriptionDetails returns the stable household-scoped subscription identity.
func (client *Client) GetSubscriptionDetails(ctx context.Context) (Subscription, error) {
	data, err := graphQLCall[subscriptionDetailsData](
		ctx, client, "GetSubscriptionDetails", getSubscriptionDetailsQuery, nil,
	)
	return data.Subscription, err
}

// GetAccounts returns all account identities needed for scope characterization.
func (client *Client) GetAccounts(ctx context.Context) ([]Account, error) {
	data, err := graphQLCall[accountsData](ctx, client, "GetAccounts", getAccountsQuery, nil)
	return data.Accounts, err
}

// GetMerchants returns all merchant identities from the aggregate surface.
func (client *Client) GetMerchants(ctx context.Context) ([]Merchant, error) {
	data, err := graphQLCall[merchantsData](ctx, client, "GetAllMerchants", getMerchantsQuery, nil)
	if err != nil {
		return nil, err
	}
	merchants := make([]Merchant, 0, len(data.ByMerchant))
	for _, aggregate := range data.ByMerchant {
		if aggregate.GroupBy.Merchant != nil {
			merchants = append(merchants, *aggregate.GroupBy.Merchant)
		}
	}
	return merchants, nil
}

// GetCategoryGroups returns the complete provider category-group list.
func (client *Client) GetCategoryGroups(ctx context.Context) ([]CategoryGroup, error) {
	data, err := graphQLCall[categoryGroupsData](
		ctx, client, "ManageGetCategoryGroups", getCategoryGroupsQuery, nil,
	)
	return data.CategoryGroups, err
}

// GetCategories returns the complete provider category list.
func (client *Client) GetCategories(ctx context.Context) ([]Category, error) {
	data, err := graphQLCall[categoriesData](ctx, client, "GetCategories", getCategoriesQuery, nil)
	return data.Categories, err
}

// GetTransactionsPage returns one unfiltered visible or hidden transaction page.
func (client *Client) GetTransactionsPage(
	ctx context.Context,
	page TransactionPageRequest,
) (TransactionPage, error) {
	variables := map[string]any{
		"offset":  page.Offset,
		"limit":   page.Limit,
		"orderBy": "date",
		"filters": map[string]any{"hideFromReports": page.Hidden},
	}
	data, err := graphQLCall[transactionsData](
		ctx, client, "GetTransactionsList", getTransactionsQuery, variables,
	)
	return data.AllTransactions, err
}

func sleepContext(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func cloneURL(source *url.URL) *url.URL {
	clone := *source
	return &clone
}
