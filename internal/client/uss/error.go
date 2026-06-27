package uss

import "strings"

// HTTPError represents a non-200 response returned from the USS service.
type HTTPError struct {
	body string
	code int
}

// Error returns the USS error response body.
func (e HTTPError) Error() string { return e.body }

// StatusCode returns the HTTP status code from the USS response.
func (e HTTPError) StatusCode() int { return e.code }

// IsHTTPError reports whether the error is a USS non-200 HTTP response.
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

// IsProfileNotFound reports whether the error indicates a missing customer profile.
func IsProfileNotFound(err error) bool {
	if err == nil {
		return false
	}
	switch e := err.(type) {
	case *HTTPError:
		return isProfileNotFoundBody(e.body)
	case HTTPError:
		return isProfileNotFoundBody(e.body)
	default:
		return false
	}
}

func isProfileNotFoundBody(body string) bool {
	if body == "" {
		return false
	}
	if strings.Contains(body, "uss-ae.profile.data_not_found") {
		return true
	}
	return strings.Contains(body, "profile.data_not_found")
}
