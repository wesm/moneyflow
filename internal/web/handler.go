package web

import (
	"fmt"
	"html"
	"io/fs"
	"net/http"
	"strconv"
	"strings"

	"github.com/wesm/moneyflow/internal/api"
)

const (
	basePathPlaceholder   = "__MONEYFLOW_BASE_PATH__"
	baseHrefPlaceholder   = "__MONEYFLOW_BASE_HREF__"
	contentSecurityPolicy = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; " +
		"img-src 'self' data:; font-src 'self'; connect-src 'self'; object-src 'none'; " +
		"base-uri 'self'; frame-ancestors 'none'; form-action 'self'"
)

type handler struct {
	basePath     string
	distribution *distribution
}

// NewHandler constructs the static application handler from the committed production assets.
func NewHandler(basePath string) (http.Handler, error) {
	return newHandler(basePath, embeddedDistribution)
}

func newHandler(basePath string, filesystem fs.FS) (http.Handler, error) {
	normalized, err := api.NormalizeBasePath(basePath)
	if err != nil {
		return nil, fmt.Errorf("new web handler: %w", err)
	}
	distribution, err := validateDistribution(filesystem)
	if err != nil {
		return nil, fmt.Errorf("new web handler: %w", err)
	}
	return &handler{basePath: normalized, distribution: distribution}, nil
}

func (handler *handler) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	if !strings.HasPrefix(request.URL.Path, handler.basePath) {
		http.NotFound(response, request)
		return
	}
	setSecurityHeaders(response.Header())
	response.Header().Set("Cache-Control", "no-store")
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		response.Header().Set("Allow", "GET, HEAD")
		writeStatus(response, request, http.StatusMethodNotAllowed)
		return
	}

	escapedPath := strings.ToLower(request.URL.EscapedPath())
	if strings.Contains(escapedPath, "%2f") || strings.Contains(escapedPath, "%5c") ||
		strings.Contains(escapedPath, "%2e") {
		writeStatus(response, request, http.StatusNotFound)
		return
	}
	relative := strings.TrimPrefix(request.URL.Path, handler.basePath)
	if !safeRequestPath(relative) {
		writeStatus(response, request, http.StatusNotFound)
		return
	}

	if relative == "" {
		handler.serveIndex(response, request)
		return
	}
	if _, ok := handler.distribution.assets[relative]; ok {
		handler.serveAsset(response, request, relative)
		return
	}
	if isNavigation(request, relative) {
		handler.serveIndex(response, request)
		return
	}
	writeStatus(response, request, http.StatusNotFound)
}

func (handler *handler) serveIndex(response http.ResponseWriter, request *http.Request) {
	content := strings.Replace(
		string(handler.distribution.index),
		basePathPlaceholder,
		html.EscapeString(handler.basePath),
		1,
	)
	content = strings.Replace(content, baseHrefPlaceholder, html.EscapeString(handler.basePath), 1)
	response.Header().Set("Content-Type", "text/html; charset=utf-8")
	response.Header().Set("Content-Length", strconv.Itoa(len(content)))
	response.WriteHeader(http.StatusOK)
	if request.Method == http.MethodGet {
		// The only runtime value is normalized and escaped above; the remaining bytes are validated,
		// committed build output whose strict CSP disallows inline execution.
		// #nosec G705 -- content cannot include unescaped request data.
		_, _ = response.Write([]byte(content))
	}
}

func (handler *handler) serveAsset(response http.ResponseWriter, request *http.Request, name string) {
	content, err := fs.ReadFile(handler.distribution.filesystem, name)
	if err != nil {
		writeStatus(response, request, http.StatusNotFound)
		return
	}
	contentType, ok := safeContentType(name)
	if !ok {
		writeStatus(response, request, http.StatusNotFound)
		return
	}
	response.Header().Set("Content-Type", contentType)
	response.Header().Set("Content-Length", strconv.Itoa(len(content)))
	response.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	response.WriteHeader(http.StatusOK)
	if request.Method == http.MethodGet {
		// Assets are selected exclusively from the validated Vite manifest and served with a fixed,
		// extension-derived MIME type under a CSP that forbids inline execution.
		// #nosec G705 -- content is committed build output, not request-derived markup.
		_, _ = response.Write(content)
	}
}

func setSecurityHeaders(header http.Header) {
	header.Set("Content-Security-Policy", contentSecurityPolicy)
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("X-Frame-Options", "DENY")
	header.Set("Referrer-Policy", "no-referrer")
}

func safeRequestPath(name string) bool {
	if name == "" {
		return true
	}
	if !isSafeDistributionName(name) || strings.HasSuffix(name, "/") {
		return false
	}
	for _, segment := range strings.Split(name, "/") {
		lower := strings.ToLower(segment)
		if strings.HasPrefix(segment, ".") || lower == "credentials" || lower == "credential" ||
			lower == "secrets" || lower == "secret" || lower == "passwd" {
			return false
		}
	}
	return true
}

func isNavigation(request *http.Request, name string) bool {
	if request.Method != http.MethodGet || !acceptsHTML(request.Header.Get("Accept")) {
		return false
	}
	first, _, _ := strings.Cut(name, "/")
	if first == "assets" || first == "api" || name == "openapi.json" || name == "openapi.yaml" {
		return false
	}
	for _, segment := range strings.Split(name, "/") {
		if strings.Contains(segment, ".") {
			return false
		}
	}
	return true
}

func acceptsHTML(accept string) bool {
	for _, value := range strings.Split(accept, ",") {
		mediaType := strings.TrimSpace(strings.SplitN(value, ";", 2)[0])
		if mediaType == "text/html" || mediaType == "application/xhtml+xml" {
			return true
		}
	}
	return false
}

func safeContentType(name string) (string, bool) {
	for extension, contentType := range map[string]string{
		".js": "text/javascript; charset=utf-8", ".css": "text/css; charset=utf-8",
		".json": "application/json; charset=utf-8", ".svg": "image/svg+xml",
		".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg",
		".gif": "image/gif", ".webp": "image/webp", ".avif": "image/avif",
		".ico": "image/x-icon", ".woff": "font/woff", ".woff2": "font/woff2",
		".ttf": "font/ttf",
	} {
		if strings.HasSuffix(name, extension) {
			return contentType, true
		}
	}
	return "", false
}

func writeStatus(response http.ResponseWriter, request *http.Request, status int) {
	response.WriteHeader(status)
	if request.Method == http.MethodGet {
		_, _ = response.Write([]byte(http.StatusText(status) + "\n"))
	}
}
