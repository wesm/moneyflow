package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/amazonimport"
	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/importer/amazon"
	"github.com/wesm/moneyflow/internal/profilecatalog"
)

func TestAmazonImportExecuteNamesWhyTheInstallFailed(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		err    error
		status int
		code   string
		logged bool
	}{
		{
			name:   "currency differs from the profile",
			err:    fmt.Errorf("plan: %w", app.ErrAmazonSettingsMismatch),
			status: http.StatusUnprocessableEntity, code: string(CodeAmazonCurrencyMismatch),
		},
		{
			name:   "profile changed during the import",
			err:    &app.AppError{Code: app.AppStoreBusy, Detail: "The profile is busy."},
			status: http.StatusConflict, code: string(CodeAmazonProfileChanged),
		},
		{
			name:   "profile could not be written",
			err:    fmt.Errorf("disk-full-detail: %w", &app.AppError{Code: app.AppStoreError}),
			status: http.StatusInternalServerError, code: string(CodeAmazonImportStorage),
			logged: true,
		},
		{
			name:   "unexpected failure",
			err:    errors.New("unexpected-install-detail"),
			status: http.StatusInternalServerError, code: "internal_error", logged: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			server, logged := newLoggedServer(t, amazonImportConfig(t, test.err))
			attempt := startAmazonImport(t, server, AmazonImportStartBody{
				Version: AmazonImportWireVersion, Currency: "USD", Scale: 2,
			})
			staged := stageAmazonImport(t, server, attempt)

			path, err := ProfileAPIPath("/", testProfileID, "amazon-import/"+staged.AttemptID+"/execute")
			require.NoError(t, err)
			response := requestScopedMutation(t, server, testProfileID, path, AmazonImportAttemptBody{
				Version: AmazonImportWireVersion, ExpectedStateVersion: staged.StateVersion,
			})

			require.Equal(t, test.status, response.Code, response.Body.String())
			var problem Problem
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &problem))
			assert.Equal(t, test.code, problem.Code)
			assert.NotEmpty(t, problem.Detail)
			assert.NotContains(t, response.Body.String(), "-detail")
			if test.logged {
				assert.Contains(t, logged.String(), "-detail")
			}
		})
	}
}

func TestAmazonImportStartSaysWhenTheCategorySourceCannotBeRead(t *testing.T) {
	t.Parallel()
	_, missing := profilecatalog.ResolveEntries(nil, otherProfileID)
	require.Error(t, missing)
	tests := []struct {
		name   string
		err    error
		status int
		detail string
		logged bool
	}{
		{
			name: "source profile is missing", err: fmt.Errorf("resolve: %w", missing),
			status: http.StatusUnprocessableEntity, detail: "was not found",
		},
		{
			name: "source profile cannot be read", err: errors.New("taxonomy-source-detail"),
			status: http.StatusInternalServerError, detail: "could not read the categories",
			logged: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			config := amazonImportConfig(t, nil)
			config.LoadAmazonTaxonomy = func(context.Context, string) (*app.TaxonomyClone, error) {
				return nil, test.err
			}
			server, logged := newLoggedServer(t, config)
			path, err := ProfileAPIPath("/", testProfileID, "amazon-import/start")
			require.NoError(t, err)

			response := requestScopedMutation(t, server, testProfileID, path, AmazonImportStartBody{
				Version: AmazonImportWireVersion, Currency: "USD", Scale: 2,
				TaxonomySourceID: otherProfileID,
			})

			require.Equal(t, test.status, response.Code, response.Body.String())
			var problem Problem
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &problem))
			assert.Equal(t, string(CodeAmazonTaxonomySourceInvalid), problem.Code)
			assert.Contains(t, problem.Detail, test.detail)
			assert.NotContains(t, response.Body.String(), "taxonomy-source-detail")
			assert.Equal(t, test.logged, strings.Contains(logged.String(), "taxonomy-source-detail"))
		})
	}
}

func amazonImportConfig(t *testing.T, installErr error) Config {
	t.Helper()
	service, err := app.NewService(nil)
	require.NoError(t, err)
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	coordinator, err := amazonimport.New(amazonimport.Config{
		InstanceID: "server-a", Now: time.Now,
		Random: strings.NewReader(strings.Repeat("a", 256)), Limits: amazon.ProductionLimits,
		ResolveTarget: func(context.Context, string) (amazonimport.Target, error) {
			return amazonimport.Target{
				ProfileID: testProfileID, Root: root,
				Import: func(context.Context, app.AmazonImportRequest) (app.AmazonImportResult, error) {
					return app.AmazonImportResult{}, installErr
				},
			}, nil
		},
		Discover: amazon.DiscoverDirectory,
		Parse: func(
			context.Context, []amazon.SourceFile, amazon.Settings, amazon.Limits, amazon.ObserveFunc,
		) (amazon.Candidate, error) {
			return amazon.Candidate{Digest: "candidate", ObservedOrderIDs: []string{"order"}}, nil
		},
	})
	require.NoError(t, err)
	return Config{
		Resolver: resolverForService(testProfileID, service), AmazonImports: coordinator,
	}
}

func startAmazonImport(
	t *testing.T, server *Server, body AmazonImportStartBody,
) AmazonImportStatusResponse {
	t.Helper()
	path, err := ProfileAPIPath("/", testProfileID, "amazon-import/start")
	require.NoError(t, err)
	response := requestScopedMutation(t, server, testProfileID, path, body)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var started AmazonImportStatusResponse
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &started))
	return started
}

func stageAmazonImport(
	t *testing.T, server *Server, attempt AmazonImportStatusResponse,
) AmazonImportStatusResponse {
	t.Helper()
	path, err := ProfileAPIPath("/", testProfileID, "amazon-import/"+attempt.AttemptID+"/files")
	require.NoError(t, err)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("version", AmazonImportWireVersion))
	require.NoError(t, writer.WriteField("expected_state_version", attempt.StateVersion))
	file, err := writer.CreateFormFile("files", "Retail.OrderHistory.1.csv")
	require.NoError(t, err)
	_, err = file.Write([]byte("header\n"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	issued, err := server.security.Issue(testProfileID)
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodPost, path, &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set("Origin", server.security.origin.Origin())
	request.Header.Set("Sec-Fetch-Site", "same-origin")
	request.Header.Set(MutationTokenHeader, issued.Value)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var staged AmazonImportStatusResponse
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &staged))
	return staged
}
