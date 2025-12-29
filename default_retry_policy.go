package client

import (
	"context"
	"errors"

	"github.com/go-resty/resty/v2"
)

func DefaultRetryPolicy(r *resty.Response, err error) bool {
	// Retry on all connection errors, except for context.Canceled and context.DeadlineExceeded
	if err != nil {
		return !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded)
	}

	// Retry on 429 and 5xx errors
	return r.StatusCode() == 429 || r.StatusCode() >= 500
}
