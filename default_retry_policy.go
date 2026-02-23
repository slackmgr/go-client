package client

import (
	"context"
	"errors"
	"net"
	"syscall"

	"github.com/go-resty/resty/v2"
)

// permanentConnErrors are syscall errors that represent definitive, immediate
// connection failures. Retrying them will always produce the same result.
//
//   - ECONNREFUSED: port not listening
//   - ENETUNREACH:  no route to destination network
//   - EHOSTUNREACH: no route to destination host
//   - EACCES:       OS or firewall actively blocking the connection
var permanentConnErrors = []syscall.Errno{ //nolint:gochecknoglobals
	syscall.ECONNREFUSED,
	syscall.ENETUNREACH,
	syscall.EHOSTUNREACH,
	syscall.EACCES,
}

// DefaultRetryPolicy is the default retry condition used by [Client]. It
// retries on HTTP 429 (rate limit) and 5xx server errors, and on transient
// connection errors. It does not retry on context cancellation, deadline
// exceeded, DNS resolution failures, or permanent connection failures
// (connection refused, network/host unreachable, permission denied).
//
// Supply a custom function via [WithRetryPolicy] to override this behaviour.
func DefaultRetryPolicy(r *resty.Response, err error) bool {
	if err != nil {
		// Don't retry on context cancellation or deadline exceeded
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return false
		}

		// Don't retry on DNS resolution errors
		var dnsErr *net.DNSError
		if errors.As(err, &dnsErr) {
			return false
		}

		// Don't retry on permanent connection failures — these are immediate,
		// deterministic rejections that will not resolve on a subsequent attempt.
		var opErr *net.OpError
		if errors.As(err, &opErr) {
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
	return r.StatusCode() == 429 || r.StatusCode() >= 500
}
