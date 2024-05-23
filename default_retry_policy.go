package client

import "github.com/go-resty/resty/v2"

func DefaultRetryPolicy(r *resty.Response, err error) bool {
	// Retry on all connection errors
	if err != nil {
		return true
	}

	// Retry on 429 and 5xx errors
	return r.StatusCode() == 429 || r.StatusCode() >= 500
}
