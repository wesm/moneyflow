package mcp

import (
	"context"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wesm/moneyflow/internal/domain"
)

const (
	resourceAccount      = "moneyflow://account"
	resourceCategories   = "moneyflow://categories"
	resourceMerchantsTop = "moneyflow://merchants/top"
	resourceMonthly      = "moneyflow://spending/monthly"
	resourceRecent       = "moneyflow://transactions/recent"
)

func registerResources(server *Server, dependencies Dependencies) {
	addResource(server, resourceAccount, "Moneyflow account", "Credential-blind local profile information", func(ctx context.Context) (any, error) {
		return accountDocument(ctx, dependencies)
	})
	addResource(server, resourceCategories, "Moneyflow categories", "Active category groups and categories", func(ctx context.Context) (any, error) {
		return categoriesDocument(ctx, dependencies.Service, CategoriesInput{GroupLimit: intPointer(1_000), CategoryLimit: intPointer(1_000)})
	})
	addResource(server, resourceMerchantsTop, "Top Moneyflow merchants", "Top effective merchants by transaction count", func(ctx context.Context) (any, error) {
		return merchantsDocument(ctx, dependencies.Service, WindowInput{Limit: intPointer(50)})
	})
	addResource(server, resourceMonthly, "Monthly Moneyflow spending", "Current calendar month effective spending by category", func(ctx context.Context) (any, error) {
		start, end, err := currentMonth(dependencies.Clock())
		if err != nil {
			return nil, err
		}
		return spendingDocument(ctx, dependencies.Service, dependencies.Clock, SpendingSummaryInput{StartDate: start.String(), EndDate: end.String(), GroupBy: string(domain.DimensionCategory), Limit: intPointer(1_000)})
	})
	addResource(server, resourceRecent, "Recent Moneyflow transactions", "Recent effective transactions", func(ctx context.Context) (any, error) {
		includeHidden := true
		return getTransactionsDocument(ctx, dependencies.Service, GetTransactionsInput{IncludeHidden: &includeHidden, Limit: intPointer(50)})
	})
}

func intPointer(value int) *int { return &value }

func addResource(
	server *Server,
	uri, name, description string,
	project func(context.Context) (any, error),
) {
	server.SDK.AddResource(&mcpsdk.Resource{URI: uri, Name: name, Title: name, Description: description, MIMEType: "application/json"},
		func(ctx context.Context, _ *mcpsdk.ReadResourceRequest) (*mcpsdk.ReadResourceResult, error) {
			document, err := project(ctx)
			if err != nil {
				if mapped, ok := ApplicationErrorDocument(err, serverRevision(server)); ok {
					document = mapped
				} else {
					document = ErrorDocument{Header: NewHeader(StatusError, serverRevision(server)), Code: "mcp_internal_error", Detail: "The Moneyflow MCP resource could not be read."}
				}
			}
			canonical, _, encodeErr := encodeDocument(document)
			if encodeErr != nil {
				return nil, encodeErr
			}
			if len(canonical) > MaxResponseContentBytes {
				canonical, _, encodeErr = encodeDocument(responseTooLargeDocument(serverRevision(server)))
				if encodeErr != nil {
					return nil, encodeErr
				}
			}
			return &mcpsdk.ReadResourceResult{Contents: []*mcpsdk.ResourceContents{{URI: uri, MIMEType: "application/json", Text: string(canonical)}}}, nil
		})
}

func currentMonth(value time.Time) (domain.Date, domain.Date, error) {
	value = value.UTC()
	start, err := domain.NewDate(value.Year(), value.Month(), 1)
	if err != nil {
		return domain.Date{}, domain.Date{}, err
	}
	next := time.Date(value.Year(), value.Month()+1, 1, 0, 0, 0, 0, time.UTC)
	end, err := domain.NewDate(next.AddDate(0, 0, -1).Year(), next.AddDate(0, 0, -1).Month(), next.AddDate(0, 0, -1).Day())
	return start, end, err
}
