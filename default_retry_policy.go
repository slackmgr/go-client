package client

import (
	"context"
	"errors"
	"net"
	"net/http"
	"syscall"

	"github.com/go-resty/resty/v2"
)

// permanentConnErrors are syscall errors that represent definitive, immediate
// connection failures. Retrying them will always produce the same result.
//
// Connection refused (ECONNREFUSED) is deliberately not included: it is what
// clients typically get while the API restarts or is redeployed (e.g. a
// Kubernetes service without ready pods), and it resolves within seconds.
//
//   - ENETUNREACH:  no route to destination network
//   - EHOSTUNREACH: no route to destination host
//   - EACCES:       OS or firewall actively blocking the connection
var permanentConnErrors = []syscall.Errno{ //nolint:gochecknoglobals
	syscall.ENETUNREACH,
	syscall.EHOSTUNREACH,
	syscall.EACCES,
}

// DefaultRetryCondition is the default retry condition used by [Client] (see
// [WithRetryCondition]). It retries on HTTP 429 (rate limit), 5xx server
// errors and transient connection errors, including connection refused (as
// while the API restarts) and attempts that exceed [WithTimeout]. It does not
// retry on context cancellation, DNS resolution failures, or permanent
// connection failures (network/host unreachable, permission denied).
//
// The client never retries once the context of the send is done (cancelled or
// past its deadline), regardless of the retry condition.
//
// statusCode is the HTTP status code of the response, or 0 if no response was
// received. Call it from a custom condition to extend the default behaviour.
func DefaultRetryCondition(statusCode int, err error) bool {
	if err != nil {
		// Don't retry on context cancellation. Deadline exceeded is not checked: the error of an attempt that
		// exceeds the request timeout (WithTimeout) matches context.DeadlineExceeded, and is worth retrying. If
		// the context of the send is past its deadline, the client stops before calling the retry condition.
		if errors.Is(err, context.Canceled) {
			return false
		}

		// Don't retry on DNS resolution errors
		if _, ok := errors.AsType[*net.DNSError](err); ok {
			return false
		}

		// Don't retry on permanent connection failures — these are immediate,
		// deterministic rejections that will not resolve on a subsequent attempt.
		if opErr, ok := errors.AsType[*net.OpError](err); ok {
			for _, permErr := range permanentConnErrors {
				if errors.Is(opErr.Err, permErr) {
					return false
				}
			}
		}

		// Retry on other connection errors
		return true
	}

	// Retry on 429 (rate limit) and 5xx (server errors)
	return statusCode == http.StatusTooManyRequests || statusCode >= http.StatusInternalServerError
}

// DefaultRetryPolicy is [DefaultRetryCondition] with the signature of [WithRetryPolicy].
//
// Deprecated: Use [DefaultRetryCondition], which does not depend on resty types.
func DefaultRetryPolicy(r *resty.Response, err error) bool {
	return DefaultRetryCondition(statusCode(r), err)
}

// statusCode returns the status code of the response, or 0 if there is none.
func statusCode(r *resty.Response) int {
	if r == nil || r.RawResponse == nil {
		return 0
	}

	return r.StatusCode()
}
