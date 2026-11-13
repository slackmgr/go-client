package client

import (
	"context"
	"fmt"
	"math"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
)

// execute sends the request, retrying according to the retry policy. It returns the response to the last attempt
// (nil if no attempt got one), and an error if the last attempt failed: an [*APIError] for a non-2xx response, and
// the wrapped transport error otherwise. When the context is done while waiting for a retry, the error also wraps
// the context error, so that both [errors.Is] with the context error and [errors.As] with the last error work.
//
// The wait between attempts is the Retry-After header value when present (up to maxRateLimitWait), plus up to
// [retryAfterJitter] more, and capped exponential backoff with jitter otherwise. Retrying stops early when the API
// asks for a longer wait than maxRateLimitWait, or when the wait would exceed the context deadline, as the retry
// could not succeed anyway.
func (c *Client) execute(ctx context.Context, client *resty.Client, method, path string, body []byte) (*resty.Response, error) {
	for attempt := 1; ; attempt++ {
		request := client.R().SetContext(ctx)
		if body != nil {
			request.SetBody(body)
		}

		response, err := request.Execute(method, path)

		var lastErr error

		switch {
		case err != nil:
			lastErr = fmt.Errorf("%s %s failed: %w", method, path, err)
		case response == nil || !response.IsSuccess():
			lastErr = c.newAPIError(response, method, path, attempt)
		default:
			return response, nil
		}

		if ctx.Err() != nil || attempt > c.options.retryCount || !c.options.retryPolicy(response, err) {
			return response, lastErr
		}

		wait, fromHeader := c.retryWait(response, attempt)

		if fromHeader {
			if wait > c.options.maxRateLimitWait {
				c.options.requestLogger.Warnf("%s %s: not retrying, Retry-After %v exceeds the max wait %v", method, path, wait, c.options.maxRateLimitWait)
				return response, lastErr
			}

			wait += jitter(wait)
		}

		if deadline, ok := ctx.Deadline(); ok && time.Now().Add(wait).After(deadline) {
			c.options.requestLogger.Warnf("%s %s: not retrying, wait %v exceeds the context deadline", method, path, wait)
			return response, lastErr
		}

		c.options.requestLogger.Debugf("%s %s: attempt %d failed, retrying in %v", method, path, attempt, wait)

		if !sleep(ctx, wait) {
			return response, fmt.Errorf("%w (retry cancelled: %w)", lastErr, ctx.Err())
		}
	}
}

// retryAfterJitter is the largest extra wait added to a Retry-After wait, as a fraction of it. Concurrent requests
// rejected with the same Retry-After then spread their retries, instead of all retrying at the same moment.
const retryAfterJitter = 0.2

// jitter returns a random duration between zero and [retryAfterJitter] of wait.
func jitter(wait time.Duration) time.Duration {
	limit := time.Duration(float64(wait) * retryAfterJitter)
	if limit <= 0 {
		return 0
	}

	return rand.N(limit) // #nosec G404 -- retry jitter needs no cryptographic randomness
}

// retryWait returns the wait before the next attempt, and whether it comes from the Retry-After header.
func (c *Client) retryWait(response *resty.Response, attempt int) (time.Duration, bool) {
	if wait, ok := retryAfter(response, time.Now()); ok {
		return wait, true
	}

	return backoff(c.options.retryWaitTime, c.options.retryMaxWaitTime, attempt), false
}

// maxRetryAfter caps the Retry-After waits parsed by [retryAfter], so that huge values cannot overflow
// [time.Duration]. It matches the longest Retry-After the API sends (when the requested tokens can never be
// available), and is far above [maxMaxRateLimitWait], so a capped wait is never waited for.
const maxRetryAfter = 24 * time.Hour

// retryAfter parses the Retry-After header of the response, as either delay seconds or an HTTP date relative
// to now, capped at [maxRetryAfter]. A date in the past gives zero, i.e. retry immediately. The bool is false
// when the header is absent or invalid.
func retryAfter(response *resty.Response, now time.Time) (time.Duration, bool) {
	if response == nil || response.RawResponse == nil {
		return 0, false
	}

	value := strings.TrimSpace(response.Header().Get("Retry-After"))
	if value == "" {
		return 0, false
	}

	if seconds, err := strconv.Atoi(value); err == nil {
		if seconds < 0 {
			return 0, false
		}

		if seconds > int(maxRetryAfter/time.Second) {
			return maxRetryAfter, true
		}

		return time.Duration(seconds) * time.Second, true
	}

	if t, err := http.ParseTime(value); err == nil {
		return min(max(t.Sub(now), 0), maxRetryAfter), true
	}

	return 0, false
}

// backoff returns the capped exponential backoff with equal jitter for the given attempt (1-based): a random
// duration between half and all of min(maxWait, minWait * 2^(attempt-1)).
func backoff(minWait, maxWait time.Duration, attempt int) time.Duration {
	capped := math.Min(float64(maxWait), float64(minWait)*math.Exp2(float64(attempt-1)))
	half := time.Duration(capped / 2)

	if half <= 0 {
		return minWait
	}

	return half + rand.N(half) // #nosec G404 -- retry jitter needs no cryptographic randomness
}

// sleep waits for d, or until ctx is done. It returns false if ctx is done first.
func sleep(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}

	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}
