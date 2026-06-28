package wps

// HTTPError represents a non-200 response returned from the WPS service.
type HTTPError struct {
	body string
	code int
}

// Error returns the WPS error response body.
func (e HTTPError) Error() string { return e.body }

// StatusCode returns the HTTP status code from the WPS response.
func (e HTTPError) StatusCode() int { return e.code }

// IsHTTPError reports whether the error is a WPS non-200 HTTP response.
func IsHTTPError(err error) bool {
	if err == nil {
		return false
	}
	switch err.(type) {
	case *HTTPError, HTTPError:
		return true
	default:
		return false
	}
}
