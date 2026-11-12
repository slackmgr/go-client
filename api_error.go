package client

import (
	"errors"
	"fmt"
	"net/http"
	"time"
)

// ErrRateLimited matches, via [errors.Is], an [*APIError] with status code 429 (Too Many Requests).
// Use [errors.As] to get the [*APIError] itself, for example to read [APIError.RetryAfter].
var ErrRateLimited = errors.New("rate limited")

// APIError is returned by [Client.Send], [Client.SendWithResponse] and [Client.Ping] (and wrapped by
// [Client.Connect]) when the API responds with a non-2xx status code, after any retries. It is also returned,
// with status 429 and [APIError.NotSent] set, when client-side rate limit pacing does not send a request. When
// the context is done while waiting for a retry, it is wrapped together with the context error.
type APIError struct {
	// Method is the HTTP method of the failed request.
	Method string

	// URL is the request URL, with any credentials redacted.
	URL string

	// StatusCode is the HTTP status code of the last response.
	StatusCode int

	// Message is the error message from the response body, or the raw body if it is not a JSON error response.
	Message string

	// RetryAfter is the wait time requested by the Retry-After header of the last response, relative to when
	// it was received (capped at 24 hours), or the predicted wait when [APIError.NotSent] is true. It is zero
	// when the header is absent or invalid.
	RetryAfter time.Duration

	// Attempts is the number of requests made, including retries.
	Attempts int

	// NotSent is true when the client did not send the (last) request, because its model of the API's rate
	// limiter predicted a 429 and the wait exceeded [WithMaxRateLimitWait] or the context deadline. See
	// [WithRateLimitPacing].
	NotSent bool
}

// Error implements the error interface.
func (e *APIError) Error() string {
	return fmt.Sprintf("%s %s failed with status code %d: %s", e.Method, e.URL, e.StatusCode, e.Message)
}

// Is reports whether the error matches target. It matches [ErrRateLimited] when the status code is 429.
func (e *APIError) Is(target error) bool {
	return target == ErrRateLimited && e.StatusCode == http.StatusTooManyRequests
}
