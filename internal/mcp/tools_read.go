package mcp

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
)

func registerReadTools(server *Server) {
	registerTool(server, "search_transactions", "Search effective transactions with literal text matching.", true,
		func(ctx context.Context, dependencies Dependencies, input SearchTransactionsInput) (any, error) {
			return searchTransactionsDocument(ctx, dependencies.Service, input)
		})
	registerTool(server, "get_transactions", "Return a filtered effective transaction window.", true,
		func(ctx context.Context, dependencies Dependencies, input GetTransactionsInput) (any, error) {
			return getTransactionsDocument(ctx, dependencies.Service, input)
		})
	registerTool(server, "get_spending_summary", "Summarize effective expenses by category or merchant.", true,
		func(ctx context.Context, dependencies Dependencies, input SpendingSummaryInput) (any, error) {
			return spendingDocument(ctx, dependencies.Service, dependencies.Clock, input)
		})
	registerTool(server, "get_insights", insightsDescription, true,
		func(ctx context.Context, dependencies Dependencies, input GetInsightsInput) (any, error) {
			return insightsDocument(ctx, dependencies.Service, input)
		})
	registerTool(server, "get_categories", "List active category groups and categories.", true,
		func(ctx context.Context, dependencies Dependencies, input CategoriesInput) (any, error) {
			return categoriesDocument(ctx, dependencies.Service, input)
		})
	registerTool(server, "get_merchants", "List active merchants and exact totals.", true,
		func(ctx context.Context, dependencies Dependencies, input WindowInput) (any, error) {
			return merchantsDocument(ctx, dependencies.Service, input)
		})
	registerTool(server, "get_account_info", "Return credential-blind local profile information.", true,
		func(ctx context.Context, dependencies Dependencies, input AccountInfoInput) (any, error) {
			return accountDocument(ctx, dependencies, input)
		})
	registerTool(server, "get_uncategorized_transactions", "Return effective uncategorized transactions.", true,
		func(ctx context.Context, dependencies Dependencies, input UncategorizedInput) (any, error) {
			return uncategorizedDocument(ctx, dependencies.Service, input)
		})
	registerTool(server, "find_duplicates", findDuplicatesDescription, true,
		func(ctx context.Context, dependencies Dependencies, input FindDuplicatesInput) (any, error) {
			return duplicatesDocument(ctx, dependencies.Service, input)
		})
	registerTool(server, "get_amazon_order_details", "Return bounded Amazon enrichment for one transaction.", true,
		func(ctx context.Context, dependencies Dependencies, input TransactionDetailsInput) (any, error) {
			return transactionInfoDocument(ctx, dependencies.Service, input)
		})
	registerTool(server, "get_transaction_details", "Return committed details for one transaction.", true,
		func(ctx context.Context, dependencies Dependencies, input TransactionDetailsInput) (any, error) {
			return transactionInfoDocument(ctx, dependencies.Service, input)
		})
	registerTool(server, "review_changes", "Return bounded active and redo journal summaries.", true,
		func(ctx context.Context, dependencies Dependencies, input ReviewChangesInput) (any, error) {
			return reviewDocument(ctx, dependencies.Service, input)
		})
	registerTool(server, "get_commit_status", "Return the durable provider-write status without provider I/O.", true,
		func(ctx context.Context, dependencies Dependencies, _ EmptyInput) (any, error) {
			account, err := dependencies.Service.AccountProjection(ctx, app.AccountProjectionRequest{
				Partitions: app.CollectionWindowRequest{Limit: 1},
			})
			if err != nil {
				return nil, err
			}
			return CommitStatusDocument{Header: NewHeader(StatusOK, account.Revision), Write: writeStatusDocument(account.Write)}, nil
		})
	registerTool(server, "refresh_data", "Start one explicit provider refresh when available. Monarch reads the last 90 days unless complete_history is set or complete history is due.", false,
		func(ctx context.Context, dependencies Dependencies, input RefreshDataInput) (any, error) {
			return refreshDataDocument(ctx, dependencies, input.CompleteHistory)
		})
	registerTool(server, "get_refresh_status", "Return process-local provider refresh status.", true,
		func(ctx context.Context, dependencies Dependencies, input RefreshStatusInput) (any, error) {
			return refreshStatusDocument(ctx, dependencies, input)
		})
	registerTool(server, "confirm_refresh_deletions", "Confirm one process-local provider deletion candidate.", false,
		func(ctx context.Context, dependencies Dependencies, input ConfirmRefreshInput) (any, error) {
			return confirmRefreshDocument(ctx, dependencies, input)
		})
}

func refreshDataDocument(
	ctx context.Context,
	dependencies Dependencies,
	completeHistory bool,
) (any, error) {
	connection, err := dependencies.Service.ProviderConnection(ctx)
	if err != nil {
		return nil, err
	}
	if dependencies.Service.ProfileKind() == "amazon" {
		return capabilityUnavailable(
			dependencies.Service,
			"Amazon import requires the TUI, web, or provider import command.",
		), nil
	}
	if !connection.Bound {
		return capabilityUnavailable(
			dependencies.Service, "This local profile has no provider to refresh.",
		), nil
	}
	if connection.Kind != "monarch" && connection.Kind != "ynab" && connection.Kind != "simplefin" {
		return capabilityUnavailable(
			dependencies.Service,
			"This provider has no MCP refresh operation.",
		), nil
	}
	refresh, err := dependencies.Service.ProviderRefreshCapability(ctx)
	if err != nil {
		return nil, err
	}
	if !refresh.Available {
		return capabilityUnavailable(dependencies.Service, refresh.Reason), nil
	}
	request := app.ProviderRefreshRequest{
		Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
		Window: app.WindowRequest{Limit: 1}, CompleteHistory: completeHistory,
	}
	if err = dependencies.Supervisor.ExpireObsolete(ctx, dependencies.Service); err != nil {
		return nil, err
	}
	attempt, err := dependencies.Supervisor.StartRefresh(RefreshScopeOf(request, connection.Kind),
		heldRefresh(dependencies, request))
	if err != nil {
		return nil, err
	}
	return refreshAttemptDocument(attempt.AttemptStatus, dependencies.Service.Revision(), false), nil
}

// heldRefresh runs request under its own hold on the profile, so the profile stays open until the
// refresh ends.
func heldRefresh(
	dependencies Dependencies,
	request app.ProviderRefreshRequest,
) func(context.Context) (app.ProviderRefreshResult, error) {
	return func(workerContext context.Context) (app.ProviderRefreshResult, error) {
		service, release, err := dependencies.Hold(workerContext)
		if err != nil {
			return app.ProviderRefreshResult{}, err
		}
		defer release()
		return service.RefreshProvider(workerContext, request)
	}
}

func refreshStatusDocument(
	ctx context.Context,
	dependencies Dependencies,
	input RefreshStatusInput,
) (any, error) {
	attempt, err := dependencies.Supervisor.RefreshStatus(input.AttemptID)
	if err != nil {
		if errors.Is(err, ErrAttemptNotFound) {
			return attemptNotFoundDocument(dependencies.Service.Revision()), nil
		}
		return nil, err
	}
	if attempt.State == AttemptRunning {
		// The attempt records the provider's status only when it ends, so a running attempt reads
		// its progress from the service that runs it.
		live, err := dependencies.Service.ProviderStatus(ctx)
		if err != nil {
			return nil, err
		}
		attempt.Refresh.Fetched = live.Fetched
		attempt.Refresh.Total = live.Total
		attempt.Refresh.Verifying = live.Verifying
		attempt.Refresh.Percent = live.Percent
	}
	return refreshAttemptDocument(attempt, dependencies.Service.Revision(), true), nil
}

func confirmRefreshDocument(
	_ context.Context,
	dependencies Dependencies,
	input ConfirmRefreshInput,
) (any, error) {
	attempt, confirmErr := dependencies.Supervisor.ConfirmRefresh(
		input.AttemptID, input.ConfirmationToken,
		func(workerContext context.Context) (app.ProviderRefreshResult, error) {
			return dependencies.Service.ConfirmProviderRefresh(workerContext, app.ProviderRefreshRequest{
				Manual: true, ConfirmationToken: input.ConfirmationToken,
				State: app.DefaultViewState(), Selection: app.EmptySelection(),
				Window: app.WindowRequest{Limit: 1},
			})
		},
	)
	if errors.Is(confirmErr, ErrAttemptNotFound) {
		return confirmationInvalidDocument(dependencies.Service.Revision()), nil
	}
	if confirmErr != nil {
		return nil, confirmErr
	}
	return refreshAttemptDocument(attempt, dependencies.Service.Revision(), false), nil
}

func refreshAttemptDocument(
	status AttemptStatus,
	currentRevision uint64,
	includeToken bool,
) RefreshAttemptDocument {
	revision := status.Revision
	if revision == 0 {
		revision = currentRevision
	}
	document := RefreshAttemptDocument{
		Header: NewHeader(StatusOK, revision), AttemptID: status.ID,
		State: string(status.State), Code: status.Code,
		Generation: strconv.FormatUint(status.Generation, 10),
		StartedAt:  formatOptionalTime(status.StartedAt), FinishedAt: formatOptionalTime(status.FinishedAt),
		Guidance: refreshAttemptGuidance(status), Provider: providerStatusDocument(status.Refresh),
		Summary: refreshSummaryDocument(status.Refresh),
	}
	if includeToken {
		document.ConfirmationToken = status.ConfirmationToken
	}
	return document
}

func refreshAttemptGuidance(status AttemptStatus) string {
	switch status.State {
	case AttemptReconnectRequired:
		return "Unlock or reconnect this profile before starting a new refresh. A YNAB vault must be unlocked in the calling process."
	case AttemptConfirmationRequired:
		return "Review the removal counts and confirm this process-local candidate if they are expected."
	case AttemptFailed:
		return "Resolve the reported provider condition before starting a new refresh."
	case AttemptRunning, AttemptCompleted:
		return ""
	default:
		return ""
	}
}

func searchTransactionsDocument(ctx context.Context, service *app.Service, input SearchTransactionsInput) (TransactionWindowDocument, error) {
	limit := defaultedLimit(input.Limit, 50)
	window, err := service.TransactionWindow(ctx, app.TransactionWindowRequest{
		Filter: app.TransactionFilter{LiteralQuery: input.Query, IncludeHidden: true}, Offset: input.Offset, Limit: limit,
	})
	if err != nil {
		return TransactionWindowDocument{}, err
	}
	return transactionWindowDocument(window), nil
}

func getTransactionsDocument(ctx context.Context, service *app.Service, input GetTransactionsInput) (TransactionWindowDocument, error) {
	filter, err := transactionFilter(input.TransactionFilterInput)
	if err != nil {
		return TransactionWindowDocument{}, newAppInputError(service.Revision(), err)
	}
	limit := defaultedLimit(input.Limit, 100)
	window, err := service.TransactionWindow(ctx, app.TransactionWindowRequest{Filter: filter, Offset: input.Offset, Limit: limit})
	if err != nil {
		return TransactionWindowDocument{}, err
	}
	return transactionWindowDocument(window), nil
}

func transactionFilter(input TransactionFilterInput) (app.TransactionFilter, error) {
	filter := app.TransactionFilter{
		CategoryID: domain.EntityID(input.CategoryID), CategoryLabel: input.CategoryLabel,
		MerchantSubstring: input.Merchant, IncludeHidden: true,
	}
	if input.IncludeHidden != nil {
		filter.IncludeHidden = *input.IncludeHidden
	}
	if input.StartDate != "" {
		value, parseErr := domain.ParseDate(input.StartDate)
		if parseErr != nil {
			return filter, parseErr
		}
		filter.StartDate = &value
	}
	if input.EndDate != "" {
		value, parseErr := domain.ParseDate(input.EndDate)
		if parseErr != nil {
			return filter, parseErr
		}
		filter.EndDate = &value
	}
	if input.MinAmount != "" || input.MaxAmount != "" {
		if input.Currency == "" || input.Scale == nil {
			return filter, errors.New("amount bounds require currency and scale")
		}
	}
	if (input.Currency == "") != (input.Scale == nil) {
		return filter, errors.New("currency and scale must be provided together")
	}
	if input.Currency != "" && !domain.IsValidCurrency(domain.Currency(input.Currency)) {
		return filter, errors.New("currency is invalid")
	}
	if input.MinAmount != "" {
		value, parseErr := domain.ParseMoney(input.MinAmount, domain.Currency(input.Currency), *input.Scale)
		if parseErr != nil {
			return filter, parseErr
		}
		filter.MinAmount = &value
	}
	if input.MaxAmount != "" {
		value, parseErr := domain.ParseMoney(input.MaxAmount, domain.Currency(input.Currency), *input.Scale)
		if parseErr != nil {
			return filter, parseErr
		}
		filter.MaxAmount = &value
	}
	return filter, nil
}

func transactionWindowDocument(window app.TransactionWindow) TransactionWindowDocument {
	rows := make([]TransactionDocument, len(window.Rows))
	for index := range window.Rows {
		rows[index] = transactionDocument(window.Rows[index])
	}
	return TransactionWindowDocument{
		Header: NewHeader(StatusOK, window.Revision), Total: window.Total, Offset: window.Offset,
		Limit: window.Limit, Returned: len(rows), Transactions: rows, Pending: pendingDocument(window.Pending),
	}
}

func categoriesDocument(ctx context.Context, service *app.Service, input CategoriesInput) (CategoriesDocument, error) {
	groupLimit := defaultedLimit(input.GroupLimit, 100)
	categoryLimit := defaultedLimit(input.CategoryLimit, 100)
	projection, err := service.CatalogProjection(ctx, app.CatalogWindowRequest{
		Groups:     app.CollectionWindowRequest{Offset: input.GroupOffset, Limit: groupLimit},
		Categories: app.CollectionWindowRequest{Offset: input.CategoryOffset, Limit: categoryLimit},
		Merchants:  app.CollectionWindowRequest{Limit: 1},
	})
	if err != nil {
		return CategoriesDocument{}, err
	}
	document := CategoriesDocument{
		Header:         NewHeader(StatusOK, projection.Revision),
		GroupWindow:    CollectionWindow{Total: projection.GroupTotal, Offset: projection.GroupOffset, Limit: groupLimit, Returned: len(projection.Groups)},
		CategoryWindow: CollectionWindow{Total: projection.CategoryTotal, Offset: projection.CategoryOffset, Limit: categoryLimit, Returned: len(projection.Categories)},
	}
	for _, entry := range projection.Groups {
		document.Groups = append(document.Groups, GroupDocument{ID: string(entry.Group.ID), Label: entry.Group.Label, TransactionCount: entry.TransactionCount})
	}
	for _, entry := range projection.Categories {
		document.Categories = append(document.Categories, CategoryDocument{ID: string(entry.Category.ID), Label: entry.Category.Label, GroupID: string(entry.Category.GroupID), Protected: entry.Category.Protected, TransactionCount: entry.TransactionCount})
	}
	return document, nil
}

func merchantsDocument(ctx context.Context, service *app.Service, input WindowInput) (MerchantsDocument, error) {
	limit := defaultedLimit(input.Limit, 100)
	projection, err := service.CatalogProjection(ctx, app.CatalogWindowRequest{
		Groups: app.CollectionWindowRequest{Limit: 1}, Categories: app.CollectionWindowRequest{Limit: 1},
		Merchants: app.CollectionWindowRequest{Offset: input.Offset, Limit: limit},
	})
	if err != nil {
		return MerchantsDocument{}, err
	}
	document := MerchantsDocument{Header: NewHeader(StatusOK, projection.Revision), Window: CollectionWindow{Total: projection.MerchantTotal, Offset: projection.MerchantOffset, Limit: limit, Returned: len(projection.Merchants)}}
	for _, entry := range projection.Merchants {
		merchant := MerchantDocument{ID: string(entry.Merchant.ID), Label: entry.Merchant.Label, TransactionCount: entry.TransactionCount}
		for _, total := range entry.Totals {
			merchant.Totals = append(merchant.Totals, MoneyDocument(total))
		}
		document.Merchants = append(document.Merchants, merchant)
	}
	return document, nil
}

func accountDocument(
	ctx context.Context,
	dependencies Dependencies,
	input AccountInfoInput,
) (AccountDocument, error) {
	limit := defaultedLimit(input.PartitionLimit, 100)
	projection, err := dependencies.Service.AccountProjection(ctx, app.AccountProjectionRequest{
		Partitions: app.CollectionWindowRequest{Offset: input.PartitionOffset, Limit: limit},
	})
	if err != nil {
		return AccountDocument{}, err
	}
	document := AccountDocument{
		Header: NewHeader(StatusOK, projection.Revision), ProfileID: dependencies.ProfileID,
		ProfileName: dependencies.ProfileName, ProfileKind: projection.ProfileKind,
		PartitionWindow:  CollectionWindow{Total: projection.PartitionTotal, Offset: projection.PartitionOffset, Limit: projection.PartitionLimit, Returned: len(projection.MoneyPartitions)},
		TransactionCount: projection.TransactionCount, CategoryCount: projection.CategoryCount,
		Pending: pendingDocument(projection.Pending), Provider: providerStatusDocument(projection.Provider), Write: writeStatusDocument(projection.Write),
	}
	for _, partition := range projection.MoneyPartitions {
		document.MoneyPartitions = append(document.MoneyPartitions, MoneyPartitionDocument{Currency: string(partition.Currency), Scale: partition.Scale, TransactionCount: partition.TransactionCount})
	}
	if projection.DateRange != nil {
		document.DateRange = &DateRangeDocument{Start: projection.DateRange.Start.String(), End: projection.DateRange.End.String()}
	}
	for _, capability := range projection.Capabilities {
		document.Capabilities = append(document.Capabilities, CapabilityDocument{Action: string(capability.Action), Available: capability.Available, Reason: capability.Reason})
	}
	return document, nil
}

func spendingDocument(ctx context.Context, service *app.Service, clock func() time.Time, input SpendingSummaryInput) (SpendingDocument, error) {
	start, end, err := spendingDates(clock, input.StartDate, input.EndDate)
	if err != nil {
		return SpendingDocument{}, newAppInputError(service.Revision(), err)
	}
	groupBy := domain.DimensionCategory
	if input.GroupBy != "" {
		groupBy = domain.Dimension(input.GroupBy)
	}
	limit := defaultedLimit(input.Limit, 100)
	projection, err := service.SpendingSummary(ctx, app.SpendingSummaryRequest{StartDate: start, EndDate: end, GroupBy: groupBy, Offset: input.Offset, Limit: limit})
	if err != nil {
		return SpendingDocument{}, err
	}
	document := SpendingDocument{Header: NewHeader(StatusOK, projection.Revision), GroupBy: string(groupBy), Window: CollectionWindow{Total: projection.Total, Offset: projection.Offset, Limit: projection.Limit, Returned: len(projection.Groups)}}
	for _, group := range projection.Groups {
		document.Groups = append(document.Groups, SpendingGroupDocument{ID: group.ID, Label: group.Label, TransactionCount: group.TransactionCount, Money: MoneyDocument(group.Total)})
	}
	return document, nil
}

func spendingDates(clock func() time.Time, startText, endText string) (*domain.Date, *domain.Date, error) {
	if startText == "" && endText == "" {
		endTime := clock()
		end, err := domain.NewDate(endTime.Year(), endTime.Month(), endTime.Day())
		if err != nil {
			return nil, nil, err
		}
		start, err := end.AddDays(-29)
		return &start, &end, err
	}
	var start, end *domain.Date
	if startText != "" {
		value, err := domain.ParseDate(startText)
		if err != nil {
			return nil, nil, err
		}
		start = &value
	}
	if endText != "" {
		value, err := domain.ParseDate(endText)
		if err != nil {
			return nil, nil, err
		}
		end = &value
	}
	return start, end, nil
}

func uncategorizedDocument(ctx context.Context, service *app.Service, input UncategorizedInput) (TransactionWindowDocument, error) {
	limit := defaultedLimit(input.Limit, 100)
	window, err := service.TransactionWindow(ctx, app.TransactionWindowRequest{Filter: app.TransactionFilter{CategoryID: domain.UncategorizedCategoryID, MerchantSubstring: input.Merchant, IncludeHidden: true}, Offset: input.Offset, Limit: limit})
	if err != nil {
		return TransactionWindowDocument{}, err
	}
	return transactionWindowDocument(window), nil
}

func transactionInfoDocument(ctx context.Context, service *app.Service, input TransactionDetailsInput) (TransactionInfoDocument, error) {
	matchLimit := defaultedLimit(input.MatchLimit, 20)
	itemLimit := defaultedLimit(input.ItemLimit, 20)
	if matchLimit <= 0 || matchLimit > 20 || itemLimit <= 0 || itemLimit > 100 {
		return TransactionInfoDocument{}, newAppInputError(service.Revision(), errors.New("transaction information window is invalid"))
	}
	info, err := service.TransactionInfo(ctx, app.TransactionInfoRequest{TransactionID: input.TransactionID, MatchOffset: input.MatchOffset, MatchLimit: matchLimit, ItemOffset: input.ItemOffset, ItemLimit: itemLimit})
	if err != nil {
		return TransactionInfoDocument{}, err
	}
	document := TransactionInfoDocument{
		Header: NewHeader(StatusOK, info.Revision), Transaction: transactionDocument(info.Transaction), AmazonQualified: info.AmazonQualified,
		TotalMatches: info.TotalMatches, MatchOffset: info.MatchOffset, MatchLimit: info.MatchLimit, ItemOffset: info.ItemOffset, ItemLimit: info.ItemLimit,
	}
	if info.AmazonItem != nil {
		item := amazonItemDocument(*info.AmazonItem)
		document.AmazonItem = &item
	}
	for _, match := range info.Matches {
		converted := AmazonMatchDocument{Class: string(match.Class), Confidence: string(match.Confidence), ProfileID: match.ProfileID, ProfileName: match.ProfileName, OrderID: match.OrderID, OrderDate: match.OrderDate.String(), OrderTotal: MoneyDocument(match.OrderTotal), DateDistanceDays: match.DateDistanceDays, AmountDifferenceMinor: strconv.FormatInt(match.AmountDifferenceMinor, 10), FirstProduct: match.FirstProduct, TotalItems: match.TotalItems}
		for _, item := range match.Items {
			converted.Items = append(converted.Items, amazonItemDocument(item))
		}
		document.Matches = append(document.Matches, converted)
	}
	return document, nil
}

func amazonItemDocument(item app.AmazonOrderItemInfo) AmazonItemDocument {
	document := AmazonItemDocument{OrderID: item.OrderID, ProductName: item.ProductName, ASIN: item.ASIN, Quantity: item.Quantity, OrderStatus: item.OrderStatus, ShipmentStatus: item.ShipmentStatus}
	if item.UnitPrice != nil {
		money := MoneyDocument(*item.UnitPrice)
		document.UnitPrice = &money
	}
	return document
}

func reviewDocument(ctx context.Context, service *app.Service, input ReviewChangesInput) (ReviewDocument, error) {
	revision, err := strconv.ParseUint(input.ExpectedRevision, 10, 64)
	if err != nil || revision == 0 {
		return ReviewDocument{}, newAppInputError(service.Revision(), errors.New("expected revision is invalid"))
	}
	operationLimit := defaultedLimit(input.OperationLimit, 100)
	if input.OperationOffset < 0 || operationLimit <= 0 || operationLimit > MaxRows {
		return ReviewDocument{}, newAppInputError(service.Revision(), errors.New("operation window is invalid"))
	}
	targetLimit := defaultedLimit(input.TargetLimit, 20)
	if targetLimit <= 0 {
		return ReviewDocument{}, newAppInputError(service.Revision(), errors.New("target window is invalid"))
	}
	projection, err := service.Review(ctx, revision, app.ReviewWindow{OperationID: input.OperationID, Offset: input.TargetOffset, Limit: targetLimit})
	if err != nil {
		return ReviewDocument{}, err
	}
	operationStart := min(input.OperationOffset, len(projection.Operations))
	operationEnd := min(operationStart+operationLimit, len(projection.Operations))
	targetTotal := 0
	if input.OperationID != "" {
		for _, operation := range projection.Operations {
			if operation.OperationID == input.OperationID {
				targetTotal = operation.AffectedCount
				break
			}
		}
	}
	return ReviewDocument{
		Header: NewHeader(StatusOK, projection.Revision), Pending: pendingDocument(projection.Pending),
		OperationWindow: CollectionWindow{Total: len(projection.Operations), Offset: operationStart, Limit: operationLimit, Returned: operationEnd - operationStart},
		Operations:      reviewOperationDocuments(projection.Operations[operationStart:operationEnd]),
		TargetWindow:    CollectionWindow{Total: targetTotal, Offset: projection.Window.Offset, Limit: projection.Window.Limit, Returned: len(projection.Targets)},
		Targets:         reviewTargetDocuments(projection.Targets),
	}, nil
}

func reviewOperationDocuments(values []app.ReviewOperation) []ReviewOperationDocument {
	result := make([]ReviewOperationDocument, 0, len(values))
	for _, value := range values {
		result = append(result, ReviewOperationDocument{
			OperationID: value.OperationID, Type: string(value.Type), Active: value.Active,
			AffectedCount: value.AffectedCount, Before: value.Before, After: value.After,
			TaxonomyEffect: value.TaxonomyEffect, Annotation: value.Annotation,
		})
	}
	return result
}

func reviewTargetDocuments(values []app.ReviewTarget) []ReviewTargetDocument {
	result := make([]ReviewTargetDocument, 0, len(values))
	for _, value := range values {
		result = append(result, ReviewTargetDocument{
			TransactionID: string(value.TransactionID), Date: value.Date.String(),
			Merchant: value.Merchant, Category: value.Category, Money: MoneyDocument(value.Amount),
			Hidden: value.Hidden,
		})
	}
	return result
}

func capabilityUnavailable(service *app.Service, detail string) ErrorDocument {
	return ErrorDocument{Header: NewHeader(StatusError, service.Revision()), Code: "capability_unavailable", Detail: detail}
}

func defaultedLimit(value *int, fallback int) int {
	if value == nil {
		return fallback
	}
	return *value
}

func newAppInputError(revision uint64, _ error) error {
	return &app.AppError{Code: app.AppInvalidOperation, Detail: "The requested operation is invalid.", CurrentRevision: revision}
}
