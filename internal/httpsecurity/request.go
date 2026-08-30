package httpsecurity

import (
	"errors"
	"net/http"
)

// ValidateCanonicalHost requires the request authority to equal the configured canonical host.
func ValidateCanonicalHost(request *http.Request, origin OriginConfig) error {
	if request == nil || origin.Canonical == nil || request.Host != origin.Canonical.Host {
		return errors.New("request host is not canonical")
	}
	return nil
}

// ValidateOptionalOrigin permits no Origin header for non-browser clients and otherwise requires
// one exact canonical value. Multiple, null, and comma-joined values are rejected.
func ValidateOptionalOrigin(request *http.Request, origin OriginConfig) error {
	if request == nil {
		return errors.New("request is nil")
	}
	values := request.Header.Values("Origin")
	if len(values) == 0 {
		return nil
	}
	if len(values) != 1 || values[0] == "" || values[0] == "null" || values[0] != origin.Origin() {
		return errors.New("request origin is not canonical")
	}
	return nil
}
