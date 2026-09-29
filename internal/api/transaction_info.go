package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/danielgtaylor/huma/v2"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
)

// TransactionInformationWireVersion identifies the bounded detail contract.
const TransactionInformationWireVersion = "1"

// TransactionInformationBody requests bounded detail for one visible transaction.
type TransactionInformationBody struct {
	Version          string           `json:"version"`
	ExpectedRevision string           `json:"expected_revision" pattern:"^[0-9]+$" maxLength:"20"`
	Query            string           `json:"query" maxLength:"65536"`
	Target           TransitionTarget `json:"target"`
	MatchWindow      Window           `json:"match_window"`
	ItemWindow       Window           `json:"item_window"`
}

// AmazonOrderItemInformation is one bounded Amazon order-item projection.
type AmazonOrderItemInformation struct {
	OrderID        string `json:"order_id"`
	ProductName    string `json:"product_name"`
	ASIN           string `json:"asin,omitempty"`
	Quantity       string `json:"quantity" pattern:"^[0-9]+$"`
	OrderStatus    string `json:"order_status,omitempty"`
	ShipmentStatus string `json:"shipment_status,omitempty"`
	UnitPrice      *Money `json:"unit_price,omitempty"`
}

// TransactionInformationMatch is one deterministic Amazon-order match candidate.
type TransactionInformationMatch struct {
	Class                 string                       `json:"class"`
	Confidence            string                       `json:"confidence"`
	OrderID               string                       `json:"order_id"`
	OrderDate             string                       `json:"order_date" format:"date"`
	OrderTotal            Money                        `json:"order_total"`
	DateDistanceDays      int                          `json:"date_distance_days"`
	AmountDifferenceMinor string                       `json:"amount_difference_minor" pattern:"^-?[0-9]+$"`
	FirstProduct          string                       `json:"first_product"`
	TotalItems            int                          `json:"total_items"`
	Items                 []AmazonOrderItemInformation `json:"items"`
}

// TransactionInformationResponse combines transaction detail with bounded Amazon context.
type TransactionInformationResponse struct {
	Version         string                        `json:"version"`
	Revision        string                        `json:"revision" pattern:"^[0-9]+$"`
	CanonicalQuery  string                        `json:"canonical_query"`
	Transaction     DetailRow                     `json:"transaction"`
	AmazonQualified bool                          `json:"amazon_qualified"`
	AmazonItem      *AmazonOrderItemInformation   `json:"amazon_item,omitempty"`
	TotalMatches    int                           `json:"total_matches"`
	MatchWindow     ReturnedWindow                `json:"match_window"`
	ItemWindow      ReturnedWindow                `json:"item_window"`
	Matches         []TransactionInformationMatch `json:"matches"`
}

type transactionInformationInput struct {
	ProfileID string `path:"profile_id"`
	Body      TransactionInformationBody
}
type transactionInformationOutput struct {
	Body TransactionInformationResponse
}

func (server *Server) registerTransactionInformationEndpoint() {
	huma.Register(server.api, huma.Operation{
		OperationID: "readTransactionInformation", Method: http.MethodPost,
		Path: server.profilePath("transaction-information"), Summary: "Read bounded transaction information",
		Errors: []int{400, 404, 409, 413, 422, 500},
	}, func(ctx context.Context, input *transactionInformationInput) (*transactionInformationOutput, error) {
		body := input.Body
		if body.Version != TransactionInformationWireVersion || body.Target.Kind != app.IdentityTransaction {
			return nil, problemFromError(invalidMutationRequest(errors.New("invalid transaction information request")))
		}
		revision, err := parseRevision(body.ExpectedRevision)
		if err != nil {
			return nil, problemFromError(invalidMutationRequest(err))
		}
		_, canonical, err := DecodeViewQuery(body.Query)
		if err != nil {
			return nil, err
		}
		info, err := profileService(ctx).TransactionInfo(ctx, app.TransactionInfoRequest{
			ExpectedRevision: revision, TransactionID: body.Target.Identity,
			MatchOffset: body.MatchWindow.Offset, MatchLimit: body.MatchWindow.Limit,
			ItemOffset: body.ItemWindow.Offset, ItemLimit: body.ItemWindow.Limit,
		})
		if err != nil {
			return nil, problemFromError(err)
		}
		return &transactionInformationOutput{Body: transactionInformationToWire(info, canonical)}, nil
	})
}

func transactionInformationToWire(info app.TransactionInfo, canonical string) TransactionInformationResponse {
	response := TransactionInformationResponse{
		Version: TransactionInformationWireVersion, Revision: strconv.FormatUint(info.Revision, 10),
		CanonicalQuery: canonical, Transaction: transactionToDetailRow(info.Transaction),
		AmazonQualified: info.AmazonQualified, TotalMatches: info.TotalMatches,
		MatchWindow: ReturnedWindow{Offset: info.MatchOffset, Limit: info.MatchLimit, Count: len(info.Matches)},
		ItemWindow:  ReturnedWindow{Offset: info.ItemOffset, Limit: info.ItemLimit},
		Matches:     make([]TransactionInformationMatch, 0, len(info.Matches)),
	}
	if info.AmazonItem != nil {
		item := amazonItemInformationToWire(*info.AmazonItem)
		response.AmazonItem = &item
	}
	for _, match := range info.Matches {
		wire := TransactionInformationMatch{Class: string(match.Class), Confidence: string(match.Confidence), OrderID: match.OrderID, OrderDate: match.OrderDate.String(), OrderTotal: moneyToWire(match.OrderTotal), DateDistanceDays: match.DateDistanceDays, AmountDifferenceMinor: strconv.FormatInt(match.AmountDifferenceMinor, 10), FirstProduct: match.FirstProduct, TotalItems: match.TotalItems, Items: make([]AmazonOrderItemInformation, 0, len(match.Items))}
		for _, item := range match.Items {
			wire.Items = append(wire.Items, amazonItemInformationToWire(item))
		}
		response.Matches = append(response.Matches, wire)
	}
	return response
}

func transactionToDetailRow(transaction domain.Transaction) DetailRow {
	return DetailRow{Identity: transaction.ID, Date: transaction.Date.String(), Account: transaction.Account.Name, Merchant: transaction.Merchant.Name, Category: transaction.Category.Name, Group: transaction.Category.Group, Amount: moneyToWire(transaction.Amount), Flags: Flags{Hidden: transaction.Hidden, Pending: transaction.Pending}}
}

func amazonItemInformationToWire(item app.AmazonOrderItemInfo) AmazonOrderItemInformation {
	result := AmazonOrderItemInformation{OrderID: item.OrderID, ProductName: item.ProductName, ASIN: item.ASIN, Quantity: strconv.FormatInt(item.Quantity, 10), OrderStatus: item.OrderStatus, ShipmentStatus: item.ShipmentStatus}
	if item.UnitPrice != nil {
		price := moneyToWire(*item.UnitPrice)
		result.UnitPrice = &price
	}
	return result
}
