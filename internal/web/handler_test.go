package web

import (
	"bytes"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/api"
)

const testCSP = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data:; font-src 'self'; connect-src 'self'; object-src 'none'; " +
	"base-uri 'self'; frame-ancestors 'none'; form-action 'self'"

func TestValidateDistribution(t *testing.T) {
	t.Parallel()
	err := ValidateDistribution(testDistribution())
	require.NoError(t, err)

	tests := []struct {
		name   string
		mutate func(fstest.MapFS)
	}{
		{name: "missing index", mutate: func(files fstest.MapFS) { delete(files, "dist/index.html") }},
		{name: "missing manifest", mutate: func(files fstest.MapFS) { delete(files, "dist/.vite/manifest.json") }},
		{name: "missing marker", mutate: func(files fstest.MapFS) { delete(files, "dist/.moneyflow-production.json") }},
		{name: "forged marker", mutate: func(files fstest.MapFS) {
			files["dist/.moneyflow-production.json"] = &fstest.MapFile{Data: []byte(`{"kind":"placeholder"}`)}
		}},
		{name: "missing asset", mutate: func(files fstest.MapFS) { delete(files, "dist/assets/index-Ab12_cd3.js") }},
		{name: "source map", mutate: func(files fstest.MapFS) {
			files["dist/assets/index-Ab12_cd3.js.map"] = &fstest.MapFile{Data: []byte("{}")}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			files := testDistribution()
			test.mutate(files)
			err := ValidateDistribution(files)
			assert.Error(t, err)
		})
	}
}

func TestHandlerServesIndexAssetsAndHEAD(t *testing.T) {
	t.Parallel()
	handler := newTestHandler(t, "/money&flow/")

	index := request(t, handler, http.MethodGet, "/money&flow/", "text/html")
	assert.Equal(t, http.StatusOK, index.Code)
	assert.Equal(t, "text/html; charset=utf-8", index.Header().Get("Content-Type"))
	assert.Equal(t, "no-store", index.Header().Get("Cache-Control"))
	assert.Contains(t, index.Body.String(), `content="/money&amp;flow/"`)
	assert.Contains(t, index.Body.String(), `<base href="/money&amp;flow/"`)
	assert.Contains(t, index.Body.String(), `name="moneyflow-mutation-token"`)
	assert.NotContains(t, index.Body.String(), mutationTokenPlaceholder)
	assert.NotContains(t, index.Body.String(), originWarningPlaceholder)
	assert.NotContains(t, index.Body.String(), "__MONEYFLOW_BASE_PATH__")
	assertSecurityHeaders(t, index)

	asset := request(t, handler, http.MethodGet, "/money&flow/assets/index-Ab12_cd3.js", "*/*")
	assert.Equal(t, http.StatusOK, asset.Code)
	assert.Equal(t, "text/javascript; charset=utf-8", asset.Header().Get("Content-Type"))
	assert.Equal(t, "public, max-age=31536000, immutable", asset.Header().Get("Cache-Control"))
	assert.Equal(t, "compiled", asset.Body.String())

	head := request(t, handler, http.MethodHead, "/money&flow/assets/index-Ab12_cd3.js", "*/*")
	assert.Equal(t, http.StatusOK, head.Code)
	assert.Empty(t, head.Body.String())
	assert.Equal(t, asset.Header().Get("Content-Length"), head.Header().Get("Content-Length"))
}

func TestHandlerNavigationFallbackAndReservations(t *testing.T) {
	t.Parallel()
	handler := newTestHandler(t, "/moneyflow/")

	for _, path := range []string{"/moneyflow/accounts", "/moneyflow/transactions/detail"} {
		response := request(t, handler, http.MethodGet, path, "text/html,application/xhtml+xml")
		assert.Equal(t, http.StatusOK, response.Code, path)
		assert.Contains(t, response.Body.String(), "moneyflow-base-path")
		assert.Contains(t, response.Body.String(), `<base href="/moneyflow/"`)
	}
	for _, path := range []string{
		"/moneyflow/assets/missing-Ab12_cd3.js",
		"/moneyflow/api/v1/missing",
		"/moneyflow/openapi.json",
		"/moneyflow/openapi.yaml",
		"/moneyflow/route.with-dot",
		"/outside",
	} {
		response := request(t, handler, http.MethodGet, path, "text/html")
		assert.Equal(t, http.StatusNotFound, response.Code, path)
	}
	assert.Equal(t, http.StatusNotFound, request(t, handler, http.MethodGet, "/moneyflow/accounts", "application/json").Code)
	assert.Equal(t, http.StatusNotFound, request(t, handler, http.MethodHead, "/moneyflow/accounts", "text/html").Code)
}

func TestHandlerRejectsUnsafePathsAndMethods(t *testing.T) {
	t.Parallel()
	handler := newTestHandler(t, "/")

	for _, path := range []string{
		"/.env", "/credentials", "/assets/.secret", "/../index.html", "/%2e%2e/index.html",
		"/assets%2findex-Ab12_cd3.js", "/assets%5cindex-Ab12_cd3.js", "/assets\\index-Ab12_cd3.js",
	} {
		response := request(t, handler, http.MethodGet, path, "text/html")
		assert.Equal(t, http.StatusNotFound, response.Code, path)
	}
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodOptions} {
		response := request(t, handler, method, "/", "text/html")
		assert.Equal(t, http.StatusMethodNotAllowed, response.Code, method)
		assert.Equal(t, "GET, HEAD", response.Header().Get("Allow"))
	}
}

func TestEmbeddedDistributionConstructs(t *testing.T) {
	t.Parallel()
	handler, err := NewHandler("/")
	require.NoError(t, err)
	response := request(t, handler, http.MethodGet, "/", "text/html")
	assert.Equal(t, http.StatusOK, response.Code)
}

func TestHandlerWarnsOnDirectListenerAndLinksCanonicalOrigin(t *testing.T) {
	t.Parallel()
	origin, err := api.ResolveOrigin(
		"127.0.0.1:8080", "/moneyflow", "https://moneyflow.example/moneyflow",
	)
	require.NoError(t, err)
	security, err := api.NewMutationSecurity(
		origin, bytes.NewReader(bytes.Repeat([]byte{0x42}, 32)), nil,
	)
	require.NoError(t, err)
	handler, err := newHandler("/moneyflow", testDistribution(), origin, security, true)
	require.NoError(t, err)

	direct := request(t, handler, http.MethodGet, "/moneyflow/", "text/html")
	assert.Contains(t, direct.Body.String(), "This listener is read-only")
	assert.Contains(t, direct.Body.String(), `href="https://moneyflow.example/moneyflow/"`)
	canonicalRequest := httptest.NewRequest(http.MethodGet, "/moneyflow/", http.NoBody)
	canonicalRequest.Host = "moneyflow.example"
	canonicalRequest.Header.Set("Accept", "text/html")
	canonical := httptest.NewRecorder()
	handler.ServeHTTP(canonical, canonicalRequest)
	assert.NotContains(t, canonical.Body.String(), "This listener is read-only")
}

func testDistribution() fstest.MapFS {
	return fstest.MapFS{
		"dist/index.html":                 {Data: []byte(`<!doctype html><base href="__MONEYFLOW_BASE_HREF__"><meta name="moneyflow-base-path" content="__MONEYFLOW_BASE_PATH__"><meta name="moneyflow-mutation-token" content="__MONEYFLOW_MUTATION_TOKEN__"><link rel="canonical" href="__MONEYFLOW_CANONICAL_URL__"><body>__MONEYFLOW_ORIGIN_WARNING__<script src="./assets/index-Ab12_cd3.js"></script>`)},
		"dist/.moneyflow-production.json": {Data: []byte(`{"schema_version":1,"kind":"moneyflow-production","entry":"index.html"}`)},
		"dist/.vite/manifest.json":        {Data: []byte(`{"index.html":{"file":"assets/index-Ab12_cd3.js","isEntry":true,"src":"index.html"}}`)},
		"dist/assets/index-Ab12_cd3.js":   {Data: []byte("compiled")},
	}
}

func newTestHandler(t testing.TB, basePath string) http.Handler {
	t.Helper()
	origin, err := api.ResolveOrigin("example.com:80", basePath, "")
	require.NoError(t, err)
	security, err := api.NewMutationSecurity(
		origin, bytes.NewReader(bytes.Repeat([]byte{0x42}, 32)), nil,
	)
	require.NoError(t, err)
	handler, err := newHandler(basePath, testDistribution(), origin, security, false)
	require.NoError(t, err)
	return handler
}

func request(t testing.TB, handler http.Handler, method, path, accept string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, http.NoBody)
	req.Header.Set("Accept", accept)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	return response
}

func assertSecurityHeaders(t testing.TB, response *httptest.ResponseRecorder) {
	t.Helper()
	assert.Equal(t, testCSP, response.Header().Get("Content-Security-Policy"))
	assert.Equal(t, "nosniff", response.Header().Get("X-Content-Type-Options"))
	assert.Equal(t, "DENY", response.Header().Get("X-Frame-Options"))
	assert.Equal(t, "no-referrer", response.Header().Get("Referrer-Policy"))
	assert.Empty(t, response.Header().Values("Set-Cookie"))
	assert.Contains(t, response.Header().Get("Content-Security-Policy"), "style-src 'self' 'unsafe-inline'")
	assert.NotContains(t, response.Header().Get("Content-Security-Policy"), "script-src 'self' 'unsafe-inline'")
}

var _ fs.FS = fstest.MapFS{}
