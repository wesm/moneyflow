package api

import (
	"cmp"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/wesm/moneyflow/internal/access"
)

// Operation names one API route and the authority it needs.
type Operation struct {
	ID     string
	Method string
	// Path is absolute: it begins with the API base path and uses ServeMux {name} wildcards.
	Path  string
	Class access.Class
}

var operationClasses = map[string]access.Class{
	"bootstrap":                     access.Read,
	"profileBootstrap":              access.Read,
	"health":                        access.Read,
	"listProfiles":                  access.Read,
	"projectView":                   access.Read,
	"transitionView":                access.Read,
	"projectInsights":               access.Read,
	"reviewProfile":                 access.Read,
	"reviewProfileTargets":          access.Read,
	"projectProfileDuplicates":      access.Read,
	"readTransactionInformation":    access.Read,
	"editorCatalog":                 access.Read,
	"previewMerchantEdit":           access.Read,
	"previewProfileExport":          access.Read,
	"providerWriteStatus":           access.Read,
	"openAPIJSON":                   access.Read,
	"openAPIYAML":                   access.Read,
	"createProfile":                 access.Stage,
	"activateProfile":               access.Stage,
	"cancelNewProfile":              access.Stage,
	"mutateProfile":                 access.Stage,
	"undoProfile":                   access.Stage,
	"redoProfile":                   access.Stage,
	"downloadProfileExport":         access.Stage,
	"providerStatus":                access.Stage,
	"refreshProvider":               access.Stage,
	"startProfileOnboarding":        access.Stage,
	"submitProfileOnboarding":       access.Stage,
	"profileOnboardingStatus":       access.Stage,
	"cancelProfileOnboarding":       access.Stage,
	"startSimpleFINOnboarding":      access.Stage,
	"submitSimpleFINOnboarding":     access.Stage,
	"readSimpleFINOnboardingStatus": access.Stage,
	"cancelSimpleFINOnboarding":     access.Stage,
	"startAmazonImport":             access.Stage,
	"stageAmazonImportFiles":        access.Stage,
	"executeAmazonImport":           access.Stage,
	"readAmazonImportStatus":        access.Stage,
	"cancelAmazonImport":            access.Stage,
	"commitProfile":                 access.Commit,
	"confirmProviderRefresh":        access.Commit,
	"pauseProviderWrite":            access.Commit,
	"resumeProviderWrite":           access.Commit,
	"reconcileProviderWrite":        access.Commit,
	"confirmProviderWriteReconcile": access.Commit,
	"recoverProfile":                access.Commit,
	"removeProfile":                 access.Commit,
}

// Operations lists every route the server answers, sorted by path and then method.
func (server *Server) Operations() []Operation {
	return slices.Clone(server.operations)
}

func (server *Server) collectOperations(includeLegacy bool) ([]Operation, error) {
	operations := []Operation{
		{ID: "openAPIJSON", Method: http.MethodGet, Path: server.basePath + "openapi.json"},
		{ID: "openAPIYAML", Method: http.MethodGet, Path: server.basePath + "openapi.yaml"},
	}
	for _, item := range server.api.OpenAPI().Paths {
		for _, operation := range []*huma.Operation{
			item.Get, item.Put, item.Post, item.Delete, item.Options, item.Head, item.Patch, item.Trace,
		} {
			if operation != nil {
				operations = append(operations, Operation{
					ID: operation.OperationID, Method: operation.Method, Path: operation.Path,
				})
			}
		}
	}
	for index := range operations {
		class, ok := operationClasses[operations[index].ID]
		if !ok {
			return nil, fmt.Errorf("operation %s has no class", operations[index].ID)
		}
		operations[index].Class = class
	}
	if includeLegacy {
		for _, operation := range operations {
			endpoint, profileRoute := strings.CutPrefix(operation.Path, server.profilePath(""))
			if _, legacy := legacyProfileEndpoints[endpoint]; profileRoute && legacy {
				operation.Path = server.basePath + "api/v1/" + endpoint
				operations = append(operations, operation)
			}
		}
	}
	slices.SortFunc(operations, func(left, right Operation) int {
		return cmp.Or(strings.Compare(left.Path, right.Path), strings.Compare(left.Method, right.Method))
	})
	return operations, nil
}
