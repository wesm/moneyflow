// Package httpsecurity owns transport-neutral URL and request validation shared by HTTP surfaces.
package httpsecurity

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

var dnsLabelPattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?$`)

// OriginConfig contains one canonical HTTP URL and normalized mount path.
type OriginConfig struct {
	Canonical *url.URL
	BasePath  string
}

// Origin returns the serialized scheme and authority used by the Origin header.
func (config OriginConfig) Origin() string {
	if config.Canonical == nil {
		return ""
	}
	return config.Canonical.Scheme + "://" + config.Canonical.Host
}

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

// ResolveOrigin validates a listener, optional external URL, and their exact base-path contract.
func ResolveOrigin(listen string, basePath string, externalURL string) (OriginConfig, error) {
	normalized, err := NormalizeBasePath(basePath)
	if err != nil {
		return OriginConfig{}, fmt.Errorf("resolve origin: %w", err)
	}
	if externalURL == "" {
		if err = ValidateAuthority(listen); err != nil {
			return OriginConfig{}, fmt.Errorf("resolve origin: listener: %w", err)
		}
		canonical, parseErr := url.Parse("http://" + listen + normalized)
		if parseErr != nil {
			return OriginConfig{}, fmt.Errorf("resolve origin: listener URL: %w", parseErr)
		}
		canonicalizeOrigin(canonical)
		return OriginConfig{Canonical: canonical, BasePath: normalized}, nil
	}
	canonical, err := url.Parse(externalURL)
	if err != nil {
		return OriginConfig{}, fmt.Errorf("resolve origin: external URL: %w", err)
	}
	if canonical.Scheme != "http" && canonical.Scheme != "https" {
		return OriginConfig{}, errors.New("resolve origin: external URL must use http or https")
	}
	if canonical.User != nil || canonical.RawQuery != "" || canonical.ForceQuery || canonical.Fragment != "" {
		return OriginConfig{}, errors.New("resolve origin: external URL contains unsupported components")
	}
	if err = ValidateAuthority(canonical.Host); err != nil {
		return OriginConfig{}, fmt.Errorf("resolve origin: external URL: %w", err)
	}
	externalPath, err := NormalizeBasePath(canonical.EscapedPath())
	if err != nil || externalPath != normalized {
		return OriginConfig{}, errors.New("resolve origin: external URL path must equal base path")
	}
	canonical.Path = normalized
	canonical.RawPath = ""
	canonicalizeOrigin(canonical)
	return OriginConfig{Canonical: canonical, BasePath: normalized}, nil
}

// ValidateListen validates one explicit host and port. When loopbackOnly is true, only localhost
// and explicit loopback IPs are accepted; arbitrary DNS names are not resolved or trusted.
func ValidateListen(address string, loopbackOnly bool) (string, error) {
	if strings.ContainsAny(address, "/?#\\\x00\r\n") {
		return "", errors.New("listen address must contain only a host and port")
	}
	host, portText, err := net.SplitHostPort(address)
	if err != nil {
		return "", fmt.Errorf("invalid listen address: %w", err)
	}
	if host == "" {
		return "", errors.New("listen host is required")
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return "", errors.New("listen port must be between 1 and 65535")
	}
	if err = validateHost(host); err != nil {
		return "", err
	}
	if loopbackOnly && !IsLoopbackHost(host) {
		return "", errors.New("listen host must be an explicit loopback address")
	}
	return host, nil
}

// IsLoopbackHost reports explicit localhost and loopback IP addresses.
func IsLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// ValidateAuthority rejects wildcard, malformed, and ambiguous HTTP authorities.
func ValidateAuthority(authority string) error {
	if authority == "" || strings.ContainsAny(authority, "/?#@\x00\r\n") {
		return errors.New("origin authority is invalid")
	}
	host := authority
	if parsedHost, portText, err := net.SplitHostPort(authority); err == nil {
		host = parsedHost
		port, portErr := strconv.Atoi(portText)
		if portErr != nil || port < 1 || port > 65535 {
			return errors.New("origin authority has an invalid port")
		}
	} else if strings.Contains(authority, ":") && net.ParseIP(strings.Trim(authority, "[]")) == nil {
		return errors.New("origin authority has an invalid port")
	}
	return validateHost(strings.Trim(host, "[]"))
}

func validateHost(host string) error {
	if host == "" || strings.Contains(host, "*") {
		return errors.New("origin host is invalid")
	}
	if ip := net.ParseIP(host); ip != nil {
		if ip.IsUnspecified() {
			return errors.New("wildcard origin hosts are forbidden")
		}
		return nil
	}
	if len(host) > 253 {
		return errors.New("origin host is invalid")
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) > 63 || !dnsLabelPattern.MatchString(label) {
			return errors.New("origin host is invalid")
		}
	}
	return nil
}

func canonicalizeOrigin(value *url.URL) {
	value.Scheme = strings.ToLower(value.Scheme)
	host := strings.ToLower(value.Hostname())
	port := value.Port()
	if (value.Scheme == "http" && port == "80") || (value.Scheme == "https" && port == "443") {
		port = ""
	}
	if port != "" {
		value.Host = net.JoinHostPort(host, port)
	} else if strings.Contains(host, ":") {
		value.Host = "[" + host + "]"
	} else {
		value.Host = host
	}
}
