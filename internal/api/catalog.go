package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/danielgtaylor/huma/v2"

	"github.com/wesm/moneyflow/internal/app"
)

const editorCatalogLimit = 10000

var errEditorCatalogTooLarge = errors.New("editor catalog exceeds the bounded response limit")

// EditorCatalogBody requests active editor choices at one exact profile revision.
type EditorCatalogBody struct {
	Version          string `json:"version"`
	ExpectedRevision string `json:"expected_revision" pattern:"^[0-9]+$" maxLength:"20"`
}

// EditorChoice is one stable, renderer-safe taxonomy or merchant choice.
type EditorChoice struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	ParentID  string `json:"parent_id,omitempty"`
	Protected bool   `json:"protected"`
}

// EditorCatalogResponse returns bounded active editor choices without transaction data.
type EditorCatalogResponse struct {
	Version    string         `json:"version"`
	Revision   string         `json:"revision" pattern:"^[0-9]+$"`
	Merchants  []EditorChoice `json:"merchants"`
	Categories []EditorChoice `json:"categories"`
	Groups     []EditorChoice `json:"groups"`
}

type editorCatalogInput struct {
	ProfileID string `path:"profile_id"`
	Body      EditorCatalogBody
}
type editorCatalogOutput struct{ Body EditorCatalogResponse }

func (server *Server) registerEditorCatalogEndpoint(_ Config) {
	huma.Register(server.api, huma.Operation{
		OperationID: "editorCatalog", Method: http.MethodPost,
		Path: server.profilePath("editor-catalog"), Summary: "Load bounded active editor choices",
		Errors: []int{400, 403, 409, 413, 422, 500, 503},
	}, func(ctx context.Context, input *editorCatalogInput) (*editorCatalogOutput, error) {
		expected, err := reviewRevision(input.Body.Version, input.Body.ExpectedRevision)
		if err != nil {
			return nil, problemFromError(err)
		}
		catalog, err := profileService(ctx).EditorCatalogAt(ctx, expected)
		if err != nil {
			return nil, problemFromError(err)
		}
		if len(catalog.Merchants) > editorCatalogLimit || len(catalog.Categories) > editorCatalogLimit || len(catalog.Groups) > editorCatalogLimit {
			return nil, problemFromError(invalidMutationRequest(errEditorCatalogTooLarge))
		}
		return &editorCatalogOutput{Body: EditorCatalogResponse{
			Version: MutationSchemaVersion, Revision: strconv.FormatUint(expected, 10),
			Merchants: editorChoicesToWire(catalog.Merchants), Categories: editorChoicesToWire(catalog.Categories),
			Groups: editorChoicesToWire(catalog.Groups),
		}}, nil
	})
}

func editorChoicesToWire(choices []app.EditorChoice) []EditorChoice {
	result := make([]EditorChoice, 0, len(choices))
	for _, choice := range choices {
		result = append(result, EditorChoice{
			ID: string(choice.ID), Label: choice.Label, ParentID: string(choice.ParentID),
			Protected: choice.Protected,
		})
	}
	return result
}
