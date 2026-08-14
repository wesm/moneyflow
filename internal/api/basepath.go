package api

import (
	"errors"
	"net/url"
	"strings"
)

// NormalizeBasePath returns one leading and trailing slash for a safe mount path.
func NormalizeBasePath(input string) (string, error) {
	if input == "" || input == "/" {
		return "/", nil
	}
	if strings.ContainsAny(input, "?#\\\x00\r\n") {
		return "", errors.New("base path contains a query, fragment, backslash, or control character")
	}
	lower := strings.ToLower(input)
	if strings.Contains(lower, "%2f") || strings.Contains(lower, "%5c") {
		return "", errors.New("base path contains an encoded slash or backslash")
	}
	decoded, err := url.PathUnescape(input)
	if err != nil {
		return "", errors.New("base path contains invalid escaping")
	}
	if strings.Contains(decoded, "%") {
		return "", errors.New("base path contains nested escaping")
	}
	if strings.ContainsAny(decoded, "?#\\\x00\r\n") {
		return "", errors.New("decoded base path contains a query, fragment, backslash, or control character")
	}
	if strings.Contains(decoded, "://") || strings.HasPrefix(decoded, "//") {
		return "", errors.New("base path must not be an absolute URL")
	}
	trimmed := strings.Trim(decoded, "/")
	if trimmed == "" {
		return "/", nil
	}
	segments := strings.Split(trimmed, "/")
	for _, segment := range segments {
		if segment == "" || segment == "." || segment == ".." {
			return "", errors.New("base path contains an empty or dot segment")
		}
	}
	return "/" + strings.Join(segments, "/") + "/", nil
}
