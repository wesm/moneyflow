package api

import (
	"context"
	"net/http"
	"strconv"

	"github.com/danielgtaylor/huma/v2"
)

// MerchantPreviewResponse contains an exact affected count and a bounded transaction sample.
type MerchantPreviewResponse struct {
	Version              string      `json:"version"`
	Revision             string      `json:"revision" pattern:"^[0-9]+$"`
	AffectedTransactions int         `json:"affected_transactions"`
	Transactions         []DetailRow `json:"transactions"`
}

type merchantPreviewOutput struct{ Body MerchantPreviewResponse }

func (server *Server) registerMerchantPreviewEndpoint() {
	huma.Register(server.api, huma.Operation{
		OperationID: "previewMerchantEdit", Method: http.MethodPost,
		Path: server.profilePath("merchant-preview"), Summary: "Preview affected merchant transactions",
		Errors: []int{400, 403, 409, 413, 422, 500, 503},
	}, func(ctx context.Context, input *mutationInput) (*merchantPreviewOutput, error) {
		request, _, _, err := mutationToApp(input.Body)
		if err != nil {
			return nil, problemFromError(err)
		}
		preview, err := profileService(ctx).PreviewMerchantEdit(ctx, request)
		if err != nil {
			return nil, problemFromError(err)
		}
		response := MerchantPreviewResponse{
			Version: MutationSchemaVersion, Revision: strconv.FormatUint(preview.Revision, 10),
			AffectedTransactions: preview.AffectedTransactions,
			Transactions:         make([]DetailRow, 0, len(preview.Transactions)),
		}
		for _, transaction := range preview.Transactions {
			response.Transactions = append(response.Transactions, transactionToDetailRow(transaction))
		}
		return &merchantPreviewOutput{Body: response}, nil
	})
}
